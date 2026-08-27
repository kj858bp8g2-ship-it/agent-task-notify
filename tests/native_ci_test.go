package tests

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func TestNativeWorkflowArtifactNameIncludesMatrixLabelAndRunnerIdentity(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "native.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "name: native-candidate-${{ matrix.os }}-${{ runner.os }}-${{ runner.arch }}") {
		t.Fatal("artifact name must include matrix label plus runner OS and architecture")
	}
}

func TestNativeCIScriptFilesecCounterfactualEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		mode       string
		wantCode   int
		wantMarker string
	}{
		{name: "expected rejection", mode: "expected", wantCode: 0, wantMarker: "filesec-counterfactual: expected rejection confirmed"},
		{name: "partial rejection text", mode: "partial", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected failure evidence"},
		{name: "unexpected pass", mode: "pass", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected pass"},
		{name: "unrelated failure", mode: "unrelated", wantCode: 1, wantMarker: "filesec-counterfactual: unexpected failure evidence"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runnerTemp := t.TempDir()
			log := filepath.Join(t.TempDir(), "security.log")
			fakeSecurity := writeFakeSecurity(t)
			fakeGoDir := writeFakeGo(t)
			oldACLHash := historicalFileSHA256(t, "0062d3b1ccc08c2b81112d8c843b8800f3af4df2:internal/store/acl_darwin.go")
			currentTestHash := localFileSHA256(t, filepath.Join("..", "internal", "store", "files_darwin_test.go"))
			code, output := runNativeCIScript(t, productionNativeCIScript(t), map[string]string{
				"CI":                           "true",
				"RUNNER_TEMP":                  runnerTemp,
				"ATN_TEST_FAKE_SECURITY":       "1",
				"ATN_TEST_SECURITY_BIN":        filepath.ToSlash(fakeSecurity),
				"ATN_FAKE_SECURITY_LOG":        filepath.ToSlash(log),
				"ATN_FAKE_FILESEC_MODE":        scenario.mode,
				"ATN_FAKE_OLD_ACL_SHA256":      oldACLHash,
				"ATN_FAKE_CURRENT_TEST_SHA256": currentTestHash,
				"PATH":                         filepath.ToSlash(fakeGoDir) + ";" + os.Getenv("PATH"),
			})
			if code != scenario.wantCode || !strings.Contains(output, scenario.wantMarker) {
				t.Fatalf("unexpected filesec result: code=%d output=%q", code, output)
			}
			if _, err := os.Stat(log); err != nil {
				t.Fatalf("fake fixture did not run: %v", err)
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
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = root
	cmd.Env = withoutNativeCIEnvironment(os.Environ())
	for key, value := range extra {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("native CI script exceeded bounded test timeout: %v", ctx.Err())
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

func withoutNativeCIEnvironment(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "CI", "RUNNER_TEMP", "ATN_TEST_FAKE_SECURITY", "ATN_TEST_SECURITY_BIN", "ATN_FAKE_SECURITY_LOG", "ATN_FAKE_SECURITY_FAIL_DEFAULT", "ATN_FAKE_FILESEC_MODE":
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
case "$command" in
  create-keychain) : > "$last" ;;
  list-keychains)
    if test "$*" = "-d user"; then printf '"old-search.keychain-db"\n'; fi
    ;;
  default-keychain)
    if test "$*" = "-d user"; then
      printf '"old-default.keychain-db"\n'
    elif [[ "$*" == *synthetic.keychain-db && "${ATN_FAKE_SECURITY_FAIL_DEFAULT:-}" = 1 ]]; then
      exit 73
    fi
    ;;
  delete-keychain) rm -f -- "$last" ;;
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
if [[ "$*" == *TestDarwinRejectsIncompleteFileSecurity* ]]; then
  if test "$*" != 'test -count=1 -v -timeout=45s -run ^TestDarwinRejectsIncompleteFileSecurity$ ./internal/store' ||
      [[ "$PWD" != */atn-go-tmp.*/filesec-counterfactual ]] ||
      ! test -f go.mod || ! test -f go.sum ||
      ! test -f internal/store/files_darwin_test.go ||
      test "$(sha256sum internal/store/acl_darwin.go | awk '{print $1}')" != "${ATN_FAKE_OLD_ACL_SHA256:?}" ||
      test "$(sha256sum internal/store/files_darwin_test.go | awk '{print $1}')" != "${ATN_FAKE_CURRENT_TEST_SHA256:?}"; then
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

func historicalFileSHA256(t *testing.T, spec string) string {
	t.Helper()
	output, err := exec.Command("git", "show", spec).Output()
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(output))
}

func localFileSHA256(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(contents))
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
