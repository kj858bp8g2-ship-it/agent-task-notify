package configuration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/secrets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("fixture root unavailable")
	}
	return root
}

func repositoryFixture(t *testing.T) *Repository {
	t.Helper()
	root := fixtureRoot(t)
	pkg := filepath.Join(root, "package")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "data"), pkg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestViewIsSyntacticAndReadOnly(t *testing.T) {
	r := repositoryFixture(t)
	s, configured, err := r.View()
	if err != nil || configured || s.MinSeconds != 1800 {
		t.Fatal("missing bundle view")
	}
	if _, err := os.Lstat(r.Directory()); !os.IsNotExist(err) {
		t.Fatal("view created state")
	}
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	if err := store.WriteAtomic(filepath.Join(r.Directory(), "installation.json"), []byte(`{"schemaVersion":1,"installationId":"`+id+`"}`)); err != nil {
		t.Fatal(err)
	}
	// Syntactically valid, intentionally unauthentic. No foreground key exists.
	state := bundle{1, s, map[string]json.RawMessage{"bark": json.RawMessage(`{"schemaVersion":1,"backend":"dpapi","installationId":"` + id + `","purpose":"credential:bark","ciphertext":"AQ=="}`)}}
	encoded, _ := json.Marshal(state)
	path := filepath.Join(r.Directory(), "configuration.json")
	if err := store.WriteAtomic(path, encoded); err != nil {
		t.Fatal(err)
	}
	s, configured, err = r.View()
	if err != nil || !configured || s.MinSeconds != 1800 {
		t.Fatal("view authenticated envelope")
	}
	s.Icons["codex"] = "changed"
	again, _, err := r.View()
	if err != nil || again.Icons["codex"] == "changed" {
		t.Fatal("view shares mutable settings")
	}
	t.Run("authenticated-methods-unchanged", func(t *testing.T) {
		requireSyntheticProtection(t)
		if _, err := r.Settings(); err == nil {
			t.Fatal("authenticated Settings semantics changed")
		}
		if _, err := r.Credential("bark", secrets.Background); err == nil {
			t.Fatal("authenticated Credential semantics changed")
		}
	})
	if after, _ := os.ReadFile(path); !bytes.Equal(after, encoded) {
		t.Fatal("read mutated bundle")
	}
	for _, bad := range []string{`{}`, strings.Replace(string(encoded), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(encoded), `"minSeconds":1800,`, "", 1), strings.Replace(string(encoded), `"ciphertext":"AQ=="`, `"ciphertext":null`, 1)} {
		if err := store.WriteAtomic(path, []byte(bad)); err != nil {
			t.Fatal(err)
		}
		if _, configured, err := r.View(); err == nil || configured {
			t.Fatal("invalid bundle accepted")
		}
	}
}

// Any test that can create a macOS account must be inside the disposable CI
// fixture. The CI script deletes the whole generated Keychain after the suite.
func requireSyntheticProtection(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	if os.Getenv("CI") != "true" {
		t.Skip("disposable CI Keychain required")
	}
	fixture, err := filepath.EvalSymlinks(os.Getenv("ATN_TEST_KEYCHAIN"))
	root, rootErr := filepath.EvalSymlinks(os.Getenv("RUNNER_TEMP"))
	if err != nil || rootErr != nil || !filepath.IsAbs(root) {
		t.Fatal("disposable CI fixture missing")
	}
	rel, err := filepath.Rel(root, fixture)
	dir := filepath.Dir(rel)
	if err != nil || filepath.Base(fixture) != "synthetic.keychain-db" || filepath.Base(dir) != dir || !strings.HasPrefix(dir, "atn-keychain.") {
		t.Fatal("unsafe CI fixture")
	}
}

