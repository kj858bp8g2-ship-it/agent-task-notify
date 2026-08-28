package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/hostfile"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

var nativeBuildOnce sync.Once
var nativeBuildError error
var nativeRoot, nativeBinary string
var nativePreserve bool
var nativeFixtureNumber int

func TestMain(m *testing.M) {
	code := m.Run()
	if nativeRoot != "" && !nativePreserve {
		// Only this process's freshly generated, canonical fixture tree.
		if !filepath.IsAbs(nativeRoot) || !strings.HasPrefix(filepath.Base(nativeRoot), "atn-native-process-") || filepath.Dir(nativeRoot) == nativeRoot {
			code = 1
		} else if err := os.RemoveAll(nativeRoot); err != nil {
			var errno syscall.Errno
			if errors.As(err, &errno) {
				fmt.Fprintf(os.Stderr, "native fixture cleanup initial failure: os-code=%d\n", errno)
			} else {
				fmt.Fprintln(os.Stderr, "native fixture cleanup initial failure: filesystem")
			}
			deadline := time.Now().Add(2 * time.Second)
			for err != nil && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
				err = os.RemoveAll(nativeRoot)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "native fixture cleanup failed; fixture retained")
				code = 1
			}
		}
	}
	os.Exit(code)
}

func nativeExecutable(t *testing.T) string {
	t.Helper()
	nativeBuildOnce.Do(func() {
		var err error
		nativeRoot, err = os.MkdirTemp("", "atn-native-process-")
		if err != nil {
			nativeBuildError = err
			return
		}
		nativeRoot, err = filepath.EvalSymlinks(nativeRoot)
		if err != nil {
			nativeBuildError = err
			return
		}
		owned := filepath.Join(nativeRoot, "owned")
		pkg := filepath.Join(owned, "中文 软件包")
		for _, p := range []string{owned, pkg} {
			if err = store.EnsurePrivateDirectory(p); err != nil {
				nativeBuildError = err
				return
			}
		}
		name := "通知 工具"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		raw := filepath.Join(nativeRoot, "raw-build"+filepath.Ext(name))
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", raw, "./cmd/agent-task-notify")
		cmd.Dir = filepath.Dir(mustGetwd(t))
		cmd.WaitDelay = time.Second
		if out, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() != nil {
				nativePreserve = true
			}
			nativeBuildError = fmt.Errorf("build: %w: %s", err, out)
			return
		}
		data, err := os.ReadFile(raw)
		if err != nil {
			nativeBuildError = err
			return
		}
		nativeBinary = filepath.Join(pkg, name)
		// The build output may use the runner's group TokenOwner. Initialize a
		// distinct, absent final file explicitly; never repair the raw object's owner.
		if err = store.WriteAtomic(nativeBinary, data); err != nil {
			nativeBuildError = err
			return
		}
		if runtime.GOOS != "windows" {
			if err = os.Chmod(nativeBinary, 0700); err != nil {
				nativeBuildError = err
				return
			}
		}
		if _, err = store.ReadPrivate(nativeBinary, 64<<20); err != nil {
			nativeBuildError = err
			return
		}
		if _, err = hostfile.Read(nativeBinary, 64<<20); err != nil {
			nativeBuildError = err
			return
		}
	})
	if nativeBuildError != nil {
		t.Fatal(nativeBuildError)
	}
	return nativeBinary
}

type nativeFixture struct {
	t                    *testing.T
	exe, root, data, cwd string
	env                  []string
	cleanupHTTP          func()
	closeHTTP            func()
}

