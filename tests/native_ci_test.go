package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Canonical provenance is a release invariant: the Mac26 consumers must fetch
// the Mac15 artifacts, not silently build their own replacement executable.
func TestNativeCICanonicalPackages(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "native.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(data), "\r\n", "\n")
	parts := strings.Split(source, "\n  package-check:\n")
	if len(parts) != 2 {
		t.Fatal("dependent canonical package-check job missing")
	}
	build, check := parts[0], parts[1]
	verification := strings.Split(check, "\n      - name: Verify and run the downloaded canonical archive\n")
	if len(verification) != 2 || strings.Count(source, "ATN_PACKAGE_DIAGNOSTICS") != 1 ||
		!strings.Contains(strings.SplitN(verification[1], "\n      - ", 2)[0], "\n        env:\n          ATN_PACKAGE_DIAGNOSTICS: ${{ runner.os == 'Windows' && '1' || '' }}\n        run: |") {
		t.Fatal("package diagnostics must be opt-in only in the Windows consumer verification step")
	}
	for _, value := range []string{"needs: native", "contents: read", "actions/download-artifact@018cc2cf5baa6db3ef3c5f8a56943fffe632ef53", "go run ./cmd/package-native verify", "--checksums", "--extract-to"} {
		if !strings.Contains(check, value) {
			t.Fatalf("package-check missing provenance gate %q", value)
		}
	}
	for _, pair := range [][2]string{{"windows-latest", "windows-amd64"}, {"macos-15", "darwin-arm64"}, {"macos-15-intel", "darwin-amd64"}, {"macos-26", "darwin-arm64"}, {"macos-26-intel", "darwin-amd64"}} {
		if !strings.Contains(check, "os: "+pair[0]+"\n            platform: "+pair[1]) {
			t.Fatal("canonical consumer mapping missing", pair)
		}
		canonical := "true"
		if strings.HasPrefix(pair[0], "macos-26") {
			canonical = "false"
		}
		if !strings.Contains(build, "os: "+pair[0]+"\n            platform: "+pair[1]+"\n            canonical: "+canonical) {
			t.Fatal("canonical producer mapping changed", pair)
		}
	}
	if strings.Count(build, "canonical: true") != 3 || strings.Count(build, "canonical: false") != 2 || strings.Count(check, "          - os:") != 5 {
		t.Fatal("canonical producer/consumer count changed")
	}
	for _, value := range []string{"go run ./cmd/package-native build", "name: native-candidate-${{ matrix.platform }}", "if: matrix.canonical", "node --test tests/opencode-bridge.test.mjs", "if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"} {
		if !strings.Contains(build, value) {
			t.Fatalf("canonical producer missing gate %q", value)
		}
	}
	if strings.Contains(check, "./cmd/agent-task-notify") || strings.Contains(check, "package-native build") || strings.Contains(source, "contents: write") {
		t.Fatal("consumer rebuild or release authority is forbidden")
	}
}

func TestNativeCIDiagnosticProjectionBoundsAndRedaction(t *testing.T) {
	for _, c := range []struct{ line, kind, want string }{
		{"create-keychain -p SECRET /PRIVATE/synthetic.keychain-db", "security", "create"},
		{"set-keychain-settings -lut 3600 /PRIVATE/synthetic.keychain-db", "security", "settings"},
		{"unlock-keychain -p SECRET /PRIVATE/synthetic.keychain-db", "security", "unlock"},
		{"list-keychains -d user", "security", "search-read"},
		{"default-keychain -d user", "security", "default-read"},
		{"list-keychains -d user -s /PRIVATE/synthetic.keychain-db", "security", "search-set"},
		{"default-keychain -d user -s /PRIVATE/synthetic.keychain-db", "security", "default-set"},
		{"default-keychain -d user -s old-default.keychain-db", "security", "default-restore"},
		{"list-keychains -d user -s old-search.keychain-db", "security", "search-restore"},
		{"delete-keychain /PRIVATE/synthetic.keychain-db", "security", "delete"},
		{"cleanup-rm -rf -- /PRIVATE/atn-keychain.abcdefgh", "security", "cleanup-fixture"},
		{"cleanup-rm -rf -- /PRIVATE/atn-go-tmp.abcdefgh", "security", "cleanup-tmp"},
		{"test -count=1 -v -timeout=45s -run ^TestDarwinRejectsIncompleteFileSecurity$ ./internal/store", "go", "counterfactual"},
		{"test -p 1 -count=1 -v ./...", "go", "suite"},
		{"https://PRIVATE/TOKEN BODY", "security", "other"},
		{"test -p PRIVATE ./...", "go", "other"},
	} {
		path := filepath.Join(t.TempDir(), "synthetic.log")
		if err := os.WriteFile(path, []byte(c.line+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got := nativeCILogSnapshot(path, c.kind)
		if got.ReadCategory != "read" || got.Count != 1 || got.Truncated || got.LastCall != c.want {
			t.Fatalf("unexpected fixed log projection: %+v", got)
		}
		encoded, _ := json.Marshal(got)
		if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "https") {
			t.Fatal("diagnostic leaked synthetic data")
		}
	}
	path := filepath.Join(t.TempDir(), "synthetic.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("PRIVATE\n", 1000)), 0600); err != nil {
		t.Fatal(err)
	}
	got := nativeCILogSnapshot(path, "security")
	if got.Count != 64 || !got.Truncated || got.LastCall != "other" {
		t.Fatal("line count unbounded")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("PRIVATE", 20000)), 0600); err != nil {
		t.Fatal(err)
	}
	got = nativeCILogSnapshot(path, "security")
	if !got.Truncated || got.Count > 64 {
		t.Fatal("byte read unbounded")
	}
	if got := nativeCILogSnapshot(path+"-missing", "go"); got.ReadCategory != "missing" || got.Count != 0 {
		t.Fatal("missing log not classified")
	}
	if got := nativeCILogSnapshot(t.TempDir(), "go"); got.ReadCategory != "unavailable" {
		t.Fatal("non-file log accepted")
	}
	if got := nativeCIDuration(99 * time.Hour); got != 60000 {
		t.Fatal("elapsed duration unbounded")
	}
	if got := nativeCIDuration(-time.Second); got != 0 {
		t.Fatal("negative duration")
	}
}