func TestOpenPrepareAndDefaultsAreIndependent(t *testing.T) {
	r := repositoryFixture(t)
	if _, err := os.Lstat(r.Directory()); !os.IsNotExist(err) {
		t.Fatal("Open created state")
	}
	a, err := r.Settings()
	if err != nil || a.MinSeconds != 1800 || a.LongTaskSeconds != 3600 {
		t.Fatal("missing defaults")
	}
	a.Icons["codex"] = "changed"
	b, err := r.Settings()
	if err != nil || b.Icons["codex"] == "changed" {
		t.Fatal("defaults share mutable state")
	}
	if _, err := r.Vault(context.Background(), secrets.Background); err == nil {
		t.Fatal("background created identity")
	}
	if _, err := os.Lstat(r.Directory()); !os.IsNotExist(err) {
		t.Fatal("reads created state")
	}
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.Directory())
	if err != nil || len(entries) != 6 {
		t.Fatal("wrong prepared layout")
	}
	for _, name := range []string{"sessions", "runs", "jobs", "locks", "receipts", "backups"} {
		if err := store.CheckPrivateDirectory(filepath.Join(r.Directory(), name)); err != nil {
			t.Fatal("nonprivate prepared leaf")
		}
	}
}

func TestDirectoryIsolationAndNoMutation(t *testing.T) {
	root := fixtureRoot(t)
	pkg := filepath.Join(root, "package")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	for _, path := range []string{pkg, filepath.Join(pkg, "data"), home, filepath.VolumeName(root) + string(filepath.Separator), "relative", filepath.Join(root, "x") + string(filepath.Separator) + "..", filepath.Join(root, "bad\nname"), filepath.Join(root, "AgentTaskNotify"), filepath.Join(home, ".codex", "long-task-notify", "child"), filepath.Dir(exe)} {
		if _, err := Open(path, pkg); err == nil {
			t.Fatal("unsafe directory accepted")
		}
	}
	for _, kind := range []string{"git-file", "git-directory", "zip", "config-link"} {
		t.Run(kind, func(t *testing.T) {
			source := filepath.Join(root, kind)
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "git-file":
				if err := os.WriteFile(filepath.Join(source, ".git"), []byte("gitdir: synthetic"), 0600); err != nil {
					t.Fatal(err)
				}
			case "git-directory":
				if err := os.Mkdir(filepath.Join(source, ".git"), 0700); err != nil {
					t.Fatal(err)
				}
			case "zip", "config-link":
				if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("marker contents must not matter"), 0600); err != nil {
					t.Fatal(err)
				}
				config := filepath.Join(source, "config")
				if kind == "config-link" {
					if err := os.Symlink(pkg, config); err != nil {
						if runtime.GOOS == "windows" {
							t.Skip("symlink privilege unavailable")
						}
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(config, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(config, "native-source-files.json"), []byte("marker"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			candidate := filepath.Join(source, "data")
			if _, err := Open(candidate, pkg); err == nil {
				t.Fatal("source marker accepted")
			}
			if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
				t.Fatal("rejected source mutated")
			}
		})
	}
	// Neither arbitrary CWD nor its name is a package/source authority.
	independent := filepath.Join(root, "independent")
	if err := os.Mkdir(independent, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(independent)
	if _, err := Open(filepath.Join(independent, "data"), pkg); err != nil {
		t.Fatal("unrelated CWD rejected")
	}
}

func TestDirectoryPrecedenceAndDefaultBase(t *testing.T) {
	root := fixtureRoot(t)
	pkg := filepath.Join(root, "package")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATN_DATA_DIRECTORY", filepath.Join(root, "from-env"))
	r, err := Open(filepath.Join(root, "explicit"), pkg)
	if err != nil || r.Directory() != filepath.Join(root, "explicit") {
		t.Fatal("explicit precedence")
	}
	r, err = Open("", pkg)
	if err != nil || r.Directory() != filepath.Join(root, "from-env") {
		t.Fatal("environment precedence")
	}
	t.Setenv("ATN_DATA_DIRECTORY", "")
	base := filepath.Join(root, "local")
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", base)
	} else {
		t.Setenv("HOME", root)
		base = filepath.Join(root, "Library", "Application Support")
	}
	if _, err := Open("", pkg); err == nil {
		t.Fatal("missing default base accepted")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	r, err = Open("", pkg)
	if err != nil || r.Directory() != filepath.Join(base, "AgentTaskNotifyNative") {
		t.Fatal("wrong default")
	}
	if _, err := os.Lstat(r.Directory()); !os.IsNotExist(err) {
		t.Fatal("default resolution wrote state")
	}
}

func TestOpenRejectsPackageRootLinkedAncestor(t *testing.T) {
	root := fixtureRoot(t)
	real := filepath.Join(root, "real")
	pkg := filepath.Join(real, "package")
	if err := os.MkdirAll(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "cmd.exe", "/d", "/c", "mklink", "/J", alias, real).Run(); err != nil {
			t.Fatal("test-owned junction creation failed")
		}
	} else if err := os.Symlink(real, alias); err != nil {
		t.Fatal("test-owned symlink creation failed")
	}
	t.Cleanup(func() {
		if err := os.Remove(alias); err != nil {
			t.Error("test-owned link cleanup failed")
		}
	})
	candidate := filepath.Join(pkg, "data")
	if _, err := Open(candidate, filepath.Join(alias, "package")); err == nil {
		t.Fatal("linked package ancestor bypassed isolation")
	}
	if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
		t.Fatal("package isolation check wrote state")
	}
	// Reject a linked exclusion root even if this candidate is unrelated.
	if _, err := Open(filepath.Join(root, "outside"), filepath.Join(alias, "package")); err == nil {
		t.Fatal("linked exclusion root accepted")
	}
}

func TestWindowsPackageShortNameCannotBypassIsolation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows short-name fixture")
	}
	root := fixtureRoot(t)
	pkg := filepath.Join(root, "LongPackageDirectory")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	// Query only this test-owned path. No recursive enumeration, mutation,
	// short-name policy change or filesystem-wide 8.3 enablement is allowed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "cmd.exe", "/d", "/c", "for %I in (.) do @echo %~fsI")
	command.Dir = pkg
	out, err := command.Output()
	if err != nil {
		t.Fatal("short-name query failed")
	}
	short := strings.TrimSpace(string(out))
	if strings.EqualFold(short, pkg) {
		t.Skip("test volume does not provide a distinct 8.3 alias")
	}
	longInfo, err := os.Lstat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	shortInfo, err := os.Lstat(short)
	if err != nil || !os.SameFile(longInfo, shortInfo) {
		t.Fatal("short-name fixture does not identify the same directory")
	}
	for _, paths := range [][2]string{{filepath.Join(pkg, "data"), short}, {filepath.Join(short, "data"), pkg}} {
		if _, err := Open(paths[0], paths[1]); err == nil {
			t.Fatal("directory alias bypassed package isolation")
		}
		if _, err := os.Lstat(paths[0]); !os.IsNotExist(err) {
			t.Fatal("alias check created data")
		}
	}
}

