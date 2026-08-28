package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func fixture(t *testing.T) *Runtime {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "package")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	repo, err := configuration.Open(filepath.Join(root, "data"), pkg)
	if err != nil {
		t.Fatal(err)
	}
	r := New(repo, filepath.Join(pkg, "agent-task-notify"))
	r.Now = func() time.Time { return epoch }
	r.Spawn = func(exe, data, key string) error {
		if exe != r.Executable || data != repo.Directory() || !core.ValidKey(key) {
			t.Fatal("wrong spawn arguments")
		}
		return nil
	}
	return r
}

func put(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAtomic(path, data); err != nil {
		t.Fatal(err)
	}
}

func syntacticSettings(t *testing.T, r *Runtime, patch string) core.Settings {
	t.Helper()
	if err := r.Repository.Prepare(); err != nil {
		t.Fatal(err)
	}
	base, _ := core.Defaults()
	s, err := core.ParseSettings([]byte(patch), base)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	put(t, filepath.Join(r.Repository.Directory(), "installation.json"), map[string]any{"schemaVersion": 1, "installationId": id})
	put(t, filepath.Join(r.Repository.Directory(), "configuration.json"), map[string]any{"schemaVersion": 1, "settings": s, "credentials": map[string]any{s.Provider: map[string]any{"schemaVersion": 1, "backend": "dpapi", "installationId": id, "purpose": "credential:" + s.Provider, "ciphertext": "AQ=="}}})
	return s
}

func eventAt(t *testing.T, r *Runtime, e core.Event, at time.Time) EventResult {
	t.Helper()
	r.Now = func() time.Time { return at }
	got, err := r.Handle(context.Background(), e)
	if err != nil {
		t.Fatalf("event: %v (%s)", err, got.Diagnostic)
	}
	return got
}

func recordPath(r *Runtime, dir, key string) string {
	return filepath.Join(r.Repository.Directory(), dir, key+".json")
}
func readJobTest(t *testing.T, r *Runtime, key string) jobRecord {
	t.Helper()
	var job jobRecord
	data, err := store.ReadPrivate(recordPath(r, "jobs", key), 4<<20)
	if err != nil || json.Unmarshal(data, &job) != nil {
		t.Fatal("missing job")
	}
	return job
}

func TestNativeDuplicateStartDoesNotResetOrReopen(t *testing.T) {
	r := fixture(t)
	var jobs []string
	r.Spawn = func(_, _, key string) error { jobs = append(jobs, key); return nil }
	e := core.Event{AgentID: "codex", SessionID: "synthetic-session", NativeRunID: "synthetic-run", EventType: "started"}
	eventAt(t, r, e, epoch)
	eventAt(t, r, e, epoch.Add(20*time.Minute))
	e.EventType = "stopped"
	result := eventAt(t, r, e, epoch.Add(31*time.Minute))
	if !result.Queued || readJobTest(t, r, result.JobKey).Message.DurationSeconds != 1860 {
		t.Fatal("first timestamp reset")
	}
	e.EventType = "started"
	eventAt(t, r, e, epoch.Add(time.Hour))
	e.EventType = "stopped"
	eventAt(t, r, e, epoch.Add(2*time.Hour))
	if len(jobs) != 1 {
		t.Fatal("duplicate job")
	}
}

