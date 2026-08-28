package install

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/hostfile"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/secrets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

type fixture struct {
	root    string
	repo    *configuration.Repository
	options Options
}

func privateDirectory(t *testing.T, path string) string {
	t.Helper()
	if err := store.EnsurePrivateDirectory(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := store.WriteAtomic(path, data); err != nil {
		t.Fatal(err)
	}
}

// All parents/files are newly created synthetic private objects. This also
// explicitly selects TokenUser ownership on Windows with a group TokenOwner.
func setup(t *testing.T, agent string, original []byte) fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = privateDirectory(t, filepath.Join(root, "owned"))
	pkg := privateDirectory(t, filepath.Join(root, "package"))
	exe := filepath.Join(pkg, "agent-task-notify")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	put(t, exe, []byte("synthetic-not-executed"))
	if runtime.GOOS != "windows" {
		if err := os.Chmod(exe, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "hooks.json")
	if agent == "opencode" {
		target = filepath.Join(root, "opencode.json")
		privateDirectory(t, filepath.Join(root, "plugins"))
		integrations := privateDirectory(t, filepath.Join(pkg, "integrations"))
		bridgeDir := privateDirectory(t, filepath.Join(integrations, "opencode"))
		put(t, filepath.Join(bridgeDir, "bridge.mjs"), []byte("export function createAgentTaskNotify() {}"))
	}
	if original != nil {
		// Private-store files deliberately have inheritable Windows ACEs.
		// A host fixture must instead use the host-file creation policy, whose
		// regular-file metadata supports exact preservation on replacement.
		before, err := hostfile.Read(target, 4<<20)
		if err != nil || before.Exists {
			t.Fatal("new synthetic host fixture unavailable", err)
		}
		if err := hostfile.Replace(target, before, original); err != nil {
			t.Fatal("create synthetic host fixture", err)
		}
	}
	r, err := configuration.Open(filepath.Join(root, "state"), pkg)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{root, r, Options{AgentID: agent, ConfigPath: target, Executable: exe, PackageRoot: pkg, CommandShell: "powershell"}}
}

func TestPlanningWithoutShellDoesNotMutateHost(t *testing.T) {
	original := []byte(`{"version":1,"hooks":{"stop":[{"command":"echo synthetic-external"}]}}`)
	f := setup(t, "cursor", original)
	f.options.CommandShell = ""
	p, err := PlanInstall(context.Background(), f.repo, f.options)
	if !errors.Is(err, ErrShellRequired) || p.TargetPath != f.options.ConfigPath {
		t.Fatal("implicit unverified shell")
	}
	got, _ := os.ReadFile(f.options.ConfigPath)
	if !bytes.Equal(got, original) {
		t.Fatal("plan changed host")
	}
	if _, err := os.Lstat(f.repo.Directory()); !os.IsNotExist(err) {
		t.Fatal("plan created private state")
	}
	if ApplyInstall(context.Background(), f.repo, p) == nil {
		t.Fatal("incomplete plan applied")
	}
}

func TestPlanningAllRegistriesAndMissingParents(t *testing.T) {
	for _, agent := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			f := setup(t, agent, []byte(`{"keep":1e400,"hooks":{}}`))
			if agent == "opencode" {
				f.options.CommandShell = ""
			}
			p, err := PlanInstall(context.Background(), f.repo, f.options)
			if err != nil || p.AgentID != agent || p.Action != "install" {
				t.Fatal("valid preview", err)
			}
			want := f.options.ConfigPath
			if agent == "opencode" {
				want = filepath.Join(f.root, "plugins", "agent-task-notify.js")
			}
			if p.TargetPath != want || p.Experimental != (runtime.GOOS == "darwin") {
				t.Fatal("wrong target/experimental status")
			}
			f.options.ConfigPath = filepath.Join(f.root, "missing", "config.json")
			p, err = PlanInstall(context.Background(), f.repo, f.options)
			if !errors.Is(err, ErrParentRequired) || p.TargetPath == "" {
				t.Fatal("missing parent not reported", err)
			}
			if ApplyInstall(context.Background(), f.repo, p) == nil {
				t.Fatal("display plan authorized mutation")
			}
			if _, err := os.Lstat(filepath.Join(f.root, "missing")); !os.IsNotExist(err) {
				t.Fatal("created host directory")
			}
		})
	}
}

