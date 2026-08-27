# Native Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the approved native-runtime design's three platform gates before migrating the existing notification product.

**Architecture:** Add a small Go executable alongside the unchanged PowerShell release. Separate protected storage, private files/locks, detached processes, and bounded delivery scheduling. Use real OS integration and loopback-only tests; enable no installed hooks.

**Tech Stack:** Go 1.27.0; golang.org/x/sys v0.47.0; github.com/keybase/go-keychain v0.0.1 (MIT, Darwin only), with a narrow Security.framework interaction-control bridge; standard-library HTTP, JSON and AES-GCM.

**Spec:** `docs/superpowers/specs/2026-08-28-native-runtime-design.md`

## Global Constraints

- In the same repository; preserve the legacy implementation and do not alter any real installed notifier, host configuration, or credential. Tests use synthetic data and loopback servers only.
- Windows x64 and macOS Intel/Apple Silicon. End users do not install PowerShell, Node.js, Python or Go for the native notification tool.
- Mac is experimental. No signing purchases, quarantine removal, Gatekeeper changes, or claims of real phone/desktop-agent verification.
- Secrets and original configuration backups must be encrypted. No plaintext fallback, secret command-line arguments, full endpoint URLs, raw response bodies, or underlying exception text in diagnostics.
- Default task thresholds remain 1800/3600 seconds; Bark target ringing 45/60 seconds, sound alarm. Main delivery has at most five attempts with additional delays 5/15/30/60 seconds; one extension only. These defaults are not changed by this foundation plan.
- All source changes use TDD with recorded RED and GREEN evidence. Source artifacts are explicitly allowlisted; test reports, credentials, generated binaries and local paths are not public source.
- This plan delivers platform foundations, not a functioning six-agent replacement. After these gates pass, execute a separate migration plan for adapters, configuration/providers, persisted runtime, installer, packaging, Skill and documentation. Do not label this phase a completed product upgrade.

## File map and ordering

| Task | Responsibility | Files |
| --- | --- | --- |
| 1 | Auditable Go scaffold, safe version command, exact source manifest | `go.mod`, `go.sum`, `cmd/agent-task-notify/main.go`, `internal/cli/app.go`, `internal/cli/app_test.go`, `tests/native_cli_test.go`, `config/native-source-files.json`, `scripts/Test-Release.ps1`, `.gitignore` |
| 2 | Native credential protection and non-interactive Keychain access | `internal/secrets/vault.go`, `envelope.go`, `native_windows.go`, `native_darwin.go`, `interaction_darwin.go`, `vault_test.go`, `native_windows_test.go`, `native_darwin_test.go`; `THIRD_PARTY_NOTICES.md`, dependency files and source manifest |
| 3 | Private atomic files, cross-process locks, detached workers, bounded schedule | `internal/store/files.go`, `files_windows.go`, `files_darwin.go`, `lock.go`, `lock_windows.go`, `lock_darwin.go`, `files_test.go`, `files_windows_test.go`, `files_darwin_test.go`, `lock_test.go`; `internal/worker/spawn.go`, `spawn_windows.go`, `spawn_darwin.go`, `delivery.go`, `spawn_test.go`, `delivery_test.go`; source manifest |
| 4 | Platform CI and explicit gate evidence, not public stable release | `.github/workflows/native.yml`, `scripts/native-ci-macos.sh`, `docs/native-validation.md`; source manifest |

Package paths below are relative to the repository; filenames within a package in this map remain in that package. No additional commands, GUI, service, telemetry, providers, adapters, or installation behavior belong to this foundation phase. If a listed file needs a responsibility split, ask the controller for a recorded plan amendment before creating extra files.

### Task 1: Standalone entry point and strict source inventory

**Files:** Create `go.mod`, `cmd/agent-task-notify/main.go`, `internal/cli/app.go`, `internal/cli/app_test.go`, `tests/native_cli_test.go`, `config/native-source-files.json`. Modify `scripts/Test-Release.ps1`, `tests/Test-Distribution.ps1`, `.gitignore`. `go.sum` is added when Task 2 adds dependencies, not as an empty file.

**Interfaces:**

```go
// internal/cli
func Run(args []string, stdout, stderr io.Writer) int
const Version = "0.2.0-dev"
```

