package install

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if path := os.Getenv("ATN_INSTALL_EVENT_HELPER"); path != "" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
		if err != nil || len(data) > 4096 {
			os.Exit(2)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(2)
		}
		err = json.NewEncoder(file).Encode(struct {
			Args  []string        `json:"args"`
			Input json.RawMessage `json:"input"`
		}{os.Args[1:], data})
		if file.Close() != nil || err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if os.Getenv("ATN_INSTALL_ARGV_HELPER") == "1" {
		if json.NewEncoder(os.Stdout).Encode(os.Args[1:]) != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestGeneratedShimLoadsAndSpawnsNativeHelper(t *testing.T) {
	requireProtection(t)
	f := setup(t, "opencode", nil)
	bridge, err := os.ReadFile(filepath.Join("..", "..", "integrations", "opencode", "bridge.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(f.options.PackageRoot, "integrations", "opencode", "bridge.mjs"), bridge)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := privateDirectory(t, filepath.Join(f.options.PackageRoot, "native 中文's"))
	f.options.Executable = filepath.Join(dir, "argv helper")
	if runtime.GOOS == "windows" {
		f.options.Executable += ".exe"
	}
	put(t, f.options.Executable, program)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(f.options.Executable, 0700); err != nil {
			t.Fatal(err)
		}
	}
	p := installFixture(t, f)
	// Only the generated shim, packaged bridge, and synthetic argv/stdin
	// recorder execute. This is not a real Agent/plugin-host load claim.
	script := `const {pathToFileURL}=await import('node:url');
const module=await import(pathToFileURL(process.argv[1]));
if(Object.keys(module).length!==1)throw Error('unexpected exports');
const plugin=await module.default({client:{session:{async get(){return {data:{}}}}}});
await plugin.event({event:{type:'message.updated',properties:{info:{sessionID:'root',id:'u',role:'user',text:'PRIVATE'}}}});
await plugin.event({event:{type:'session.idle',properties:{sessionID:'root'}}});`
	path := filepath.Join(f.root, "events.jsonl")
	cmd := exec.Command("node", "--input-type=module", "-e", script, p.TargetPath)
	cmd.Env = append(os.Environ(), "ATN_INSTALL_EVENT_HELPER="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated shim failed: %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatal("native lifecycle count")
	}
	for i, event := range []string{"started", "stopped"} {
		var got struct {
			Args  []string       `json:"args"`
			Input map[string]any `json:"input"`
		}
		if json.Unmarshal([]byte(lines[i]), &got) != nil || !reflect.DeepEqual(got.Args, []string{"hook", "--agent", "opencode", "--data-directory", f.repo.Directory()}) ||
			!reflect.DeepEqual(got.Input, map[string]any{"schemaVersion": float64(1), "event": event, "sessionId": "root", "runId": "u"}) {
			t.Fatal("native shim argv/stdin mismatch")
		}
	}
	t.Log("generated ESM shim loaded; native helper received exact argv and started/stopped stdin")
}

func TestRenderersRejectUnsafeArguments(t *testing.T) {
	f := setup(t, "cursor", nil)
	for _, shell := range []string{"posix", "powershell", "cmd"} {
		for _, bad := range []string{"bad\nname", "bad\x00name", "bad\rname"} {
			if _, err := renderCommand(shell, f.options.Executable, "cursor", filepath.Join(f.root, bad)); err == nil {
				t.Fatal("control accepted")
			}
		}
	}
	for _, bad := range []string{`%PATH%`, `bang!`, `a"b`, `a&b`, `a|b`, `a^b`, `a(b)`, `a<b`, `a>b`, `a` + "`" + `b`} {
		if _, err := renderCommand("cmd", f.options.Executable, "cursor", filepath.Join(f.root, bad)); err == nil {
			t.Fatal("cmd expansion accepted")
		}
	}
}

func TestRenderedCommandsExecuteWithExactArguments(t *testing.T) {
	f := setup(t, "cursor", nil)
	shells := []string{"posix"}
	if runtime.GOOS == "windows" {
		shells = []string{"cmd", "powershell", "posix"}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			dir := privateDirectory(t, filepath.Join(f.root, shell+" package 中文's"))
			executable := filepath.Join(dir, "argv helper")
			if runtime.GOOS == "windows" {
				executable += ".exe"
			}
			put(t, executable, program)
			if runtime.GOOS != "windows" {
				if err := os.Chmod(executable, 0700); err != nil {
					t.Fatal(err)
				}
			}
			data := filepath.Join(f.root, "data 中文's")
			command, err := renderCommand(shell, executable, "cursor", data)
			if err != nil {
				t.Fatal(err)
			}
			var cmd *exec.Cmd
			switch shell {
			case "cmd":
				// UTF-8 batch content is interpreted by actual cmd. The fixed
				// launcher name needs no cmd-specific Go command-line escaping.
				batch := filepath.Join(f.root, "argv.cmd")
				put(t, batch, []byte("@echo off\r\nchcp 65001 >nul\r\n"+command+"\r\n"))
				cmd = exec.Command("cmd.exe", "/d", "/v:off", "/c", "argv.cmd")
				cmd.Dir = f.root
			case "powershell":
				cmd = exec.Command("pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
			case "posix":
				sh := "/bin/sh"
				if runtime.GOOS == "windows" {
					sh = os.Getenv("ATN_TEST_BASH")
					if sh == "" {
						sh, err = exec.LookPath("bash.exe")
						if err != nil {
							t.Fatal("Git Bash required for Windows shell gate")
						}
					}
				}
				cmd = exec.Command(sh, "-c", command)
			}
			cmd.Env = append(os.Environ(), "ATN_INSTALL_ARGV_HELPER=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("actual shell failed: %v %s", err, out)
			}
			var args []string
			if json.Unmarshal(out, &args) != nil || !reflect.DeepEqual(args, []string{"hook", "--agent", "cursor", "--data-directory", data}) {
				t.Fatalf("actual argv mismatch: %s", out)
			}
			t.Logf("actual interpreter=%s argv=%s", cmd.Path, strings.TrimSpace(string(out)))
		})
	}
}
