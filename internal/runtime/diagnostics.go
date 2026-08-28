package runtime

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

type Diagnostic struct {
	SchemaVersion    int             `json:"schemaVersion"`
	Configured       bool            `json:"configured"`
	ActiveRuns       int             `json:"activeRuns"`
	PendingJobs      int             `json:"pendingJobs"`
	SendingJobs      int             `json:"sendingJobs"`
	SentJobs         int             `json:"sentJobs"`
	FailedJobs       int             `json:"failedJobs"`
	AmbiguousJobs    int             `json:"ambiguousJobs"`
	InputErrors      int             `json:"inputErrors"`
	Truncated        bool            `json:"truncated"`
	Receipts         map[string]bool `json:"receipts"`
	DiagnosticCounts map[string]int  `json:"diagnosticCounts"`
}

var diagnosticKeys = []string{"credential", "transport", "http-client", "http-server", "business-client", "business-server", "malformed-response", "spawn-failed", "state-write", "invalid-record", "ambiguous", "other"}
var receiptKeys = []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "workbuddy"}

func diagnostic(text string) string {
	for _, key := range diagnosticKeys {
		if text == key {
			return key
		}
	}
	return "other"
}
func countDiagnostic(d *Diagnostic, text string) {
	if text != "" {
		key := diagnostic(text)
		d.DiagnosticCounts[key] = min(1000, d.DiagnosticCounts[key]+1)
	}
}

// Enumeration is bounded before reading entries, not after an unbounded ReadDir.
// Invalid names consume the same budget. At most 1001 are seen, 1000 visited.
func (r *Runtime) walkRecords(ctx context.Context, visit func(dir, key string)) (bool, error) {
	return r.walkRecordCategories(ctx, []string{"sessions", "runs", "jobs"}, visit)
}

func (r *Runtime) walkRecordCategories(ctx context.Context, dirs []string, visit func(dir, key string)) (bool, error) {
	type category struct {
		dir  string
		file *os.File
	}
	var categories []category
	defer func() {
		for _, c := range categories {
			if c.file != nil {
				c.file.Close()
			}
		}
	}()
	for _, dir := range dirs {
		if ctx.Err() != nil {
			return false, errUnavailable
		}
		path := filepath.Join(r.Repository.Directory(), dir)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		}
		if store.CheckPrivateDirectory(path) != nil {
			return false, errUnavailable
		}
		f, err := os.Open(path)
		if err != nil {
			return false, errUnavailable
		}
		categories = append(categories, category{dir, f})
	}
	seen, open := 0, len(categories)
	// Small round-robin reads give every existing category an opportunity even
	// when another category is much larger than the total operation budget.
	for open > 0 {
		for i := range categories {
			c := &categories[i]
			if c.file == nil {
				continue
			}
			if ctx.Err() != nil {
				return false, errUnavailable
			}
			entries, readErr := c.file.ReadDir(min(32, 1001-seen))
			for _, entry := range entries {
				seen++
				if seen > 1000 {
					return true, nil
				}
				if ctx.Err() != nil {
					return false, errUnavailable
				}
				key := strings.TrimSuffix(entry.Name(), ".json")
				if !strings.HasSuffix(entry.Name(), ".json") || !core.ValidKey(key) || !entry.Type().IsRegular() {
					key = ""
				}
				visit(c.dir, key)
			}
			if readErr == io.EOF {
				c.file.Close()
				c.file = nil
				open--
			} else if readErr != nil {
				return false, errUnavailable
			}
		}
	}
	return false, nil
}

type inputRecord struct {
	SchemaVersion int `json:"schemaVersion"`
	Count         int `json:"count"`
}

func (r *Runtime) readInput() (inputRecord, error) {
	var record inputRecord
	found, err := readRecord(filepath.Join(r.Repository.Directory(), "input-diagnostics.json"), &record)
	if err != nil {
		return record, err
	}
	if !found {
		return inputRecord{1, 0}, nil
	}
	if record.SchemaVersion != 1 || record.Count < 0 || record.Count > 1000 {
		return record, errInvalidRecord
	}
	return record, nil
}
func (r *Runtime) RecordInputError(ctx context.Context) error {
	if !r.usable(ctx) {
		return errUnavailable
	}
	if r.Repository.Prepare() != nil {
		return errStateWrite
	}
	release, err := r.acquire(ctx, "input", core.Key("input"))
	if err != nil {
		return err
	}
	defer release()
	record, err := r.readInput()
	if err != nil {
		return err
	}
	record.Count = min(1000, record.Count+1)
	return writeRecord(filepath.Join(r.Repository.Directory(), "input-diagnostics.json"), record)
}