func TestNativeCIDiagnosticTimeoutAndFailedProcess(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("bash required")
	}
	log := filepath.Join(t.TempDir(), "synthetic.log")
	if err := os.WriteFile(log, []byte("default-keychain -d user -s old-default.keychain-db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Already-expired context is deterministic without a startup timing SLA or
	// a live shell/descendant. A separate builtin-only failure verifies collection.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", "exit 73")
	_, err, observed := observeNativeCICommand(ctx, cmd, map[string]string{"ATN_FAKE_SECURITY_LOG": log})
	if err == nil || !observed.TimedOut || observed.ProcessStarted || !observed.ProcessCompleted || observed.ProcessCategory != "not-started" || observed.Security.LastCall != "default-restore" {
		t.Fatalf("timeout observation: %+v", observed)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	cmd = exec.CommandContext(ctx2, bash, "-c", "printf PRIVATE_OUTPUT; exit 73")
	_, err, observed = observeNativeCICommand(ctx2, cmd, nil)
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 || observed.TimedOut || !observed.ProcessStarted || !observed.ProcessCompleted || observed.ProcessCategory != "exit" || !observed.OutputPresent {
		t.Fatalf("failure observation: %+v", observed)
	}
	encoded, _ := json.Marshal(observed)
	if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), bash) {
		t.Fatal("process diagnostic disclosed output/path")
	}
	var shape map[string]json.RawMessage
	if json.Unmarshal(encoded, &shape) != nil {
		t.Fatal("diagnostic JSON")
	}
	keys := map[string]bool{}
	for key := range shape {
		keys[key] = true
	}
	want := map[string]bool{"schemaVersion": true, "timedOut": true, "processStarted": true, "processCompleted": true, "processCategory": true, "outputPresent": true, "elapsedMs": true, "security": true, "go": true}
	if !reflect.DeepEqual(keys, want) {
		t.Fatal("unexpected diagnostic fields")
	}
}

func TestNativeCIScriptRefusesBeforeSecurityCommand(t *testing.T) {
	runnerTemp := t.TempDir()
	log := filepath.Join(t.TempDir(), "security.log")
	fake := writeFakeSecurity(t)
	code, output := runNativeCIScript(t, guardScriptForTest(t), map[string]string{
		"RUNNER_TEMP":            runnerTemp,
		"ATN_TEST_FAKE_SECURITY": "1",
		"ATN_TEST_SECURITY_BIN":  filepath.ToSlash(fake),
		"ATN_FAKE_SECURITY_LOG":  filepath.ToSlash(log),
	})
	assertNonCIRefusal(t, code, output, log)
}

func TestNativeCIScriptRestoresAfterPartialConfigurationFailure(t *testing.T) {
	runnerTemp := t.TempDir()
	log := filepath.Join(t.TempDir(), "security.log")
	fake := writeFakeSecurity(t)
	code, output := runNativeCIScript(t, productionNativeCIScript(t), map[string]string{
		"CI":                             "true",
		"RUNNER_TEMP":                    runnerTemp,
		"ATN_TEST_FAKE_SECURITY":         "1",
		"ATN_TEST_SECURITY_BIN":          filepath.ToSlash(fake),
		"ATN_FAKE_SECURITY_LOG":          filepath.ToSlash(log),
		"ATN_FAKE_SECURITY_FAIL_DEFAULT": "1",
	})
	if code != 73 {
		t.Fatalf("expected fake default-keychain failure 73, got %d: %s", code, output)
	}
	entries, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(entries)), "\n")
	searchMutation := findLine(t, lines, "list-keychains -d user -s", "synthetic.keychain-db")
	defaultFailure := findLine(t, lines, "default-keychain -d user -s", "synthetic.keychain-db")
	defaultRestore := findLine(t, lines, "default-keychain -d user -s old-default.keychain-db")
	searchRestore := findLine(t, lines, "list-keychains -d user -s old-search.keychain-db")
	if !(searchMutation < defaultFailure && defaultFailure < defaultRestore && defaultRestore < searchRestore) {
		t.Fatalf("backup/mutation/restore order is unsafe: %q", lines)
	}
	deleteLine := findLine(t, lines, "delete-keychain", "synthetic.keychain-db")
	if strings.Count(lines[deleteLine], "synthetic.keychain-db") != 1 || strings.Contains(lines[deleteLine], "old-") {
		t.Fatalf("cleanup deleted a non-generated target: %q", lines[deleteLine])
	}
	remaining, err := os.ReadDir(runnerTemp)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("generated fixture or test temporary directory remained: %v", remaining)
	}
}