The main package only passes `os.Args[1:]`, stdout and stderr to Run and exits with its result. `version` outputs `agent-task-notify 0.2.0-dev <GOOS>/<GOARCH>\n`. No arguments or any other argument sequence returns exit 2, empty stdout, and `usage: agent-task-notify version\n` to stderr, without echoing input. No files, credentials, settings, network or subprocesses are touched by Run.

- [ ] Write `TestVersionAndUnknownArguments` as a table-driven test of Run, including an unknown argument containing `synthetic-sensitive-value`. The success check is literal for the name/version prefix plus runtime platform; reject-command checks compare exact empty stdout and exact static usage output.

```go
func TestUnknownArgumentsDoNotEcho(t *testing.T) {
    var out, errout bytes.Buffer
    code := Run([]string{"synthetic-sensitive-value"}, &out, &errout)
    if code != 2 || out.String() != "" || errout.String() != "usage: agent-task-notify version\n" {
        t.Fatalf("unsafe command response: code=%d", code)
    }
}
```

- [ ] Create only the module declaration (`github.com/kj858bp8g2-ship-it/agent-task-notify`, Go 1.27.0), run `go test ./internal/cli`, and capture the missing-Run failure. Then implement Run with this dispatch and the exact version formatting above:

```go
if len(args) != 1 || args[0] != "version" {
    fmt.Fprintln(stderr, "usage: agent-task-notify version")
    return 2
}
fmt.Fprintf(stdout, "agent-task-notify %s %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)
return 0
```

- [ ] Add `TestNativeCLIWithoutLanguageRuntime` in the `tests` Go package. Build the command with `go build -trimpath` into a `t.TempDir()` child named `通知 工具`. Invoke the absolute executable with PATH set to a new empty directory, stdin closed, and a five-second context. Assert exit zero, exact version output, empty stderr, and no new runtime/configuration files. Invoke an unknown command and assert exit 2 with no input echoed. Build uses the original test process PATH; only the resulting executable's PATH is empty. The executable suffix is `.exe` only on Windows. Keep SystemRoot and other essential Windows OS environment fields.

```go
run := exec.CommandContext(ctx, binaryPath, "version")
// Filter every case-insensitive PATH entry before appending the empty-directory PATH.
run.Env = append(withoutPath(os.Environ()), "PATH="+emptyDirectory)
```

`withoutPath` belongs only to the test file. Capture a RED run before creating the command main, then GREEN after it exists.

- [ ] Evolve the old release scanner without weakening it: append an explicit JSON string-array inventory loaded from `config/native-source-files.json` to its existing list. Every native entry must be a nonempty relative slash path, have no backslash/absolute/`.`/`..` segment, and not duplicate another allowed entry. The native manifest itself and the approved design/this plan are named in the old fixed list. All listed files remain required, all actual unlisted files remain rejected, and all existing privacy scans continue. Do not use wildcard allowances for `internal`, `docs`, or `tests`. Add only files actually present at this task; later tasks extend the same manifest. Ignore private `agent_memory`, generated native binaries, and Go cache only through the existing separation of public source versus build output; never allow them as release files.

```powershell
$nativeManifest = Join-Path $root 'config/native-source-files.json'
$nativeFiles = @(Get-Content -LiteralPath $nativeManifest -Raw | ConvertFrom-Json)
foreach ($entry in $nativeFiles) {
    if ($entry -isnot [string] -or [string]::IsNullOrWhiteSpace($entry) -or
        $entry -match '(^/|\\|:|(^|/)\.\.?(/|$))' -or $entry -in $allowed) {
        throw 'Invalid native release inventory entry.'
    }
    $allowed += $entry
}
```

The source scanner currently rejects the newly tracked design file before any runtime code exists; record this known baseline failure and verify it disappears with exact inventory entries, not with a broad exclusion. Add inventory regression cases in the existing distribution test only if needed to prove malformed/duplicate entries are rejected, restoring the staged manifest between cases.

- [ ] Run `go test ./internal/cli ./tests`, `go vet ./...`, and the existing `tests/Run.Tests.ps1` on Windows. No native credentials or hooks are used. Commit with `feat: add standalone native command scaffold` and report exact commands/results and the restored legacy baseline.

### Task 2: Native protected envelopes and non-interactive access

