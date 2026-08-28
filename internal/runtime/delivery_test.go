package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	platform "runtime"
	"strings"
	"testing"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

func deliveryFixture(t *testing.T, patch string) (*Runtime, string) {
	t.Helper()
	if platform.GOOS == "darwin" {
		if os.Getenv("CI") != "true" {
			t.Skip("disposable CI Keychain required")
		}
		keychain, err := filepath.EvalSymlinks(os.Getenv("ATN_TEST_KEYCHAIN"))
		root, rootErr := filepath.EvalSymlinks(os.Getenv("RUNNER_TEMP"))
		if err != nil || rootErr != nil || !filepath.IsAbs(root) {
			t.Fatal("CI keychain missing")
		}
		rel, err := filepath.Rel(root, keychain)
		dir := filepath.Dir(rel)
		if err != nil || filepath.Base(keychain) != "synthetic.keychain-db" || filepath.Base(dir) != dir || !strings.HasPrefix(dir, "atn-keychain.") {
			t.Fatal("unsafe CI keychain")
		}
	}
	r := fixture(t)
	if err := r.Repository.Configure(context.Background(), "bark", providers.Credential{Endpoint: "http://127.0.0.1/synthetic"}, []byte(patch)); err != nil {
		t.Fatal(err)
	}
	got, err := r.Preview(context.Background(), "codex", true)
	if err != nil || !got.Queued {
		t.Fatal("preview missing")
	}
	return r, got.JobKey
}

// Real filesystem refusal at a test-owned file, with bounded, symmetric cleanup.
func blockReplacement(t *testing.T, path string) func() {
	t.Helper()
	if platform.GOOS == "windows" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		}
	}
	flag := func(value string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "/usr/bin/chflags", value, path).Run(); err != nil {
			t.Fatal("test-owned immutable operation failed")
		}
	}
	flag("uchg")
	return func() { flag("nouchg") }
}

func TestDeliveryDurableBeforeNetworkAndExtension(t *testing.T) {
	r, key := deliveryFixture(t, `{}`)
	var calls int
	var waits []time.Duration
	r.Send = func(_ context.Context, s core.Settings, c providers.Credential, m providers.Message) providers.Result {
		calls++
		j := readJobTest(t, r, key)
		if s.Sound != "alarm" || c.Endpoint != "http://127.0.0.1/synthetic" || !m.Preview {
			t.Fatal("wrong frozen delivery")
		}
		if calls == 1 {
			if j.Status != "sending" || j.Attempts != 1 || j.Accepted {
				t.Fatal("intent not durable")
			}
			return providers.Result{Accepted: true}
		}
		if j.Status != "sent" || !j.Accepted || j.ExtensionStatus != "sending" || !j.ExtensionAttempted {
			t.Fatal("acceptance/extension intent not durable")
		}
		return providers.Result{Retryable: true, Diagnostic: "http-server"}
	}
	r.Sleep = func(_ context.Context, d time.Duration) error {
		j := readJobTest(t, r, key)
		if !j.Accepted || j.Status != "sent" || j.ExtensionStatus != "pending" {
			t.Fatal("sleep before acceptance persisted")
		}
		waits = append(waits, d)
		return nil
	}
	if err := r.RunJob(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	j := readJobTest(t, r, key)
	if calls != 2 || len(waits) != 1 || waits[0] <= 14*time.Second || waits[0] > 15*time.Second || j.Status != "sent" || !j.Accepted || j.Attempts != 1 || j.ExtensionStatus != "failed" || !j.ExtensionAttempted || j.ExtensionAccepted {
		t.Fatal("extension outcome")
	}
	_ = r.RunJob(context.Background(), key)
	if calls != 2 {
		t.Fatal("accepted main replayed")
	}
}

func TestDeliveryRetryScheduleAndPermanentFailures(t *testing.T) {
	for _, retryable := range []bool{true, false} {
		t.Run(map[bool]string{true: "retry", false: "permanent"}[retryable], func(t *testing.T) {
			r, key := deliveryFixture(t, `{}`)
			calls := 0
			var waits []time.Duration
			r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
				calls++
				j := readJobTest(t, r, key)
				if j.Status != "sending" || j.Attempts != calls {
					t.Fatal("attempt not durable")
				}
				return providers.Result{Retryable: retryable, Diagnostic: "http-server"}
			}
			r.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
			if err := r.RunJob(context.Background(), key); err == nil {
				t.Fatal("failed delivery reported success")
			}
			want := 1
			if retryable {
				want = 5
				if !reflect.DeepEqual(waits, []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second}) {
					t.Fatal("retry schedule")
				}
			}
			j := readJobTest(t, r, key)
			if calls != want || j.Attempts != want || j.Status != "failed" || j.Accepted {
				t.Fatal("retry cap")
			}
			_ = r.RunJob(context.Background(), key)
			if calls != want {
				t.Fatal("failed job replayed")
			}
		})
	}
}