func newNativeFixture(t *testing.T) *nativeFixture {
	t.Helper()
	exe := nativeExecutable(t)
	nativeFixtureNumber++
	root := filepath.Join(nativeRoot, "owned", fmt.Sprintf("fixture-%d", nativeFixtureNumber))
	for _, p := range []string{root, filepath.Join(root, "home"), filepath.Join(root, "user-profile"), filepath.Join(root, "app-data"), filepath.Join(root, "local-app-data"), filepath.Join(root, "working-directory"), filepath.Join(root, "empty-path")} {
		if err := store.EnsurePrivateDirectory(p); err != nil {
			t.Fatal(err)
		}
	}
	f := &nativeFixture{t: t, exe: exe, root: root, data: filepath.Join(root, "atn-data"), cwd: filepath.Join(root, "working-directory")}
	f.env = nativeCommandEnvironment(filepath.Join(root, "empty-path"), root)
	f.env = append(withoutEnvironmentNames(f.env, "XDG_CONFIG_HOME"), "XDG_CONFIG_HOME="+filepath.Join(root, "home", ".config"))
	t.Cleanup(func() {
		// command() always kills/waits its directly owned process before it
		// returns. No direct process can spawn another child after this point.
		if f.cleanupHTTP != nil {
			f.cleanupHTTP()
		}
		if !f.waitComplete(80 * time.Second) {
			nativePreserve = true
			t.Error("native worker logical completion unconfirmed; synthetic fixture retained")
			return // Keep loopback service and files available until process exit.
		}
		if f.closeHTTP != nil {
			f.closeHTTP()
		}
	})
	return f
}

func (f *nativeFixture) command(input string, args ...string) (int, string, string, time.Duration) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.exe, args...)
	cmd.Dir = f.cwd
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = time.Second
	var out, errors bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errors
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		f.t.Fatal("native command exceeded deadline (direct process stopped and waited)")
	}
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			f.t.Fatal("native command failed to launch")
		}
	}
	return code, out.String(), errors.String(), elapsed
}

func (f *nativeFixture) jobs() []map[string]any {
	entries, err := os.ReadDir(filepath.Join(f.data, "jobs"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []map[string]any{{}}
	}
	var jobs []map[string]any
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := store.ReadPrivate(filepath.Join(f.data, "jobs", entry.Name()), 4<<20)
		var j map[string]any
		if err != nil || json.Unmarshal(b, &j) != nil {
			return []map[string]any{{}}
		}
		j["key"] = strings.TrimSuffix(entry.Name(), ".json")
		jobs = append(jobs, j)
	}
	return jobs
}