func TestNativeCIScriptRejectsEmptyBackupsBeforeMutation(t *testing.T) {
	for _, empty := range []string{"search", "default"} {
		t.Run(empty, func(t *testing.T) {
			runnerTemp, log, env := nativeCIFakeFixture(t)
			env["ATN_FAKE_SECURITY_EMPTY_BACKUP"] = empty
			code, output := runNativeCIScript(t, nativeCIScriptWithCleanupObserver(t), env)
			if code == 0 {
				t.Errorf("empty %s backup allowed a successful run: %s", empty, output)
			}
			entries, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(entries), "-d user -s") {
				t.Errorf("empty %s backup allowed Keychain configuration mutation: %s", empty, entries)
			}
			assertNativeCICleanup(t, runnerTemp, log, false, "")
		})
	}
}

func TestNativeCIScriptCleanupExitStatus(t *testing.T) {
	for _, body := range []string{"success", "failure73"} {
		for _, failure := range []string{"none", "default", "search", "unlock", "delete", "fixture", "tmp", "all"} {
			t.Run(body+"/"+failure, func(t *testing.T) {
				runnerTemp, log, env := nativeCIFakeFixture(t)
				env["ATN_FAKE_CLEANUP_FAILURE"] = failure
				if body == "failure73" {
					env["ATN_FAKE_SECURITY_FAIL_DEFAULT"] = "1"
				}
				code, output := runNativeCIScript(t, nativeCIScriptWithCleanupObserver(t), env)
				if body == "failure73" {
					if code != 73 {
						t.Errorf("cleanup hid original body failure 73: code=%d output=%s", code, output)
					}
				} else if failure == "none" {
					if code != 0 {
						t.Errorf("successful body and cleanup failed: code=%d output=%s", code, output)
					}
				} else if code == 0 {
					t.Errorf("cleanup %s failure did not fail successful body: %s", failure, output)
				}
				assertNativeCICleanup(t, runnerTemp, log, true, failure)
			})
		}
	}
}