func TestMalformedAndUnsafePlanningIsReadOnly(t *testing.T) {
	for _, data := range []string{"", " ", "null", "[]", `{"x":1,"x":2}`, `{"hooks":null}`, `{"hooks":{"stop":null}}`, `{"version":2}`, `{"version":1.0}`, `{"version":"1"}`} {
		f := setup(t, "cursor", []byte(data))
		if _, err := PlanInstall(context.Background(), f.repo, f.options); err == nil {
			t.Fatalf("accepted malformed config %q", data)
		}
		got, _ := os.ReadFile(f.options.ConfigPath)
		if string(got) != data {
			t.Fatal("rejected plan changed data")
		}
	}
	f := setup(t, "cursor", nil)
	for _, mutate := range []func(*Options){
		func(o *Options) { o.AgentID = "unknown" }, func(o *Options) { o.Executable = "relative" },
		func(o *Options) { o.Executable = o.PackageRoot }, func(o *Options) { o.Executable = filepath.Join(f.root, "outside.exe") },
		func(o *Options) { o.ConfigPath = "relative" }, func(o *Options) { o.ConfigPath = o.Executable },
		func(o *Options) { o.CommandShell = "guessed" },
	} {
		o := f.options
		mutate(&o)
		if _, err := PlanInstall(context.Background(), f.repo, o); err == nil {
			t.Fatal("unsafe options accepted")
		}
	}
	f.options.AgentID = "workbuddy"
	if _, err := PlanInstall(context.Background(), f.repo, f.options); !errors.Is(err, ErrManualPackageRequired) {
		t.Fatal("workbuddy not manual")
	}
	if _, err := PlanUninstall(context.Background(), f.repo, "workbuddy"); !errors.Is(err, ErrManualPackageRequired) {
		t.Fatal("workbuddy automatic uninstall")
	}
}

func TestLegacyDirectEncodedAndCodexTOMLAreRefused(t *testing.T) {
	legacy := "& 'synthetic/agent-task-notify.ps1' -Mode Hook"
	units := utf16.Encode([]rune(legacy))
	raw := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	commands := []string{"CodexLongTaskNotify.ps1", legacy}
	for _, flag := range []string{"EncodedCommand", "e", "ec", "en", "enco", "ENCODEDC"} {
		commands = append(commands, "pwsh -"+flag+" "+base64.StdEncoding.EncodeToString(raw))
	}
	for _, command := range commands {
		entry, _ := json.Marshal(command)
		f := setup(t, "cursor", []byte(`{"hooks":{"external":[{"command":`+string(entry)+`}]}}`))
		if _, err := PlanInstall(context.Background(), f.repo, f.options); !errors.Is(err, ErrConflict) {
			t.Fatal("legacy accepted", err)
		}
	}
	f := setup(t, "codex", []byte(`{"notify":["existing"]}`))
	path := filepath.Join(f.root, "config.toml")
	original := []byte(`notify = ["pwsh", "CodexLongTaskNotify.ps1"]`)
	put(t, path, original)
	if _, err := PlanInstall(context.Background(), f.repo, f.options); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy TOML accepted")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatal("TOML changed")
	}
}

func TestDefaultPathsStayWithinSyntheticHome(t *testing.T) {
	for agent, relative := range map[string]string{"codex": ".codex/hooks.json", "claude-code": ".claude/settings.json", "cursor": ".cursor/hooks.json", "gemini-cli": ".gemini/settings.json", "opencode": ".config/opencode/opencode.json"} {
		f := setup(t, agent, nil)
		if runtime.GOOS == "windows" {
			t.Setenv("USERPROFILE", f.root)
		} else {
			t.Setenv("HOME", f.root)
		}
		t.Setenv("XDG_CONFIG_HOME", "")
		f.options.ConfigPath = ""
		p, err := PlanInstall(context.Background(), f.repo, f.options)
		want := filepath.Join(f.root, filepath.FromSlash(relative))
		if agent == "opencode" {
			want = filepath.Join(filepath.Dir(want), "plugins", "agent-task-notify.js")
		}
		if !errors.Is(err, ErrParentRequired) || p.TargetPath != want {
			t.Fatal("wrong user path", err)
		}
		if agent == "opencode" {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(f.root, "xdg"))
			p, err = PlanInstall(context.Background(), f.repo, f.options)
			if !errors.Is(err, ErrParentRequired) || p.TargetPath != filepath.Join(f.root, "xdg", "opencode", "plugins", "agent-task-notify.js") {
				t.Fatal("XDG ignored")
			}
		}
	}
}