func TestNoReplayAmbiguityAndBoundedJobLock(t *testing.T) {
	r, key := deliveryFixture(t, `{}`)
	r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
		t.Fatal("unexpected send")
		return providers.Result{}
	}
	base := readJobTest(t, r, key)
	for _, status := range []string{"pending", "sending"} {
		j := base
		j.Status = status
		j.Attempts = 1
		put(t, recordPath(r, "jobs", key), j)
		if err := r.RunJob(context.Background(), key); err == nil {
			t.Fatal("ambiguity not reported")
		}
		j = readJobTest(t, r, key)
		if j.Diagnostic != "ambiguous" {
			t.Fatal("ambiguity not persisted")
		}
	}
	put(t, recordPath(r, "jobs", key), base)
	held, err := store.Acquire(context.Background(), r.lockPath("job", key))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(recordPath(r, "jobs", key))
	start := time.Now()
	err = r.RunJob(context.Background(), key)
	held()
	if err == nil || err.Error() != "runtime unavailable" || time.Since(start) < 1900*time.Millisecond || time.Since(start) > 5*time.Second {
		t.Fatal("lock not bounded to two seconds")
	}
	after, _ := os.ReadFile(recordPath(r, "jobs", key))
	if string(before) != string(after) {
		t.Fatal("busy lock mutated job")
	}
	for _, bad := range []string{"../secret", strings.Repeat("A", 64), ""} {
		if err := r.RunJob(context.Background(), bad); err == nil {
			t.Fatal("invalid key")
		}
	}
}

func TestPersistenceFailureStopsNetworkAndExtension(t *testing.T) {
	for _, stage := range []string{"intent", "accepted", "failure", "extension"} {
		t.Run(stage, func(t *testing.T) {
			r, key := deliveryFixture(t, `{}`)
			path := recordPath(r, "jobs", key)
			var unblock func()
			calls := 0
			defer func() {
				if unblock != nil {
					unblock()
				}
			}()
			if stage == "intent" {
				unblock = blockReplacement(t, path)
			}
			r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
				calls++
				if calls == 1 && (stage == "accepted" || stage == "failure") {
					unblock = blockReplacement(t, path)
				}
				if stage == "failure" {
					return providers.Result{Retryable: true, Diagnostic: "transport"}
				}
				return providers.Result{Accepted: true}
			}
			r.Sleep = func(context.Context, time.Duration) error {
				if stage != "extension" {
					t.Fatal("continued after failed persist")
				}
				unblock = blockReplacement(t, path)
				return nil
			}
			if err := r.RunJob(context.Background(), key); err == nil || err.Error() != "state-write" {
				t.Fatal("write failure hidden")
			}
			want := 1
			if stage == "intent" {
				want = 0
			}
			if calls != want {
				t.Fatal("network despite failed persistence")
			}
		})
	}
}