func (f *nativeFixture) waitComplete(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		complete := true
		for _, j := range f.jobs() {
			if (j["status"] != "sent" && j["status"] != "failed") || (j["extensionStatus"] != "none" && j["extensionStatus"] != "sent" && j["extensionStatus"] != "failed" && j["extensionStatus"] != "ambiguous") {
				complete = false
				break
			}
			key, ok := j["key"].(string)
			if !ok || !core.ValidKey(key) {
				complete = false
				break
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			release, err := store.Acquire(ctx, filepath.Join(f.data, "locks", "job-"+key+".lock"))
			cancel()
			if err != nil {
				complete = false
				break
			}
			_ = release()
		}
		if complete {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Read-only preconditions in BOTH contexts; do not infer a changed HOME shares
// the runner's default Keychain. The shell fixture alone owns configuration.
func nativeKeychainGuard(t *testing.T, env []string) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return ""
	}
	if os.Getenv("CI") != "true" {
		t.Skip("disposable CI Keychain required")
	}
	root, err := filepath.EvalSymlinks(os.Getenv("RUNNER_TEMP"))
	fixture, ferr := filepath.EvalSymlinks(os.Getenv("ATN_TEST_KEYCHAIN"))
	if err != nil || ferr != nil || !filepath.IsAbs(root) || !filepath.IsAbs(fixture) {
		t.Fatal("disposable Keychain fixture missing")
	}
	rel, err := filepath.Rel(root, fixture)
	dir := filepath.Dir(rel)
	info, statErr := os.Lstat(fixture)
	if err != nil || filepath.Base(dir) != dir || !strings.HasPrefix(dir, "atn-keychain.") || len(dir) <= len("atn-keychain.") || filepath.Base(fixture) != "synthetic.keychain-db" || statErr != nil || !info.Mode().IsRegular() {
		t.Fatal("unsafe disposable Keychain fixture")
	}
	for _, environment := range [][]string{os.Environ(), env} {
		for _, action := range []string{"default-keychain", "list-keychains"} {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := exec.CommandContext(ctx, "/usr/bin/security", action, "-d", "user")
			cmd.Env = environment
			cmd.WaitDelay = time.Second
			data, err := cmd.Output()
			cancel()
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if err != nil || len(lines) != 1 {
				t.Fatal("isolated HOME Keychain precondition failed before foreground access")
			}
			actual, err := filepath.EvalSymlinks(strings.Trim(strings.TrimSpace(lines[0]), "\""))
			if err != nil || actual != fixture {
				t.Fatal("isolated HOME Keychain precondition failed before foreground access")
			}
		}
	}
	return fixture
}

func (f *nativeFixture) configure(provider, endpoint, patch string) {
	f.t.Helper()
	nativeKeychainGuard(f.t, f.env)
	settings := filepath.Join(f.root, "设置 文件.json")
	if err := store.WriteAtomic(settings, []byte(patch)); err != nil {
		f.t.Fatal(err)
	}
	c := map[string]any{"endpoint": endpoint}
	if provider == "ntfy" {
		c["token"] = "synthetic-local-token"
	}
	input, _ := json.Marshal(c)
	code, out, errors, _ := f.command(string(input), "configure", "--provider", provider, "--credential-stdin", "--settings-file", settings, "--data-directory", f.data)
	if code != 0 || !strings.Contains(out, "configured") || strings.Contains(out+errors, endpoint) || errors != "" {
		f.t.Fatalf("native configure failed safely: code=%d", code)
	}
}

func (f *nativeFixture) hook(event string) {
	f.t.Helper()
	input := `{"hook_event_name":"` + event + `","session_id":"合成会话","turn_id":"合成运行","prompt":"planted-private-task"}`
	code, out, errors, elapsed := f.command(input, "hook", "--agent", "codex")
	if code != 0 || out != "{\"continue\":true}\n" || errors != "" || elapsed > 2*time.Second {
		f.t.Fatalf("hook/pipe EOF contract: code=%d elapsed=%v", code, elapsed)
	}
}

func TestNativeRuntimeReadOnlyAndMalformed(t *testing.T) {
	f := newNativeFixture(t)
	before := runtimeEntries(t, f.root)
	for _, args := range [][]string{{"doctor"}, {"preview", "--agent", "codex"}, {"uninstall", "--agent", "cursor"}, {"help"}} {
		code, _, errors, _ := f.command("", args...)
		if code != 0 || errors != "" {
			t.Fatalf("read-only %s failed", args[0])
		}
	}
	if after := runtimeEntries(t, f.root); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatal("read-only process wrote state")
	}
	for _, input := range []string{"{", string([]byte{255}), strings.Repeat("x", (4<<20)+1)} {
		code, out, errors, elapsed := f.command(input, "hook", "--agent", "workbuddy")
		if code != 0 || out != "{\"continue\":true}\n" || errors != "" || elapsed > 2*time.Second {
			t.Fatal("malformed process input not neutral")
		}
	}
	code, out, _, _ := f.command("", "doctor")
	var d map[string]any
	_ = json.Unmarshal([]byte(out), &d)
	if code != 0 || d["inputErrors"] != float64(3) || d["configured"] != false {
		t.Fatal("process input diagnostics incorrect")
	}
	code, out, errors, _ := f.command("", "worker", "--data-directory", f.data, "--job", strings.Repeat("a", 64))
	if code != 1 || out != "" || errors != "" {
		t.Fatal("missing job not silent")
	}
}

