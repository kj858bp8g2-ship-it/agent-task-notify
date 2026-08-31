package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/adapters"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/install"
	notify "github.com/kj858bp8g2-ship-it/agent-task-notify/internal/runtime"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

type commandOptions struct {
	command, agent, provider, directory, settingsFile, configPath, shell, job string
	credentialStdin, send, apply                                              bool
}

// No general flag parser: its default diagnostics can echo sensitive argv.
// Each command accepts only the exact, separate public/internal flag tokens.
func parseCommand(args []string) (commandOptions, bool) {
	var o commandOptions
	if len(args) == 0 {
		return o, false
	}
	o.command = args[0]
	allowed := map[string]bool{"--data-directory": false}
	switch o.command {
	case "configure":
		allowed["--provider"] = false
		allowed["--settings-file"] = false
		allowed["--credential-stdin"] = true
	case "doctor":
	case "preview":
		allowed["--agent"] = false
		allowed["--send"] = true
	case "install":
		allowed["--agent"] = false
		allowed["--config-path"] = false
		allowed["--command-shell"] = false
		allowed["--apply"] = true
	case "uninstall":
		allowed["--agent"] = false
		allowed["--apply"] = true
	case "hook":
		allowed["--agent"] = false
	case "worker":
		allowed["--job"] = false
	default:
		return o, false
	}
	seen := make(map[string]bool)
	for i := 1; i < len(args); i++ {
		flag := args[i]
		boolean, ok := allowed[flag]
		if !ok || seen[flag] {
			return o, false
		}
		seen[flag] = true
		value := ""
		if !boolean {
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "--") {
				return o, false
			}
			value = args[i]
		}
		switch flag {
		case "--agent":
			o.agent = value
		case "--provider":
			o.provider = value
		case "--settings-file":
			o.settingsFile = value
		case "--data-directory":
			o.directory = value
		case "--config-path":
			o.configPath = value
		case "--command-shell":
			o.shell = value
		case "--job":
			o.job = value
		case "--credential-stdin":
			o.credentialStdin = true
		case "--send":
			o.send = true
		case "--apply":
			o.apply = true
		}
	}
	if _, needsAgent := allowed["--agent"]; needsAgent {
		if _, err := core.AgentByID(o.agent); err != nil {
			return o, false
		}
	}
	if o.command == "configure" && o.provider != "bark" && o.provider != "ntfy" {
		return o, false
	}
	if o.shell != "" && o.shell != "posix" && o.shell != "cmd" && o.shell != "powershell" {
		return o, false
	}
	if o.command == "worker" && (o.directory == "" || !core.ValidKey(o.job)) {
		return o, false
	}
	return o, true
}

func openRuntime(o commandOptions) (*notify.Runtime, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	r, err := configuration.Open(o.directory, filepath.Dir(exe))
	if err != nil {
		return nil, err
	}
	return notify.New(r, exe), nil
}

func runHook(o commandOptions, valid bool, stdin io.Reader, stdout io.Writer) int {
	var data []byte
	defer func() { fmt.Fprintln(stdout, string(adapters.Neutral(o.agent, data))); clear(data) }()
	if !valid {
		return 0
	}
	var err error
	data, err = readBounded(stdin)
	r, openErr := openRuntime(o)
	if openErr != nil {
		return 0
	}
	ctx := context.Background()
	if err != nil {
		_ = r.RecordInputError(ctx)
		return 0
	}
	event, accepted, err := adapters.Normalize(o.agent, data)
	if err != nil {
		_ = r.RecordInputError(ctx)
		return 0
	}
	if accepted {
		_, _ = r.Handle(ctx, event)
	}
	return 0
}

func runCommand(o commandOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	r, err := openRuntime(o)
	if err != nil {
		return commandFailure(o, stderr)
	}
	ctx := context.Background()
	switch o.command {
	case "worker":
		// Cooperative deadline: no claim that synchronous OS calls can be killed.
		ctx, cancel := context.WithTimeout(ctx, 240*time.Second)
		defer cancel()
		if r.RunJob(ctx, o.job) != nil {
			return 1
		}
		return 0
	case "configure":
		return configure(o, r.Repository, stdin, stdout, stderr)
	case "doctor":
		d, err := r.Inspect(ctx)
		if err != nil {
			return commandFailure(o, stderr)
		}
		if json.NewEncoder(stdout).Encode(d) != nil {
			return 1
		}
		return 0
	case "preview":
		settings, _, err := r.Repository.View()
		if err != nil {
			return commandFailure(o, stderr)
		}
		result, err := r.Preview(ctx, o.agent, o.send)
		if err != nil {
			return commandFailure(o, stderr)
		}
		if o.send {
			if !result.Queued {
				return commandFailure(o, stderr)
			}
			fmt.Fprintln(stdout, "notification queued; delivery not confirmed")
			return 0
		}
		view := struct {
			Provider     string `json:"provider"`
			Agent        string `json:"agent"`
			RingTarget   int    `json:"ringTargetSeconds"`
			Continuous   bool   `json:"continuous"`
			Experimental bool   `json:"experimental"`
		}{settings.Provider, o.agent, settings.MediumRingSeconds, settings.Continuous, runtime.GOOS == "darwin" || o.agent == "workbuddy"}
		if json.NewEncoder(stdout).Encode(view) != nil {
			return 1
		}
		return 0
	case "install", "uninstall":
		return runInstall(o, r, stdout, stderr)
	}
	return commandFailure(o, stderr)
}