func TestThresholdsSourcesNonnativeAndFrozenSettings(t *testing.T) {
	r := fixture(t)
	syntacticSettings(t, r, `{"minSeconds":300,"longTaskSeconds":1200,"icons":{"codex":""}}`)
	keys := map[string]bool{}
	for _, agent := range core.Agents() {
		for _, c := range []struct {
			seconds int
			ring    int
		}{{299, 0}, {300, 45}, {1199, 45}, {1200, 60}} {
			e := core.Event{AgentID: agent.ID, SessionID: "same", NativeRunID: string(rune('a' + c.seconds)), EventType: "started", Reason: "PRIVATE_BODY"}
			eventAt(t, r, e, epoch)
			e.EventType = "failed"
			got := eventAt(t, r, e, epoch.Add(time.Duration(c.seconds)*time.Second))
			if got.Queued != (c.ring > 0) {
				t.Fatal("threshold")
			}
			if got.Queued {
				j := readJobTest(t, r, got.JobKey)
				if keys[got.JobKey] || j.RingSeconds != c.ring || j.Message.Reason != "failed" || j.Settings.MinSeconds != 300 {
					t.Fatal("collision or snapshot")
				}
				if _, ok := j.Settings.Icons[agent.ID]; !ok {
					t.Fatal("resolved icon not frozen")
				}
				keys[got.JobKey] = true
			}
		}
		for range 2 {
			e := core.Event{AgentID: agent.ID, SessionID: "local", EventType: "started"}
			eventAt(t, r, e, epoch)
			eventAt(t, r, e, epoch.Add(time.Second))
			e.EventType = "stopped"
			got := eventAt(t, r, e, epoch.Add(300*time.Second))
			if !got.Queued || keys[got.JobKey] {
				t.Fatal("local run reused")
			}
			keys[got.JobKey] = true
		}
	}
	for key := range keys {
		if readJobTest(t, r, key).Settings.MinSeconds != 300 {
			t.Fatal("snapshot changed")
		}
	}
	syntacticSettings(t, r, `{"minSeconds":999,"longTaskSeconds":2000,"sound":"changed"}`)
	for key := range keys {
		j := readJobTest(t, r, key)
		if j.Settings.MinSeconds != 300 || j.Settings.Sound != "alarm" {
			t.Fatal("reconfiguration changed jobs")
		}
	}
	for _, dir := range []string{"sessions", "runs", "jobs"} {
		entries, _ := os.ReadDir(filepath.Join(r.Repository.Directory(), dir))
		for _, entry := range entries {
			data, _ := os.ReadFile(filepath.Join(r.Repository.Directory(), dir, entry.Name()))
			if bytes.Contains(data, []byte("PRIVATE_BODY")) || bytes.Contains(data, []byte(`"same"`)) || bytes.Contains(data, []byte(`"local"`)) {
				t.Fatal("raw input persisted")
			}
		}
	}
}

func TestIgnoreDelayedStopClockAndInvalidSettings(t *testing.T) {
	r := fixture(t)
	for _, e := range []core.Event{{AgentID: "unknown", SessionID: "s", EventType: "started"}, {AgentID: "codex", SessionID: "s", EventType: "started", IsChild: true}, {AgentID: "codex", SessionID: " ", EventType: "started"}, {AgentID: "codex", SessionID: "s", EventType: "attention"}} {
		if got := eventAt(t, r, e, epoch); got.Queued {
			t.Fatal("ignored event queued")
		}
	}
	if _, err := os.Lstat(r.Repository.Directory()); !os.IsNotExist(err) {
		t.Fatal("ignored input created data")
	}
	e := core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "old", EventType: "stopped"}
	if eventAt(t, r, e, epoch).Queued {
		t.Fatal("orphan")
	}
	e.EventType = "started"
	eventAt(t, r, e, epoch)
	e.NativeRunID = "new"
	eventAt(t, r, e, epoch.Add(time.Minute))
	e.NativeRunID = "old"
	e.EventType = "stopped"
	if !eventAt(t, r, e, epoch.Add(time.Hour)).Queued {
		t.Fatal("delayed stop lost")
	}
	e.NativeRunID = "new"
	if eventAt(t, r, e, epoch.Add(-time.Hour)).Queued {
		t.Fatal("reverse clock notified")
	}
	run, _, err := r.readRun(core.Key("codex", "s", "new"))
	if err != nil || run.Status != "stopped" || run.FinishedAt != run.StartedAt {
		t.Fatal("reverse clock not clamped")
	}
	e.NativeRunID = "bad-settings"
	e.EventType = "started"
	eventAt(t, r, e, epoch)
	if err := store.WriteAtomic(filepath.Join(r.Repository.Directory(), "configuration.json"), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	e.EventType = "stopped"
	got, err := r.Handle(context.Background(), e)
	if err == nil || got.Queued || got.Diagnostic != "credential" {
		t.Fatal("bad settings accepted")
	}
	if err := store.RemovePrivate(filepath.Join(r.Repository.Directory(), "configuration.json")); err != nil {
		t.Fatal(err)
	}
	if eventAt(t, r, e, epoch.Add(time.Hour)).Queued {
		t.Fatal("bad settings reopened timing")
	}
}

