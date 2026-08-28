// Package install plans and applies exact-owned user-level registrations.
package install

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/hostfile"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/secrets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

var (
	ErrInvalid               = errors.New("invalid installation request")
	ErrUnavailable           = errors.New("installation unavailable")
	ErrConflict              = errors.New("installation changed; manual review required")
	ErrShellRequired         = errors.New("verified command shell required")
	ErrParentRequired        = errors.New("existing owned host parent required")
	ErrManualPackageRequired = errors.New("manual experimental package required")
)

type Options struct{ AgentID, ConfigPath, Executable, PackageRoot, CommandShell string }

// Exported fields are display-only. A copied/edited preview does not authorize
// applying to another path; only the private bound state is used by Apply.
type Plan struct {
	AgentID, TargetPath, Action string
	Experimental                bool
	state                       *planned
}
type planned struct {
	options           Options
	directory         string
	before            hostfile.Snapshot
	replacement       []byte
	action, target    string
	afterExists       bool
	noHost            bool
	recordBytes       []byte
	backupBytes       []byte
	desired, previous *receipt
	guards            []fileGuard
	guardPaths        []string
}

type fileGuard struct{ content, access string }

func guard(snapshot hostfile.Snapshot) (fileGuard, error) {
	a, err := snapshot.AccessDigest()
	return fileGuard{contentDigest(snapshot.Exists, snapshot.Data), a}, err
}

type operations struct {
	write   func(string, []byte) error
	replace func(string, hostfile.Snapshot, []byte) error
	remove  func(string, hostfile.Snapshot) error
}

func realOperations() operations {
	return operations{store.WriteAtomic, hostfile.Replace, hostfile.Remove}
}

