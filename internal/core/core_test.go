package core_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/assets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/config"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
)

func TestThresholdsAndIndependentDefaults(t *testing.T) {
	s, err := core.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seconds int64
		ring    int
	}{{1799, 0}, {1800, 45}, {3599, 45}, {3600, 60}} {
		if got := core.RingSeconds(s, tc.seconds); got != tc.ring {
			t.Fatalf("%d: %d", tc.seconds, got)
		}
	}
	s.Icons["codex"] = ""
	next, err := core.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if _, changed := next.Icons["codex"]; changed {
		t.Fatal("shared mutable defaults")
	}
}

func TestEmbeddedResourcesAndAgentsAreIndependent(t *testing.T) {
	defaults, err := os.ReadFile("../../config/defaults.json")
	if err != nil {
		t.Fatal(err)
	}
	icons, err := os.ReadFile("../../assets/agent-icons.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := config.DefaultsJSON(); !bytes.Equal(got, defaults) {
		t.Fatal("embedded defaults differ")
	}
	if got := assets.AgentIconsJSON(); !bytes.Equal(got, icons) {
		t.Fatal("embedded icons differ")
	}
	copyDefaults := config.DefaultsJSON()
	copyDefaults[0] ^= 1
	if bytes.Equal(copyDefaults, config.DefaultsJSON()) {
		t.Fatal("defaults returned shared bytes")
	}
	copyIcons := assets.AgentIconsJSON()
	copyIcons[0] ^= 1
	if bytes.Equal(copyIcons, assets.AgentIconsJSON()) {
		t.Fatal("icons returned shared bytes")
	}
	agents := core.Agents()
	if len(agents) != 8 {
		t.Fatalf("agents=%d", len(agents))
	}
	agents[0].ID = "changed"
	if core.Agents()[0].ID == "changed" {
		t.Fatal("agents returned shared slice")
	}
	for _, id := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "workbuddy", "openclaw", "hermes"} {
		a, err := core.AgentByID(id)
		if err != nil || a.ID != id || !strings.HasPrefix(a.IconURL, "https://") {
			t.Fatalf("agent %s: %#v %v", id, a, err)
		}
		settings, err := core.ParseSettings([]byte(`{"icons":{"`+id+`":"https://example.test/`+id+`.png"}}`), mustDefaults(t))
		if err != nil || core.Icon(id, settings) != "https://example.test/"+id+".png" {
			t.Fatalf("valid override %s: %q %v", id, core.Icon(id, settings), err)
		}
		for _, override := range []string{"", "http://invalid.example/icon.png", "not a URL"} {
			settings, err := core.ParseSettings([]byte(`{"icons":{"`+id+`":"`+override+`"}}`), mustDefaults(t))
			if err != nil || core.Icon(id, settings) != "" {
				t.Fatalf("invalid override %s/%q: %q %v", id, override, core.Icon(id, settings), err)
			}
		}
	}
	if _, err := core.AgentByID("Codex"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func mustDefaults(t *testing.T) core.Settings {
	t.Helper()
	settings, err := core.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestSettingsContract(t *testing.T) {
	base, err := core.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	patch := []byte(`{"minSeconds":300,"longTaskSeconds":1200,"mediumRingSeconds":30,"longRingSeconds":45,"sound":"minuet","icons":{"codex":"https://example.test/codex.png"}}`)
	got, err := core.ParseSettings(patch, base)
	if err != nil || got.MinSeconds != 300 || got.Sound != "minuet" || core.Icon("codex", got) != "https://example.test/codex.png" {
		t.Fatalf("settings=%+v err=%v", got, err)
	}
	for _, raw := range []string{
		`{"MinSeconds":300}`, `{"unknown":"value"}`, `{"continuous":"true"}`, `{"minSeconds":12.5}`, `{"minSeconds":0}`, `{"minSeconds":1800,"longTaskSeconds":1800}`,
		`{"mediumRingSeconds":29}`, `{"provider":"other"}`, `{"ntfyPriority":6}`, `{"icons":null}`, `{"icons":{"unknown":"https://example.test/x"}}`,
	} {
		if _, err := core.ParseSettings([]byte(raw), base); err == nil {
			t.Fatalf("accepted invalid settings %s", raw)
		}
	}
	long := strings.Repeat("a", 4097)
	for _, raw := range []string{`{"sound":"` + long + `"}`, `{"icons":{"codex":"` + long + `"}}`} {
		if _, err := core.ParseSettings([]byte(raw), base); err == nil {
			t.Fatal("accepted overlong setting")
		}
	}
	for _, raw := range []string{`{"icons":{"codex":""}}`, `{"icons":{"codex":"http://invalid.example/codex.png"}}`} {
		s, err := core.ParseSettings([]byte(raw), base)
		if err != nil || core.Icon("codex", s) != "" {
			t.Fatalf("invalid override %s: %q %v", raw, core.Icon("codex", s), err)
		}
	}
}

func TestKeysAreStableAndUnambiguous(t *testing.T) {
	if core.Key() != core.Key([]string{}...) || core.Key("a", "bc") == core.Key("ab", "c") {
		t.Fatal("key encoding is ambiguous")
	}
	if !core.ValidKey(core.Key("a")) || core.ValidKey(strings.ToUpper(core.Key("a"))) || core.ValidKey("abc") {
		t.Fatal("key validation incorrect")
	}
	if !reflect.DeepEqual(core.Agents(), core.Agents()) {
		t.Fatal("agent copy changed")
	}
}