func TestNativeRuntimeRetryExtensionAndDuplicateStop(t *testing.T) {
	for _, provider := range []string{"bark", "ntfy"} {
		t.Run(provider, func(t *testing.T) {
			f := newNativeFixture(t)
			nativeKeychainGuard(t, f.env)
			var cleanup atomic.Bool
			var mu sync.Mutex
			var times []time.Time
			var bodies [][]byte
			gate := make(chan struct{})
			var gateOnce sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if cleanup.Load() {
					w.WriteHeader(400)
					return
				}
				body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				mu.Lock()
				times = append(times, time.Now())
				bodies = append(bodies, body)
				n := len(times)
				mu.Unlock()
				if n == 1 {
					select {
					case <-gate:
					case <-r.Context().Done():
						return
					}
					if cleanup.Load() {
						w.WriteHeader(400)
						return
					}
					w.WriteHeader(503)
					return
				}
				if provider == "bark" {
					io.WriteString(w, `{"code":200}`)
				} else {
					io.WriteString(w, `{"id":"synthetic-id","event":"message","topic":"synthetic-topic"}`)
				}
			}))
			f.cleanupHTTP = func() { cleanup.Store(true); gateOnce.Do(func() { close(gate) }) }
			f.closeHTTP = server.Close
			endpoint := server.URL + "/synthetic-topic"
			f.configure(provider, endpoint, `{"minSeconds":1,"longTaskSeconds":3600,"mediumRingSeconds":31,"continuous":true,"icons":{"codex":"https://synthetic.invalid/frozen-icon"}}`)
			f.hook("UserPromptSubmit")
			time.Sleep(1100 * time.Millisecond)
			f.hook("Stop") // Must reach stdout/stderr EOF even while child is gated.
			// Change display settings after job creation. Retries and extension
			// must retain the queued 31-second target and original icon.
			f.configure(provider, endpoint, `{"mediumRingSeconds":60,"icons":{"codex":"https://synthetic.invalid/changed-icon"}}`)
			gateOnce.Do(func() { close(gate) })
			f.hook("Stop")
			if !f.waitComplete(18 * time.Second) {
				t.Fatal("real retry/extension did not finish")
			}
			mu.Lock()
			gotTimes := append([]time.Time(nil), times...)
			gotBodies := append([][]byte(nil), bodies...)
			mu.Unlock()
			want := 3
			if provider == "ntfy" {
				want = 2
			}
			if len(gotTimes) != want {
				t.Fatalf("request count=%d want=%d", len(gotTimes), want)
			}
			if delay := gotTimes[1].Sub(gotTimes[0]); delay < 5*time.Second || delay > 9*time.Second {
				t.Fatalf("first retry delay=%v", delay)
			}
			if provider == "bark" {
				if delay := gotTimes[2].Sub(gotTimes[1]); delay < 900*time.Millisecond || delay > 4*time.Second {
					t.Fatalf("extension delay=%v", delay)
				}
			}
			for _, body := range gotBodies {
				if bytes.Contains(body, []byte("planted-private-task")) {
					t.Fatal("task text sent")
				}
				if provider == "bark" && !bytes.Contains(body, []byte("frozen-icon")) {
					t.Fatal("frozen icon missing")
				}
			}
			jobs := f.jobs()
			if len(jobs) != 1 || jobs[0]["attempts"] != float64(2) || jobs[0]["status"] != "sent" {
				t.Fatal("duplicate stop or retry state incorrect")
			}
			if provider == "ntfy" && jobs[0]["extensionStatus"] != "none" {
				t.Fatal("ntfy continuation")
			}
			if provider == "bark" && jobs[0]["extensionStatus"] != "sent" {
				t.Fatal("Bark extension missing")
			}
			// This is a terminal-state + released-lock assertion, not OS-exit proof.
			f.hook("Stop")
			time.Sleep(1200 * time.Millisecond)
			mu.Lock()
			count := len(times)
			mu.Unlock()
			if count != want || len(f.jobs()) != 1 {
				t.Fatal("duplicate created another delivery")
			}
			entries, _ := os.ReadDir(filepath.Join(f.data, "runs"))
			for _, entry := range entries {
				b, _ := os.ReadFile(filepath.Join(f.data, "runs", entry.Name()))
				if bytes.Contains(b, []byte("合成")) || bytes.Contains(b, []byte("planted-private-task")) {
					t.Fatal("raw event identifiers persisted")
				}
			}
		})
	}
}