func TestAttentionIsExplicitOneShotAndIndependent(t *testing.T) {
	r := fixture(t)
	e := core.Event{AgentID: "cursor", SessionID: "s", NativeRunID: "r", EventType: "started"}
	eventAt(t, r, e, epoch)
	e.EventType = "needs_attention"
	if eventAt(t, r, e, epoch.Add(time.Hour)).Queued {
		t.Fatal("attention enabled by default")
	}
	syntacticSettings(t, r, `{"enableAttention":true}`)
	a := eventAt(t, r, e, epoch.Add(time.Hour))
	if !a.Queued || readJobTest(t, r, a.JobKey).Message.Reason != "attention" {
		t.Fatal("attention missing")
	}
	if eventAt(t, r, e, epoch.Add(2*time.Hour)).Queued {
		t.Fatal("attention duplicate")
	}
	e.EventType = "stopped"
	b := eventAt(t, r, e, epoch.Add(2*time.Hour))
	if !b.Queued || a.JobKey == b.JobKey {
		t.Fatal("terminal collided")
	}
	e.EventType = "needs_attention"
	if eventAt(t, r, e, epoch.Add(3*time.Hour)).Queued {
		t.Fatal("attention reopened")
	}
}

func TestPreviewReadOnlyAndSpawnFailureEvidence(t *testing.T) {
	r := fixture(t)
	if got, err := r.Preview(context.Background(), "codex", false); err != nil || got.Queued || got.JobKey != "" {
		t.Fatal("dry preview")
	}
	if _, err := os.Lstat(r.Repository.Directory()); !os.IsNotExist(err) {
		t.Fatal("dry preview writes")
	}
	syntacticSettings(t, r, `{}`)
	if _, err := r.Preview(context.Background(), "codex", false); err != nil {
		t.Fatal("dry preview decrypts")
	}
	r.Spawn = func(_, _, _ string) error { return errors.New("PRIVATE_URL_SECRET") }
	got, err := r.Preview(context.Background(), "codex", true)
	if err == nil || got.Queued || got.Diagnostic != "spawn-failed" {
		t.Fatal("spawn falsely accepted")
	}
	j := readJobTest(t, r, got.JobKey)
	if j.Status != "pending" || j.Diagnostic != "spawn-failed" || !j.Message.Preview {
		t.Fatal("failed spawn lost")
	}
	d, err := r.Inspect(context.Background())
	if err != nil || !d.Configured || d.PendingJobs != 1 || d.DiagnosticCounts["spawn-failed"] != 1 {
		t.Fatal("spawn diagnostics")
	}
}

func TestStateJobAndSessionWriteFailures(t *testing.T) {
	for _, boundary := range []string{"runs", "sessions", "jobs"} {
		t.Run(boundary, func(t *testing.T) {
			r := fixture(t)
			if err := r.Repository.Prepare(); err != nil {
				t.Fatal(err)
			}
			e := core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "r", EventType: "started"}
			sk, rk := core.Key("codex", "s"), core.Key("codex", "s", "r")
			key := rk
			if boundary == "sessions" {
				key = sk
			}
			if boundary == "jobs" {
				eventAt(t, r, e, epoch)
				e.EventType = "stopped"
				r.Now = func() time.Time { return epoch.Add(time.Hour) }
				key = core.Key(rk, "terminal")
			}
			if err := os.Mkdir(recordPath(r, boundary, key), 0700); err != nil {
				t.Fatal(err)
			}
			got, err := r.Handle(context.Background(), e)
			if err == nil || got.Queued || (got.Diagnostic != "state-write" && got.Diagnostic != "invalid-record") {
				t.Fatal("write failure accepted")
			}
			if boundary == "jobs" {
				run, _, err := r.readRun(rk)
				if err != nil || run.Status != "stopped" {
					t.Fatal("failed job reopened timer")
				}
			}
		})
	}
}