**Files:** Create `internal/secrets/vault.go`, `internal/secrets/envelope.go`, `internal/secrets/native_windows.go`, `internal/secrets/native_darwin.go`, `internal/secrets/interaction_darwin.go`, `internal/secrets/vault_test.go`, `internal/secrets/native_windows_test.go`, and `internal/secrets/native_darwin_test.go`; modify `go.mod`, add `go.sum`, extend `config/native-source-files.json`, update `THIRD_PARTY_NOTICES.md`.

**Interfaces:**

```go
type AccessMode uint8
const (Foreground AccessMode = iota; Background)
var ErrInvalid, ErrUnavailable, ErrIntegrity error // static safe errors
type Vault struct { /* unexported backend and installation identity */ }
func Open(installationID string, mode AccessMode) (*Vault, error)
func (v *Vault) Protect(purpose string, plaintext []byte) ([]byte, error)
func (v *Vault) Unprotect(purpose string, envelope []byte) ([]byte, error)
```

Installation IDs are exactly 32 lowercase hexadecimal characters. Purposes are `credential:bark`, `credential:ntfy`, or `backup:` plus one of the six existing agent IDs (`codex`, `claude-code`, `cursor`, `gemini-cli`, `opencode`, `workbuddy`). Plaintext is limited to 4 MiB; envelope to 6 MiB. Reject unknown modes, invalid IDs/purposes, malformed/duplicate/trailing JSON, unsupported versions/backends and mismatched identity/purpose with safe errors. Do not embed caller data in errors.

The JSON envelope fields are `schemaVersion` (1), `backend` (`dpapi` or `keychain-aes-gcm`), `installationId`, `purpose`, and base64 `ciphertext`. For AES-GCM ciphertext contains nonce followed by sealed bytes. Bind the canonical JSON array `["agent-task-notify",1,installationID,purpose,backend]` as DPAPI optional entropy or AES-GCM additional authenticated data. Do not bind filesystem paths: moving an encrypted staging file must not break decryption.

- [ ] Write roundtrip/tamper/scope tests before implementation. Use an installation ID generated from `crypto/rand` and synthetic bytes only. The test case below must exercise the real platform backend, not a mock; cleanup Mac test namespace using the exact synthetic service/account only.

```go
plain := []byte("synthetic notification credential 中文")
v, err := Open(id, Foreground)
if err != nil { t.Fatal("synthetic vault unavailable") }
sealed, err := v.Protect("credential:bark", plain)
if err != nil || bytes.Contains(sealed, plain) { t.Fatal("not protected") }
got, err := v.Unprotect("credential:bark", sealed)
if err != nil || !bytes.Equal(got, plain) { t.Fatal("roundtrip failed") }
if _, err := v.Unprotect("credential:ntfy", sealed); err == nil { t.Fatal("purpose was not bound") }
```

Additional named tests: separate Protect calls produce different ciphertext; changed ciphertext and installationId are rejected; copied envelope still opens from a different staging path; wrong/duplicate fields and sizes fail; no plaintext in error strings. Native tests must never discover/read preexisting user keychain entries. Compile RED first with missing Open/Protect, then implement common envelope validation.

- [ ] Pin `golang.org/x/sys v0.47.0` and `github.com/keybase/go-keychain v0.0.1`; inspect downloaded native code and licenses. Run `go mod tidy` and `go mod verify`. Add third-party notices for actually linked dependencies and document test-only transitive dependencies separately; no dependency updates outside this plan.

- [ ] Implement Windows using `windows.CryptProtectData`, `windows.CryptUnprotectData`, `windows.DataBlob`, `CRYPTPROTECT_UI_FORBIDDEN`, CurrentUser scope (no LOCAL_MACHINE), and `windows.LocalFree`. Copy returned memory before freeing it, retain Go input/entropy references until the call completes, reject empty/oversized native output, and zero temporary buffers where practical. Return only the three static package errors.

```go
var output windows.DataBlob
err := windows.CryptProtectData(&input, nil, &entropy, 0, nil,
    windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
if err != nil { return nil, ErrUnavailable }
// Copy output into Go-owned bytes, then LocalFree the original native allocation.
```