var barkFixture = providers.Credential{Endpoint: "https://example.invalid/synthetic_bark_secret"}
var ntfyFixture = providers.Credential{Endpoint: "https://example.invalid/synthetic_ntfy_topic", Token: "synthetic_ntfy_secret"}

func save(t *testing.T, r *Repository, provider string, credential providers.Credential, patch string) {
	t.Helper()
	if err := r.Configure(context.Background(), provider, credential, []byte(patch)); err != nil {
		t.Fatal("synthetic save failed")
	}
}

func TestProtectedConfigurationRoundtripAndStableInstallation(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	save(t, r, "bark", barkFixture, `{"minSeconds":123}`)
	identity, err := os.ReadFile(filepath.Join(r.Directory(), "installation.json"))
	if err != nil {
		t.Fatal(err)
	}
	save(t, r, "ntfy", ntfyFixture, `{"volume":7}`)
	for provider, want := range map[string]providers.Credential{"bark": barkFixture, "ntfy": ntfyFixture} {
		got, err := r.Credential(provider, secrets.Background)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("credential roundtrip failed")
		}
	}
	settings, err := r.Settings()
	if err != nil || settings.MinSeconds != 123 || settings.Volume != 7 || settings.Provider != "ntfy" {
		t.Fatal("settings transaction lost fields")
	}
	other := repositoryFixture(t)
	if err := other.Prepare(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"installation.json", "configuration.json"} {
		data, err := os.ReadFile(filepath.Join(r.Directory(), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.WriteAtomic(filepath.Join(other.Directory(), name), data); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := other.Credential("bark", secrets.Background); err != nil || got != barkFixture {
		t.Fatal("staging path changed protection identity")
	}
	save(t, other, "bark", barkFixture, `{}`)
	if got, _ := os.ReadFile(filepath.Join(other.Directory(), "installation.json")); !bytes.Equal(got, identity) {
		t.Fatal("identity regenerated")
	}
	if err := filepath.WalkDir(r.Directory(), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, marker := range []string{"synthetic_bark_secret", "synthetic_ntfy_topic", "synthetic_ntfy_secret"} {
			if bytes.Contains(data, []byte(marker)) {
				t.Error("plaintext credential persisted")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedConfigurationPreservesBundle(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	save(t, r, "bark", barkFixture, `{}`)
	path := filepath.Join(r.Directory(), "configuration.json")
	before, _ := os.ReadFile(path)
	for _, patch := range []string{`null`, `{"minSeconds":0}`, `{"unknown":"secret"}`, `{"volume":1,"volume":2}`, `{"provider":"ntfy"}`} {
		if err := r.Configure(context.Background(), "bark", barkFixture, []byte(patch)); err == nil {
			t.Fatal("invalid patch accepted")
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
			t.Fatal("invalid patch changed bundle")
		}
	}
	// Block the real atomic rename, not a mocked writer or global failure hook.
	if runtime.GOOS == "windows" {
		reader, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
	} else {
		// Keep the old launch-path evidence separate from the real flag operation.
		_, legacyErr := os.Lstat("/bin/chflags")
		t.Logf("stage=immutable-legacy-command exists=%t missing=%t", legacyErr == nil, os.IsNotExist(legacyErr))
		defer func() {
			if category, code := immutableFixtureCommand(path, "nouchg"); category != "ok" {
				t.Errorf("stage=immutable-cleanup category=%s exit=%d", category, code)
			}
		}()
		if category, code := immutableFixtureCommand(path, "uchg"); category != "ok" {
			t.Fatalf("stage=immutable-setup category=%s exit=%d", category, code)
		}
	}
	if err := r.Configure(context.Background(), "ntfy", ntfyFixture, []byte(`{"volume":2}`)); err == nil {
		t.Fatal("blocked commit succeeded")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
		t.Fatal("failed commit split credential/settings")
	}
	if got, err := r.Credential("bark", secrets.Background); err != nil || got != barkFixture {
		t.Fatal("prior credential lost")
	}
	if _, err := r.Credential("ntfy", secrets.Background); err == nil {
		t.Fatal("failed provider became visible")
	}
	entries, _ := os.ReadDir(r.Directory())
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatal("failed commit leaked temporary")
		}
	}
}

// Only this synthetic fixture invokes chflags. Neither native error text nor
// subprocess output is logged, and cleanup has the same bounded lifetime.
func immutableFixtureCommand(path, flag string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "/usr/bin/chflags", flag, path).Run()
	switch {
	case ctx.Err() != nil:
		return "timeout", -1
	case err == nil:
		return "ok", 0
	case errors.Is(err, os.ErrNotExist):
		return "missing", -1
	case errors.Is(err, os.ErrPermission):
		return "access", -1
	default:
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return "exit", exited.ExitCode()
		}
		return "other", -1
	}
}

func TestMalformedIdentityAndBundleNeverReset(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(r.Directory(), "installation.json")
	for _, bad := range []string{`{}`, `{"schemaVersion":1,"installationId":"BAD"}`, `{"schemaVersion":1,"installationId":"00000000000000000000000000000000","extra":1}`} {
		if err := store.WriteAtomic(identityPath, []byte(bad)); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Vault(context.Background(), secrets.Foreground); err == nil {
			t.Fatal("bad identity replaced")
		}
		if after, _ := os.ReadFile(identityPath); string(after) != bad {
			t.Fatal("identity mutated")
		}
	}
	if err := store.RemovePrivate(identityPath); err != nil {
		t.Fatal(err)
	}
	save(t, r, "bark", barkFixture, `{}`)
	path := filepath.Join(r.Directory(), "configuration.json")
	valid, _ := os.ReadFile(path)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(valid, &object); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`null`, `{}`, `{"schemaVersion":2}`, strings.Replace(string(valid), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(valid), `"ciphertext":"`, `"ciphertext":"!`, 1), strings.Replace(string(valid), `"minSeconds":1800,`, "", 1)} {
		if err := store.WriteAtomic(path, []byte(bad)); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Settings(); err == nil {
			t.Fatal("malformed bundle silently reset")
		}
		if _, err := r.Credential("bark", secrets.Background); err == nil {
			t.Fatal("malformed credential accepted")
		}
		if err := r.Configure(context.Background(), "ntfy", ntfyFixture, []byte(`{}`)); err == nil {
			t.Fatal("malformed prior bundle overwritten")
		}
		if after, _ := os.ReadFile(path); string(after) != bad {
			t.Fatal("malformed bundle mutated")
		}
	}
}

func TestVaultAndConfigureLockingAreBounded(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	release, err := store.Acquire(context.Background(), filepath.Join(r.Directory(), "configuration.lock"))
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func(context.Context) error{
		func(ctx context.Context) error { _, err := r.Vault(ctx, secrets.Foreground); return err },
		func(ctx context.Context) error { return r.Configure(ctx, "bark", barkFixture, []byte(`{}`)) },
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		start := time.Now()
		err := operation(ctx)
		cancel()
		if err == nil || time.Since(start) > time.Second {
			t.Fatal("configuration lock unbounded")
		}
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Configure(ctx, "bark", barkFixture, []byte(`{}`)); err != nil {
		t.Fatal("nested vault locking")
	}
}

func TestFailedFirstConfigurationKeepsStableIdentity(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	// A valid foreground vault may exist without a successful configuration.
	if _, err := r.Vault(context.Background(), secrets.Foreground); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.Directory(), "installation.json")
	before, _ := os.ReadFile(path)
	if err := r.Configure(context.Background(), "bark", barkFixture, []byte(`{"volume":99}`)); err == nil {
		t.Fatal("invalid first save accepted")
	}
	if _, err := os.Lstat(filepath.Join(r.Directory(), "configuration.json")); !os.IsNotExist(err) {
		t.Fatal("failed first save visible")
	}
	save(t, r, "bark", barkFixture, `{}`)
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("identity replaced after failed first save")
	}
}

