package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

type forbiddenInput struct{}

func (forbiddenInput) Read([]byte) (int, error) { panic("unexpected input read") }

func invoke(args []string, input io.Reader) (int, string, string) {
	var out, errors bytes.Buffer
	code := RunWithInput(args, input, &out, &errors)
	return code, out.String(), errors.String()
}

func cliDirectory(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "data")
}

// Catches accepting arbitrary/duplicate flags, echoing rejected values, or
// reading a real terminal on a command which needs no input.
func TestArgumentContracts(t *testing.T) {
	for _, args := range [][]string{{"configure", "--provider", "bark", "--endpoint", "planted-secret"}, {"configure", "--provider", "bark", "--token", "planted-secret"}, {"configure", "--provider", "bark", "--password", "planted-secret"}, {"doctor", "--data-directory", "planted-secret", "--data-directory", "other"}, {"preview", "--agent", "codex", "--send", "--send"}, {"preview", "--agent", "planted-secret"}, {"install", "--agent", "cursor", "--executable", "planted-secret"}, {"install", "--agent", "cursor", "--command-shell", "planted-secret"}, {"uninstall", "--agent", "cursor", "--config-path", "planted-secret"}, {"configure", "--provider", "planted-secret"}, {"worker", "--job", strings.Repeat("a", 64)}, {"worker", "--data-directory", "planted-secret"}, {"worker", "--data-directory", "planted-secret", "--job", "bad"}, {"doctor", "--data-directory=" + "planted-secret"}, {"version", "planted-secret"}} {
		code, out, errors := invoke(args, forbiddenInput{})
		if code != 2 || out != "" || strings.Contains(out+errors, "planted-secret") {
			t.Fatalf("unsafe argument response: %v code=%d", args[0], code)
		}
		if args[0] == "worker" && errors != "" {
			t.Fatal("worker wrote diagnostics")
		}
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		code, out, errors := invoke(args, forbiddenInput{})
		if code != 0 || out != testUsage || errors != "" {
			t.Fatal("help not pure")
		}
	}
}

func TestDryPreviewAndDoctorDoNotCreateState(t *testing.T) {
	directory := cliDirectory(t)
	for _, args := range [][]string{{"preview", "--agent", "workbuddy"}, {"doctor"}, {"uninstall", "--agent", "cursor"}} {
		code, out, errors := invoke(append(args, "--data-directory", directory), forbiddenInput{})
		if code != 0 || errors != "" || !json.Valid([]byte(out)) {
			t.Fatalf("read only command failed %s %d %s", args[0], code, errors)
		}
		if _, err := os.Lstat(directory); !os.IsNotExist(err) {
			t.Fatal("read-only created state")
		}
	}
}

// An unauthentic envelope proves these views do not decrypt credentials.
func TestDryPreviewAndDoctorNeverDecryptOrDisclose(t *testing.T) {
	directory := cliDirectory(t)
	r, err := configuration.Open(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Prepare(); err != nil {
		t.Fatal(err)
	}
	s, _ := core.Defaults()
	s.Icons["codex"] = "https://synthetic.invalid/private-icon"
	id := strings.Repeat("a", 32)
	identity := []byte(`{"schemaVersion":1,"installationId":"` + id + `"}`)
	if err := store.WriteAtomic(filepath.Join(directory, "installation.json"), identity); err != nil {
		t.Fatal(err)
	}
	bundle := map[string]any{"schemaVersion": 1, "settings": s, "credentials": map[string]any{"bark": map[string]any{"schemaVersion": 1, "backend": "dpapi", "installationId": id, "purpose": "credential:bark", "ciphertext": "AQ=="}}}
	data, _ := json.Marshal(bundle)
	if err := store.WriteAtomic(filepath.Join(directory, "configuration.json"), data); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"preview", "doctor"} {
		args := []string{command, "--data-directory", directory}
		if command == "preview" {
			args = append(args, "--agent", "codex")
		}
		code, out, errors := invoke(args, forbiddenInput{})
		if code != 0 || errors != "" || strings.Contains(out, id) || strings.Contains(out, "private-icon") || strings.Contains(out, directory) {
			t.Fatal("unsafe view")
		}
		var object map[string]any
		if json.Unmarshal([]byte(out), &object) != nil {
			t.Fatal("invalid safe view")
		}
		if command == "preview" && len(object) != 5 {
			t.Fatal("unexpected preview fields")
		}
		if command == "doctor" && (len(object) != 12 || object["configured"] != true || object["inputErrors"] != float64(0)) {
			t.Fatal("unexpected doctor fields")
		}
	}
	after, _ := os.ReadFile(filepath.Join(directory, "configuration.json"))
	if !bytes.Equal(data, after) {
		t.Fatal("view changed bundle")
	}
	if after, _ := os.ReadFile(filepath.Join(directory, "installation.json")); !bytes.Equal(after, identity) {
		t.Fatal("view changed key identity")
	}
}