func TestStrictRecordsAndBoundedSafeDiagnostics(t *testing.T) {
	r := fixture(t)
	syntacticSettings(t, r, `{"icons":{"codex":"https://example.invalid/PRIVATE_ICON"}}`)
	if err := r.RecordInputError(context.Background()); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(r.Repository.Directory(), "input-diagnostics.json"), map[string]any{"schemaVersion": 1, "count": 1000})
	if err := r.RecordInputError(context.Background()); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(r.Repository.Directory(), "receipts", "codex.json"), map[string]any{"secret": "PRIVATE_PATH_BODY"})
	got, err := r.Preview(context.Background(), "codex", true)
	if err != nil {
		t.Fatal(err)
	}
	j := readJobTest(t, r, got.JobKey)
	j.Diagnostic = "https://PRIVATE_TOKEN"
	put(t, recordPath(r, "jobs", got.JobKey), j)
	d, err := r.Inspect(context.Background())
	if err != nil || d.InputErrors != 1000 || !d.Receipts["codex"] || d.Receipts["cursor"] || d.DiagnosticCounts["other"] != 1 {
		t.Fatal("diagnostic projection")
	}
	encoded, _ := json.Marshal(d)
	if bytes.Contains(encoded, []byte("PRIVATE")) || bytes.Contains(encoded, []byte("https")) {
		t.Fatal("diagnostic leaks")
	}
	valid, _ := json.Marshal(j)
	for _, bad := range []string{`{}`, strings.Replace(string(valid), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(valid), `"status":"pending"`, `"status":"alien"`, 1), strings.Replace(string(valid), `"attempts":0`, `"attempts":null`, 1), strings.Replace(string(valid), `"minSeconds":1800,`, "", 1), strings.Replace(string(valid), `"preview":true`, `"preview":null`, 1)} {
		if err := store.WriteAtomic(recordPath(r, "jobs", got.JobKey), []byte(bad)); err != nil {
			t.Fatal(err)
		}
		if err := r.RunJob(context.Background(), got.JobKey); err == nil {
			t.Fatal("corrupt job accepted")
		}
	}
	for i := range 1001 {
		name := core.Key("invalid", string(rune(i))) + ".txt"
		if err := os.WriteFile(filepath.Join(r.Repository.Directory(), "sessions", name), []byte("PRIVATE_ID"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d, err = r.Inspect(context.Background())
	if err != nil || !d.Truncated || d.DiagnosticCounts["invalid-record"] != 1000 {
		t.Fatal("bounded scan did not count invalid names")
	}
}

func TestRetentionPreservesOutstandingAndUsesRunLock(t *testing.T) {
	r := fixture(t)
	e := core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "r", EventType: "started"}
	eventAt(t, r, e, epoch)
	e.EventType = "stopped"
	got := eventAt(t, r, e, epoch.Add(time.Hour))
	rk := core.Key("codex", "s", "r")
	r.Now = func() time.Time { return epoch.Add(9 * 24 * time.Hour) }
	r.cleanup(context.Background())
	if _, err := os.Stat(recordPath(r, "runs", rk)); err != nil {
		t.Fatal("run with pending job removed")
	}
	j := readJobTest(t, r, got.JobKey)
	j.Status = "sent"
	j.Attempts = 1
	j.Accepted = true
	j.FinishedAt = epoch.Add(time.Hour).Format(time.RFC3339Nano)
	j.ExtensionStatus = "pending"
	put(t, recordPath(r, "jobs", got.JobKey), j)
	r.cleanup(context.Background())
	if _, err := os.Stat(recordPath(r, "jobs", got.JobKey)); err != nil {
		t.Fatal("extension removed")
	}
	j.ExtensionStatus = "none"
	put(t, recordPath(r, "jobs", got.JobKey), j)
	held, err := store.Acquire(context.Background(), r.lockPath("session", core.Key("codex", "s")))
	if err != nil {
		t.Fatal(err)
	}
	r.cleanup(context.Background())
	held()
	if _, err := os.Stat(recordPath(r, "runs", rk)); err != nil {
		t.Fatal("run removed under held lock")
	}
	r.cleanup(context.Background())
	if _, err := os.Stat(recordPath(r, "jobs", got.JobKey)); !os.IsNotExist(err) {
		t.Fatal("expired job retained")
	}
	if _, err := os.Stat(recordPath(r, "runs", rk)); !os.IsNotExist(err) {
		t.Fatal("expired run retained")
	}
	// Retained session pointer is not permanent native-ID history.
	e.EventType = "started"
	eventAt(t, r, e, epoch.Add(10*24*time.Hour))
	if _, err := os.Stat(recordPath(r, "runs", rk)); err != nil {
		t.Fatal("retention boundary incorrect")
	}
}

func TestAtomicCreationAndSpawnDiagnosticWriteFailures(t *testing.T) {
	for _, dir := range []string{"runs", "sessions"} {
		t.Run(dir, func(t *testing.T) {
			r := fixture(t)
			e := core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "r", EventType: "started"}
			key := core.Key("codex", "s", "r")
			if dir == "sessions" {
				key = core.Key("codex", "s")
			}
			// Clock callback occurs after private reads; insert a refused target at
			// this exact atomic creation boundary, not before record validation.
			r.Now = func() time.Time {
				if err := os.Mkdir(recordPath(r, dir, key), 0700); err != nil {
					t.Fatal(err)
				}
				return epoch
			}
			got, err := r.Handle(context.Background(), e)
			if err == nil || got.Queued || got.Diagnostic != "state-write" {
				t.Fatal("atomic creation failure hidden")
			}
			if dir == "sessions" {
				run, found, err := r.readRun(core.Key("codex", "s", "r"))
				if err != nil || !found || run.Diagnostic != "state-write" {
					t.Fatal("partial state evidence missing")
				}
			}
		})
	}
	r := fixture(t)
	calls := 0
	r.Spawn = func(_, _, _ string) error { calls++; return nil }
	r.Now = func() time.Time {
		// Preview has acquired its deterministic job lock but has not written
		// the first job yet. Enumerate only this isolated test fixture.
		entries, err := os.ReadDir(filepath.Join(r.Repository.Directory(), "locks"))
		if err != nil || len(entries) != 1 {
			t.Fatal("preview lock missing")
		}
		key := strings.TrimSuffix(strings.TrimPrefix(entries[0].Name(), "job-"), ".lock")
		if err := os.Mkdir(recordPath(r, "jobs", key), 0700); err != nil {
			t.Fatal(err)
		}
		return epoch
	}
	got, err := r.Preview(context.Background(), "codex", true)
	if err == nil || got.Diagnostic != "state-write" || got.Queued || calls != 0 {
		t.Fatal("job creation failure reached spawn")
	}
	r = fixture(t)
	var unblock func()
	defer func() {
		if unblock != nil {
			unblock()
		}
	}()
	r.Spawn = func(_, _, key string) error {
		unblock = blockReplacement(t, recordPath(r, "jobs", key))
		return errors.New("PRIVATE_ERROR")
	}
	got, err = r.Preview(context.Background(), "codex", true)
	if err == nil || got.Diagnostic != "state-write" || got.Queued {
		t.Fatal("failed diagnostic persistence accepted")
	}
}

