package worker

import (
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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestMain(m *testing.M) {
	if marker := os.Getenv("NOTIFY_SPAWN_MARKER"); marker != "" {
		// An invalid-argument test child makes any accidental launch observable.
		_ = os.WriteFile(marker, []byte("started"), 0600)
		os.Exit(9)
	}
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		os.Exit(workerHelper())
	}
	if len(os.Args) == 4 && os.Args[1] == "probe-hook" {
		os.Exit(probeHookHelper(os.Args[2], os.Args[3]))
	}
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		fixtureStage(os.Args[2], "hook-entry")
		fixtureStage(os.Args[2], "spawn-before")
		if err := SpawnWorker(os.Args[0], os.Args[2], fixtureKey); err != nil {
			os.Exit(2)
		}
		fixtureStage(os.Args[2], "spawn-after")
		fixtureStage(os.Args[2], "hook-exit")
		fmt.Println(`{"ok":true}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixtureStage(dir, stage string) {
	_ = os.WriteFile(filepath.Join(dir, stage), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
}

func workerHelper() int {
	if len(os.Args) != 6 || os.Args[2] != "--data-directory" || os.Args[4] != "--job" || os.Args[5] != fixtureKey {
		return 3
	}
	dir := os.Args[3]
	fixtureStage(dir, "worker-entry")
	defer os.WriteFile(filepath.Join(dir, "worker-stopped"), nil, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if _, err := os.Stat(filepath.Join(dir, "probe-worker")); err == nil {
		if waitFixtureGate(ctx, dir, "release-worker") {
			return 0
		}
		return 10
	}
	// Parent writes this marker only after hook exit and pipe EOF are observed.
	for {
		if _, err := os.Stat(filepath.Join(dir, "abort")); err == nil {
			return 8
		}
		if _, err := os.Stat(filepath.Join(dir, "hook-exited")); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return 4
		case <-time.After(10 * time.Millisecond):
		}
	}
	endpoint, err := os.ReadFile(filepath.Join(dir, "endpoint"))
	if err != nil {
		return 5
	}
	client := &http.Client{Timeout: time.Second}
	report := Deliver(ctx, 31, true, func(ctx context.Context, extension bool) Result {
		body := "main"
		if extension {
			body = "extension"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, string(endpoint), strings.NewReader(body))
		if err != nil {
			return Result{}
		}
		resp, err := client.Do(req)
		if err != nil {
			return Result{Retryable: true}
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return Result{Accepted: resp.StatusCode == 200, Retryable: resp.StatusCode == 503}
	}, nil)
	if report.MainAttempts != 2 || !report.ExtensionAccepted {
		return 6
	}
	if err := os.WriteFile(filepath.Join(dir, "worker-done"), []byte("ok"), 0600); err != nil {
		return 7
	}
	return 0
}

// These controls deliberately introduce distinct failure mechanisms only in the
// compiled test helper; production dispatch and SpawnWorker remain unchanged.
func probeHookHelper(dir, mode string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	fixtureStage(dir, "hook-entry")
	if mode == "hook-delay" && !waitFixtureGate(ctx, dir, "release-hook") {
		return 11
	}
	fixtureStage(dir, "spawn-before")
	if mode == "inherited-pipes" {
		// Negative control: intentionally retain the hook's two pipe writers.
		child := exec.Command(os.Args[0], "worker", "--data-directory", dir, "--job", fixtureKey)
		child.Stdin = os.Stdin
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.SysProcAttr = detachedAttributes()
		if child.Start() != nil {
			return 12
		}
		if child.Process.Release() != nil {
			return 13
		}
	} else if SpawnWorker(os.Args[0], dir, fixtureKey) != nil {
		return 14
	}
	fixtureStage(dir, "spawn-after")
	fixtureStage(dir, "hook-exit")
	fmt.Println(`{"ok":true}`)
	return 0
}

func waitFixtureGate(ctx context.Context, dir, gate string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, "abort")); err == nil {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, gate)); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type probePipeRead struct {
	stream    string
	completed time.Time
	err       error
}
type pipeProbe struct {
	dir           string
	start         time.Time
	exited        chan struct{}
	exitErr       error
	exitedAt      time.Time
	readComplete  chan probePipeRead
	delivered     chan probePipeRead
	allowDelivery chan struct{}
	deliveryOnce  sync.Once
}

func startPipeProbe(t *testing.T, mode string) *pipeProbe {
	t.Helper()
	p := &pipeProbe{dir: t.TempDir(), exited: make(chan struct{}), readComplete: make(chan probePipeRead, 2), delivered: make(chan probePipeRead, 2), allowDelivery: make(chan struct{})}
	if err := os.WriteFile(filepath.Join(p.dir, "probe-worker"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		t.Fatal(err)
	}
	for _, file := range []*os.File{stdoutR, stdoutW, stderrR, stderrW} {
		t.Cleanup(func() { _ = file.Close() })
	}
	hook := exec.Command(os.Args[0], "probe-hook", p.dir, mode)
	hook.Env = []string{"PATH=", "SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("WINDIR")}
	// Explicit files: Cmd.Wait must not close read ends and manufacture EOF.
	hook.Stdout = stdoutW
	hook.Stderr = stderrW
	p.start = time.Now()
	if err := hook.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.exitErr = hook.Wait(); p.exitedAt = time.Now(); close(p.exited) }()
	t.Cleanup(func() {
		p.releaseDelivery()
		_ = os.WriteFile(filepath.Join(p.dir, "release-hook"), nil, 0600)
		_ = os.WriteFile(filepath.Join(p.dir, "release-worker"), nil, 0600)
		_ = os.WriteFile(filepath.Join(p.dir, "abort"), nil, 0600)
		select {
		case <-p.exited:
		default:
			_ = hook.Process.Kill()
		}
		select {
		case <-p.exited:
		case <-time.After(3 * time.Second):
			t.Error("probe hook cleanup did not finish")
			return
		}
		// No future spawn after hook exit. A started worker has its own 12s
		// context and sees abort/release; wait before removing its fixtures.
		if _, err := os.Stat(filepath.Join(p.dir, "spawn-after")); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if !probeMarker(ctx, p.dir, "worker-stopped") {
				t.Error("probe worker cleanup did not finish")
			}
		}
	})
	for _, writer := range []*os.File{stdoutW, stderrW} {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		// Write checks Go's closed-file state; Windows Stat instead exposes a
		// native invalid-handle error, which is not errors.Is(os.ErrClosed).
		if _, err := writer.Write([]byte{0}); !errors.Is(err, os.ErrClosed) {
			t.Fatal("parent still owns a pipe writer")
		}
	}
	if mode != "observer-delay" {
		p.releaseDelivery()
	}
	for _, pipe := range []struct {
		name string
		file *os.File
	}{{"stdout", stdoutR}, {"stderr", stderrR}} {
		go func() {
			_, err := io.Copy(io.Discard, pipe.file)
			result := probePipeRead{pipe.name, time.Now(), err}
			p.readComplete <- result
			<-p.allowDelivery
			p.delivered <- result
		}()
	}
	return p
}

func (p *pipeProbe) releaseDelivery() { p.deliveryOnce.Do(func() { close(p.allowDelivery) }) }
func probeMarker(ctx context.Context, dir, name string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func collectProbeReads(t *testing.T, ctx context.Context, ch <-chan probePipeRead) []probePipeRead {
	t.Helper()
	var results []probePipeRead
	for len(results) < 2 {
		select {
		case result := <-ch:
			if result.err != nil {
				t.Fatal("probe read failed")
			}
			results = append(results, result)
		case <-ctx.Done():
			t.Fatal("probe read observation deadline")
		}
	}
	return results
}

func TestPipeEOFMechanismControls(t *testing.T) {
	for _, mode := range []string{"hook-delay", "observer-delay", "inherited-pipes"} {
		t.Run(mode, func(t *testing.T) {
			p := startPipeProbe(t, mode)
			gate, cancelGate := context.WithDeadline(context.Background(), p.start.Add(2*time.Second))
			defer cancelGate()
			bound, cancelBound := context.WithDeadline(context.Background(), p.start.Add(12*time.Second))
			defer cancelBound()
			if !probeMarker(gate, p.dir, "hook-entry") {
				t.Fatal("probe hook failed to reach controlled phase before gate")
			}
			if mode == "hook-delay" {
				if _, err := os.Stat(filepath.Join(p.dir, "spawn-before")); !os.IsNotExist(err) {
					t.Fatal("delayed hook passed spawn gate")
				}
				select {
				case <-p.exited:
					t.Fatal("delayed hook already exited")
				default:
				}
			} else {
				select {
				case <-p.exited:
					if p.exitErr != nil {
						t.Fatal("probe hook failed")
					}
				case <-gate.Done():
					t.Fatal("probe hook did not exit before gate")
				}
				if !probeMarker(gate, p.dir, "worker-entry") {
					t.Fatal("probe worker not ready")
				}
			}
			var completed []probePipeRead
			if mode == "observer-delay" {
				completed = collectProbeReads(t, gate, p.readComplete)
			}
			// Each negative control intentionally crosses the same start+2s gate.
			// It is not a relaxation of TestDetachedWorkerClosesHookPipes.
			select {
			case <-p.delivered:
				t.Fatal("negative control unexpectedly delivered EOF before release")
			case <-gate.Done():
			}
			select {
			case <-p.delivered:
				t.Fatal("negative control EOF already queued")
			default:
			}
			switch mode {
			case "hook-delay":
				t.Logf("gate exceeded: hook active before spawn; elapsed=%s", time.Since(p.start))
				if err := os.WriteFile(filepath.Join(p.dir, "release-hook"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "observer-delay":
				for _, result := range completed {
					if result.completed.Sub(p.start) >= 2*time.Second {
						t.Fatal("read itself exceeded gate")
					}
					t.Logf("gate exceeded: %s completed=%s; delivery still held at=%s", result.stream, result.completed.Sub(p.start), time.Since(p.start))
				}
				p.releaseDelivery()
			case "inherited-pipes":
				select {
				case <-p.readComplete:
					t.Fatal("inherited pipe reached EOF while worker retained writer")
				default:
				}
				t.Logf("gate exceeded: hook exited=%s; worker still holds writers at=%s", p.exitedAt.Sub(p.start), time.Since(p.start))
				if err := os.WriteFile(filepath.Join(p.dir, "release-worker"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			results := collectProbeReads(t, bound, p.delivered)
			for _, result := range results {
				t.Logf("after release: %s read-completed=%s received=%s", result.stream, result.completed.Sub(p.start), time.Since(p.start))
			}
			select {
			case <-p.exited:
				if p.exitErr != nil {
					t.Fatal("probe hook failed")
				}
			case <-bound.Done():
				t.Fatal("probe hook did not finish")
			}
			if err := os.WriteFile(filepath.Join(p.dir, "release-worker"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if !probeMarker(bound, p.dir, "worker-stopped") {
				t.Fatal("probe worker did not stop")
			}
		})
	}
}

func TestDetachedWorkerClosesHookPipes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文 空间")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "helper")
	if strings.HasSuffix(os.Args[0], ".exe") {
		executable += ".exe"
	}
	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(executable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil || closeErr != nil {
		t.Fatal("copy helper failed")
	}
	calls := make(chan string, 8)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 100))
		calls <- string(body)
		if count.Add(1) == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(dir, "endpoint"), []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	hook := exec.Command(executable, "hook", dir)
	hook.Env = []string{"PATH=", "SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("WINDIR")}
	stdout, err := hook.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := hook.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	t.Cleanup(func() {
		for _, stage := range []string{"hook-entry", "spawn-before", "spawn-after", "hook-exit", "worker-entry"} {
			data, err := os.ReadFile(filepath.Join(dir, stage))
			if err != nil {
				t.Logf("stage %s: not reached", stage)
				continue
			}
			nanos, err := strconv.ParseInt(string(data), 10, 64)
			if err == nil {
				t.Logf("stage %s: %s after parent start", stage, time.Unix(0, nanos).Sub(start))
			}
		}
	})
	if err := hook.Start(); err != nil {
		t.Fatal(err)
	}
	t.Logf("hook Start returned after %s", time.Since(start))
	t.Cleanup(func() {
		if hook.ProcessState == nil {
			_ = hook.Process.Kill()
			_ = hook.Wait()
		}
		_ = os.WriteFile(filepath.Join(dir, "abort"), nil, 0600)
		until := time.Now().Add(15 * time.Second)
		for time.Now().Before(until) {
			if _, err := os.Stat(filepath.Join(dir, "worker-stopped")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	type pipeResult struct {
		stream    string
		data      []byte
		completed time.Time
		err       error
	}
	eof := make(chan pipeResult, 2)
	go func() { b, err := io.ReadAll(stdout); eof <- pipeResult{"stdout", b, time.Now(), err} }()
	go func() { b, err := io.ReadAll(stderr); eof <- pipeResult{"stderr", b, time.Now(), err} }()
	var output []byte
	for i := 0; i < 2; i++ {
		select {
		case result := <-eof:
			t.Logf("pipe %s read completed after %s; parent received after %s", result.stream, result.completed.Sub(start), time.Since(start))
			if result.err != nil {
				t.Fatal("hook pipe read failed")
			}
			output = append(output, result.data...)
		case <-time.After(2 * time.Second):
			hook.Process.Kill()
			t.Fatal("hook pipes did not close within two seconds")
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("slow pipe EOF")
	}
	if err := hook.Wait(); err != nil {
		t.Fatal(err)
	}
	var neutral struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(output, &neutral) != nil || !neutral.OK {
		t.Fatal("hook output not neutral JSON")
	}
	if err := os.WriteFile(filepath.Join(dir, "hook-exited"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var got []string
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for len(got) < 3 {
		select {
		case value := <-calls:
			got = append(got, value)
		case <-deadline.C:
			t.Fatalf("delivery incomplete: %v", got)
		}
	}
	if !reflect.DeepEqual(got, []string{"main", "main", "extension"}) {
		t.Fatal(got)
	}
	if time.Since(start) < 6*time.Second {
		t.Fatal("real retry or extension delay omitted")
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "worker-done")); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("worker did not finish delivery")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case extra := <-calls:
		t.Fatalf("unexpected extra request: %s", extra)
	default:
	}
	// Wait briefly for the copied Windows executable to leave the process image.
	time.Sleep(100 * time.Millisecond)
}

func TestSpawnRejectsInvalidArguments(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	marker := filepath.Join(dir, "spawned")
	t.Setenv("NOTIFY_SPAWN_MARKER", marker)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeExecutable, err := filepath.Rel(cwd, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	relativeDirectory, err := filepath.Rel(cwd, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ exe, dir, key string }{{relativeExecutable, dir, fixtureKey}, {os.Args[0], relativeDirectory, fixtureKey}, {os.Args[0], dir, "short"}, {os.Args[0], dir, strings.ToUpper(fixtureKey)}, {os.Args[0], dir, strings.Repeat("g", 64)}} {
		if err := SpawnWorker(tc.exe, tc.dir, tc.key); err == nil {
			t.Fatal("invalid spawn accepted")
		}
	}
	if err := SpawnWorker(filepath.Join(dir, "missing"), dir, fixtureKey); err == nil {
		t.Fatal("missing executable accepted")
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("invalid arguments launched a process")
	}
}