func within(path, root string) bool {
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		root = strings.ToLower(root)
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// Called after hostfile validates the target's existing, link-free ancestry.
// Text spellings (notably Windows 8.3) are not directory identities. Only the
// data leaf may be absent, already validated as such by Repository.Open;
// absence is not used as evidence of a distinct existing directory.
func outsideInstallationRoots(target, packageRoot, directory string) error {
	var identities []os.FileInfo
	for i, root := range []string{packageRoot, directory} {
		if within(target, root) {
			return ErrInvalid
		}
		info, err := os.Lstat(root)
		if i == 1 && os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !os.SameFile(info, info) {
			return ErrInvalid
		}
		identities = append(identities, info)
	}
	for path := filepath.Dir(target); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !os.SameFile(info, info) {
			return ErrInvalid
		}
		for _, root := range identities {
			if os.SameFile(info, root) {
				return ErrInvalid
			}
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	return nil
}

func PlanInstall(ctx context.Context, repository *configuration.Repository, options Options) (Plan, error) {
	p := Plan{AgentID: options.AgentID, Action: "install", Experimental: runtime.GOOS == "darwin"}
	if ctx == nil || ctx.Err() != nil || repository == nil {
		return p, ErrInvalid
	}
	config, target, err := resolveTarget(options.AgentID, options.ConfigPath)
	p.TargetPath = target
	if err != nil {
		return p, err
	}
	options.ConfigPath = config
	if !cleanAbsolute(options.PackageRoot) || !cleanAbsolute(options.Executable) || !within(options.Executable, options.PackageRoot) || options.Executable == options.PackageRoot {
		return p, ErrInvalid
	}
	if within(target, options.PackageRoot) || within(target, repository.Directory()) {
		return p, ErrInvalid
	}
	if _, err := configuration.Open(repository.Directory(), options.PackageRoot); err != nil {
		return p, ErrInvalid
	}
	if info, err := os.Lstat(filepath.Dir(target)); os.IsNotExist(err) {
		return p, ErrParentRequired
	} else if err != nil || !info.IsDir() {
		return p, ErrUnavailable
	}
	executable, err := hostfile.Read(options.Executable, 64<<20)
	if err != nil || !executable.Exists {
		return p, ErrInvalid
	}
	executableGuard, err := guard(executable)
	clear(executable.Data)
	if err != nil {
		return p, ErrInvalid
	}
	guards := []fileGuard{executableGuard}
	guardPaths := []string{options.Executable}
	if options.AgentID != "opencode" && options.CommandShell != "posix" && options.CommandShell != "cmd" && options.CommandShell != "powershell" {
		return p, ErrShellRequired
	}
	before, err := hostfile.Read(target, strictjson.MaxBytes)
	if err != nil {
		return p, ErrUnavailable
	}
	if err := outsideInstallationRoots(target, options.PackageRoot, repository.Directory()); err != nil {
		return p, err
	}
	record, recordBytes, err := readRecord(repository, options.AgentID)
	if err != nil {
		return p, err
	}
	if r := recordTarget(record); r != nil && (r.TargetPath != target || r.ConfigPath != config) {
		return p, ErrConflict
	}
	previous, err := resolveRecord(record, before)
	if err != nil {
		return p, err
	}
	backupBytes, err := readBackup(recordTarget(record))
	if err != nil {
		return p, err
	}
	var oldEntries []ownedEntry
	if previous != nil && previous.State == "active" {
		oldEntries = previous.Entries
	}
	var entries []ownedEntry
	var replacement []byte
	if options.AgentID == "opencode" {
		locator, e := hostfile.Read(config, strictjson.MaxBytes)
		if e != nil {
			return p, ErrUnavailable
		}
		if _, e := parseHost(locator.Data, locator.Exists); e != nil {
			return p, e
		}
		locatorGuard, e := guard(locator)
		if e != nil {
			return p, ErrUnavailable
		}
		guards = append(guards, locatorGuard)
		guardPaths = append(guardPaths, config)
		bridge, e := hostfile.Read(filepath.Join(options.PackageRoot, "integrations", "opencode", "bridge.mjs"), strictjson.MaxBytes)
		if e != nil || !bridge.Exists {
			return p, ErrInvalid
		}
		bridgeGuard, e := guard(bridge)
		if e != nil {
			return p, ErrInvalid
		}
		guards = append(guards, bridgeGuard)
		guardPaths = append(guardPaths, filepath.Join(options.PackageRoot, "integrations", "opencode", "bridge.mjs"))
		if previous != nil && previous.State == "active" {
			if !before.Exists || string(before.Data) != previous.ShimText {
				return p, ErrConflict
			}
		} else if before.Exists {
			return p, ErrConflict
		}
		replacement, err = renderShim(options.Executable, options.PackageRoot, repository.Directory())
	} else {
		if options.AgentID == "codex" {
			toml, e := hostfile.Read(filepath.Join(filepath.Dir(config), "config.toml"), strictjson.MaxBytes)
			if e != nil {
				return p, ErrUnavailable
			}
			if legacyString(string(toml.Data)) {
				return p, ErrConflict
			}
			tomlGuard, e := guard(toml)
			if e != nil {
				return p, ErrUnavailable
			}
			guards = append(guards, tomlGuard)
			guardPaths = append(guardPaths, filepath.Join(filepath.Dir(config), "config.toml"))
		}
		command, e := renderCommand(options.CommandShell, options.Executable, options.AgentID, repository.Directory())
		if e != nil {
			return p, e
		}
		entries = ownedEntries(options.AgentID, command)
		replacement, err = mergeJSON(options.AgentID, before.Data, before.Exists, oldEntries, entries)
	}
	if err != nil {
		return p, err
	}
	desired := receiptFrom(options, target, repository.Directory(), entries, replacement)
	if r := recordTarget(record); r != nil {
		desired.Backup = r.Backup
	}
	p.state = &planned{options: options, directory: repository.Directory(), before: before, replacement: replacement, action: "install", target: target, afterExists: true, recordBytes: recordBytes, backupBytes: backupBytes, desired: desired, previous: previous, guards: guards, guardPaths: guardPaths}
	return p, nil
}

func ApplyInstall(ctx context.Context, repository *configuration.Repository, plan Plan) error {
	if plan.Action != "install" {
		return ErrInvalid
	}
	return applyWith(ctx, repository, plan, realOperations())
}
func PlanUninstall(ctx context.Context, repository *configuration.Repository, agentID string) (Plan, error) {
	p := Plan{AgentID: agentID, Action: "uninstall", Experimental: runtime.GOOS == "darwin"}
	if ctx == nil || ctx.Err() != nil || repository == nil {
		return p, ErrInvalid
	}
	if agentID == "workbuddy" {
		return p, ErrManualPackageRequired
	}
	record, raw, err := readRecord(repository, agentID)
	if err != nil {
		return p, err
	}
	if record == nil {
		p.state = &planned{options: Options{AgentID: agentID}, directory: repository.Directory(), action: "uninstall", noHost: true}
		return p, nil
	}
	base := recordTarget(record)
	p.TargetPath = base.TargetPath
	before, err := hostfile.Read(base.TargetPath, strictjson.MaxBytes)
	if err != nil {
		return p, ErrConflict
	}
	if err := outsideInstallationRoots(base.TargetPath, base.PackageRoot, repository.Directory()); err != nil {
		return p, err
	}
	previous, err := resolveRecord(record, before)
	if err != nil {
		return p, err
	}
	backupBytes, err := readBackup(base)
	if err != nil {
		return p, err
	}
	desired := *base
	desired.State = "inactive"
	opts := Options{AgentID: agentID, ConfigPath: base.ConfigPath, Executable: base.Executable, PackageRoot: base.PackageRoot, CommandShell: base.Renderer}
	state := &planned{options: opts, directory: repository.Directory(), before: before, replacement: before.Data, action: "uninstall", target: base.TargetPath, afterExists: before.Exists, noHost: true, recordBytes: raw, backupBytes: backupBytes, desired: &desired, previous: previous}
	if previous != nil && previous.State == "active" {
		if agentID == "opencode" {
			if !before.Exists || string(before.Data) != previous.ShimText {
				return p, ErrConflict
			}
			state.replacement = nil
			state.afterExists = false
		} else {
			state.replacement, err = mergeJSON(agentID, before.Data, before.Exists, previous.Entries, nil)
			if err != nil {
				return p, err
			}
			state.afterExists = true
		}
		state.noHost = false
	}
	p.state = state
	return p, nil
}
func ApplyUninstall(ctx context.Context, repository *configuration.Repository, plan Plan) error {
	if plan.Action != "uninstall" {
		return ErrInvalid
	}
	return applyWith(ctx, repository, plan, realOperations())
}

func validPlan(ctx context.Context, r *configuration.Repository, p Plan) bool {
	s := p.state
	return ctx != nil && ctx.Err() == nil && r != nil && s != nil && s.directory == r.Directory() && p.AgentID == s.options.AgentID && p.TargetPath == s.target && p.Action == s.action && p.Experimental == (runtime.GOOS == "darwin") && automaticAgent(p.AgentID)
}

func samePlan(a, b *planned) bool {
	if a == nil || b == nil || a.options != b.options || a.directory != b.directory || a.action != b.action || a.target != b.target || a.afterExists != b.afterExists || a.noHost != b.noHost || !bytes.Equal(a.recordBytes, b.recordBytes) || !bytes.Equal(a.backupBytes, b.backupBytes) || !bytes.Equal(a.replacement, b.replacement) || !slices.Equal(a.guards, b.guards) || !slices.Equal(a.guardPaths, b.guardPaths) {
		return false
	}
	if a.desired == nil || b.desired == nil {
		return a.desired == nil && b.desired == nil
	}
	ag, ae := guard(a.before)
	bg, be := guard(b.before)
	return ae == nil && be == nil && ag == bg
}

// Only boundary operations are injectable per invocation. Public Apply always
// uses the real private store and opaque hostfile comparison; no global hooks.
func applyWith(ctx context.Context, r *configuration.Repository, p Plan, ops operations) error {
	if !validPlan(ctx, r, p) {
		return ErrInvalid
	}
	s := p.state
	if s.desired == nil {
		record, _, err := readRecord(r, p.AgentID)
		if err != nil {
			return err
		}
		if record != nil {
			return ErrConflict
		}
		return nil
	}
	if ctx.Err() != nil || r.Prepare() != nil {
		return ErrUnavailable
	}
	// Foreground authorization must happen outside the installation lock.
	vault, err := r.Vault(ctx, secrets.Foreground)
	if err != nil {
		return ErrUnavailable
	}
	lockContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	release, err := store.Acquire(lockContext, filepath.Join(r.Directory(), "locks", "install-"+p.AgentID+".lock"))
	cancel()
	if err != nil {
		return ErrUnavailable
	}
	defer release()
	var fresh Plan
	if p.Action == "install" {
		fresh, err = PlanInstall(ctx, r, s.options)
	} else {
		fresh, err = PlanUninstall(ctx, r, p.AgentID)
	}
	if err != nil || !samePlan(s, fresh.state) {
		return ErrConflict
	}
	desired := *s.desired
	if desired.Backup != nil {
		if verifyBackup(vault, p.AgentID, desired.Backup) != nil {
			return ErrUnavailable
		}
	} else {
		desired.Backup, err = protectBackup(ctx, r, vault, p.AgentID, s.before.Exists, s.before.Data, ops.write)
		if err != nil {
			return err
		}
	}
	backupBytes, err := readBackup(&desired)
	if err != nil || (s.backupBytes != nil && !bytes.Equal(s.backupBytes, backupBytes)) {
		return ErrConflict
	}
	access, err := s.before.AccessDigest()
	if err != nil {
		return ErrUnavailable
	}
	afterAccess, err := s.before.ExpectedAccessDigests(s.afterExists)
	if err != nil {
		return ErrUnavailable
	}
	if s.noHost {
		afterAccess = []string{access}
	}
	transition := &transition{BeforeDigest: contentDigest(s.before.Exists, s.before.Data), AfterDigest: contentDigest(s.afterExists, s.replacement), BeforeAccessDigest: access, AfterAccessDigests: afterAccess, Desired: &desired, Previous: s.previous}
	pending := &receiptRecord{SchemaVersion: 1, AgentID: p.AgentID, State: "pending", Pending: transition}
	if err := recheckDependencies(ctx, r, s, s.recordBytes, &desired, backupBytes); err != nil {
		return err
	}
	if err := writeRecord(ctx, r, pending, ops.write); err != nil {
		return err
	}
	pendingBytes, err := marshalRecord(r, pending)
	if err != nil {
		return err
	}
	if err := recheckDependencies(ctx, r, s, pendingBytes, &desired, backupBytes); err != nil {
		return err
	}
	if !s.noHost {
		if s.afterExists {
			err = ops.replace(s.target, s.before, s.replacement)
		} else {
			err = ops.remove(s.target, s.before)
		}
		if err != nil {
			// In particular ErrVerification is NOT evidence of no mutation.
			// Keep pending even on precommit errors; never restore full bytes.
			if errors.Is(err, hostfile.ErrConflict) {
				return ErrConflict
			}
			return ErrUnavailable
		}
	}
	after, err := hostfile.Read(s.target, strictjson.MaxBytes)
	if err != nil {
		return ErrConflict
	}
	afterDigest, err := after.AccessDigest()
	if err != nil || contentDigest(after.Exists, after.Data) != transition.AfterDigest || !slices.Contains(transition.AfterAccessDigests, afterDigest) {
		return ErrConflict
	}
	if err := recheckDependencies(ctx, r, s, pendingBytes, &desired, backupBytes); err != nil {
		return err
	}
	return writeRecord(ctx, r, &receiptRecord{SchemaVersion: 1, AgentID: p.AgentID, State: desired.State, Receipt: &desired}, ops.write)
}

// Recheck observed dependencies at each mutation boundary. These checks do not
// claim cross-file CAS against a same-owner editor after the final check.
func recheckDependencies(ctx context.Context, r *configuration.Repository, s *planned, recordBytes []byte, desired *receipt, backupBytes []byte) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	if outsideInstallationRoots(s.target, s.options.PackageRoot, r.Directory()) != nil {
		return ErrConflict
	}
	_, actual, err := readRecord(r, s.options.AgentID)
	if err != nil || !bytes.Equal(actual, recordBytes) {
		return ErrConflict
	}
	actual, err = readBackup(desired)
	if err != nil || !bytes.Equal(actual, backupBytes) {
		return ErrConflict
	}
	for i, path := range s.guardPaths {
		limit := int64(strictjson.MaxBytes)
		if i == 0 {
			limit = 64 << 20
		}
		snapshot, err := hostfile.Read(path, limit)
		if err != nil {
			return ErrConflict
		}
		actual, err := guard(snapshot)
		clear(snapshot.Data)
		if err != nil || actual != s.guards[i] {
			return ErrConflict
		}
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}