func TestLocksAndInputCounterAreReadOnlyOnContention(t *testing.T) {
	r := fixture(t)
	if err := r.RecordInputError(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"input", "session"} {
		key := core.Key("input")
		if kind == "session" {
			key = core.Key("codex", "s")
		}
		release, err := store.Acquire(context.Background(), r.lockPath(kind, key))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		if kind == "input" {
			err = r.RecordInputError(ctx)
		} else {
			_, err = r.Handle(ctx, core.Event{AgentID: "codex", SessionID: "s", EventType: "started"})
		}
		cancel()
		release()
		if err == nil {
			t.Fatal("held lock ignored")
		}
	}
	d, err := r.Inspect(context.Background())
	if err != nil || d.InputErrors != 1 || d.ActiveRuns != 0 {
		t.Fatal("contended operation mutated state")
	}
	put(t, filepath.Join(r.Repository.Directory(), "input-diagnostics.json"), map[string]any{"schemaVersion": 1, "count": -1})
	if err := r.RecordInputError(context.Background()); err == nil {
		t.Fatal("corrupt counter reset")
	}
	d, err = r.Inspect(context.Background())
	if err != nil || d.DiagnosticCounts["invalid-record"] != 1 {
		t.Fatal("corrupt counter hidden")
	}
}

func TestMalformedLifecycleRecordsAreNeverReset(t *testing.T) {
	for _, dir := range []string{"sessions", "runs"} {
		t.Run(dir, func(t *testing.T) {
			r := fixture(t)
			e := core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "r", EventType: "started"}
			eventAt(t, r, e, epoch)
			key := core.Key("codex", "s", "r")
			if dir == "sessions" {
				key = core.Key("codex", "s")
			}
			path := recordPath(r, dir, key)
			valid, err := store.ReadPrivate(path, 4<<20)
			if err != nil {
				t.Fatal(err)
			}
			badRecords := []string{`{}`, strings.Replace(string(valid), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(valid), `"schemaVersion":1`, `"schemaVersion":null`, 1)}
			if dir == "runs" {
				badRecords = append(badRecords, strings.Replace(string(valid), `"status":"active"`, `"status":"alien"`, 1), strings.Replace(string(valid), `"startedAt":"2026-01-01T00:00:00Z"`, `"startedAt":"PRIVATE"`, 1))
			}
			for _, bad := range badRecords {
				if err := store.WriteAtomic(path, []byte(bad)); err != nil {
					t.Fatal(err)
				}
				got, err := r.Handle(context.Background(), e)
				if err == nil || got.Diagnostic != "invalid-record" || got.Queued {
					t.Fatal("bad lifecycle accepted")
				}
				after, _ := os.ReadFile(path)
				if string(after) != bad {
					t.Fatal("bad state silently reset")
				}
			}
		})
	}
}