func access(t *testing.T, path string) string {
	t.Helper()
	s, err := hostfile.Read(path, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.AccessDigest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func requireProtection(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	if os.Getenv("CI") != "true" {
		t.Skip("disposable CI Keychain required")
	}
	f, err := filepath.EvalSymlinks(os.Getenv("ATN_TEST_KEYCHAIN"))
	r, rerr := filepath.EvalSymlinks(os.Getenv("RUNNER_TEMP"))
	if err != nil || rerr != nil || !filepath.IsAbs(r) {
		t.Fatal("disposable CI fixture missing")
	}
	rel, err := filepath.Rel(r, f)
	dir := filepath.Dir(rel)
	if err != nil || filepath.Base(f) != "synthetic.keychain-db" || filepath.Base(dir) != dir || !strings.HasPrefix(dir, "atn-keychain.") {
		t.Fatal("unsafe CI fixture")
	}
}

func installFixture(t *testing.T, f fixture) Plan {
	t.Helper()
	p, err := PlanInstall(context.Background(), f.repo, f.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyInstall(context.Background(), f.repo, p); err != nil {
		t.Fatal("apply install", err)
	}
	return p
}

func readRecordForTest(t *testing.T, f fixture) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.repo.Directory(), "receipts", f.options.AgentID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		t.Fatal("invalid receipt JSON")
	}
	return v
}

func assertOriginalBackup(t *testing.T, f fixture, original []byte, existed bool) string {
	t.Helper()
	record := readRecordForTest(t, f)
	r := record["receipt"].(map[string]any)
	backup := r["backup"].(map[string]any)
	if backup["existed"] != existed {
		t.Fatal("backup existence lost")
	}
	path := backup["path"].(string)
	envelope, err := store.ReadPrivate(path, 6<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) > 0 && bytes.Contains(envelope, original) {
		t.Fatal("plaintext backup")
	}
	v, err := f.repo.Vault(context.Background(), secrets.Background)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := v.Unprotect("backup:"+f.options.AgentID, envelope)
	if err != nil || !bytes.Equal(plain, original) {
		t.Fatal("original bytes not recovered")
	}
	return path
}

func TestInstallReinstallUninstallPreservesExternalStateAndBackup(t *testing.T) {
	requireProtection(t)
	for _, agent := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			original := []byte("{\r\n \"unknown\": 1e400, \"precise\": 9007199254740993123456789, \"notify\": [\"external\"], \"hooks\": {\"unrelated\": [{\"command\":\"echo keep\"}]}\r\n}\r\n")
			f := setup(t, agent, original)
			if err := f.repo.Configure(context.Background(), "ntfy", providers.Credential{Endpoint: "https://example.invalid/synthetic", Token: "synthetic"}, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(f.repo.Directory(), "configuration.json")
			credentials, _ := os.ReadFile(configPath)
			parentAccess := os.FileMode(0)
			if info, err := os.Stat(f.root); err == nil {
				parentAccess = info.Mode()
			}
			var expectedAccess []string
			if agent != "opencode" {
				snapshot, err := hostfile.Read(f.options.ConfigPath, 4<<20)
				if err != nil {
					t.Fatal(err)
				}
				expectedAccess, err = snapshot.ExpectedAccessDigests(true)
				if err != nil {
					t.Fatal("positive fixture access unsupported", err)
				}
			}
			p := installFixture(t, f)
			initialAccess := access(t, p.TargetPath)
			if agent != "opencode" && !slices.Contains(expectedAccess, initialAccess) {
				t.Fatal("installation changed original access policy")
			}
			installed, err := os.ReadFile(p.TargetPath)
			if err != nil {
				t.Fatal(err)
			}
			if agent != "opencode" && (!bytes.Contains(installed, []byte("1e400")) || !bytes.Contains(installed, []byte("9007199254740993123456789"))) {
				t.Fatal("unknown numeric lexeme lost")
			}
			backupOriginal := original
			existed := true
			if agent == "opencode" {
				backupOriginal = nil
				existed = false
			}
			backupPath := assertOriginalBackup(t, f, backupOriginal, existed)
			installFixture(t, f)
			after, _ := os.ReadFile(p.TargetPath)
			if !bytes.Equal(after, installed) {
				t.Fatal("reinstall duplicated or changed exact registration")
			}
			if agent != "opencode" {
				changed := bytes.Replace(after, []byte(`"unknown":`), []byte(`"later":"external edit","unknown":`), 1)
				if err := os.WriteFile(p.TargetPath, changed, 0600); err != nil {
					t.Fatal(err)
				}
				if access(t, p.TargetPath) != initialAccess {
					t.Fatal("content-only edit changed access")
				}
			} else {
				locator, _ := os.ReadFile(f.options.ConfigPath)
				if !bytes.Equal(locator, original) {
					t.Fatal("OpenCode locator mutated")
				}
			}
			up, err := PlanUninstall(context.Background(), f.repo, agent)
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplyUninstall(context.Background(), f.repo, up); err != nil {
				t.Fatal(err)
			}
			if agent == "opencode" {
				if _, err := os.Lstat(p.TargetPath); !os.IsNotExist(err) {
					t.Fatal("shim retained")
				}
			} else {
				after, _ = os.ReadFile(p.TargetPath)
				for _, want := range []string{`"later":"external edit"`, `1e400`, `9007199254740993123456789`, `"notify":["external"]`, `echo keep`} {
					if !bytes.Contains(after, []byte(want)) {
						t.Fatal("uninstall lost external field")
					}
				}
				if bytes.Contains(after, []byte("--data-directory")) {
					t.Fatal("owned command retained")
				}
				if access(t, p.TargetPath) != initialAccess {
					t.Fatal("host access metadata changed")
				}
			}
			if readRecordForTest(t, f)["state"] != "inactive" {
				t.Fatal("inactive receipt missing")
			}
			if _, err := os.Stat(backupPath); err != nil {
				t.Fatal("backup deleted")
			}
			got, _ := os.ReadFile(configPath)
			if !bytes.Equal(got, credentials) {
				t.Fatal("credentials changed")
			}
			if info, err := os.Stat(f.root); err != nil || info.Mode() != parentAccess {
				t.Fatal("parent permissions changed")
			}
			installFixture(t, f)
			if assertOriginalBackup(t, f, backupOriginal, existed) != backupPath {
				t.Fatal("original backup replaced on reactivation")
			}
		})
	}
}

