package install

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

type ownedEntry struct {
	Event string          `json:"event"`
	Value json.RawMessage `json:"value"`
}

func automaticAgent(agent string) bool {
	switch agent {
	case "codex", "claude-code", "cursor", "gemini-cli", "opencode":
		return true
	}
	return false
}

func resolveTarget(agent, explicit string) (config, target string, err error) {
	if !automaticAgent(agent) {
		if agent == "workbuddy" {
			return "", "", ErrManualPackageRequired
		}
		return "", "", ErrInvalid
	}
	config = explicit
	if config == "" {
		home, e := os.UserHomeDir()
		if e != nil || !cleanAbsolute(home) {
			return "", "", ErrUnavailable
		}
		switch agent {
		case "codex":
			config = filepath.Join(home, ".codex", "hooks.json")
		case "claude-code":
			config = filepath.Join(home, ".claude", "settings.json")
		case "cursor":
			config = filepath.Join(home, ".cursor", "hooks.json")
		case "gemini-cli":
			config = filepath.Join(home, ".gemini", "settings.json")
		case "opencode":
			base := os.Getenv("XDG_CONFIG_HOME")
			if base == "" {
				base = filepath.Join(home, ".config")
			}
			if !cleanAbsolute(base) {
				return "", "", ErrInvalid
			}
			config = filepath.Join(base, "opencode", "opencode.json")
		}
	}
	if !cleanAbsolute(config) {
		return "", "", ErrInvalid
	}
	target = config
	if agent == "opencode" {
		target = filepath.Join(filepath.Dir(config), "plugins", "agent-task-notify.js")
	}
	return config, target, nil
}

func ownedEntries(agent, command string) []ownedEntry {
	var events []string
	switch agent {
	case "codex":
		events = []string{"UserPromptSubmit", "Stop"}
	case "claude-code":
		events = []string{"UserPromptSubmit", "Stop", "StopFailure"}
	case "cursor":
		events = []string{"beforeSubmitPrompt", "stop"}
	case "gemini-cli":
		events = []string{"BeforeAgent", "AfterAgent"}
	}
	entries := make([]ownedEntry, 0, len(events))
	for _, event := range events {
		value := map[string]any{"command": command}
		if agent != "cursor" {
			value = map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
		}
		raw, _ := json.Marshal(value)
		entries = append(entries, ownedEntry{event, raw})
	}
	return entries
}

func canonical(data []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
func equalJSON(a, b []byte) bool { return bytes.Equal(canonical(a), canonical(b)) }

var encodedCommand = regexp.MustCompile(`(?i)(?:^|\s)[-/]([a-z]+)\s+['"]?([A-Za-z0-9+/=]+)`)

func scriptMarker(value string) bool {
	v := strings.ToLower(value)
	return strings.Contains(v, "codexlongtasknotify") || strings.Contains(v, "agent-task-notify.ps1")
}

// Decode only, bounded to one UTF-16LE command. Never invoke legacy contents.
func legacyString(value string) bool {
	if scriptMarker(value) {
		return true
	}
	for _, match := range encodedCommand.FindAllStringSubmatch(value, -1) {
		flag := strings.ToLower(match[1])
		if flag != "ec" && !strings.HasPrefix("encodedcommand", flag) {
			continue
		}
		if len(match[2]) > 65536 {
			return true
		}
		data, err := base64.StdEncoding.Strict().DecodeString(match[2])
		if err != nil || len(data)%2 != 0 {
			return true
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		if scriptMarker(string(utf16.Decode(units))) {
			return true
		}
	}
	return false
}

func containsString(data []byte, match func(string) bool) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	for {
		token, err := dec.Token()
		if err != nil {
			return false
		}
		if value, ok := token.(string); ok && match(value) {
			return true
		}
	}
}

func parseHost(data []byte, exists bool) (map[string]json.RawMessage, error) {
	if !exists {
		return make(map[string]json.RawMessage), nil
	}
	object, err := strictjson.Object(data)
	if err != nil {
		return nil, ErrInvalid
	}
	if containsString(data, legacyString) {
		return nil, ErrConflict
	}
	return object, nil
}

// Merge raw values rather than float64-decoded documents. Unknown numbers and
// fields survive, and only exact single owned entries can be removed.
func mergeJSON(agent string, data []byte, exists bool, previous, desired []ownedEntry) ([]byte, error) {
	object, err := parseHost(data, exists)
	if err != nil {
		return nil, err
	}
	hooks := make(map[string]json.RawMessage)
	if value, ok := object["hooks"]; ok {
		hooks, err = strictjson.Object(value)
		if err != nil {
			return nil, ErrInvalid
		}
	}
	if agent == "cursor" {
		if raw, ok := object["version"]; ok {
			version, e := strictjson.Integer(raw)
			if e != nil || version != 1 {
				return nil, ErrInvalid
			}
		}
		if len(desired) > 0 {
			object["version"] = json.RawMessage(`1`)
		}
	}
	arrays := make(map[string][]json.RawMessage)
	for event, raw := range hooks {
		var entries []json.RawMessage
		if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' || json.Unmarshal(raw, &entries) != nil {
			return nil, ErrInvalid
		}
		arrays[event] = entries
	}
	for _, entry := range previous {
		matches := 0
		index := -1
		for i, v := range arrays[entry.Event] {
			if equalJSON(v, entry.Value) {
				matches++
				index = i
			}
		}
		if matches != 1 {
			return nil, ErrConflict
		}
		values := arrays[entry.Event]
		arrays[entry.Event] = append(values[:index:index], values[index+1:]...)
	}
	for _, values := range arrays {
		for _, v := range values {
			if containsString(v, func(s string) bool { return strings.Contains(strings.ToLower(s), "agent-task-notify") }) {
				return nil, ErrConflict
			}
		}
	}
	for _, entry := range desired {
		for _, value := range arrays[entry.Event] {
			if equalJSON(value, entry.Value) {
				return nil, ErrConflict
			}
		}
		arrays[entry.Event] = append(arrays[entry.Event], entry.Value)
	}
	for event, values := range arrays {
		if len(values) == 0 {
			removed := false
			for _, entry := range previous {
				if entry.Event == event {
					removed = true
				}
			}
			if removed {
				delete(hooks, event)
				continue
			}
		}
		hooks[event], _ = json.Marshal(values)
	}
	object["hooks"], _ = json.Marshal(hooks)
	out, err := json.Marshal(object)
	if err != nil || len(out) > strictjson.MaxBytes {
		return nil, ErrInvalid
	}
	return append(out, '\n'), nil
}