func commandFailure(o commandOptions, stderr io.Writer) int {
	if o.command != "worker" {
		fmt.Fprintln(stderr, "operation unavailable; check local configuration and permissions")
	}
	return 1
}

func configure(o commandOptions, r *configuration.Repository, stdin io.Reader, stdout, stderr io.Writer) int {
	patch := []byte(`{}`)
	if o.settingsFile != "" {
		file, err := os.Open(o.settingsFile)
		if err != nil {
			return commandFailure(o, stderr)
		}
		patch, err = readBounded(file)
		_ = file.Close()
		if err != nil {
			return invalidConfiguration(stderr)
		}
	}
	// Validate against the current nonsecret settings BEFORE input/Prepare.
	base, _, err := r.View()
	if err != nil {
		return commandFailure(o, stderr)
	}
	base.Provider = o.provider
	settings, err := core.ParseSettings(patch, base)
	if err != nil || settings.Provider != o.provider {
		return invalidConfiguration(stderr)
	}
	credential, err := readCredential(o.provider, stdin, stderr, o.credentialStdin, terminalReader(stdin))
	if err != nil {
		return invalidConfiguration(stderr)
	}
	if r.Configure(context.Background(), o.provider, credential, patch) != nil {
		return commandFailure(o, stderr)
	}
	fmt.Fprintln(stdout, "notification provider configured locally")
	return 0
}

func invalidConfiguration(stderr io.Writer) int {
	fmt.Fprintln(stderr, "invalid local configuration or input; use a terminal or explicit --credential-stdin")
	return 2
}

func runInstall(o commandOptions, r *notify.Runtime, stdout, stderr io.Writer) int {
	ctx := context.Background()
	var plan install.Plan
	var err error
	if o.command == "install" {
		plan, err = install.PlanInstall(ctx, r.Repository, install.Options{AgentID: o.agent, ConfigPath: o.configPath, Executable: r.Executable, PackageRoot: filepath.Dir(r.Executable), CommandShell: o.shell})
	} else {
		plan, err = install.PlanUninstall(ctx, r.Repository, o.agent)
	}
	if err != nil && !errors.Is(err, install.ErrParentRequired) {
		switch {
		case errors.Is(err, install.ErrManualPackageRequired):
			fmt.Fprintf(stderr, "%s requires its manual experimental integration package\n", o.agent)
		case errors.Is(err, install.ErrShellRequired):
			fmt.Fprintln(stderr, "select the host's verified command shell with --command-shell")
		default:
			return commandFailure(o, stderr)
		}
		return 1
	}
	// Display even a missing-parent plan, but never apply its incomplete state.
	view := struct {
		Action       string `json:"action"`
		Agent        string `json:"agent"`
		Target       string `json:"target"`
		Experimental bool   `json:"experimental"`
	}{plan.Action, plan.AgentID, plan.TargetPath, plan.Experimental}
	if json.NewEncoder(stdout).Encode(view) != nil {
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "existing owned host parent required; create and verify it locally before installation")
		return 1
	}
	if !o.apply {
		return 0
	}
	if o.command == "install" {
		err = install.ApplyInstall(ctx, r.Repository, plan)
	} else {
		err = install.ApplyUninstall(ctx, r.Repository, plan)
	}
	if err != nil {
		return commandFailure(o, stderr)
	}
	return 0
}

var errInput = errors.New("invalid local input")

func readBounded(input io.Reader) ([]byte, error) {
	if input == nil {
		return nil, errInput
	}
	data, err := io.ReadAll(io.LimitReader(input, strictjson.MaxBytes+1))
	if err != nil || len(data) > strictjson.MaxBytes {
		clear(data)
		return nil, errInput
	}
	return data, nil
}