func (r *Runtime) Inspect(ctx context.Context) (Diagnostic, error) {
	d := Diagnostic{SchemaVersion: 1, Receipts: make(map[string]bool), DiagnosticCounts: make(map[string]int)}
	for _, key := range diagnosticKeys {
		d.DiagnosticCounts[key] = 0
	}
	for _, key := range receiptKeys {
		d.Receipts[key] = false
	}
	if !r.usable(ctx) {
		return d, errUnavailable
	}
	_, configured, err := r.Repository.View()
	if err != nil {
		return d, errCredential
	}
	d.Configured = configured
	if _, err := os.Lstat(r.Repository.Directory()); os.IsNotExist(err) {
		return d, nil
	}
	if record, err := r.readInput(); err != nil {
		countDiagnostic(&d, "invalid-record")
	} else {
		d.InputErrors = record.Count
	}
	d.Truncated, err = r.walkRecords(ctx, func(dir, key string) {
		if key == "" {
			countDiagnostic(&d, "invalid-record")
			return
		}
		switch dir {
		case "sessions":
			if _, _, err := r.readSession(key); err != nil {
				countDiagnostic(&d, "invalid-record")
			}
		case "runs":
			run, found, err := r.readRun(key)
			if err != nil {
				countDiagnostic(&d, "invalid-record")
			} else if found {
				if run.Status == "active" {
					d.ActiveRuns++
				}
				countDiagnostic(&d, run.Diagnostic)
			}
		case "jobs":
			j, found, err := r.readJob(key)
			if err != nil {
				countDiagnostic(&d, "invalid-record")
				return
			}
			if !found {
				return
			}
			switch j.Status {
			case "pending":
				d.PendingJobs++
			case "sending":
				d.SendingJobs++
			case "sent":
				d.SentJobs++
			case "failed":
				d.FailedJobs++
			}
			if j.Status == "sending" || (j.Status == "pending" && j.Attempts > 0) || j.ExtensionStatus == "pending" || j.ExtensionStatus == "sending" || j.ExtensionStatus == "ambiguous" {
				d.AmbiguousJobs++
			}
			countDiagnostic(&d, j.Diagnostic)
			countDiagnostic(&d, j.ExtensionDiagnostic)
		}
	})
	if err != nil {
		return d, err
	}
	receipts := filepath.Join(r.Repository.Directory(), "receipts")
	if _, err := os.Lstat(receipts); !os.IsNotExist(err) {
		if store.CheckPrivateDirectory(receipts) != nil {
			return d, errUnavailable
		}
		for _, key := range receiptKeys {
			info, err := os.Lstat(filepath.Join(receipts, key+".json"))
			d.Receipts[key] = err == nil && info.Mode().IsRegular()
		}
	}
	return d, nil
}

func expired(at string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, at)
	return err == nil && now.Sub(t) > 7*24*time.Hour
}
func removableJob(j jobRecord, now time.Time) bool {
	return (j.Status == "sent" || j.Status == "failed") && j.ExtensionStatus != "pending" && j.ExtensionStatus != "sending" && j.ExtensionStatus != "ambiguous" && expired(j.FinishedAt, now)
}

// Retention is best-effort, never a replay/reconciliation service. Native-ID
// duplicate suppression lasts only as long as retained run history (7 days).
// A deadline bounds waits, not the operating system's filesystem I/O latency.
func (r *Runtime) cleanup(parent context.Context) {
	if !r.usable(parent) {
		return
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	now := r.Now()
	_, _ = r.walkRecordCategories(ctx, []string{"runs", "jobs"}, func(dir, key string) {
		if key == "" || ctx.Err() != nil {
			return
		}
		switch dir {
		case "jobs":
			j, found, err := r.readJob(key)
			if err != nil || !found || !removableJob(j, now) {
				return
			}
			release, err := r.cleanupLock(ctx, "job", key)
			if err != nil {
				return
			}
			defer release()
			j, found, err = r.readJob(key)
			if err == nil && found && removableJob(j, now) {
				_ = store.RemovePrivate(r.path("jobs", key))
			}
		case "runs":
			r.cleanupRun(ctx, key, now)
		}
	})
}

// Contention is skipped, not allowed to consume the whole cleanup budget.
func (r *Runtime) cleanupLock(ctx context.Context, kind, key string) (func() error, error) {
	short, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	return store.Acquire(short, r.lockPath(kind, key))
}
func (r *Runtime) cleanupRun(ctx context.Context, key string, now time.Time) {
	run, found, err := r.readRun(key)
	if err != nil || !found || run.Status == "active" || !expired(run.FinishedAt, now) {
		return
	}
	release, err := r.cleanupLock(ctx, "session", run.SessionKey)
	if err != nil {
		return
	}
	defer release()
	run, found, err = r.readRun(key)
	if err != nil || !found || run.Status == "active" || !expired(run.FinishedAt, now) {
		return
	}
	keys := []string{core.Key(key, "terminal"), core.Key(key, "attention")}
	sort.Strings(keys)
	for _, jobKey := range keys {
		release, err := r.cleanupLock(ctx, "job", jobKey)
		if err != nil {
			return
		}
		defer release()
	}
	for _, jobKey := range keys {
		j, found, err := r.readJob(jobKey)
		if err != nil || (found && !removableJob(j, now)) {
			return
		}
	}
	if ctx.Err() == nil {
		_ = store.RemovePrivate(r.path("runs", key))
	}
}