func TestRetentionBoundariesAndReadOnlyInspection(t *testing.T) {
	r := fixture(t)
	if err := r.Repository.Prepare(); err != nil {
		t.Fatal(err)
	}
	s, _ := core.Defaults()
	now := epoch.Add(8 * 24 * time.Hour)
	type candidate struct {
		status, extension string
		finished          time.Time
		remove            bool
	}
	cases := []candidate{{"pending", "none", time.Time{}, false}, {"sending", "none", time.Time{}, false}, {"sent", "ambiguous", epoch, false}, {"sent", "sending", epoch, false}, {"failed", "none", epoch, true}, {"sent", "none", epoch, true}, {"sent", "none", epoch.Add(24 * time.Hour), false}}
	var keys []string
	for i, c := range cases {
		rk := core.Key("retention", string(rune('a'+i)))
		key := core.Key(rk, "preview")
		keys = append(keys, key)
		j := newJob(rk, "preview", s, providers.Message{AgentID: "codex", Reason: "stopped", Preview: true}, 45, epoch)
		j.Status = c.status
		j.ExtensionStatus = c.extension
		if c.status != "pending" {
			j.Attempts = 1
		}
		if c.status == "sent" {
			j.Accepted = true
		}
		if !c.finished.IsZero() {
			j.FinishedAt = stamp(c.finished)
		}
		if c.extension == "sending" {
			j.ExtensionAttempted = true
		}
		put(t, recordPath(r, "jobs", key), j)
	}
	r.Now = func() time.Time { return now }
	if _, err := r.Inspect(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventAt(t, r, core.Event{AgentID: "codex", SessionID: "active", NativeRunID: "r", EventType: "started"}, now)
	for _, key := range keys {
		if _, err := os.Stat(recordPath(r, "jobs", key)); err != nil {
			t.Fatal("Hook or Inspect performed cleanup")
		}
	}
	r.cleanup(context.Background())
	for i, key := range keys {
		_, err := os.Stat(recordPath(r, "jobs", key))
		if os.IsNotExist(err) != cases[i].remove {
			t.Fatal("wrong retention eligibility")
		}
	}
	d, err := r.Inspect(context.Background())
	if err != nil || d.ActiveRuns != 1 || d.AmbiguousJobs != 3 {
		t.Fatal("active/ambiguous lost")
	}
}

func fillInvalidRecords(t *testing.T, r *Runtime, dir string, count int) {
	t.Helper()
	path := filepath.Join(r.Repository.Directory(), dir)
	if err := store.EnsurePrivateDirectory(path); err != nil {
		t.Fatal(err)
	}
	for i := range count {
		name := core.Key("invalid", string(rune(i))) + ".txt"
		if err := os.WriteFile(filepath.Join(path, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecordTraversalFairnessCapAndCancellation(t *testing.T) {
	r := fixture(t)
	if err := r.Repository.Prepare(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"sessions", "runs", "jobs"} {
		fillInvalidRecords(t, r, dir, 1001)
	}
	visits := make(map[string]int)
	truncated, err := r.walkRecords(context.Background(), func(dir, _ string) { visits[dir]++ })
	if err != nil || !truncated || visits["sessions"]+visits["runs"]+visits["jobs"] != 1000 {
		t.Fatalf("scan budget/truncation: %v %v %v", visits, truncated, err)
	}
	for _, dir := range []string{"sessions", "runs", "jobs"} {
		if visits[dir] == 0 {
			t.Errorf("crowded category starved %s", dir)
		}
	}
	visits = make(map[string]int)
	truncated, err = r.walkRecordCategories(context.Background(), []string{"runs", "jobs"}, func(dir, _ string) { visits[dir]++ })
	if err != nil || !truncated || visits["sessions"] != 0 || visits["runs"] == 0 || visits["jobs"] == 0 || visits["runs"]+visits["jobs"] != 1000 {
		t.Fatalf("cleanup category fairness/cap: %v %v %v", visits, truncated, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	visited := 0
	if _, err := r.walkRecords(ctx, func(_, _ string) { visited++ }); err == nil || visited != 0 {
		t.Fatal("canceled enumeration visited records")
	}
}

func TestRecordTraversalExactCapIsNotTruncated(t *testing.T) {
	r := fixture(t)
	if err := r.Repository.Prepare(); err != nil {
		t.Fatal(err)
	}
	fillInvalidRecords(t, r, "sessions", 1000)
	visits := 0
	truncated, err := r.walkRecords(context.Background(), func(_, _ string) { visits++ })
	if err != nil || truncated || visits != 1000 {
		t.Fatalf("exact cap falsely truncated: visits=%d truncated=%v err=%v", visits, truncated, err)
	}
}

func TestCleanupAndInspectReachRunsAndJobsPastSessionBacklog(t *testing.T) {
	r := fixture(t)
	e := core.Event{AgentID: "codex", SessionID: "expired", NativeRunID: "run", EventType: "started"}
	eventAt(t, r, e, epoch)
	e.EventType = "stopped"
	got := eventAt(t, r, e, epoch.Add(time.Hour))
	expiredRun := core.Key("codex", "expired", "run")
	j := readJobTest(t, r, got.JobKey)
	j.Status, j.Attempts, j.Accepted = "sent", 1, true
	j.FinishedAt = stamp(epoch.Add(time.Hour))
	j.ExtensionStatus = "none"
	put(t, recordPath(r, "jobs", got.JobKey), j)
	eventAt(t, r, core.Event{AgentID: "codex", SessionID: "active", NativeRunID: "run", EventType: "started"}, epoch)
	s, _ := core.Defaults()
	ambiguousKey := core.Key(core.Key("ambiguous"), "preview")
	ambiguous := newJob(core.Key("ambiguous"), "preview", s, providers.Message{AgentID: "codex", Reason: "stopped", Preview: true}, 45, epoch)
	ambiguous.Status, ambiguous.Attempts = "sending", 1
	put(t, recordPath(r, "jobs", ambiguousKey), ambiguous)
	fillInvalidRecords(t, r, "sessions", 1001)
	r.Now = func() time.Time { return epoch.Add(9 * 24 * time.Hour) }
	d, err := r.Inspect(context.Background())
	if err != nil || !d.Truncated || d.ActiveRuns != 1 || d.SentJobs != 1 || d.AmbiguousJobs != 1 {
		t.Errorf("Inspect starved later categories: %+v %v", d, err)
	}
	r.cleanup(context.Background())
	for _, path := range []string{recordPath(r, "runs", expiredRun), recordPath(r, "jobs", got.JobKey)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("session backlog starved expired run/job cleanup")
		}
	}
	for _, path := range []string{
		recordPath(r, "runs", core.Key("codex", "active", "run")),
		recordPath(r, "jobs", ambiguousKey),
		recordPath(r, "sessions", core.Key("codex", "expired")),
		r.lockPath("session", core.Key("codex", "expired")),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Error("active/ambiguous/session/lock record was removed")
		}
	}
}
