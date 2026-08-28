package runtime

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
)

func eventFailure(category string, err error) (EventResult, error) {
	return EventResult{Diagnostic: category}, err
}

func (r *Runtime) Handle(ctx context.Context, e core.Event) (EventResult, error) {
	_, agentErr := core.AgentByID(e.AgentID)
	if agentErr != nil || e.IsChild || strings.TrimSpace(e.SessionID) == "" || !utf8.ValidString(e.SessionID) || !utf8.ValidString(e.NativeRunID) {
		return EventResult{}, nil
	}
	switch e.EventType {
	case "started", "stopped", "failed", "needs_attention":
	default:
		return EventResult{}, nil
	}
	if !r.usable(ctx) {
		return eventFailure("other", errUnavailable)
	}
	if r.Repository.Prepare() != nil {
		return eventFailure("state-write", errStateWrite)
	}
	sk := core.Key(e.AgentID, e.SessionID)
	release, err := r.acquire(ctx, "session", sk)
	if err != nil {
		return eventFailure("other", err)
	}
	defer release()
	session, found, err := r.readSession(sk)
	if err != nil {
		return eventFailure("invalid-record", err)
	}
	native := strings.TrimSpace(e.NativeRunID) != ""
	rk := ""
	if native {
		rk = core.Key(e.AgentID, e.SessionID, e.NativeRunID)
	} else if found {
		rk = session.RunKey
	}
	var run runRecord
	var runFound bool
	if rk != "" {
		run, runFound, err = r.readRun(rk)
		if err != nil || (runFound && (run.SessionKey != sk || run.AgentID != e.AgentID)) {
			return eventFailure("invalid-record", errInvalidRecord)
		}
	}
	now := r.Now()
	if e.EventType == "started" {
		if runFound && (native || run.Status == "active") {
			return EventResult{}, nil
		}
		if !native {
			random, err := nonce()
			if err != nil {
				return eventFailure("other", err)
			}
			rk = core.Key(sk, random)
		}
		run = runRecord{SchemaVersion: 1, SessionKey: sk, AgentID: e.AgentID, Status: "active", StartedAt: stamp(now)}
		if err := writeRecord(r.path("runs", rk), run); err != nil {
			return eventFailure("state-write", err)
		}
		if err := writeRecord(r.path("sessions", sk), sessionRecord{1, rk}); err != nil {
			run.Diagnostic = "state-write"
			_ = writeRecord(r.path("runs", rk), run)
			return eventFailure("state-write", err)
		}
		return EventResult{}, nil
	}
	if !runFound || run.Status != "active" {
		return EventResult{}, nil
	}
	attention := e.EventType == "needs_attention"
	if attention && run.AttentionCreated {
		return EventResult{}, nil
	}
	started, _ := time.Parse(time.RFC3339Nano, run.StartedAt)
	if now.Before(started) {
		now = started
	}
	duration := int64(now.Sub(started) / time.Second)
	if !attention {
		// End timing before settings validation, threshold checks or job creation.
		run.Status = e.EventType
		run.FinishedAt = stamp(now)
		if err := writeRecord(r.path("runs", rk), run); err != nil {
			return eventFailure("state-write", err)
		}
	}
	settings, _, err := r.Repository.View()
	if err != nil {
		run.Diagnostic = "credential"
		if writeRecord(r.path("runs", rk), run) != nil {
			return eventFailure("state-write", errStateWrite)
		}
		return eventFailure("credential", errCredential)
	}
	ring := core.RingSeconds(settings, duration)
	if ring == 0 || (attention && !settings.EnableAttention) {
		return EventResult{}, nil
	}
	kind, reason := "terminal", e.EventType
	if attention {
		kind, reason = "attention", "attention"
	}
	key := core.Key(rk, kind)
	jobRelease, err := r.acquire(ctx, "job", key)
	if err != nil {
		run.Diagnostic = "state-write"
		_ = writeRecord(r.path("runs", rk), run)
		return eventFailure("other", err)
	}
	defer jobRelease()
	if _, exists, err := r.readJob(key); err != nil {
		return eventFailure("invalid-record", err)
	} else if exists {
		return EventResult{}, nil
	}
	// Flags precede job creation. This prevents a later duplicate from repairing
	// a partial transaction and accidentally replaying a possibly spawned job.
	if attention {
		run.AttentionCreated = true
	} else {
		run.TerminalCreated = true
	}
	if err := writeRecord(r.path("runs", rk), run); err != nil {
		return eventFailure("state-write", err)
	}
	j := newJob(rk, kind, settings, providers.Message{AgentID: e.AgentID, DurationSeconds: duration, Reason: reason}, ring, now)
	if err := writeRecord(r.path("jobs", key), j); err != nil {
		run.Diagnostic = "state-write"
		_ = writeRecord(r.path("runs", rk), run)
		return eventFailure("state-write", err)
	}
	return r.spawn(key, &j)
}

func newJob(runKey, kind string, settings core.Settings, message providers.Message, ring int, now time.Time) jobRecord {
	icons := make(map[string]string, len(settings.Icons)+1)
	for id, icon := range settings.Icons {
		icons[id] = icon
	}
	icons[message.AgentID] = core.Icon(message.AgentID, settings)
	settings.Icons = icons
	return jobRecord{SchemaVersion: 1, RunKey: runKey, Kind: kind, Settings: settings, Message: message, RingSeconds: ring, CreatedAt: stamp(now), Status: "pending", ExtensionStatus: "none"}
}

func (r *Runtime) spawn(key string, j *jobRecord) (EventResult, error) {
	if r.Spawn == nil || r.Spawn(r.Executable, r.Repository.Directory(), key) != nil {
		j.Diagnostic = "spawn-failed"
		if err := writeRecord(r.path("jobs", key), j); err != nil {
			return EventResult{JobKey: key, Diagnostic: "state-write"}, err
		}
		return EventResult{JobKey: key, Diagnostic: "spawn-failed"}, errors.New("spawn-failed")
	}
	return EventResult{JobKey: key, Queued: true}, nil
}

func (r *Runtime) Preview(ctx context.Context, agentID string, send bool) (EventResult, error) {
	if !r.usable(ctx) {
		return eventFailure("other", errUnavailable)
	}
	if _, err := core.AgentByID(agentID); err != nil {
		return eventFailure("other", errUnavailable)
	}
	settings, _, err := r.Repository.View()
	if err != nil {
		return eventFailure("credential", errCredential)
	}
	if !send {
		return EventResult{}, nil
	}
	if r.Repository.Prepare() != nil {
		return eventFailure("state-write", errStateWrite)
	}
	random, err := nonce()
	if err != nil {
		return eventFailure("other", err)
	}
	rk := core.Key("preview", random)
	key := core.Key(rk, "preview")
	release, err := r.acquire(ctx, "job", key)
	if err != nil {
		return eventFailure("other", err)
	}
	defer release()
	j := newJob(rk, "preview", settings, providers.Message{AgentID: agentID, Reason: "stopped", Preview: true}, settings.MediumRingSeconds, r.Now())
	if err := writeRecord(r.path("jobs", key), j); err != nil {
		return eventFailure("state-write", err)
	}
	return r.spawn(key, &j)
}