func TestConfigurationErrorsAreFixedAndReadsDoNotCreate(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	for _, mode := range []secrets.AccessMode{secrets.Foreground, secrets.Background, secrets.AccessMode(255)} {
		_, err := r.Credential("bark", mode)
		if err != errConfigurationUnavailable && err != errConfigurationInvalid {
			t.Fatal("raw credential error escaped")
		}
	}
	if _, err := os.Lstat(r.Directory()); !os.IsNotExist(err) {
		t.Fatal("credential read created state")
	}
	if err := r.Configure(context.Background(), "bad-provider-secret", barkFixture, []byte(`{}`)); err != errConfigurationInvalid {
		t.Fatal("raw configure error escaped")
	}
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.Directory(), "installation.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Vault(context.Background(), secrets.Foreground); err != errConfigurationUnavailable {
		t.Fatal("unreadable identity accepted")
	}
	if info, err := os.Lstat(filepath.Join(r.Directory(), "installation.json")); err != nil || !info.IsDir() {
		t.Fatal("unreadable identity replaced")
	}
}

func TestConfigureRejectsInvalidUTF8CredentialBeforeMutation(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	credential := ntfyFixture
	credential.Token = string([]byte{'s', 0xff})
	if err := r.Configure(context.Background(), "ntfy", credential, []byte(`{}`)); err != errConfigurationInvalid {
		t.Fatal("invalid UTF-8 credential normalized or accepted")
	}
	if _, err := os.Lstat(r.Directory()); !os.IsNotExist(err) {
		t.Fatal("invalid credential created state")
	}
}