func TestUnreceiptedEditedDuplicateAndMissingOwnershipRefused(t *testing.T) {
	requireProtection(t)
	for _, agent := range []string{"cursor", "opencode"} {
		for _, change := range []string{"unreceipted", "edited", "duplicate", "missing"} {
			t.Run(agent+"/"+change, func(t *testing.T) {
				f := setup(t, agent, nil)
				p := installFixture(t, f)
				before, _ := os.ReadFile(p.TargetPath)
				switch change {
				case "unreceipted":
					if err := os.Remove(filepath.Join(f.repo.Directory(), "receipts", agent+".json")); err != nil {
						t.Fatal(err)
					}
				case "edited":
					before = append(before, []byte(" ")...)
					if agent != "opencode" {
						before = bytes.Replace(before, []byte("--agent"), []byte("--edited"), 1)
					}
					if err := os.WriteFile(p.TargetPath, before, 0600); err != nil {
						t.Fatal(err)
					}
				case "duplicate":
					if agent == "opencode" {
						before = append(before, before...)
					} else {
						var object map[string]json.RawMessage
						json.Unmarshal(before, &object)
						var hooks map[string][]json.RawMessage
						json.Unmarshal(object["hooks"], &hooks)
						hooks["stop"] = append(hooks["stop"], hooks["stop"][0])
						object["hooks"], _ = json.Marshal(hooks)
						before, _ = json.Marshal(object)
					}
					if err := os.WriteFile(p.TargetPath, before, 0600); err != nil {
						t.Fatal(err)
					}
				case "missing":
					if err := os.Remove(p.TargetPath); err != nil {
						t.Fatal(err)
					}
					before = nil
				}
				if _, err := PlanInstall(context.Background(), f.repo, f.options); !errors.Is(err, ErrConflict) {
					t.Fatal("unsafe ownership reinstall", err)
				}
				if change != "unreceipted" {
					if _, err := PlanUninstall(context.Background(), f.repo, agent); !errors.Is(err, ErrConflict) {
						t.Fatal("unsafe ownership uninstall", err)
					}
				}
				after, _ := os.ReadFile(p.TargetPath)
				if !bytes.Equal(after, before) {
					t.Fatal("conflict mutated host")
				}
			})
		}
	}
}

