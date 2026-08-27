// Package adapters normalizes supported hook payloads without retaining hook content.
package adapters

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

// Normalize converts a known hook payload to its minimal event. A false accepted
// value with a nil error is an expected ignored event, including child activity.
func Normalize(agentID string, data []byte) (event core.Event, accepted bool, err error) {
	if !knownAgent(agentID) {
		return core.Event{}, false, errors.New("unknown agent source")
	}
	object, err := strictjson.Object(data)
	if err != nil {
		return core.Event{}, false, err
	}
	if childEvent(agentID, object) {
		return core.Event{}, false, nil
	}
	hook, _ := requiredString(object, "hook_event_name")
	switch agentID {
	case "codex":
		session, sessionOK := requiredString(object, "session_id")
		run, runOK := requiredString(object, "turn_id")
		if !sessionOK || !runOK {
			return core.Event{}, false, nil
		}
		switch hook {
		case "UserPromptSubmit":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "started"}, true, nil
		case "Stop":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "stopped", Reason: "stopped"}, true, nil
		}
	case "claude-code":
		session, ok := requiredString(object, "session_id")
		if !ok {
			return core.Event{}, false, nil
		}
		switch hook {
		case "UserPromptSubmit":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "started"}, true, nil
		case "Stop":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "stopped", Reason: "stopped"}, true, nil
		case "StopFailure":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "failed", Reason: "failed"}, true, nil
		}
	case "cursor":
		session, sessionOK := requiredString(object, "conversation_id")
		run, runOK := requiredString(object, "generation_id")
		if !sessionOK || !runOK {
			return core.Event{}, false, nil
		}
		if hook == "beforeSubmitPrompt" {
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "started"}, true, nil
		}
		if hook != "stop" {
			return core.Event{}, false, nil
		}
		status, _ := requiredString(object, "status")
		switch status {
		case "completed":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "stopped", Reason: "completed"}, true, nil
		case "error", "aborted":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "failed", Reason: "failed"}, true, nil
		}
	case "gemini-cli":
		session, ok := requiredString(object, "session_id")
		if !ok {
			return core.Event{}, false, nil
		}
		switch hook {
		case "BeforeAgent":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "started"}, true, nil
		case "AfterAgent":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "stopped", Reason: "stopped"}, true, nil
		}
	case "opencode":
		version, versionOK := requiredInteger(object, "schemaVersion")
		eventName, eventOK := requiredString(object, "event")
		session, sessionOK := requiredString(object, "sessionId")
		run, runOK := requiredString(object, "runId")
		if !versionOK || version != 1 || !eventOK || !sessionOK || !runOK {
			return core.Event{}, false, nil
		}
		switch eventName {
		case "started":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "started"}, true, nil
		case "stopped":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "stopped", Reason: "stopped"}, true, nil
		case "failed":
			return core.Event{AgentID: agentID, SessionID: session, NativeRunID: run, EventType: "failed", Reason: "failed"}, true, nil
		}
	case "workbuddy":
		session, ok := requiredString(object, "session_id")
		if !ok {
			return core.Event{}, false, nil
		}
		switch hook {
		case "UserPromptSubmit":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "started"}, true, nil
		case "Stop":
			return core.Event{AgentID: agentID, SessionID: session, EventType: "stopped", Reason: "stopped"}, true, nil
		}
	}
	return core.Event{}, false, nil
}

func Neutral(agentID string, data []byte) []byte {
	if agentID == "codex" || agentID == "workbuddy" {
		return []byte(`{"continue":true}`)
	}
	if agentID == "cursor" {
		object, err := strictjson.Object(data)
		if err == nil {
			if hook, ok := requiredString(object, "hook_event_name"); ok && hook == "beforeSubmitPrompt" {
				return []byte(`{"continue":true}`)
			}
		}
	}
	return []byte(`{}`)
}

func knownAgent(agentID string) bool {
	_, err := core.AgentByID(agentID)
	return err == nil
}

func childEvent(agentID string, object map[string]json.RawMessage) bool {
	if _, present := object["parent_session_id"]; present {
		return true
	}
	if _, present := object["parentSessionId"]; present {
		return true
	}
	if hook, ok := requiredString(object, "hook_event_name"); ok && hook == "SubagentStop" {
		return true
	}
	if agentID == "claude-code" {
		_, child := requiredString(object, "agent_id")
		return child
	}
	if agentID == "opencode" {
		_, child := requiredString(object, "parentId")
		return child
	}
	return false
}

func requiredString(object map[string]json.RawMessage, key string) (string, bool) {
	raw, present := object[key]
	if !present {
		return "", false
	}
	value, err := strictjson.String(raw)
	return value, err == nil && strings.TrimSpace(value) != ""
}

func requiredInteger(object map[string]json.RawMessage, key string) (int64, bool) {
	raw, present := object[key]
	if !present {
		return 0, false
	}
	value, err := strictjson.Integer(raw)
	return value, err == nil
}