func TestNativeCIScriptCleanupObserverTransform(t *testing.T) {
	contents, err := os.ReadFile(productionNativeCIScript(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range nativeCICleanupObserverFixtures(contents) {
		t.Run(fixture.name, func(t *testing.T) {
			tests := []struct {
				name     string
				contents []byte
				wantErr  bool
			}{
				{name: "LF", contents: fixture.lf},
				{name: "CRLF", contents: fixture.crlf},
				{name: "missing setup", contents: fixture.missingSetup, wantErr: true},
				{name: "repeated setup", contents: fixture.repeatedSetup, wantErr: true},
			}
			var lfTransformed string
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					transformed, err := nativeCIScriptWithCleanupObserverContents(test.contents)
					if test.wantErr {
						if err == nil || err.Error() != nativeCICleanupObserverSetupError {
							t.Fatalf("unexpected transform error: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					got := string(transformed)
					if strings.Count(got, nativeCIShellSetup) != 1 || strings.Count(got, "rm() {") != 1 {
						t.Fatalf("cleanup observer was not injected exactly once: %q", got)
					}
					if !strings.Contains(got, "cleanup_on_exit() {") || !strings.Contains(got, "filesec_counterfactual\n") {
						t.Fatal("cleanup wrapper or production test body was not retained")
					}
					if test.name == "LF" {
						lfTransformed = got
					} else if got != lfTransformed {
						t.Fatal("LF and CRLF production templates transformed differently")
					}
				})
			}
		})
	}
}

func TestNativeCIScriptCleanupObserverFixtures(t *testing.T) {
	contents, err := os.ReadFile(productionNativeCIScript(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range nativeCICleanupObserverFixtures(contents) {
		t.Run(fixture.name, func(t *testing.T) {
			if fixture.name == "LF source" && strings.Contains(string(fixture.source), "\r") {
				t.Fatalf("LF source was not canonical: %q", fixture.source)
			}
			if fixture.name == "CRLF source" && (!strings.Contains(string(fixture.source), "\r\n") || strings.Contains(string(fixture.source), "\r\r\n")) {
				t.Fatalf("CRLF source was malformed: %q", fixture.source)
			}
			if strings.Contains(string(fixture.lf), "\r") {
				t.Fatalf("LF fixture contains a carriage return: %q", fixture.lf)
			}
			if strings.Contains(string(fixture.crlf), "\r\r\n") || strings.ReplaceAll(string(fixture.crlf), "\r\n", "\n") != string(fixture.lf) {
				t.Fatalf("CRLF fixture is malformed or differs after normalization: %q", fixture.crlf)
			}
			if strings.Count(string(fixture.missingSetup), nativeCIShellSetup) != 0 || strings.Count(string(fixture.repeatedSetup), nativeCIShellSetup) != 2 {
				t.Fatalf("setup marker variants are not exact: missing=%q repeated=%q", fixture.missingSetup, fixture.repeatedSetup)
			}
		})
	}
}

func TestNativeCIScriptCleanupObserverRunsCRLFScript(t *testing.T) {
	contents, err := os.ReadFile(productionNativeCIScript(t))
	if err != nil {
		t.Fatal(err)
	}
	fixtures := nativeCICleanupObserverFixtures(contents)
	if len(fixtures) != 2 || fixtures[1].name != "CRLF source" {
		t.Fatalf("missing CRLF source fixture: %q", fixtures)
	}
	script := nativeCIScriptWithCleanupObserverFromContents(t, fixtures[1].crlf)
	runnerTemp, log, env := nativeCIFakeFixture(t)
	env["ATN_FAKE_SECURITY_FAIL_DEFAULT"] = "1"
	code, output := runNativeCIScript(t, script, env)
	if code != 73 {
		t.Fatalf("CRLF cleanup-observer copy hid body failure 73: code=%d output=%s", code, output)
	}
	assertNativeCICleanup(t, runnerTemp, log, true, "")
}

type nativeCICleanupObserverFixture struct {
	name          string
	source        []byte
	lf            []byte
	crlf          []byte
	missingSetup  []byte
	repeatedSetup []byte
}

func nativeCICleanupObserverFixtures(contents []byte) []nativeCICleanupObserverFixture {
	template := strings.ReplaceAll(string(contents), "\r\n", "\n")
	return []nativeCICleanupObserverFixture{
		nativeCICleanupObserverFixtureFromSource("LF source", template),
		nativeCICleanupObserverFixtureFromSource("CRLF source", strings.ReplaceAll(template, "\n", "\r\n")),
	}
}

func nativeCICleanupObserverFixtureFromSource(name, source string) nativeCICleanupObserverFixture {
	template := strings.ReplaceAll(source, "\r\n", "\n")
	return nativeCICleanupObserverFixture{
		name:          name,
		source:        []byte(source),
		lf:            []byte(template),
		crlf:          []byte(strings.ReplaceAll(template, "\n", "\r\n")),
		missingSetup:  []byte(strings.Replace(template, nativeCIShellSetup, "", 1)),
		repeatedSetup: []byte(strings.Replace(template, nativeCIShellSetup, nativeCIShellSetup+nativeCIShellSetup, 1)),
	}
}

func nativeCIFakeFixture(t *testing.T) (string, string, map[string]string) {
	t.Helper()
	runnerTemp := t.TempDir()
	if err := os.WriteFile(filepath.Join(runnerTemp, "unrelated"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "security.log")
	fakeGoDir := writeFakeGo(t)
	oldACL, currentTest := writeExpectedNativeCIInputs(t)
	return runnerTemp, log, map[string]string{
		"CI":                         "true",
		"RUNNER_TEMP":                runnerTemp,
		"ATN_TEST_FAKE_SECURITY":     "1",
		"ATN_TEST_SECURITY_BIN":      filepath.ToSlash(writeFakeSecurity(t)),
		"ATN_FAKE_SECURITY_LOG":      filepath.ToSlash(log),
		"ATN_FAKE_FILESEC_MODE":      "expected",
		"ATN_FAKE_OLD_ACL_FILE":      filepath.ToSlash(oldACL),
		"ATN_FAKE_CURRENT_TEST_FILE": filepath.ToSlash(currentTest),
		"ATN_TEST_FAKE_GO_DIR":       filepath.ToSlash(fakeGoDir),
		"ATN_FAKE_GO_LOG":            filepath.ToSlash(filepath.Join(t.TempDir(), "go.log")),
		"PATH":                       filepath.ToSlash(fakeGoDir) + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
}

// The production script is executed unchanged except for this local rm boundary.
// It observes/fails only the two generated cleanup directories, never other paths.
func nativeCIScriptWithCleanupObserver(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(productionNativeCIScript(t))
	if err != nil {
		t.Fatal(err)
	}
	return nativeCIScriptWithCleanupObserverFromContents(t, contents)
}

const nativeCICleanupObserver = `
rm() {
  local target="${!#}" step
  if test "$#" -ne 3 || test "$1" != -rf || test "$2" != --; then return 99; fi
  case "$target" in
    "$fixture_dir") step=fixture ;;
    "$test_tmp") step=tmp ;;
    *) return 99 ;;
  esac
  printf '%s\n' "cleanup-rm $*" >> "${ATN_FAKE_SECURITY_LOG:?}"
  if test "${ATN_FAKE_CLEANUP_FAILURE:-}" = "$step" || test "${ATN_FAKE_CLEANUP_FAILURE:-}" = all; then return 74; fi
  command rm "$@"
}
`

const (
	nativeCIShellSetup                = "set -euo pipefail\n"
	nativeCICleanupObserverSetupError = "cannot locate shell setup for cleanup observation"
)

func nativeCIScriptWithCleanupObserverContents(contents []byte) ([]byte, error) {
	source := strings.ReplaceAll(string(contents), "\r\n", "\n")
	if strings.Count(source, nativeCIShellSetup) != 1 {
		return nil, errors.New(nativeCICleanupObserverSetupError)
	}
	return []byte(strings.Replace(source, nativeCIShellSetup, nativeCIShellSetup+nativeCICleanupObserver, 1)), nil
}

func nativeCIScriptWithCleanupObserverFromContents(t *testing.T, contents []byte) string {
	t.Helper()
	transformed, err := nativeCIScriptWithCleanupObserverContents(contents)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "native-ci-cleanup-copy.sh")
	if err := os.WriteFile(script, transformed, 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func assertNativeCICleanup(t *testing.T, runnerTemp, log string, mutated bool, failure string) {
	t.Helper()
	entries, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(entries)), "\n")
	const create = "create-keychain -p atn-synthetic-ci-fixture-only "
	if !strings.HasPrefix(lines[0], create) {
		t.Fatalf("missing generated fixture creation: %q", lines)
	}
	keychain := strings.TrimPrefix(lines[0], create)
	fixture := strings.TrimSuffix(keychain, "/synthetic.keychain-db")
	if fixture == keychain || !strings.HasPrefix(filepath.Base(fixture), "atn-keychain.") {
		t.Fatalf("unexpected generated Keychain path: %q", keychain)
	}
	const rm = "cleanup-rm -rf -- "
	tmp := strings.TrimPrefix(lines[len(lines)-1], rm)
	if !strings.HasPrefix(filepath.Base(tmp), "atn-go-tmp.") || filepath.Dir(tmp) != filepath.Dir(fixture) {
		t.Fatalf("unexpected generated temporary path: %q", tmp)
	}
	want := []string{
		create + keychain,
		"set-keychain-settings -lut 3600 " + keychain,
		"unlock-keychain -p atn-synthetic-ci-fixture-only " + keychain,
		"list-keychains -d user",
		"default-keychain -d user",
	}
	if mutated {
		want = append(want,
			"list-keychains -d user -s "+keychain,
			"default-keychain -d user -s "+keychain,
			"default-keychain -d user -s old-default.keychain-db",
			"list-keychains -d user -s old-search.keychain-db")
	}
	want = append(want,
		"unlock-keychain -p atn-synthetic-ci-fixture-only "+keychain,
		"delete-keychain "+keychain, rm+fixture, rm+tmp)
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cleanup did not attempt every action in order on exact generated paths: got=%q want=%q", lines, want)
	}
	remaining, err := os.ReadDir(runnerTemp)
	if err != nil {
		t.Fatal(err)
	}
	wantRemaining := map[string]bool{"unrelated": true}
	if failure == "fixture" || failure == "all" {
		wantRemaining[filepath.Base(fixture)] = true
	}
	if failure == "tmp" || failure == "all" {
		wantRemaining[filepath.Base(tmp)] = true
	}
	if len(remaining) != len(wantRemaining) {
		t.Fatalf("unexpected cleanup leftovers: %v", remaining)
	}
	for _, entry := range remaining {
		if !wantRemaining[entry.Name()] {
			t.Fatalf("unexpected leftover %q", entry.Name())
		}
	}
	unchanged, err := os.ReadFile(filepath.Join(runnerTemp, "unrelated"))
	if err != nil || string(unchanged) != "keep" {
		t.Fatal("cleanup changed unrelated runner data")
	}
}

func TestNativeWorkflowCanonicalArtifactNameMatchesProducerAndConsumer(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "native.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(workflow), "name: native-candidate-${{ matrix.platform }}") != 2 {
		t.Fatal("canonical artifact name must match exactly between producer and consumer")
	}
}

func TestNativeCIScriptRejectsWrongGoSelection(t *testing.T) {
	runnerTemp := t.TempDir()
	log := filepath.Join(t.TempDir(), "security.log")
	fakeSecurity := writeFakeSecurity(t)
	expectedGoDir := writeFakeGo(t)
	wrongGoDir := t.TempDir()
	wrongGoLog := filepath.Join(wrongGoDir, "invoked")
	if err := os.WriteFile(filepath.Join(wrongGoDir, "go"), []byte("#!/bin/sh\nprintf invoked > \"${ATN_WRONG_GO_LOG:?}\"\nexit 79\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, output := runNativeCIScript(t, productionNativeCIScript(t), map[string]string{
		"CI":                     "true",
		"RUNNER_TEMP":            runnerTemp,
		"ATN_TEST_FAKE_SECURITY": "1",
		"ATN_TEST_SECURITY_BIN":  filepath.ToSlash(fakeSecurity),
		"ATN_FAKE_SECURITY_LOG":  filepath.ToSlash(log),
		"ATN_TEST_FAKE_GO_DIR":   filepath.ToSlash(expectedGoDir),
		"ATN_WRONG_GO_LOG":       filepath.ToSlash(wrongGoLog),
		"PATH":                   filepath.ToSlash(wrongGoDir),
	})
	if code != 98 || !strings.Contains(output, "native CI test requires fake go") {
		t.Fatalf("expected refusal before wrong go selection: code=%d output=%q", code, output)
	}
	for _, path := range []string{log, wrongGoLog} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("command ran before fake go selection was verified: path=%s err=%v", path, err)
		}
	}
}

func TestNativeCIScriptFilesecCounterfactualEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		mode       string
		alterFile  string
		wantCode   int
		wantMarker string
	}{
		{name: "expected rejection", mode: "expected", wantCode: 0, wantMarker: "filesec-counterfactual: expected rejection confirmed"},
		{name: "partial rejection text", mode: "partial", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected failure evidence"},
		{name: "unexpected pass", mode: "pass", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected pass"},
		{name: "unrelated failure", mode: "unrelated", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected failure evidence"},
		{name: "changed historical bytes", mode: "expected", alterFile: "acl_darwin.go", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected failure evidence"},
		{name: "changed current test bytes", mode: "expected", alterFile: "files_darwin_test.go", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected failure evidence"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runnerTemp := t.TempDir()
			log := filepath.Join(t.TempDir(), "security.log")
			fakeSecurity := writeFakeSecurity(t)
			fakeGoDir := writeFakeGo(t)
			goLog := filepath.Join(t.TempDir(), "go.log")
			oldACL, currentTest := writeExpectedNativeCIInputs(t)
			script := productionNativeCIScript(t)
			if scenario.alterFile != "" {
				contents, err := os.ReadFile(script)
				if err != nil {
					t.Fatal(err)
				}
				const invocation = `    (cd "$counter_root" && go test`
				if strings.Count(string(contents), invocation) != 1 {
					t.Fatal("cannot locate counterfactual invocation for byte-corruption check")
				}
				modified := strings.Replace(string(contents), invocation, "    printf '\\n' >> \"$counter_root/internal/store/"+scenario.alterFile+"\"\n"+invocation, 1)
				script = filepath.Join(t.TempDir(), "native-ci-altered-copy.sh")
				if err := os.WriteFile(script, []byte(modified), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			code, output := runNativeCIScript(t, script, map[string]string{
				"CI":                         "true",
				"RUNNER_TEMP":                runnerTemp,
				"ATN_TEST_FAKE_SECURITY":     "1",
				"ATN_TEST_SECURITY_BIN":      filepath.ToSlash(fakeSecurity),
				"ATN_FAKE_SECURITY_LOG":      filepath.ToSlash(log),
				"ATN_FAKE_FILESEC_MODE":      scenario.mode,
				"ATN_FAKE_OLD_ACL_FILE":      filepath.ToSlash(oldACL),
				"ATN_FAKE_CURRENT_TEST_FILE": filepath.ToSlash(currentTest),
				"ATN_TEST_FAKE_GO_DIR":       filepath.ToSlash(fakeGoDir),
				"ATN_FAKE_GO_LOG":            filepath.ToSlash(goLog),
				"PATH":                       filepath.ToSlash(fakeGoDir) + string(os.PathListSeparator) + os.Getenv("PATH"),
			})
			if code != scenario.wantCode || !strings.Contains(output, scenario.wantMarker) {
				t.Fatalf("unexpected filesec result: code=%d output=%q", code, output)
			}
			if _, err := os.Stat(log); err != nil {
				t.Fatalf("fake fixture did not run: %v", err)
			}
			goCalls, err := os.ReadFile(goLog)
			wantCalls := "test -count=1 -v -timeout=45s -run ^TestDarwinRejectsIncompleteFileSecurity$ ./internal/store\n"
			if scenario.wantCode == 0 {
				wantCalls += "test -p 1 -count=1 -v ./...\n"
			}
			if err != nil || string(goCalls) != wantCalls {
				t.Fatalf("unexpected fake go invocations: got=%q want=%q err=%v", goCalls, wantCalls, err)
			}
			remaining, err := os.ReadDir(runnerTemp)
			if err != nil || len(remaining) != 0 {
				t.Fatalf("generated directories were not cleaned: entries=%v err=%v", remaining, err)
			}
		})
	}
}

func guardScriptForTest(t *testing.T) string {
	t.Helper()
	script := productionNativeCIScript(t)
	if os.Getenv("ATN_GUARD_COUNTERFACTUAL") != "1" {
		return script
	}
	contents, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	const guard = `if test "${CI:-}" != "true" || test -z "${RUNNER_TEMP:-}"; then
    echo "native macOS CI fixture requires CI=true and RUNNER_TEMP" >&2
    exit 2
fi

`
	modified := strings.Replace(string(contents), guard, "", 1)
	if modified == string(contents) {
		t.Fatal("counterfactual did not remove the production guard")
	}
	copyPath := filepath.Join(t.TempDir(), "native-ci-macos-without-guard.sh")
	if err := os.WriteFile(copyPath, []byte(modified), 0o700); err != nil {
		t.Fatal(err)
	}
	return copyPath
}

func productionNativeCIScript(t *testing.T) string {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "scripts", "native-ci-macos.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func assertNonCIRefusal(t *testing.T, code int, output, log string) {
	t.Helper()
	_, err := os.Stat(log)
	logExists := err == nil
	if code != 2 || !strings.Contains(output, "requires CI=true") || logExists {
		t.Fatalf("expected non-CI refusal before security invocation: code=%d output=%q fakeSecurityInvoked=%t", code, output, logExists)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("unexpected fake security log state: %v", err)
	}
}

func runNativeCIScript(t *testing.T, script string, extra map[string]string) (int, string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("bash is required for native CI script regression tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, script)
	if fakeGoDir := extra["ATN_TEST_FAKE_GO_DIR"]; fakeGoDir != "" {
		// Resolve before sourcing in this same shell: a broken PATH must never
		// reach the real Go toolchain (which could recursively run this suite).
		const guarded = `if ! test "$(command -v go)" -ef "$1/go"; then
  printf '%s\n' 'native CI test requires fake go' >&2
  exit 98
fi
. "$2"
`
		cmd = exec.CommandContext(ctx, bash, "-c", guarded, "native-ci-test", filepath.ToSlash(fakeGoDir), script)
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = root
	cmd.Env = withoutNativeCIEnvironment(os.Environ())
	for key, value := range extra {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err, observed := observeNativeCICommand(ctx, cmd, extra)
	if observed.TimedOut {
		encoded, _ := json.Marshal(observed)
		t.Fatalf("native CI script exceeded bounded test timeout; native-ci-snapshot=%s", encoded)
	}
	if err == nil {
		return 0, string(output)
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), string(output)
	}
	t.Fatal(err)
	return -1, ""
}

type nativeCILogObservation struct {
	ReadCategory string `json:"readCategory"`
	Count        int    `json:"count"`
	Truncated    bool   `json:"truncated"`
	LastCall     string `json:"lastCall"`
}

type nativeCIObservation struct {
	SchemaVersion    int                    `json:"schemaVersion"`
	TimedOut         bool                   `json:"timedOut"`
	ProcessStarted   bool                   `json:"processStarted"`
	ProcessCompleted bool                   `json:"processCompleted"`
	ProcessCategory  string                 `json:"processCategory"`
	OutputPresent    bool                   `json:"outputPresent"`
	ElapsedMs        int64                  `json:"elapsedMs"`
	Security         nativeCILogObservation `json:"security"`
	Go               nativeCILogObservation `json:"go"`
}

func nativeCIDuration(elapsed time.Duration) int64 { return min(60000, max(0, elapsed.Milliseconds())) }

// Completion means CombinedOutput/Wait collection returned, not success. The
// existing command, cancellation and five-second budget are deliberately intact.
func observeNativeCICommand(ctx context.Context, cmd *exec.Cmd, extra map[string]string) ([]byte, error, nativeCIObservation) {
	started := time.Now()
	output, err := cmd.CombinedOutput()
	observed := nativeCIObservation{SchemaVersion: 1, TimedOut: ctx.Err() == context.DeadlineExceeded, ProcessStarted: cmd.Process != nil, ProcessCompleted: true, ProcessCategory: "not-started", OutputPresent: len(output) > 0, ElapsedMs: nativeCIDuration(time.Since(started))}
	if cmd.ProcessState != nil {
		if cmd.ProcessState.Success() {
			observed.ProcessCategory = "success"
		} else if cmd.ProcessState.Exited() {
			observed.ProcessCategory = "exit"
		} else {
			observed.ProcessCategory = "signaled"
		}
	}
	// Sample the deadline immediately after collection. Diagnostic I/O must not
	// turn a process that completed within its budget into a timeout afterward.
	observed.Security = nativeCILogObservation{ReadCategory: "missing", LastCall: "none"}
	observed.Go = nativeCILogObservation{ReadCategory: "missing", LastCall: "none"}
	if observed.TimedOut {
		observed.Security = nativeCILogSnapshot(extra["ATN_FAKE_SECURITY_LOG"], "security")
		observed.Go = nativeCILogSnapshot(extra["ATN_FAKE_GO_LOG"], "go")
	}
	return output, err, observed
}

// Only test-owned fake-command logs are supplied. Limit the actual read, and
// count at most64 lines; no raw log line, path, command or output is returned.
func nativeCILogSnapshot(path, kind string) nativeCILogObservation {
	result := nativeCILogObservation{ReadCategory: "missing", LastCall: "none"}
	if path == "" {
		return result
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return result
	}
	if err != nil {
		result.ReadCategory = "unavailable"
		return result
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		result.ReadCategory = "unavailable"
		return result
	}
	const limit = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		result.ReadCategory = "unavailable"
		return result
	}
	result.ReadCategory = "read"
	if len(data) > limit {
		result.Truncated = true
		data = data[:limit]
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return result
	}
	lines := strings.SplitN(text, "\n", 65)
	if len(lines) > 64 {
		result.Truncated = true
		lines = lines[:64]
	}
	for _, line := range lines {
		result.Count++
		result.LastCall = nativeCILastCall(strings.TrimSuffix(line, "\r"), kind)
	}
	return result
}

// A logged invocation is not evidence that the invocation completed.
func nativeCILastCall(line, kind string) string {
	if kind == "go" {
		switch line {
		case "test -count=1 -v -timeout=45s -run ^TestDarwinRejectsIncompleteFileSecurity$ ./internal/store":
			return "counterfactual"
		case "test -p 1 -count=1 -v ./...":
			return "suite"
		default:
			return "other"
		}
	}
	if kind != "security" {
		return "other"
	}
	switch line {
	case "list-keychains -d user":
		return "search-read"
	case "default-keychain -d user":
		return "default-read"
	case "list-keychains -d user -s old-search.keychain-db":
		return "search-restore"
	case "default-keychain -d user -s old-default.keychain-db":
		return "default-restore"
	}
	for _, pair := range [][2]string{{"create-keychain ", "create"}, {"set-keychain-settings ", "settings"}, {"unlock-keychain ", "unlock"}, {"list-keychains -d user -s ", "search-set"}, {"default-keychain -d user -s ", "default-set"}, {"delete-keychain ", "delete"}} {
		if strings.HasPrefix(line, pair[0]) {
			return pair[1]
		}
	}
	if target, ok := strings.CutPrefix(line, "cleanup-rm -rf -- "); ok {
		base := filepath.Base(target)
		if strings.HasPrefix(base, "atn-keychain.") {
			return "cleanup-fixture"
		}
		if strings.HasPrefix(base, "atn-go-tmp.") {
			return "cleanup-tmp"
		}
	}
	return "other"
}

func withoutNativeCIEnvironment(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "ATN_FAKE_SECURITY_EMPTY_BACKUP", "ATN_FAKE_CLEANUP_FAILURE":
			continue
		case "CI", "RUNNER_TEMP", "ATN_TEST_FAKE_SECURITY", "ATN_TEST_SECURITY_BIN", "ATN_FAKE_SECURITY_LOG", "ATN_FAKE_SECURITY_FAIL_DEFAULT", "ATN_FAKE_FILESEC_MODE", "ATN_TEST_FAKE_GO_DIR", "ATN_FAKE_GO_LOG", "ATN_FAKE_OLD_ACL_FILE", "ATN_FAKE_CURRENT_TEST_FILE", "ATN_WRONG_GO_LOG":
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func writeFakeSecurity(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "security")
	const fake = `#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "${ATN_FAKE_SECURITY_LOG:?}"
command=$1
shift
last="${!#}"
cleanup_failure() {
  if test "${ATN_FAKE_CLEANUP_FAILURE:-}" = "$1" || test "${ATN_FAKE_CLEANUP_FAILURE:-}" = all; then exit 74; fi
}
case "$command" in
  create-keychain) : > "$last" ;;
  list-keychains)
    if test "$*" = "-d user"; then
      if test "${ATN_FAKE_SECURITY_EMPTY_BACKUP:-}" != search; then printf '"old-search.keychain-db"\n'; fi
    elif test "$*" = "-d user -s old-search.keychain-db"; then
      cleanup_failure search
    fi
    ;;
  default-keychain)
    if test "$*" = "-d user"; then
      if test "${ATN_FAKE_SECURITY_EMPTY_BACKUP:-}" != default; then printf '"old-default.keychain-db"\n'; fi
    elif test "$*" = "-d user -s old-default.keychain-db"; then
      cleanup_failure default
    elif [[ "$*" == *synthetic.keychain-db && "${ATN_FAKE_SECURITY_FAIL_DEFAULT:-}" = 1 ]]; then
      exit 73
    fi
    ;;
  unlock-keychain)
    if test "$(grep -c '^unlock-keychain ' "$ATN_FAKE_SECURITY_LOG")" -gt 1; then cleanup_failure unlock; fi
    ;;
  delete-keychain) cleanup_failure delete; rm -f -- "$last" ;;
esac
`
	if err := os.WriteFile(path, []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFakeGo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "go")
	const fake = `#!/usr/bin/env bash
set -eu
# Exercise the script without undeclared hashing tools on every host.
sha256sum() { return 97; }
awk() { return 97; }
printf '%s\n' "$*" >> "${ATN_FAKE_GO_LOG:?}"
if [[ "$*" == *TestDarwinRejectsIncompleteFileSecurity* ]]; then
  if test "$*" != 'test -count=1 -v -timeout=45s -run ^TestDarwinRejectsIncompleteFileSecurity$ ./internal/store' ||
      [[ "$PWD" != */atn-go-tmp.*/filesec-counterfactual ]] ||
      ! test -f go.mod || ! test -f go.sum ||
      ! test -f internal/store/files_darwin_test.go ||
      ! cmp -s internal/store/acl_darwin.go "${ATN_FAKE_OLD_ACL_FILE:?}" ||
      ! cmp -s internal/store/files_darwin_test.go "${ATN_FAKE_CURRENT_TEST_FILE:?}"; then
    printf '%s\n' 'filesec fake contract failure'
    exit 1
  fi
  case "${ATN_FAKE_FILESEC_MODE:?}" in
    expected)
      printf '%s\n' '--- FAIL: TestDarwinRejectsIncompleteFileSecurity (0.00s)' 'stage=unfilled result=1' 'incomplete filesec accepted or complete no-ACL filesec rejected'
      exit 1
      ;;
    partial)
      printf '%s\n' '--- FAIL: TestDarwinRejectsIncompleteFileSecurity (0.00s)' 'stage=unfilled result=1' 'incomplete filesec accepted'
      exit 1
      ;;
    pass) exit 0 ;;
    unrelated) printf '%s\n' 'unrelated compiler failure'; exit 1 ;;
  esac
fi
printf '%s\n' '--- PASS: TestDarwinLockedKeychainBackgroundDenial (0.00s)' '--- PASS: TestDarwinRejectsIncompleteFileSecurity (0.00s)'
`
	if err := os.WriteFile(path, []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeExpectedNativeCIInputs(t *testing.T) (string, string) {
	t.Helper()
	oldACL, err := exec.Command("git", "show", "0062d3b1ccc08c2b81112d8c843b8800f3af4df2:internal/store/acl_darwin.go").Output()
	if err != nil {
		t.Fatal(err)
	}
	currentTest, err := os.ReadFile(filepath.Join("..", "internal", "store", "files_darwin_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	oldACLPath := filepath.Join(dir, "expected-acl")
	currentTestPath := filepath.Join(dir, "expected-test")
	for path, contents := range map[string][]byte{oldACLPath: oldACL, currentTestPath: currentTest} {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return oldACLPath, currentTestPath
}

func findLine(t *testing.T, lines []string, parts ...string) int {
	t.Helper()
	for index, line := range lines {
		matched := true
		for _, part := range parts {
			matched = matched && strings.Contains(line, part)
		}
		if matched {
			return index
		}
	}
	t.Fatalf("missing %q in %q", parts, lines)
	return -1
}