func TestApplyRejectsForgedEditedStalePlansAndCancellation(t *testing.T) {
	requireProtection(t)
	f := setup(t, "cursor", []byte(`{}`))
	p, err := PlanInstall(context.Background(), f.repo, f.options)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []Plan{{AgentID: p.AgentID, TargetPath: p.TargetPath, Action: p.Action}, {AgentID: "codex", TargetPath: p.TargetPath, Action: p.Action, state: p.state}, {AgentID: p.AgentID, TargetPath: filepath.Join(f.root, "other.json"), Action: p.Action, state: p.state}} {
		if ApplyInstall(context.Background(), f.repo, q) == nil {
			t.Fatal("forged plan applied")
		}
	}
	if err := os.WriteFile(p.TargetPath, []byte(`{"external":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ApplyInstall(context.Background(), f.repo, p), ErrConflict) {
		t.Fatal("stale plan applied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ApplyInstall(ctx, f.repo, p) == nil {
		t.Fatal("cancelled apply")
	}
	got, _ := os.ReadFile(p.TargetPath)
	if string(got) != `{"external":true}` {
		t.Fatal("stale write")
	}
}

func TestInstallationLockWaitIsBounded(t *testing.T) {
	requireProtection(t)
	f := setup(t, "cursor", []byte(`{}`))
	if err := f.repo.Prepare(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Vault(context.Background(), secrets.Foreground); err != nil {
		t.Fatal(err)
	}
	p, err := PlanInstall(context.Background(), f.repo, f.options)
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.Acquire(context.Background(), filepath.Join(f.repo.Directory(), "locks", "install-cursor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	start := time.Now()
	err = ApplyInstall(context.Background(), f.repo, p)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("unbounded installation lock wait")
	}
	got, _ := os.ReadFile(p.TargetPath)
	if string(got) != `{}` {
		t.Fatal("lock failure activated")
	}
}

func TestPendingBeforeAndAfterCommitRecoverWithoutRestore(t *testing.T) {
	requireProtection(t)
	for _, agent := range []string{"cursor", "opencode"} {
		for _, stage := range []string{"before-host", "after-host", "commit-receipt"} {
			t.Run(agent+"/"+stage, func(t *testing.T) {
				f := setup(t, agent, []byte(`{"external":"keep"}`))
				p, err := PlanInstall(context.Background(), f.repo, f.options)
				if err != nil {
					t.Fatal(err)
				}
				ops := realOperations()
				writes := 0
				ops.write = func(path string, data []byte) error {
					if filepath.Dir(path) == filepath.Join(f.repo.Directory(), "receipts") {
						writes++
						if stage == "commit-receipt" && writes == 2 {
							return errors.New("synthetic private failure")
						}
					}
					return store.WriteAtomic(path, data)
				}
				ops.replace = func(path string, before hostfile.Snapshot, data []byte) error {
					if stage == "before-host" {
						return hostfile.ErrUnsafe
					}
					if err := hostfile.Replace(path, before, data); err != nil {
						return err
					}
					if stage == "after-host" {
						return hostfile.ErrVerification
					}
					return nil
				}
				if applyWith(context.Background(), f.repo, p, ops) == nil {
					t.Fatal("fault not surfaced")
				}
				if readRecordForTest(t, f)["state"] != "pending" {
					t.Fatal("pending discarded")
				}
				before, _ := os.ReadFile(p.TargetPath)
				up, err := PlanUninstall(context.Background(), f.repo, agent)
				if err != nil {
					t.Fatal("pending recovery preview", err)
				}
				after, _ := os.ReadFile(p.TargetPath)
				if !bytes.Equal(after, before) {
					t.Fatal("recovery preview wrote host")
				}
				if err := ApplyUninstall(context.Background(), f.repo, up); err != nil {
					t.Fatal("recover/uninstall", err)
				}
				if readRecordForTest(t, f)["state"] != "inactive" {
					t.Fatal("not committed inactive")
				}
				if agent == "cursor" {
					got, _ := os.ReadFile(p.TargetPath)
					if !bytes.Contains(got, []byte(`"external":"keep"`)) || bytes.Contains(got, []byte("--data-directory")) {
						t.Fatal("recovery lost external data")
					}
				} else if _, err := os.Lstat(p.TargetPath); !os.IsNotExist(err) {
					t.Fatal("uncommitted/owned shim remains")
				}
				installFixture(t, f)
			})
		}
	}
}

func TestPendingExternalBytesOrAccessAreConflicts(t *testing.T) {
	requireProtection(t)
	for _, stage := range []string{"before-host", "after-host"} {
		for _, change := range []string{"bytes", "access"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				f := setup(t, "cursor", []byte(`{}`))
				p, err := PlanInstall(context.Background(), f.repo, f.options)
				if err != nil {
					t.Fatal(err)
				}
				ops := realOperations()
				ops.replace = func(path string, before hostfile.Snapshot, data []byte) error {
					if stage == "after-host" {
						if err := hostfile.Replace(path, before, data); err != nil {
							return err
						}
					}
					return hostfile.ErrVerification
				}
				if applyWith(context.Background(), f.repo, p, ops) == nil {
					t.Fatal("fault not surfaced")
				}
				if change == "bytes" {
					if err := os.WriteFile(p.TargetPath, []byte(`{"foreign":true}`), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Chmod(p.TargetPath, 0400); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.Chmod(p.TargetPath, 0600) })
				}
				original, _ := os.ReadFile(p.TargetPath)
				rpath := filepath.Join(f.repo.Directory(), "receipts", "cursor.json")
				pending, _ := os.ReadFile(rpath)
				if _, err := PlanInstall(context.Background(), f.repo, f.options); !errors.Is(err, ErrConflict) {
					t.Fatal("pending mismatch installed", err)
				}
				if _, err := PlanUninstall(context.Background(), f.repo, "cursor"); !errors.Is(err, ErrConflict) {
					t.Fatal("pending mismatch uninstalled", err)
				}
				after, _ := os.ReadFile(p.TargetPath)
				r, _ := os.ReadFile(rpath)
				if !bytes.Equal(original, after) || !bytes.Equal(pending, r) {
					t.Fatal("conflict overwrote host/pending")
				}
			})
		}
	}
}

func TestBackupAndPendingFailuresPreventActivation(t *testing.T) {
	requireProtection(t)
	for _, stage := range []string{"backup", "pending", "external-before-host"} {
		f := setup(t, "cursor", []byte(`{"original":true}`))
		p, err := PlanInstall(context.Background(), f.repo, f.options)
		if err != nil {
			t.Fatal(err)
		}
		ops := realOperations()
		ops.write = func(path string, data []byte) error {
			if stage == "backup" && filepath.Dir(path) == filepath.Join(f.repo.Directory(), "backups") {
				return errors.New("synthetic protected write failure")
			}
			if filepath.Dir(path) == filepath.Join(f.repo.Directory(), "receipts") {
				if stage == "pending" {
					return errors.New("synthetic pending write failure")
				}
				if stage == "external-before-host" {
					if err := os.WriteFile(p.TargetPath, []byte(`{"external":true}`), 0600); err != nil {
						return err
					}
				}
			}
			return store.WriteAtomic(path, data)
		}
		if applyWith(context.Background(), f.repo, p, ops) == nil {
			t.Fatal("boundary failure ignored")
		}
		got, _ := os.ReadFile(p.TargetPath)
		want := `{"original":true}`
		if stage == "external-before-host" {
			want = `{"external":true}`
		}
		if string(got) != want {
			t.Fatal("failed prerequisite activated or restored host")
		}
	}
}

func TestExactEmptyBackupPrimitive(t *testing.T) {
	requireProtection(t)
	f := setup(t, "cursor", nil)
	if err := f.repo.Prepare(); err != nil {
		t.Fatal(err)
	}
	v, err := f.repo.Vault(context.Background(), secrets.Foreground)
	if err != nil {
		t.Fatal(err)
	}
	for _, exists := range []bool{true, false} {
		backup, err := protectBackup(context.Background(), f.repo, v, "cursor", exists, []byte{}, store.WriteAtomic)
		if err != nil || backup.Existed != exists {
			t.Fatal("empty backup failed", err)
		}
		data, err := store.ReadPrivate(backup.Path, 6<<20)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := v.Unprotect("backup:cursor", data)
		if err != nil || len(plain) != 0 {
			t.Fatal("empty exact bytes lost")
		}
	}
}

// These cases catch revalidation performed only once at lock acquisition: an
// observed external edit at a later write boundary must not be overwritten.
func TestExternalDependenciesRecheckedAtMutationBoundaries(t *testing.T) {
	requireProtection(t)
	for _, stage := range []string{"backup", "pending", "host"} {
		for _, changed := range []string{"receipt", "backup", "executable", "locator"} {
			t.Run(stage+"/"+changed, func(t *testing.T) {
				f := setup(t, "opencode", []byte(`{}`))
				p, err := PlanInstall(context.Background(), f.repo, f.options)
				if err != nil {
					t.Fatal(err)
				}
				var backupPath string
				mutate := func() {
					path := map[string]string{"receipt": receiptPath(f.repo, "opencode"), "backup": backupPath, "executable": f.options.Executable, "locator": f.options.ConfigPath}[changed]
					if _, err := os.Lstat(path); os.IsNotExist(err) {
						put(t, path, []byte(`{"external":true}`))
					} else if err := os.WriteFile(path, []byte(`{"external":true}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				ops := realOperations()
				ops.write = func(path string, data []byte) error {
					if err := store.WriteAtomic(path, data); err != nil {
						return err
					}
					if filepath.Dir(path) == filepath.Join(f.repo.Directory(), "backups") {
						backupPath = path
						if stage == "backup" {
							mutate()
						}
					} else if stage == "pending" {
						mutate()
					}
					return nil
				}
				ops.replace = func(path string, before hostfile.Snapshot, data []byte) error {
					if err := hostfile.Replace(path, before, data); err != nil {
						return err
					}
					if stage == "host" {
						mutate()
					}
					return nil
				}
				if applyWith(context.Background(), f.repo, p, ops) == nil {
					t.Fatal("external dependency change ignored")
				}
				_, err = os.Lstat(p.TargetPath)
				if stage != "host" && !os.IsNotExist(err) {
					t.Fatal("external dependency activated host")
				}
				if stage == "host" && err != nil {
					t.Fatal("committed host rolled back")
				}
				if changed == "receipt" {
					b, err := os.ReadFile(receiptPath(f.repo, "opencode"))
					if err != nil || string(b) != `{"external":true}` {
						t.Fatal("external receipt overwritten")
					}
				} else if stage == "host" && readRecordForTest(t, f)["state"] != "pending" {
					t.Fatal("uncertain activation committed")
				}
			})
		}
	}
}

