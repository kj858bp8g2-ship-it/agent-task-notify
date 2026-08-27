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
	moduleRoot := filepath.Dir(mustGetwd(t))
	before := configurationFiles(t, moduleRoot)
	temp := t.TempDir()
	binaryName := "通知 工具"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(temp, binaryName)

	build := exec.Command("go", "build", "-trimpath", "-o", binaryPath, "./cmd/agent-task-notify")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native command: %v\n%s", err, output)
	}

	emptyPath := filepath.Join(temp, "empty-path")
	if err := os.Mkdir(emptyPath, 0o700); err != nil {
		t.Fatalf("create empty PATH directory: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := exec.CommandContext(ctx, binaryPath, "version")
	run.Env = append(withoutPath(os.Environ()), "PATH="+emptyPath)
	var versionStderr bytes.Buffer
	run.Stderr = &versionStderr
	output, err := run.Output()
	if err != nil {
		t.Fatalf("run version command: %v", err)
	}
	if got, want := string(output), "agent-task-notify 0.2.0-dev "+runtime.GOOS+"/"+runtime.GOARCH+"\n"; got != want {
		t.Fatalf("version stdout = %q, want %q", got, want)
	}
	if got := versionStderr.String(); got != "" {
		t.Fatalf("version stderr = %q, want empty", got)
	}

	unknown := exec.CommandContext(ctx, binaryPath, "synthetic-sensitive-value")
	unknown.Env = append(withoutPath(os.Environ()), "PATH="+emptyPath)
	var unknownStderr bytes.Buffer
	unknown.Stderr = &unknownStderr
	unknownOutput, unknownErr := unknown.Output()
	if exitErr, ok := unknownErr.(*exec.ExitError); !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("unknown command error = %v, want exit code 2", unknownErr)
	}
	if got := string(unknownOutput); got != "" {
		t.Fatalf("unknown command stdout = %q, want empty", got)
	}
	if got, want := unknownStderr.String(), "usage: agent-task-notify version\n"; got != want {
		t.Fatalf("unknown command stderr = %q, want %q", got, want)
	}

	if after := configurationFiles(t, moduleRoot); !slices.Equal(before, after) {
		t.Fatalf("native command changed configuration files: before %q, after %q", before, after)
	}
}

func withoutPath(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, "PATH") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func configurationFiles(t *testing.T, moduleRoot string) []string {
	t.Helper()
	var files []string
	configRoot := filepath.Join(moduleRoot, "config")
	if err := filepath.WalkDir(configRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("list configuration files: %v", err)
	}
	slices.Sort(files)
	return files
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return workingDirectory
}