- [ ] Implement Darwin using pinned go-keychain for exact generic-password lookup/add. Service is `agenttasknotify.native.v1`; account is the validated installation ID. Store one random 32-byte DEK, explicitly `SynchronizableNo`. Background Open only reads and never creates an item. Foreground creates if absent; concurrent duplicate creation must requery the existing key rather than replace it. Common AES-GCM encryption uses the DEK in memory; ordinary files contain only envelopes. Keychain failure or wrong key length fails closed.

```go
item := keychain.NewItem()
item.SetSecClass(keychain.SecClassGenericPassword)
item.SetService("agenttasknotify.native.v1")
item.SetAccount(id)
item.SetSynchronizable(keychain.SynchronizableNo)
item.SetMatchLimit(keychain.MatchLimitOne)
item.SetReturnData(true)
```

Use a narrow Darwin cgo bridge around `SecKeychainGetUserInteractionAllowed`/`SecKeychainSetUserInteractionAllowed`: serialize all package Keychain operations with a mutex; save existing interaction setting, force false for Background, restore it before unlock, and surface bridge failure safely. Foreground does not override a preexisting disabled setting. No secret passes through any command line and no `security` subprocess occurs in production. Review the interaction APIs against SDK headers; if the library/bridge cannot satisfy the fail-closed gate on a tested Mac runner, stop Mac migration and report rather than introduce plaintext or external CLI fallback.

- [ ] Add platform tests. Windows runs synthetic real DPAPI cases. Darwin runs the same synthetic cases, missing-account Background failure, and background reopening of the already-created synthetic account. A CI-only locked-Keychain test requires `CI=true`, `ATN_TEST_KEYCHAIN` and a fixture path within `RUNNER_TEMP`, so it can lock only the dedicated temporary CI keychain, never a user's login keychain. In that test, use the absolute `/usr/bin/security` command only to lock/unlock the synthetic fixture, with synthetic test password; actual credential reads still use production Vault. Assert Background Open fails within five seconds and creates no replacement; after unlock the original envelope still opens. CI prepares the dedicated keychain in Task 4. Non-CI runs explicitly skip this one destructive-to-fixture test and report the skip, not a passed denial gate.

- [ ] Run focused RED/GREEN Windows tests, all Go tests once, `go vet ./...`, `go mod verify`, and the release scan. Mac execution awaits Task 4; no Windows result is labeled Mac verification. Commit `feat: protect native credentials with OS key storage` and record the exact unverified Mac gate.

### Task 3: Private state primitives and detached bounded delivery

**Files:** Create `internal/store/files.go`, `internal/store/files_windows.go`, `internal/store/files_darwin.go`, `internal/store/lock.go`, `internal/store/lock_windows.go`, `internal/store/lock_darwin.go`, `internal/store/files_test.go`, `internal/store/files_windows_test.go`, `internal/store/files_darwin_test.go`, `internal/store/lock_test.go`, `internal/worker/spawn.go`, `internal/worker/spawn_windows.go`, `internal/worker/spawn_darwin.go`, `internal/worker/delivery.go`, `internal/worker/spawn_test.go`, and `internal/worker/delivery_test.go`; extend the native source manifest. Platform-specific permission tests independently inspect the real Windows DACL or Darwin modes without putting testing helpers in production files. No CLI worker command or installed hook is enabled in this task; tests use a helper branch of the compiled test executable to exercise the real spawning primitive.

**Interfaces:**

```go
// internal/store: callers use an explicit owned private directory, not a default user path.
func EnsurePrivateDirectory(path string) error
func WriteAtomic(path string, data []byte) error
func Acquire(ctx context.Context, lockPath string) (release func() error, err error)
// internal/worker
func SpawnWorker(executable, dataDirectory, jobKey string) error
type Result struct { Accepted, Retryable bool }
type Report struct { MainAttempts int; Accepted, ExtensionAttempted, ExtensionAccepted bool }
type Attempt func(context.Context, bool) Result // bool is extension
type Sleep func(context.Context, time.Duration) error
func Deliver(ctx context.Context, ringSeconds int, continuous bool, send Attempt, sleep Sleep) Report
```

`SpawnWorker` requires absolute executable/data paths and a 64-character lowercase hexadecimal job key. It supplies only `worker --data-directory <absolute-path> --job <key>`, never arbitrary commands. stdin/stdout/stderr are the null device. Native child attributes detach from the console/session, hide Windows windows, and do not inherit hook pipes. Release the process handle after a successful start; a spawn failure returns a static safe error. Do not use a shell, PowerShell, Python, Node or `Start-Process`.

