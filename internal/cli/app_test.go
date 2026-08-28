package cli

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestVersionAndUnknownArguments(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{
			name:     "version",
			args:     []string{"version"},
			wantCode: 0,
			wantOut:  "agent-task-notify 0.2.0-dev " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
			wantErr:  "",
		},
		{
			name:     "no arguments",
			args:     nil,
			wantCode: 2,
			wantOut:  "",
			wantErr:  testUsage,
		},
		{
			name:     "unknown argument",
			args:     []string{"synthetic-sensitive-value"},
			wantCode: 2,
			wantOut:  "",
			wantErr:  testUsage,
		},
		{
			name:     "extra argument",
			args:     []string{"version", "extra"},
			wantCode: 2,
			wantOut:  "",
			wantErr:  testUsage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errout bytes.Buffer
			if code := Run(tt.args, &out, &errout); code != tt.wantCode || out.String() != tt.wantOut || errout.String() != tt.wantErr {
				t.Fatalf("Run(%q) = code %d, stdout %q, stderr %q; want code %d, stdout %q, stderr %q", tt.args, code, out.String(), errout.String(), tt.wantCode, tt.wantOut, tt.wantErr)
			}
		})
	}
}

func TestUnknownArgumentsDoNotEcho(t *testing.T) {
	var out, errout bytes.Buffer
	code := Run([]string{"synthetic-sensitive-value"}, &out, &errout)
	if code != 2 || out.String() != "" || errout.String() != testUsage || strings.Contains(errout.String(), "synthetic-sensitive-value") {
		t.Fatalf("unsafe command response: code=%d", code)
	}
}

const testUsage = `usage: agent-task-notify COMMAND
  version
  configure --provider bark|ntfy [--settings-file PATH] [--credential-stdin] [--data-directory PATH]
  doctor [--data-directory PATH]
  preview --agent ID [--send] [--data-directory PATH]
  install --agent ID [--config-path PATH] [--command-shell posix|cmd|powershell] [--apply] [--data-directory PATH]
  uninstall --agent ID [--apply] [--data-directory PATH]
Credentials are entered locally; never paste credentials into agent chat.
`
