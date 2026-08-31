package tests

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNativeCLIWithoutLanguageRuntime(t *testing.T) {
	temp := t.TempDir()
	binaryPath := nativeExecutable(t)

	emptyPath := filepath.Join(temp, "empty-path")
	if err := os.Mkdir(emptyPath, 0o700); err != nil {
		t.Fatalf("create empty PATH directory: %v", err)
	}
	runtimeRoot := filepath.Join(temp, "synthetic-runtime")
	workingDirectory := filepath.Join(runtimeRoot, "working-directory")
	if err := os.MkdirAll(workingDirectory, 0o700); err != nil {
		t.Fatalf("create synthetic runtime directory: %v", err)
	}
	before := runtimeEntries(t, runtimeRoot)
	commandEnvironment := nativeCommandEnvironment(emptyPath, runtimeRoot)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := exec.CommandContext(ctx, binaryPath, "version")
	run.Dir = workingDirectory
	run.Env = commandEnvironment
	var versionStderr bytes.Buffer
	run.Stderr = &versionStderr
	output, err := run.Output()
	if err != nil {
		t.Fatalf("run version command: %v", err)
	}
	if got, want := string(output), "agent-task-notify 0.2.0-rc.2 "+runtime.GOOS+"/"+runtime.GOARCH+"\n"; got != want {
		t.Fatalf("version stdout = %q, want %q", got, want)
	}
	if got := versionStderr.String(); got != "" {
		t.Fatalf("version stderr = %q, want empty", got)
	}
	if after := runtimeEntries(t, runtimeRoot); !slices.Equal(before, after) {
		t.Fatalf("version command changed synthetic runtime entries: before %q, after %q", before, after)
	}

	unknown := exec.CommandContext(ctx, binaryPath, "synthetic-sensitive-value")
	unknown.Dir = workingDirectory
	unknown.Env = commandEnvironment
	var unknownStderr bytes.Buffer
	unknown.Stderr = &unknownStderr
	unknownOutput, unknownErr := unknown.Output()
	if exitErr, ok := unknownErr.(*exec.ExitError); !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("unknown command error = %v, want exit code 2", unknownErr)
	}
	if got := string(unknownOutput); got != "" {
		t.Fatalf("unknown command stdout = %q, want empty", got)
	}
	if got := unknownStderr.String(); !strings.HasPrefix(got, "usage: agent-task-notify COMMAND\n") || !strings.Contains(got, "configure --provider bark|ntfy") || strings.Contains(got, "synthetic-sensitive-value") {
		t.Fatal("unknown command usage was incomplete or unsafe")
	}
	if after := runtimeEntries(t, runtimeRoot); !slices.Equal(before, after) {
		t.Fatalf("unknown command changed synthetic runtime entries: before %q, after %q", before, after)
	}
}

func withoutPath(environment []string) []string {
	return withoutEnvironmentNames(environment, "PATH")
}

func nativeCommandEnvironment(emptyPath, runtimeRoot string) []string {
	environment := withoutEnvironmentNames(withoutPath(os.Environ()), "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "ATN_DATA_DIRECTORY")
	return append(environment,
		"PATH="+emptyPath,
		"HOME="+filepath.Join(runtimeRoot, "home"),
		"USERPROFILE="+filepath.Join(runtimeRoot, "user-profile"),
		"APPDATA="+filepath.Join(runtimeRoot, "app-data"),
		"LOCALAPPDATA="+filepath.Join(runtimeRoot, "local-app-data"),
		"ATN_DATA_DIRECTORY="+filepath.Join(runtimeRoot, "atn-data"),
	)
}

func withoutEnvironmentNames(environment []string, names ...string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && containsFold(names, name) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func containsFold(names []string, name string) bool {
	for _, candidate := range names {
		if strings.EqualFold(candidate, name) {
			return true
		}
	}
	return false
}

func runtimeEntries(t *testing.T, runtimeRoot string) []string {
	t.Helper()
	var entries []string
	if err := filepath.WalkDir(runtimeRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(runtimeRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			relative += "/"
		}
		entries = append(entries, relative)
		return nil
	}); err != nil {
		t.Fatalf("list synthetic runtime entries: %v", err)
	}
	slices.Sort(entries)
	return entries
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return workingDirectory
}
