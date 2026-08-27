package cli

import (
	"bytes"
	"runtime"
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
			wantErr:  "usage: agent-task-notify version\n",
		},
		{
			name:     "unknown argument",
			args:     []string{"synthetic-sensitive-value"},
			wantCode: 2,
			wantOut:  "",
			wantErr:  "usage: agent-task-notify version\n",
		},
		{
			name:     "extra argument",
			args:     []string{"version", "extra"},
			wantCode: 2,
			wantOut:  "",
			wantErr:  "usage: agent-task-notify version\n",
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
	if code != 2 || out.String() != "" || errout.String() != "usage: agent-task-notify version\n" {
		t.Fatalf("unsafe command response: code=%d", code)
	}
}