func TestNativePreviewSendAndAbsentCredential(t *testing.T) {
	f := newNativeFixture(t)
	var requests atomic.Int32
	var cleanup atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cleanup.Load() {
			w.WriteHeader(400)
			return
		}
		requests.Add(1)
		io.WriteString(w, `{"code":200}`)
	}))
	f.closeHTTP = server.Close
	f.cleanupHTTP = func() { cleanup.Store(true) }
	f.configure("bark", server.URL+"/synthetic-key", `{"continuous":false}`)
	code, out, errors, _ := f.command("", "preview", "--agent", "codex", "--send")
	if code != 0 || errors != "" || !strings.Contains(out, "queued") || !strings.Contains(out, "not confirmed") {
		t.Fatal("preview claimed delivery or did not queue")
	}
	if !f.waitComplete(5*time.Second) || requests.Load() != 1 {
		t.Fatal("preview did not use real worker")
	}
	missing := newNativeFixture(t)
	code, out, errors, _ = missing.command("", "preview", "--agent", "codex", "--send")
	if code != 0 || errors != "" || !strings.Contains(out, "queued") {
		t.Fatal("absent credential queue contract")
	}
	if !missing.waitComplete(5 * time.Second) {
		t.Fatal("missing credential worker blocked")
	}
	j := missing.jobs()
	if len(j) != 1 || j[0]["diagnostic"] != "credential" || j[0]["attempts"] != float64(0) {
		t.Fatal("missing credential unsafe behavior")
	}
}

