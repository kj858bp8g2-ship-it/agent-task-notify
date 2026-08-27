# Native foundation validation

Status: the source foundation passed the five-platform native CI matrix and
legacy CI at commit
[`0d4c7c593c526328dfde7176df7910bb1976f2e7`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/commit/0d4c7c593c526328dfde7176df7910bb1976f2e7)
on `feature/native-runtime-design`. This is foundation evidence, not approval of
a complete migration or an end-user release. The native CLI exposes `version`
only; it does not replace the six-agent notification integration.

## Completed CI evidence

[Native run 33111237029](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111237029)
completed successfully at the source commit above. Every job below passed its
native tests, vet, build, and artifact upload. OS versions and architectures are
the actual recorded runner identities, not assumptions from runner labels.

| Runner label | Actual OS / build | Architecture | Successful job |
| --- | --- | --- | --- |
| `windows-latest` | Windows Server 2025 Datacenter / `10.0.26100` | `amd64` | [98654295244](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111237029/job/98654295244) |
| `macos-15` | macOS `15.7.7` / `24G720` | `arm64` | [98654295241](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111237029/job/98654295241) |
| `macos-15-intel` | macOS `15.7.9` / `24G830` | `amd64` | [98654295255](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111237029/job/98654295255) |
| `macos-26` | macOS `26.5.2` / `25F84` | `arm64` | [98654295243](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111237029/job/98654295243) |
| `macos-26-intel` | macOS `26.6.1` / `25G76` | `amd64` | [98654294985](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111237029/job/98654294985) |

[Legacy run 33111236937](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111236937),
[job 98654295127](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33111236937/job/98654295127),
also completed successfully at the same source commit. All native jobs used Go
`1.27.0`; all four macOS jobs had `CGO_ENABLED=1`. Windows package output was
nonverbose and reported success for every package.

## Candidate CI contract

The `Native foundation gates` workflow runs only for `feature/native-*` pushes,
pull requests, and manual dispatches. It is read-only except for its disposable
checkout, test fixtures, and candidate artifacts, and has `contents: read`
permission only. It uses GitHub-hosted Windows x64 and macOS standard runners.

A failed platform gate blocks native migration on that platform; it is not
replaced with plaintext storage or a CLI fallback. The completed run above does
not guarantee compatibility with later runner images or operating systems.

Every matrix job uses Go `1.27.0` and runs `go mod verify`,
`go test -count=1 ./...`, `go vet ./...`, and a `go build -trimpath` candidate
build. Artifact names contain the matrix runner label plus the actual GitHub
runner OS and architecture, so concurrent matrix jobs do not share an artifact
name. A macOS artifact additionally contains `UNSIGNED-CANDIDATE.txt`: it is a CI test
candidate, not a stable/compatible release, and it contains no Gatekeeper or
quarantine-bypass instruction.

## macOS Keychain gate

`scripts/native-ci-macos.sh` refuses to run unless `CI=true` and `RUNNER_TEMP`
is nonempty. The refusal path exits with status 2 before it invokes
`/usr/bin/security`. Local Windows shell tests use fake boundaries for refusal,
restoration, orchestration, and exact copied-byte checks; they do not exercise
macOS Keychain behavior. The four completed macOS jobs provide the actual
Darwin/Keychain test evidence.

On a GitHub-hosted macOS runner the script resolves a physical private directory
under `RUNNER_TEMP`, creates exactly
`atn-keychain.<random>/synthetic.keychain-db`, and uses the public test-only
password `atn-synthetic-ci-fixture-only`. `/usr/bin/security` is used only to
create, select, lock, unlock, and delete that disposable fixture. The built
notifier never invokes `security`. The script restores the runner's prior
Keychain default/search configuration before deleting the generated fixture.
It exports `ATN_TEST_KEYCHAIN` and a resolved private Go temporary directory,
runs the full suite verbosely, and requires the locked-Keychain test's explicit
PASS line so a skip cannot count as the denial gate.

Each macOS job explicitly recorded all of the following:

- `filesec-counterfactual: expected rejection confirmed` using the exact
  historical ACL blob from
  `0062d3b1ccc08c2b81112d8c843b8800f3af4df2` and the current test. This is
  retrospective RED/GREEN evidence, not original executed TDD or an actual
  out-of-memory injection.
- `TestDarwinRejectsIncompleteFileSecurity` and
  `TestDarwinLockedKeychainBackgroundDenial` PASS; the latter took 0.13s on
  macOS 15 ARM, 0.11s on macOS 26 ARM, 0.18s on macOS 15 Intel, and 0.22s on
  macOS 26 Intel.
