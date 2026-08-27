package main

import (
	"os"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