func TestNativeInstallPlansAndApply(t *testing.T) {
	f := newNativeFixture(t)
	nativeKeychainGuard(t, f.env)
	target := filepath.Join(f.root, "合成 hooks.json")
	before, err := hostfile.Read(target, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"version":1,"external":"keep","hooks":{"stop":[{"command":"echo synthetic-external"}]}}`)
	if err := hostfile.Replace(target, before, original); err != nil {
		t.Fatal(err)
	}
	args := []string{"install", "--agent", "cursor", "--config-path", target, "--command-shell", "powershell"}
	code, out, errors, _ := f.command("", args...)
	if code != 0 || errors != "" {
		t.Fatalf("install plan: %d", code)
	}
	var p map[string]any
	_ = json.Unmarshal([]byte(out), &p)
	if p["target"] != target || p["action"] != "install" || p["agent"] != "cursor" {
		t.Fatal("plan omitted target")
	}
	if b, _ := os.ReadFile(target); !bytes.Equal(b, original) {
		t.Fatal("plan changed host")
	}
	if _, err := os.Lstat(f.data); !os.IsNotExist(err) {
		t.Fatal("plan created data")
	}
	code, _, errors, _ = f.command("", append(args, "--apply")...)
	if code != 0 || errors != "" {
		t.Fatalf("install apply: %d", code)
	}
	installed, _ := os.ReadFile(target)
	if !bytes.Contains(installed, []byte("agent-task-notify")) && !bytes.Contains(installed, []byte("通知 工具")) {
		t.Fatal("self executable missing")
	}
	code, _, errors, _ = f.command("", "uninstall", "--agent", "cursor")
	if code != 0 || errors != "" {
		t.Fatal("uninstall plan failed")
	}
	if b, _ := os.ReadFile(target); !bytes.Equal(b, installed) {
		t.Fatal("uninstall plan changed host")
	}
	code, _, errors, _ = f.command("", "uninstall", "--agent", "cursor", "--apply")
	if code != 0 || errors != "" {
		t.Fatal("uninstall apply failed")
	}
	b, _ := os.ReadFile(target)
	if !bytes.Contains(b, []byte("synthetic-external")) || bytes.Contains(b, []byte("--data-directory")) {
		t.Fatal("uninstall ownership incorrect")
	}
	missing := filepath.Join(f.root, "missing", "hooks.json")
	code, out, errors, _ = f.command("", "install", "--agent", "cursor", "--config-path", missing, "--apply")
	if code != 1 || !strings.Contains(out, "target") || !strings.Contains(errors, "parent") {
		t.Fatal("missing parent guidance")
	}
	if _, err := os.Lstat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("created missing host parent")
	}
}

func TestNativeLockedKeychainWorkerIsSilentAndBounded(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Keychain gate")
	}
	f := newNativeFixture(t)
	fixture := nativeKeychainGuard(t, f.env)
	var cleanup atomic.Bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cleanup.Load() {
			w.WriteHeader(400)
			return
		}
		requests.Add(1)
		io.WriteString(w, `{"code":200}`)
	}))
	f.cleanupHTTP = func() { cleanup.Store(true) }
	f.closeHTTP = server.Close
	f.configure("bark", server.URL+"/synthetic-key", `{"continuous":false}`)
	security := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
		cmd.WaitDelay = time.Second
		return cmd.Run()
	}
	t.Cleanup(func() {
		if security("unlock-keychain", "-p", "atn-synthetic-ci-fixture-only", fixture) != nil {
			t.Error("synthetic Keychain unlock failed")
		}
	})
	if security("lock-keychain", fixture) != nil {
		t.Fatal("synthetic Keychain lock failed")
	}
	code, out, errors, _ := f.command("", "preview", "--agent", "codex", "--send")
	if code != 0 || !strings.Contains(out, "queued") || errors != "" {
		t.Fatal("locked preview blocked")
	}
	if !f.waitComplete(5 * time.Second) {
		t.Fatal("locked background access blocked")
	}
	j := f.jobs()
	if len(j) != 1 || j[0]["diagnostic"] != "credential" {
		t.Fatal("locked key unexpectedly accessible")
	}
	key := j[0]["key"].(string)
	code, out, errors, elapsed := f.command("", "worker", "--data-directory", f.data, "--job", key)
	if code != 1 || out != "" || errors != "" || elapsed > 5*time.Second {
		t.Fatal("worker was not silent/bounded")
	}
	if security("unlock-keychain", "-p", "atn-synthetic-ci-fixture-only", fixture) != nil {
		t.Fatal("synthetic Keychain restore failed")
	}
	if requests.Load() != 0 {
		t.Fatal("locked credential sent a request")
	}
	code, _, errors, _ = f.command("", "preview", "--agent", "codex", "--send")
	if code != 0 || errors != "" || !f.waitComplete(5*time.Second) || requests.Load() != 1 {
		t.Fatal("restored synthetic credential not accessible")
	}
}

func TestNativeConfigureRejectsUnsafeInputWithoutWrites(t *testing.T) {
	f := newNativeFixture(t)
	before := runtimeEntries(t, f.root)
	for _, args := range [][]string{{"configure", "--provider", "bark", "--endpoint", "planted-secret"}, {"configure", "--provider", "bark"}, {"configure", "--provider", "bark", "--credential-stdin"}, {"doctor", "--data-directory", f.data, "--data-directory", f.data}} {
		code, out, errors, _ := f.command(`{"endpoint":"planted-secret"}`, args...)
		if code != 2 || out != "" || strings.Contains(errors, "planted-secret") {
			t.Fatal("unsafe process configuration accepted or echoed")
		}
	}
	if after := runtimeEntries(t, f.root); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatal("invalid configure process created state")
	}
}