- Full store-package success, including positive and negative ACL/mode and
  inherited-ACL checks, private-file and lock tests;
  `TestDetachedWorkerClosesHookPipes` and
  `TestNativeCLIWithoutLanguageRuntime` PASS.

Earlier macOS no-ACL/filesec failures remain part of the validation history.
The corrected complete-filesec/no-ACL checks now pass on all four actual
runners; this does not permit arbitrary `NULL` or `ENOENT` acceptance.

Security.framework deprecated-API warnings remain. Some CI log annotations
were error-formatted, but compilation and tests succeeded. An ARM cache-save
collision was nonfatal. Neither observation has been suppressed or converted
into a safety fallback, and neither establishes a future-OS guarantee.

No phone credentials, user Keychain entries, installed notifier configuration,
or end-user Keychain operation belongs to this workflow. Keychain configuration
changes are limited to the disposable CI runner fixture and its restoration.

## Downloaded candidate evidence

All five candidate ZIPs from native run `33111237029` were downloaded, their
SHA-256 digests compared with GitHub artifact metadata, and their exact contents
and executable machine architectures inspected.

| Platform | Artifact ID | Verified ZIP SHA-256 |
| --- | --- | --- |
| Windows x64 | `9662628168` | `a0e93dc9debdd8f898eef890fef41413e6c2330c5b972980ce80a9c2c3650853` |
| macOS 15 ARM | `9662631637` | `b6b77ab6fae24521cf73541f768a173e28c0cbbc882144d57c2e92f37e6c3bb0` |
| macOS 15 Intel | `9662675384` | `8fe4c5b2ab7ce5030b2d28eb61a9b0652605272e6ce02190da8a29019b16f5af` |
| macOS 26 ARM | `9662632565` | `9538f74776a934332740776f7a9b3cac06c32c9e19c31b47db129009c252f663` |
| macOS 26 Intel | `9662667490` | `bdf1b22431eb6cf45a2288f231176503504671282f349a177b67e68df325f1cf` |

The Windows ZIP contains only `agent-task-notify.exe`, verified as PE x64.
That downloaded binary was extracted into a path containing Chinese characters
and spaces, then actually executed with an empty `PATH` and isolated home,
profile, app-data, notifier-data, and working directories. It returned exactly
`agent-task-notify 0.2.0-dev windows/amd64`, exit 0, with no stderr or runtime
writes. Its executable SHA-256 is
`56a851ae5f0b2fa9d29ad23c9f3bb9728cc5fc558590f9e915eb204a06d77486`.

Each macOS ZIP contains exactly `agent-task-notify` and
`UNSIGNED-CANDIDATE.txt`, with the expected three-line unsigned-candidate notice
and no extra source or secrets. All four passed content and Mach-O architecture
inspection: ARM binaries were `arm64`, Intel binaries `amd64`. Both ARM
executables had SHA-256
`bd602e3a81e260b8c6edfef62320a58ffea68a21056a490de3927a8e62efd6b1`;
both Intel executables had SHA-256
`14ef93e956852b1ad7313b9caf1d333e7e62275c482f574277e2e272fb1626d0`.

The downloaded macOS executables were **not launched after extraction** on the
Windows validation host. Actual macOS execution evidence is from equivalent
programs compiled from source by the CI tests, not from a downloaded-artifact
launch test. These bare ZIPs are unsigned CI candidates, not final end-user
packages. Full migration still requires permission-preserving macOS package
extraction/launch tests and a safe unsigned-distribution boundary; there are no
Gatekeeper or quarantine-bypass instructions.

## Evidence boundaries

Local source checks were performed on Windows before push. Such checks can
validate YAML syntax, Bash syntax, source allowlisting, Go tests, vetting, and
the legacy test suite. They cannot establish macOS compilation, Keychain
behavior, runner OS/architecture, signing, notarization, real desktop-Agent
hooks, or phone delivery. The actual remote evidence is the completed matrix
above, scoped to its recorded source commit and runner identities.

Historical Windows EOF timing and legacy retry-cap failures remain
unclassified. This successful run does not identify their historical causes
or prove those causes fixed; bounded diagnostics, negative controls, and the
original timeout budgets remain necessary.

The foundation is not a full six-agent migration and must not be presented as a
complete upgrade. No real desktop-Agent integration, first Keychain permission
interaction, Android/iPhone lock-screen or sound behavior, signing,
notarization, Gatekeeper first launch, reboot recovery, or process-tree-kill
guarantee has been established. Candidate validation did not install or replace
the live notifier, read user credentials, or send phone notifications.