func TestExtensionDeadlineSubtractsPersistenceCost(t *testing.T) {
	r, key := deliveryFixture(t, `{}`)
	calls := 0
	delayNext := false
	r.Now = func() time.Time {
		if delayNext {
			delayNext = false
			time.Sleep(150 * time.Millisecond)
		}
		return epoch
	}
	r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
		calls++
		if calls == 1 {
			delayNext = true
		}
		return providers.Result{Accepted: true}
	}
	r.Sleep = func(_ context.Context, d time.Duration) error {
		if d > 14850*time.Millisecond || d < 14*time.Second {
			t.Fatalf("persistence not subtracted: %v", d)
		}
		return nil
	}
	if err := r.RunJob(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("extension missing")
	}
}

func TestNoExtensionForNtfySingleOrThirty(t *testing.T) {
	for _, kind := range []string{"ntfy", "single", "thirty"} {
		t.Run(kind, func(t *testing.T) {
			r, key := deliveryFixture(t, `{}`)
			j := readJobTest(t, r, key)
			switch kind {
			case "ntfy":
				if err := r.Repository.Configure(context.Background(), "ntfy", providers.Credential{Endpoint: "http://127.0.0.1/synthetic", AllowUnauthenticated: true}, []byte(`{}`)); err != nil {
					t.Fatal(err)
				}
				j.Settings.Provider = "ntfy"
				j.RingSeconds = 60
			case "single":
				j.Settings.Continuous = false
			case "thirty":
				j.RingSeconds = 30
			}
			put(t, recordPath(r, "jobs", key), j)
			calls := 0
			r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
				calls++
				return providers.Result{Accepted: true}
			}
			r.Sleep = func(context.Context, time.Duration) error { t.Fatal("unnecessary extension"); return nil }
			if err := r.RunJob(context.Background(), key); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("extra send")
			}
		})
	}
}

func TestLoopbackDeliveryUsesFrozenPayloadAndProvider(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error("body read")
		}
		var parsed map[string]any
		if json.Unmarshal(body, &parsed) != nil {
			t.Error("body json")
		}
		bodies = append(bodies, parsed)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200}`)
	}))
	defer server.Close()
	r, key := deliveryFixture(t, `{"icons":{"codex":""}}`)
	if err := r.Repository.Configure(context.Background(), "bark", providers.Credential{Endpoint: server.URL + "/synthetic"}, []byte(`{"sound":"changed","icons":{"codex":"https://example.invalid/new"}}`)); err != nil {
		t.Fatal(err)
	}
	r.Sleep = func(context.Context, time.Duration) error { return nil }
	if err := r.RunJob(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || !reflect.DeepEqual(bodies[0], bodies[1]) || bodies[0]["sound"] != "alarm" {
		t.Fatal("frozen payload changed")
	}
	if _, present := bodies[0]["icon"]; present {
		t.Fatal("empty frozen icon fell back")
	}
}

func TestConcurrentJobClaimAndCredentialFailure(t *testing.T) {
	r, key := deliveryFixture(t, `{"continuous":false}`)
	release, err := store.Acquire(context.Background(), r.lockPath("job", key))
	if err != nil {
		t.Fatal(err)
	}
	sends := make(chan struct{}, 2)
	done := make(chan error, 2)
	r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
		sends <- struct{}{}
		return providers.Result{Accepted: true}
	}
	for range 2 {
		go func() { done <- r.RunJob(context.Background(), key) }()
	}
	select {
	case <-sends:
		t.Fatal("send during held claim")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("claim did not finish")
		}
	}
	if len(sends) != 1 || readJobTest(t, r, key).Attempts != 1 {
		t.Fatal("duplicate concurrent send")
	}
	r = fixture(t)
	got, err := r.Preview(context.Background(), "codex", true)
	if err != nil {
		t.Fatal(err)
	}
	r.Send = func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result {
		t.Fatal("network without credentials")
		return providers.Result{}
	}
	if err := r.RunJob(context.Background(), got.JobKey); err == nil || err.Error() != "credential" {
		t.Fatal("credential failure hidden")
	}
	j := readJobTest(t, r, got.JobKey)
	if j.Status != "failed" || j.Attempts != 0 || j.Diagnostic != "credential" {
		t.Fatal("credential failure not durable")
	}
}
