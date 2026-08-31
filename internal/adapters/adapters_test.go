package adapters

import (
	"reflect"
	"testing"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
)

func TestNormalizeAllLegacyMappings(t *testing.T) {
	cases := []struct {
		name, agent, raw string
		want             core.Event
		accepted         bool
	}{
		{"codex start", "codex", `{"hook_event_name":"UserPromptSubmit","session_id":"s","turn_id":"r"}`, core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "r", EventType: "started"}, true},
		{"codex stop", "codex", `{"hook_event_name":"Stop","session_id":"s","turn_id":"r"}`, core.Event{AgentID: "codex", SessionID: "s", NativeRunID: "r", EventType: "stopped", Reason: "stopped"}, true},
		{"claude start", "claude-code", `{"hook_event_name":"UserPromptSubmit","session_id":"s"}`, core.Event{AgentID: "claude-code", SessionID: "s", EventType: "started"}, true},
		{"claude failure", "claude-code", `{"hook_event_name":"StopFailure","session_id":"s"}`, core.Event{AgentID: "claude-code", SessionID: "s", EventType: "failed", Reason: "failed"}, true},
		{"claude stop", "claude-code", `{"hook_event_name":"Stop","session_id":"s"}`, core.Event{AgentID: "claude-code", SessionID: "s", EventType: "stopped", Reason: "stopped"}, true},
		{"cursor start", "cursor", `{"hook_event_name":"beforeSubmitPrompt","conversation_id":"s","generation_id":"r"}`, core.Event{AgentID: "cursor", SessionID: "s", NativeRunID: "r", EventType: "started"}, true},
		{"cursor completed", "cursor", `{"hook_event_name":"stop","conversation_id":"s","generation_id":"r","status":"completed"}`, core.Event{AgentID: "cursor", SessionID: "s", NativeRunID: "r", EventType: "stopped", Reason: "completed"}, true},
		{"cursor error", "cursor", `{"hook_event_name":"stop","conversation_id":"s","generation_id":"r","status":"error"}`, core.Event{AgentID: "cursor", SessionID: "s", NativeRunID: "r", EventType: "failed", Reason: "failed"}, true},
		{"cursor aborted", "cursor", `{"hook_event_name":"stop","conversation_id":"s","generation_id":"r","status":"aborted"}`, core.Event{AgentID: "cursor", SessionID: "s", NativeRunID: "r", EventType: "failed", Reason: "failed"}, true},
		{"gemini start", "gemini-cli", `{"hook_event_name":"BeforeAgent","session_id":"s"}`, core.Event{AgentID: "gemini-cli", SessionID: "s", EventType: "started"}, true},
		{"gemini stop", "gemini-cli", `{"hook_event_name":"AfterAgent","session_id":"s"}`, core.Event{AgentID: "gemini-cli", SessionID: "s", EventType: "stopped", Reason: "stopped"}, true},
		{"opencode start", "opencode", `{"schemaVersion":1,"event":"started","sessionId":"s","runId":"r"}`, core.Event{AgentID: "opencode", SessionID: "s", NativeRunID: "r", EventType: "started"}, true},
		{"opencode stop", "opencode", `{"schemaVersion":1,"event":"stopped","sessionId":"s","runId":"r"}`, core.Event{AgentID: "opencode", SessionID: "s", NativeRunID: "r", EventType: "stopped", Reason: "stopped"}, true},
		{"opencode failure", "opencode", `{"schemaVersion":1,"event":"failed","sessionId":"s","runId":"r"}`, core.Event{AgentID: "opencode", SessionID: "s", NativeRunID: "r", EventType: "failed", Reason: "failed"}, true},
		{"workbuddy start", "workbuddy", `{"hook_event_name":"UserPromptSubmit","session_id":"s"}`, core.Event{AgentID: "workbuddy", SessionID: "s", EventType: "started"}, true},
		{"workbuddy stop", "workbuddy", `{"hook_event_name":"Stop","session_id":"s"}`, core.Event{AgentID: "workbuddy", SessionID: "s", EventType: "stopped", Reason: "stopped"}, true},
		{"openclaw start", "openclaw", `{"schemaVersion":1,"event":"started","sessionId":"s","runId":"r"}`, core.Event{AgentID: "openclaw", SessionID: "s", NativeRunID: "r", EventType: "started"}, true},
		{"openclaw stop", "openclaw", `{"schemaVersion":1,"event":"stopped","sessionId":"s","runId":"r"}`, core.Event{AgentID: "openclaw", SessionID: "s", NativeRunID: "r", EventType: "stopped", Reason: "stopped"}, true},
		{"openclaw failure", "openclaw", `{"schemaVersion":1,"event":"failed","sessionId":"s","runId":"r"}`, core.Event{AgentID: "openclaw", SessionID: "s", NativeRunID: "r", EventType: "failed", Reason: "failed"}, true},
		{"hermes start", "hermes", `{"hook_event_name":"pre_llm_call","session_id":"s","extra":{"turn_id":"r","user_message":"discard me"}}`, core.Event{AgentID: "hermes", SessionID: "s", NativeRunID: "r", EventType: "started"}, true},
		{"hermes start with null parent", "hermes", `{"hook_event_name":"pre_llm_call","session_id":"s","extra":{"turn_id":"r","parent_session_id":null}}`, core.Event{AgentID: "hermes", SessionID: "s", NativeRunID: "r", EventType: "started"}, true},
		{"hermes completed", "hermes", `{"hook_event_name":"on_session_end","session_id":"s","extra":{"turn_id":"r","completed":true,"failed":false,"interrupted":false}}`, core.Event{AgentID: "hermes", SessionID: "s", NativeRunID: "r", EventType: "stopped", Reason: "completed"}, true},
		{"hermes failed", "hermes", `{"hook_event_name":"on_session_end","session_id":"s","extra":{"turn_id":"r","completed":false,"failed":true,"interrupted":false}}`, core.Event{AgentID: "hermes", SessionID: "s", NativeRunID: "r", EventType: "failed", Reason: "failed"}, true},
		{"hermes interrupted reduced exit", "hermes", `{"hook_event_name":"on_session_end","session_id":"s","extra":{"interrupted":true}}`, core.Event{AgentID: "hermes", SessionID: "s", EventType: "failed", Reason: "failed"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, accepted, err := Normalize(tc.agent, []byte(tc.raw))
			if err != nil || accepted != tc.accepted || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%+v accepted=%t err=%v", got, accepted, err)
			}
		})
	}
}

