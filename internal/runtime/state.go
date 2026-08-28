// Package runtime persists hashed lifecycle records and at-most-once dispatch
// delivery intent. Independent atomic records are not an event transaction:
// a crash between state, job and spawn may lose a notification, never replay it.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/worker"
)

var (
	errUnavailable   = errors.New("runtime unavailable")
	errInvalidRecord = errors.New("invalid-record")
	errStateWrite    = errors.New("state-write")
	errCredential    = errors.New("credential")
	errDelivery      = errors.New("delivery unavailable")
)

type Runtime struct {
	Repository *configuration.Repository
	Executable string
	Now        func() time.Time
	Spawn      func(executable, dataDirectory, jobKey string) error
	Send       func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result
	Sleep      worker.Sleep
}

type EventResult struct {
	JobKey, Diagnostic string
	Queued             bool
}

func New(repository *configuration.Repository, executable string) *Runtime {
	return &Runtime{Repository: repository, Executable: executable, Now: time.Now, Spawn: worker.SpawnWorker, Send: providers.Send, Sleep: sleepContext}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type sessionRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	RunKey        string `json:"runKey"`
}

type runRecord struct {
	SchemaVersion    int    `json:"schemaVersion"`
	SessionKey       string `json:"sessionKey"`
	AgentID          string `json:"agentId"`
	Status           string `json:"status"`
	StartedAt        string `json:"startedAt"`
	FinishedAt       string `json:"finishedAt"`
	AttentionCreated bool   `json:"attentionCreated"`
	TerminalCreated  bool   `json:"terminalCreated"`
	Diagnostic       string `json:"diagnostic"`
}

type jobRecord struct {
	SchemaVersion       int               `json:"schemaVersion"`
	RunKey              string            `json:"runKey"`
	Kind                string            `json:"kind"`
	Settings            core.Settings     `json:"settings"`
	Message             providers.Message `json:"message"`
	RingSeconds         int               `json:"ringSeconds"`
	CreatedAt           string            `json:"createdAt"`
	FinishedAt          string            `json:"finishedAt"`
	Status              string            `json:"status"`
	Attempts            int               `json:"attempts"`
	Accepted            bool              `json:"accepted"`
	Diagnostic          string            `json:"diagnostic"`
	ExtensionStatus     string            `json:"extensionStatus"`
	ExtensionAttempted  bool              `json:"extensionAttempted"`
	ExtensionAccepted   bool              `json:"extensionAccepted"`
	ExtensionDiagnostic string            `json:"extensionDiagnostic"`
}

func (r *Runtime) usable(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == nil && r != nil && r.Repository != nil && r.Now != nil
}
func (r *Runtime) path(dir, key string) string {
	return filepath.Join(r.Repository.Directory(), dir, key+".json")
}
func (r *Runtime) lockPath(kind, key string) string {
	return filepath.Join(r.Repository.Directory(), "locks", kind+"-"+key+".lock")
}
func (r *Runtime) acquire(ctx context.Context, kind, key string) (func() error, error) {
	lockContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	release, err := store.Acquire(lockContext, r.lockPath(kind, key))
	if err != nil {
		return nil, errUnavailable
	}
	return release, nil
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func validStamp(s string) bool {
	t, err := time.Parse(time.RFC3339Nano, s)
	return err == nil && !t.IsZero() && stamp(t) == s
}
func nonce() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errUnavailable
	}
	return hex.EncodeToString(b[:]), nil
}

func writeRecord(path string, record any) error {
	data, err := json.Marshal(record)
	if err != nil || len(data) > strictjson.MaxBytes || store.WriteAtomic(path, data) != nil {
		return errStateWrite
	}
	return nil
}