- [ ] Write private-file tests: atomic replacement returns complete old or new data under concurrent readers; missing parent and symlink/reparse targets fail without overwriting another file; Windows DACL is protected for the current user plus SYSTEM, Darwin directory/file modes are 0700/0600. Do not chmod/chown a caller's existing nonempty unrelated directory. Only create/secure a new empty directory or accept an existing directory that already meets the private-access requirement.

```go
if err := WriteAtomic(path, []byte("first")); err != nil { t.Fatal(err) }
if err := WriteAtomic(path, []byte("second")); err != nil { t.Fatal(err) }
got, err := os.ReadFile(path)
if err != nil || string(got) != "second" { t.Fatal("replacement failed") }
```

- [ ] Run RED, then implement same-directory exclusive temporary creation, restrictive permissions before data write, Sync/Close, and platform replacement. Reject symlink/reparse paths before use; preserve the original on replacement failure; remove only the exact temporary file created by this call. Windows uses reviewed x/sys ACL and file primitives; Darwin uses native modes and rename. Do not claim crash-proof/power-loss guarantees.

- [ ] Write lock tests before lock implementation: one parent holds the lock, a child with a short context cannot acquire it, after parent releases a fresh child acquires; a child that exits while holding does not leave a permanently held lock. Use an independent process, not just goroutines. Implement Windows LockFileEx/UnlockFileEx and Darwin Flock with nonblocking attempts and context-aware 10ms polling. Keep the lock file after release to avoid inode/path races; the OS releases the lock on process exit.

```go
release, err := Acquire(context.Background(), lockPath)
if err != nil { t.Fatal(err) }
ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
defer cancel()
if other, err := Acquire(ctx, lockPath); err == nil { other(); t.Fatal("lock admitted a second owner") }
if err := release(); err != nil { t.Fatal(err) }
```

The independent-process tests supplement, not replace, this focused test. Native filenames stay behind platform build constraints.

- [ ] Write bounded-delivery tests. Record sleeps and calls, with explicit expectations: five retryable failures → five main calls and waits `[5s,15s,30s,60s]`; permanent error → one call; success after a retry → stop main retries; accepted primary with continuous/ring45 → sleep15s and one extension; ring60 → sleep30s; ring30 or continuous=false → no extension; extension failure is never retried. Canceled context makes no further send. `send` is mandatory; invalid ring outside 30–60 returns an empty report without sending.

```go
var waits []time.Duration
report := Deliver(context.Background(), 45, true,
    func(_ context.Context, extension bool) Result { return Result{Accepted: true} },
    func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
if report.MainAttempts != 1 || !report.ExtensionAttempted || !reflect.DeepEqual(waits, []time.Duration{15*time.Second}) {
    t.Fatal("wrong extension schedule")
}
```

Implement a single fixed retry-delay slice and a single extension branch after accepted primary. Do not add persistence/replay semantics here; those belong to the subsequent runtime plan. The callback boundary models a provider result, not a fake replacement for the schedule.

- [ ] Write and run `TestDetachedWorkerClosesHookPipes` RED before SpawnWorker. A helper `hook` branch of the compiled test binary launches a `worker` helper using SpawnWorker and exits after printing neutral JSON. The worker waits until the hook exited, then runs Deliver against an `httptest.Server` that returns one retryable failure and success; the real first retry waits five seconds, and a ring31 extension waits one second. Assert the hook stdout/stderr reach EOF within two seconds; eventual requests contain two main attempts and one extension within a 15-second deadline. Both executables run from a Chinese/space path with empty language-runtime PATH. Test helpers and loopback job fixtures stay in `_test.go`, never in production command dispatch. Invalid key/path tests prove no process is launched. Do not claim survival of agent-enforced process-tree killing, logout, reboot or shutdown.

- [ ] Implement platform spawn flags only after the failing process test. Run focused tests, `go test ./...`, `go vet ./...` and release scan once; preserve actual logged test timing/error evidence. Commit `feat: add private state and detached delivery primitives`.

### Task 4: Real platform CI gates and candidate evidence

**Files:** Create `.github/workflows/native.yml`, `scripts/native-ci-macos.sh`, `docs/native-validation.md`; extend native manifest.