func TestNormalizeIgnoresUnsupportedAndChildren(t *testing.T) {
	cases := []struct{ agent, raw string }{
		{"codex", `{"hook_event_name":"Unknown","session_id":"s","turn_id":"r"}`}, {"codex", `{"hook_event_name":"Stop","session_id":"s"}`},
		{"claude-code", `{"hook_event_name":"Stop"}`}, {"claude-code", `{"hook_event_name":"Stop","session_id":"s","agent_id":"child"}`},
		{"cursor", `{"hook_event_name":"stop","conversation_id":"s","status":"completed"}`}, {"cursor", `{"hook_event_name":"stop","conversation_id":"s","generation_id":"r","status":"unknown"}`},
		{"gemini-cli", `{"hook_event_name":"AfterAgent"}`}, {"gemini-cli", `{"hook_event_name":"AfterModel","session_id":"s"}`},
		{"opencode", `{"schemaVersion":1,"event":"started","sessionId":"s"}`}, {"opencode", `{"schemaVersion":2,"event":"started","sessionId":"s","runId":"r"}`},
		{"opencode", `{"schemaVersion":1.0,"event":"started","sessionId":"s","runId":"r"}`}, {"opencode", `{"schemaVersion":"1","event":"started","sessionId":"s","runId":"r"}`},
		{"opencode", `{"schemaVersion":1,"event":"started","sessionId":"s","runId":"r","parentId":"parent"}`}, {"workbuddy", `{"hook_event_name":"Stop"}`}, {"workbuddy", `{"hook_event_name":"SubagentStop","session_id":"s"}`},
		{"codex", `{"hook_event_name":"Stop","session_id":"s","turn_id":"r","parent_session_id":null}`}, {"cursor", `{"hook_event_name":"stop","conversation_id":"s","generation_id":"r","status":"completed","parentSessionId":"p"}`},
		{"openclaw", `{"schemaVersion":1,"event":"started","sessionId":"s"}`}, {"openclaw", `{"schemaVersion":2,"event":"started","sessionId":"s","runId":"r"}`},
		{"openclaw", `{"schemaVersion":1,"event":"started","sessionId":"s","runId":"r","parentSessionId":"p"}`},
		{"hermes", `{"hook_event_name":"pre_llm_call","session_id":"s","extra":{"parent_session_id":"p","turn_id":"r"}}`},
		{"hermes", `{"hook_event_name":"on_session_end","session_id":"s","extra":{"completed":false,"failed":false,"interrupted":false}}`},
	}
	for _, tc := range cases {
		got, accepted, err := Normalize(tc.agent, []byte(tc.raw))
		if err != nil || accepted || got != (core.Event{}) {
			t.Fatalf("%s: got=%+v accepted=%t err=%v", tc.agent, got, accepted, err)
		}
	}
	for _, raw := range []string{`[]`, `{"x":1,"x":2}`, `{"hook_event_name":"Stop","session_id":"s","turn_id":"r"} junk`} {
		if _, _, err := Normalize("codex", []byte(raw)); err == nil {
			t.Fatalf("accepted malformed %s", raw)
		}
	}
	if _, _, err := Normalize("unknown", []byte(`{}`)); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, _, err := Normalize("hermes", []byte(`{"hook_event_name":"on_session_end","session_id":"s","extra":{"completed":"true"}}`)); err == nil {
		t.Fatal("malformed Hermes outcome accepted")
	}
}

func TestNeutralOutput(t *testing.T) {
	for _, tc := range []struct{ agent, raw, want string }{
		{"codex", `{"hook_event_name":"Stop"}`, `{"continue":true}`}, {"workbuddy", `{}`, `{"continue":true}`}, {"cursor", `{"hook_event_name":"beforeSubmitPrompt"}`, `{"continue":true}`}, {"cursor", `{"hook_event_name":"stop"}`, `{}`}, {"claude-code", `{}`, `{}`}, {"openclaw", `{}`, `{}`}, {"hermes", `{}`, `{}`}, {"unknown", `{}`, `{}`},
	} {
		if got := string(Neutral(tc.agent, []byte(tc.raw))); got != tc.want {
			t.Fatalf("%s: %s", tc.agent, got)
		}
	}
}