func TestBackupChangedSincePreviewIsRefused(t *testing.T) {
	requireProtection(t)
	f := setup(t, "cursor", nil)
	installFixture(t, f)
	p, err := PlanUninstall(context.Background(), f.repo, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.repo.Vault(context.Background(), secrets.Background)
	if err != nil {
		t.Fatal(err)
	}
	// A different, valid ciphertext in the same installation is still an edit
	// to the exact original-backup reference, not permission to adopt it.
	b, err := v.Protect("backup:cursor", nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, p.state.desired.Backup.Path, b)
	before, _ := os.ReadFile(p.TargetPath)
	if ApplyUninstall(context.Background(), f.repo, p) == nil {
		t.Fatal("changed backup accepted")
	}
	after, _ := os.ReadFile(p.TargetPath)
	if !bytes.Equal(before, after) {
		t.Fatal("backup conflict changed host")
	}
}

func TestPendingUpgradeAndUninstallRetainExactOwnership(t *testing.T) {
	requireProtection(t)
	for _, agent := range []string{"cursor", "opencode"} {
		for _, action := range []string{"upgrade", "uninstall"} {
			for _, stage := range []string{"before-host", "after-host", "commit-receipt"} {
				t.Run(agent+"/"+action+"/"+stage, func(t *testing.T) {
					f := setup(t, agent, []byte(`{"external":"keep"}`))
					installed := installFixture(t, f)
					original := []byte(`{"external":"keep"}`)
					if agent == "opencode" {
						original = nil
					}
					backup := assertOriginalBackup(t, f, original, agent != "opencode")
					var p Plan
					var err error
					if action == "upgrade" {
						f.options.Executable = filepath.Join(f.options.PackageRoot, "updated-notifier")
						put(t, f.options.Executable, []byte("new-synthetic-not-executed"))
						p, err = PlanInstall(context.Background(), f.repo, f.options)
					} else {
						p, err = PlanUninstall(context.Background(), f.repo, agent)
					}
					if err != nil {
						t.Fatal(err)
					}
					ops := realOperations()
					writes := 0
					ops.write = func(path string, data []byte) error {
						writes++
						if stage == "commit-receipt" && writes == 2 {
							return ErrUnavailable
						}
						return store.WriteAtomic(path, data)
					}
					ops.replace = func(path string, before hostfile.Snapshot, data []byte) error {
						if stage == "before-host" {
							return hostfile.ErrUnsafe
						}
						if err := hostfile.Replace(path, before, data); err != nil {
							return err
						}
						return hostfile.ErrVerification
					}
					ops.remove = func(path string, before hostfile.Snapshot) error {
						if stage == "before-host" {
							return hostfile.ErrUnsafe
						}
						if err := hostfile.Remove(path, before); err != nil {
							return err
						}
						return hostfile.ErrVerification
					}
					if stage == "commit-receipt" {
						ops.replace = hostfile.Replace
						ops.remove = hostfile.Remove
					}
					if applyWith(context.Background(), f.repo, p, ops) == nil {
						t.Fatal("injected interruption ignored")
					}
					r := readRecordForTest(t, f)
					if r["state"] != "pending" || r["pending"].(map[string]any)["previous"].(map[string]any)["state"] != "active" {
						t.Fatal("previous committed ownership lost")
					}
					up, err := PlanUninstall(context.Background(), f.repo, agent)
					if err != nil {
						t.Fatal(err)
					}
					if err := ApplyUninstall(context.Background(), f.repo, up); err != nil {
						t.Fatal(err)
					}
					if assertOriginalBackup(t, f, original, agent != "opencode") != backup {
						t.Fatal("upgrade replaced original backup")
					}
					if agent == "opencode" {
						if _, err := os.Lstat(installed.TargetPath); !os.IsNotExist(err) {
							t.Fatal("owned shim retained")
						}
					} else {
						b, _ := os.ReadFile(installed.TargetPath)
						if !bytes.Contains(b, []byte(`"external":"keep"`)) || bytes.Contains(b, []byte("--data-directory")) {
							t.Fatal("exact uninstall failed")
						}
					}
				})
			}
		}
	}
}

func TestCancellationAtEachMutationBoundary(t *testing.T) {
	requireProtection(t)
	for _, stage := range []string{"backup", "pending", "host"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "cursor", nil)
			p, err := PlanInstall(context.Background(), f.repo, f.options)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ops := realOperations()
			ops.write = func(path string, data []byte) error {
				if ctx.Err() != nil {
					t.Fatal("write after cancellation")
				}
				if err := store.WriteAtomic(path, data); err != nil {
					return err
				}
				if stage == "backup" && filepath.Dir(path) == filepath.Join(f.repo.Directory(), "backups") || stage == "pending" && path == receiptPath(f.repo, "cursor") {
					cancel()
				}
				return nil
			}
			ops.replace = func(path string, before hostfile.Snapshot, data []byte) error {
				if ctx.Err() != nil {
					t.Fatal("host write after cancellation")
				}
				if err := hostfile.Replace(path, before, data); err != nil {
					return err
				}
				if stage == "host" {
					cancel()
				}
				return nil
			}
			if applyWith(ctx, f.repo, p, ops) == nil {
				t.Fatal("cancelled apply committed")
			}
			_, err = os.Lstat(p.TargetPath)
			if stage == "host" && err != nil || stage != "host" && !os.IsNotExist(err) {
				t.Fatal("wrong host cancellation state")
			}
			if stage != "backup" && readRecordForTest(t, f)["state"] != "pending" {
				t.Fatal("cancelled pending discarded")
			}
			p, err = PlanInstall(context.Background(), f.repo, f.options)
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplyInstall(context.Background(), f.repo, p); err != nil {
				t.Fatal("lock not released or recovery failed", err)
			}
		})
	}
}