**Interfaces:** Consume `go test ./...`, `go vet ./...`, the version command and platform tests. No release publication or auto-install entry point in this task. CI is read-only except its ephemeral checkout/test fixtures and workflow artifacts.

- [ ] Add a workflow with `push` on `feature/native-*` and `pull_request`/`workflow_dispatch`; permissions `contents: read`. Pin `actions/checkout` to `d23441a48e516b6c34aea4fa41551a30e30af803` (v6), `actions/setup-go` to `924ae3a1cded613372ab5595356fb5720e22ba16` (v6), and `actions/upload-artifact` to `b7c566a772e6b6bfb58ed0dc250532a479d7789f` (v6), verified against upstream tags. Use Go exactly 1.27.0. Matrix: `windows-latest`, `macos-15`, `macos-15-intel`, `macos-26`, `macos-26-intel`, fail-fast false. No paid large runner. Give each job a bounded timeout. Run `go mod verify`, `go test -count=1 ./...`, `go vet ./...` and `go build -trimpath -o <staging-path> ./cmd/agent-task-notify`; include OS/architecture in artifact names.

```yaml
permissions:
  contents: read
jobs:
  native:
    strategy:
      fail-fast: false
      matrix:
        os: [windows-latest, macos-15, macos-15-intel, macos-26, macos-26-intel]
    runs-on: ${{ matrix.os }}
    timeout-minutes: 20
```

- [ ] On Mac, invoke a checked-in script that first requires `CI=true` and nonempty `RUNNER_TEMP`, creates a unique directory there, creates a dedicated Keychain with a synthetic password, and sets only the ephemeral runner's keychain search/default configuration for this job. Export `ATN_TEST_KEYCHAIN` to the test command. The exact fixture is `RUNNER_TEMP/atn-keychain.<random>/synthetic.keychain-db`; its public test-only password is `atn-synthetic-ci-fixture-only`, matching the Task 2 locked-Keychain test. Install an EXIT trap to unlock and delete only that exact generated fixture; never accept an arbitrary deletion target or use a real user's keychain. Do not put any phone credential in CI. Run the suite including the locked-Keychain gate under this fixture. Explain test-only use of `/usr/bin/security`; the built notifier does not execute it.

```sh
test "${CI:-}" = true && test -n "${RUNNER_TEMP:-}" || exit 2
fixture_dir=$(mktemp -d "$RUNNER_TEMP/atn-keychain.XXXXXXXX")
test_keychain="$fixture_dir/synthetic.keychain-db"
# /usr/bin/security creates the synthetic fixture; no user secret is involved.
```

Before implementing the script, write an environment-refusal check: outside CI it exits nonzero without creating or modifying any keychain. Within CI, test evidence must explicitly show the locked-keychain case ran, not skipped. Build/downloaded candidate artifacts carry `UNSIGNED-CANDIDATE.txt` for Mac and no gate-bypass instructions; no stable/compatible label.

- [ ] Validate YAML and the shell with available parsers, run local Go/legacy tests and privacy inventory checks. The Windows host cannot validate Mac execution; record that honestly. Publish the reviewed feature branch only using already-authorized repository access, not a new account/token permission. Wait for actual matrix jobs and inspect their results. Any failed gate blocks Mac migration until fixed, with focused fixes and re-review; no silent plaintext/CLI fallback.

- [ ] Populate `docs/native-validation.md` with exact commit/run links, actual OS/arch and tests executed, and known limits. Do not prefill successful results before they exist. Source foundation can be reviewed independently, but do not merge or announce the complete native release until the subsequent migration plan is also complete. Commit `ci: verify native foundation on Windows and macOS`.

## Coverage and continuation

This plan implements spec section 4's three gates, the foundation of sections 5/8, and the CI portion of section 11. The next migration plan must explicitly cover all remaining sections: strict bounded input and six adapters; settings/icons; providers and receipt validation; persisted deduplicated lifecycle and ambiguous-send policy; safe install/configure/doctor/preview/uninstall; OpenCode and WorkBuddy packaging; standalone release integrity; updated Skill, Chinese/English guides, compatibility/evidence and same-repository release. No current installation is migrated automatically. A failed platform gate stops that platform, not unrelated safe work.