func TestCorruptAuthenticatedCiphertextIsRejected(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	save(t, r, "bark", barkFixture, `{}`)
	path := filepath.Join(r.Directory(), "configuration.json")
	data, _ := os.ReadFile(path)
	var state struct {
		SchemaVersion int                       `json:"schemaVersion"`
		Settings      json.RawMessage           `json:"settings"`
		Credentials   map[string]map[string]any `json:"credentials"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	encoded := state.Credentials["bark"]["ciphertext"].(string)
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	state.Credentials["bark"]["ciphertext"] = base64.StdEncoding.EncodeToString(sealed)
	corrupt, _ := json.Marshal(state)
	if err := store.WriteAtomic(path, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Settings(); err == nil {
		t.Fatal("unauthenticated bundle settings accepted")
	}
	if _, err := r.Credential("bark", secrets.Background); err == nil {
		t.Fatal("corrupt credential accepted")
	}
	if err := r.Configure(context.Background(), "ntfy", ntfyFixture, []byte(`{}`)); err == nil {
		t.Fatal("corrupt old credential carried forward")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(corrupt, after) {
		t.Fatal("corrupt bundle replaced")
	}
}

func TestDarwinCredentialModesDoNotCreateMissingKey(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Keychain-only missing-key contract")
	}
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	identity := []byte(`{"schemaVersion":1,"installationId":"` + id + `"}`)
	if err := store.WriteAtomic(filepath.Join(r.Directory(), "installation.json"), identity); err != nil {
		t.Fatal(err)
	}
	settings, err := core.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"schemaVersion": 1, "settings": settings, "credentials": map[string]any{"bark": map[string]any{"schemaVersion": 1, "backend": "keychain-aes-gcm", "installationId": id, "purpose": "credential:bark", "ciphertext": "AQ=="}}}
	encoded, _ := json.Marshal(state)
	if err := store.WriteAtomic(filepath.Join(r.Directory(), "configuration.json"), encoded); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []secrets.AccessMode{secrets.Foreground, secrets.Background} {
		if _, err := r.Credential("bark", mode); err == nil {
			t.Fatal("missing key accepted")
		}
		if _, err := secrets.Open(id, secrets.Background); err == nil {
			t.Fatal("credential read created a key")
		}
	}
	if after, _ := os.ReadFile(filepath.Join(r.Directory(), "installation.json")); !bytes.Equal(after, identity) {
		t.Fatal("read replaced identity")
	}
}

func TestConfigureExistingBundleUsesForegroundVaultOnce(t *testing.T) {
	requireSyntheticProtection(t)
	r := repositoryFixture(t)
	save(t, r, "bark", barkFixture, `{"minSeconds":123}`)
	identityPath := filepath.Join(r.Directory(), "installation.json")
	identity, _ := os.ReadFile(identityPath)
	var modes []secrets.AccessMode
	// The permission boundary is controlled for this one invocation only.
	// Foreground still opens a real DPAPI/CI-Keychain Vault; crypto, locking
	// and the filesystem transaction are never mocked. This is not a UI test.
	opener := func(mode secrets.AccessMode) (*secrets.Vault, error) {
		modes = append(modes, mode)
		if mode == secrets.Background {
			return nil, errConfigurationUnavailable
		}
		return r.vaultLocked(mode)
	}
	if err := r.configure(context.Background(), "ntfy", ntfyFixture, []byte(`{"volume":7}`), opener); err != nil {
		t.Fatal("foreground configure stopped at background authorization")
	}
	if !reflect.DeepEqual(modes, []secrets.AccessMode{secrets.Foreground}) {
		t.Fatal("configuration must open exactly one foreground vault")
	}
	for provider, want := range map[string]providers.Credential{"bark": barkFixture, "ntfy": ntfyFixture} {
		if got, err := r.Credential(provider, secrets.Background); err != nil || got != want {
			t.Fatal("foreground transaction lost protected credentials")
		}
	}
	if settings, err := r.Settings(); err != nil || settings.MinSeconds != 123 || settings.Volume != 7 || settings.Provider != "ntfy" {
		t.Fatal("foreground transaction lost settings")
	}
	if after, _ := os.ReadFile(identityPath); !bytes.Equal(after, identity) {
		t.Fatal("foreground authorization replaced identity")
	}
}
