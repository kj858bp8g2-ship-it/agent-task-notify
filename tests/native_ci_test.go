package tests

import (
	"context"
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
		"CI":                     "true",
		"RUNNER_TEMP":            runnerTemp,
		"ATN_TEST_FAKE_SECURITY": "1",
		"ATN_TEST_SECURITY_BIN":  filepath.ToSlash(fake),
		"ATN_FAKE_SECURITY_LOG":  filepath.ToSlash(log),
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
	return filepath.Join("..", "scripts", "native-ci-macos.sh")
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
		case "CI", "RUNNER_TEMP", "ATN_TEST_FAKE_SECURITY", "ATN_TEST_SECURITY_BIN", "ATN_FAKE_SECURITY_LOG":
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
    elif [[ "$*" == *synthetic.keychain-db ]]; then
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
