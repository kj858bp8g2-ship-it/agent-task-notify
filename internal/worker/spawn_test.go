package worker

import (
	"context"
	"encoding/json"
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