func TestReceiptRejectsMalformedOwnershipAndTransitions(t *testing.T) {
	requireProtection(t)
	f := setup(t, "cursor", nil)
	installFixture(t, f)
	committed, _ := os.ReadFile(receiptPath(f.repo, "cursor"))
	p, err := PlanUninstall(context.Background(), f.repo, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	ops := realOperations()
	ops.replace = func(string, hostfile.Snapshot, []byte) error { return hostfile.ErrUnsafe }
	if applyWith(context.Background(), f.repo, p, ops) == nil {
		t.Fatal("expected pending")
	}
	pending, _ := os.ReadFile(receiptPath(f.repo, "cursor"))
	for _, tc := range []struct {
		name   string
		raw    []byte
		mutate func(map[string]any)
	}{
		{"unknown", committed, func(r map[string]any) { r["extra"] = true }},
		{"schema", committed, func(r map[string]any) { r["schemaVersion"] = 2 }},
		{"missing", committed, func(r map[string]any) { delete(r, "pending") }},
		{"agent", committed, func(r map[string]any) { r["agentId"] = "unknown" }},
		{"arbitrary-entry", committed, func(r map[string]any) {
			r["receipt"].(map[string]any)["entries"].([]any)[0].(map[string]any)["value"] = map[string]any{"command": "foreign"}
		}},
		{"backup-escape", committed, func(r map[string]any) {
			r["receipt"].(map[string]any)["backup"].(map[string]any)["path"] = f.options.Executable
		}},
		{"relative-config", committed, func(r map[string]any) { r["receipt"].(map[string]any)["configPath"] = "relative" }},
		{"nested-transition", pending, func(r map[string]any) {
			r["pending"].(map[string]any)["previous"].(map[string]any)["pending"] = map[string]any{}
		}},
		{"zero-access", pending, func(r map[string]any) { r["pending"].(map[string]any)["afterAccessDigests"] = []string{} }},
		{"many-access", pending, func(r map[string]any) {
			r["pending"].(map[string]any)["afterAccessDigests"] = []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
		}},
		{"duplicate-access", pending, func(r map[string]any) {
			r["pending"].(map[string]any)["afterAccessDigests"] = []string{strings.Repeat("a", 64), strings.Repeat("a", 64)}
		}},
		{"upper-access", pending, func(r map[string]any) { r["pending"].(map[string]any)["beforeAccessDigest"] = strings.Repeat("A", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var object map[string]any
			if json.Unmarshal(tc.raw, &object) != nil {
				t.Fatal("fixture JSON")
			}
			tc.mutate(object)
			raw, _ := json.Marshal(object)
			put(t, receiptPath(f.repo, "cursor"), raw)
			before, _ := os.ReadFile(p.TargetPath)
			if _, err := PlanUninstall(context.Background(), f.repo, "cursor"); err == nil {
				t.Fatal("malformed ownership accepted")
			}
			after, _ := os.ReadFile(p.TargetPath)
			if !bytes.Equal(before, after) {
				t.Fatal("malformed receipt changed host")
			}
		})
	}
}

func TestLargeProtectedBackupUsesEnvelopeLimitAndExactBytes(t *testing.T) {
	requireProtection(t)
	original := []byte(`{"large":"` + strings.Repeat("x", 3<<20) + `"}`)
	f := setup(t, "cursor", original)
	installFixture(t, f)
	path := assertOriginalBackup(t, f, original, true)
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 4<<20 {
		t.Fatal("fixture did not exercise larger envelope boundary", err)
	}
	p, err := PlanUninstall(context.Background(), f.repo, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyUninstall(context.Background(), f.repo, p); err != nil {
		t.Fatal(err)
	}
	oversized := setup(t, "cursor", []byte(`{"large":"`+strings.Repeat("x", 4<<20)+`"}`))
	if _, err := PlanInstall(context.Background(), oversized.repo, oversized.options); err == nil {
		t.Fatal("oversized host accepted")
	}
}

func TestExistingEmptyOpenCodeShimIsUnowned(t *testing.T) {
	f := setup(t, "opencode", nil)
	path := filepath.Join(f.root, "plugins", "agent-task-notify.js")
	snapshot, err := hostfile.Read(path, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := hostfile.Replace(path, snapshot, []byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanInstall(context.Background(), f.repo, f.options); !errors.Is(err, ErrConflict) {
		t.Fatal("empty unowned shim overwritten", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatal("empty shim mutated")
	}
}

func TestReceiptCannotResolveAnEmptyPathFromCurrentHome(t *testing.T) {
	requireProtection(t)
	f := setup(t, "cursor", nil)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", f.root)
	} else {
		t.Setenv("HOME", f.root)
	}
	f.options.ConfigPath = filepath.Join(privateDirectory(t, filepath.Join(f.root, ".cursor")), "hooks.json")
	installFixture(t, f)
	r := readRecordForTest(t, f)
	r["receipt"].(map[string]any)["configPath"] = ""
	raw, _ := json.Marshal(r)
	put(t, receiptPath(f.repo, "cursor"), raw)
	if _, err := PlanUninstall(context.Background(), f.repo, "cursor"); err == nil {
		t.Fatal("receipt accepted non-absolute locator")
	}
}