func TestHookNeutralAndBoundedInputErrors(t *testing.T) {
	directory := cliDirectory(t)
	inputs := []string{"{", string([]byte{255}), strings.Repeat("x", 4<<20), strings.Repeat("x", (4<<20)+1)}
	for _, input := range inputs {
		code, out, errors := invoke([]string{"hook", "--agent", "codex", "--data-directory", directory}, strings.NewReader(input))
		if code != 0 || out != "{\"continue\":true}\n" || errors != "" {
			t.Fatal("hook broke neutral contract")
		}
	}
	code, out, errors := invoke([]string{"doctor", "--data-directory", directory}, forbiddenInput{})
	var d map[string]any
	_ = json.Unmarshal([]byte(out), &d)
	if code != 0 || errors != "" || d["inputErrors"] != float64(4) {
		t.Fatal("input errors not counted")
	}
	if err := store.WriteAtomic(filepath.Join(directory, "input-diagnostics.json"), []byte(`{"schemaVersion":1,"count":1000}`)); err != nil {
		t.Fatal(err)
	}
	invoke([]string{"hook", "--agent", "codex", "--data-directory", directory}, strings.NewReader("{"))
	b, _ := store.ReadPrivate(filepath.Join(directory, "input-diagnostics.json"), 1000)
	if string(b) != `{"schemaVersion":1,"count":1000}` {
		t.Fatal("unbounded input error count")
	}
	for _, tc := range []struct{ agent, input, want string }{{"cursor", `{"hook_event_name":"beforeSubmitPrompt"}`, "{\"continue\":true}\n"}, {"claude-code", "{", "{}\n"}, {"workbuddy", "{", "{\"continue\":true}\n"}} {
		code, out, errors := invoke([]string{"hook", "--agent", tc.agent, "--data-directory", directory}, strings.NewReader(tc.input))
		if code != 0 || out != tc.want || errors != "" {
			t.Fatal("wrong host neutral response")
		}
	}
	code, out, errors = invoke([]string{"hook", "--agent", "codex", "--data-directory", "relative", "--unknown", "planted-secret"}, strings.NewReader("{"))
	if code != 0 || out != "{\"continue\":true}\n" || errors != "" {
		t.Fatal("invalid hook arguments blocked host")
	}
}

func TestConfigureInvalidPatchAndNonterminalAreReadOnly(t *testing.T) {
	directory := cliDirectory(t)
	for _, patch := range []string{`{"endpoint":"planted-secret"}`, `{"minSeconds":0}`, `{"provider":"ntfy"}`, `{"continuous":null}`, strings.Repeat("x", (4<<20)+1), string([]byte{255})} {
		path := filepath.Join(filepath.Dir(directory), "settings.json")
		if err := os.WriteFile(path, []byte(patch), 0600); err != nil {
			t.Fatal(err)
		}
		code, out, errors := invoke([]string{"configure", "--provider", "bark", "--settings-file", path, "--credential-stdin", "--data-directory", directory}, forbiddenInput{})
		if code != 2 || out != "" || strings.Contains(errors, "planted-secret") {
			t.Fatal("invalid patch accepted/read credentials")
		}
		if _, err := os.Lstat(directory); !os.IsNotExist(err) {
			t.Fatal("invalid patch wrote state")
		}
	}
	code, out, _ := invoke([]string{"configure", "--provider", "bark", "--data-directory", directory}, forbiddenInput{})
	if code != 2 || out != "" {
		t.Fatal("nonterminal input not refused")
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("nonterminal wrote state")
	}
}

func TestHookReadsAtMostLimitPlusOneAndAcceptsExactLimit(t *testing.T) {
	directory := cliDirectory(t)
	exact := `{"x":"` + strings.Repeat("a", (4<<20)-8) + `"}`
	code, out, errors := invoke([]string{"hook", "--agent", "codex", "--data-directory", directory}, strings.NewReader(exact))
	if len(exact) != (4<<20) || code != 0 || out != "{\"continue\":true}\n" || errors != "" {
		t.Fatal("exact byte limit refused")
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("valid ignored input created diagnostic")
	}
	input := strings.NewReader(strings.Repeat("x", 8<<20))
	invoke([]string{"hook", "--agent", "codex", "--data-directory", directory}, input)
	if input.Len() != (4<<20)-1 {
		t.Fatal("hook consumed beyond 4MiB+1")
	}
}
