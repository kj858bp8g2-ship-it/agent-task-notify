package cli

import (
	"fmt"
	"io"
	"runtime"
)

const Version = "0.2.0-dev"

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "version" {
		fmt.Fprintln(stderr, "usage: agent-task-notify version")
		return 2
	}

	fmt.Fprintf(stdout, "agent-task-notify %s %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)
	return 0
}
