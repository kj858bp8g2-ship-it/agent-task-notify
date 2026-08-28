package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

const Version = "0.2.0-rc.1"

func Run(args []string, stdout, stderr io.Writer) int {
	return RunWithInput(args, os.Stdin, stdout, stderr)
}

const usage = `usage: agent-task-notify COMMAND
  version
  configure --provider bark|ntfy [--settings-file PATH] [--credential-stdin] [--data-directory PATH]
  doctor [--data-directory PATH]
  preview --agent ID [--send] [--data-directory PATH]
  install --agent ID [--config-path PATH] [--command-shell posix|cmd|powershell] [--apply] [--data-directory PATH]
  uninstall --agent ID [--apply] [--data-directory PATH]
Credentials are entered locally; never paste credentials into agent chat.
`

// RunWithInput makes nonterminal input an explicit caller-owned boundary.
// Version/help and invalid arguments do not inspect input or open storage.
func RunWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "version":
			fmt.Fprintf(stdout, "agent-task-notify %s %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)
			return 0
		case "help", "--help", "-h":
			fmt.Fprint(stdout, usage)
			return 0
		}
	}
	options, valid := parseCommand(args)
	if len(args) > 0 && args[0] == "hook" {
		return runHook(options, valid, stdin, stdout)
	}
	if !valid {
		if len(args) == 0 || args[0] != "worker" {
			fmt.Fprint(stderr, usage)
		}
		return 2
	}
	return runCommand(options, stdin, stdout, stderr)
}