// Require every exact field and scalar type recursively, including frozen
// settings. Partial configure patches must never fill corrupt jobs from defaults.
func strictShape(data []byte, typ reflect.Type) error {
	switch typ.Kind() {
	case reflect.Struct:
		object, err := strictjson.Object(data)
		if err != nil || len(object) != typ.NumField() {
			return errInvalidRecord
		}
		for i := range typ.NumField() {
			f := typ.Field(i)
			if err := strictShape(object[f.Tag.Get("json")], f.Type); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, err := strictjson.Object(data)
		if err != nil {
			return errInvalidRecord
		}
		for _, v := range object {
			if err := strictShape(v, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.String:
		if _, err := strictjson.String(data); err != nil {
			return errInvalidRecord
		}
	case reflect.Bool:
		if _, err := strictjson.Boolean(data); err != nil {
			return errInvalidRecord
		}
	case reflect.Int, reflect.Int64:
		if _, err := strictjson.Integer(data); err != nil {
			return errInvalidRecord
		}
	default:
		return errInvalidRecord
	}
	return nil
}

func readRecord(path string, record any) (bool, error) {
	data, err := store.ReadPrivate(path, strictjson.MaxBytes)
	if err == store.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, errInvalidRecord
	}
	if strictShape(data, reflect.TypeOf(record).Elem()) != nil || json.Unmarshal(data, record) != nil {
		return false, errInvalidRecord
	}
	return true, nil
}
func (r *Runtime) readSession(key string) (sessionRecord, bool, error) {
	var s sessionRecord
	found, err := readRecord(r.path("sessions", key), &s)
	if err == nil && found && (s.SchemaVersion != 1 || !core.ValidKey(s.RunKey)) {
		err = errInvalidRecord
	}
	return s, found, err
}
func (r *Runtime) readRun(key string) (runRecord, bool, error) {
	var run runRecord
	found, err := readRecord(r.path("runs", key), &run)
	if err != nil || !found {
		return run, found, err
	}
	_, agentErr := core.AgentByID(run.AgentID)
	valid := run.SchemaVersion == 1 && core.ValidKey(run.SessionKey) && agentErr == nil && validStamp(run.StartedAt)
	if run.Status == "active" {
		valid = valid && run.FinishedAt == "" && !run.TerminalCreated
	} else if run.Status == "stopped" || run.Status == "failed" {
		start, _ := time.Parse(time.RFC3339Nano, run.StartedAt)
		end, _ := time.Parse(time.RFC3339Nano, run.FinishedAt)
		valid = valid && validStamp(run.FinishedAt) && !end.Before(start)
	} else {
		valid = false
	}
	if !valid {
		err = errInvalidRecord
	}
	return run, found, err
}
func (r *Runtime) readJob(key string) (jobRecord, bool, error) {
	var j jobRecord
	found, err := readRecord(r.path("jobs", key), &j)
	if err != nil || !found {
		return j, found, err
	}
	_, agentErr := core.AgentByID(j.Message.AgentID)
	_, iconFrozen := j.Settings.Icons[j.Message.AgentID]
	valid := j.SchemaVersion == 1 && core.ValidKey(j.RunKey) && core.Key(j.RunKey, j.Kind) == key && core.ValidateSettings(j.Settings) == nil && iconFrozen && agentErr == nil && j.Message.DurationSeconds >= 0 && j.RingSeconds >= 30 && j.RingSeconds <= 60 && validStamp(j.CreatedAt) && j.Attempts >= 0 && j.Attempts <= 5
	switch j.Kind {
	case "preview":
		valid = valid && j.Message.Preview && j.Message.Reason == "stopped" && j.Message.DurationSeconds == 0
	case "terminal":
		valid = valid && !j.Message.Preview && (j.Message.Reason == "stopped" || j.Message.Reason == "failed")
	case "attention":
		valid = valid && !j.Message.Preview && j.Message.Reason == "attention"
	default:
		valid = false
	}
	switch j.Status {
	case "pending":
		valid = valid && !j.Accepted && j.FinishedAt == ""
	case "sending":
		valid = valid && !j.Accepted && j.Attempts > 0 && j.FinishedAt == ""
	case "sent":
		valid = valid && j.Accepted && j.Attempts > 0 && validStamp(j.FinishedAt)
	case "failed":
		valid = valid && !j.Accepted && validStamp(j.FinishedAt)
	default:
		valid = false
	}
	if j.Status != "sent" && j.ExtensionStatus != "none" {
		valid = false
	}
	switch j.ExtensionStatus {
	case "none", "pending":
		valid = valid && !j.ExtensionAttempted && !j.ExtensionAccepted
	case "sending", "failed":
		valid = valid && j.ExtensionAttempted && !j.ExtensionAccepted
	case "sent":
		valid = valid && j.ExtensionAttempted && j.ExtensionAccepted
	case "ambiguous":
		valid = valid && !j.ExtensionAccepted
	default:
		valid = false
	}
	if !valid {
		err = errInvalidRecord
	}
	return j, found, err
}
