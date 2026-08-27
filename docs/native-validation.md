# Native foundation validation

Status: pending reviewed feature-branch push and GitHub Actions execution. This
document deliberately contains no prefilled successful macOS result.

## Candidate CI contract

The `Native foundation gates` workflow runs only for `feature/native-*` pushes,
pull requests, and manual dispatches. It is read-only except for its disposable
checkout, test fixtures, and candidate artifacts, and has `contents: read`
permission only. It uses GitHub-hosted Windows x64 and macOS standard runners:

| Runner label | Intended platform | Actual run / commit evidence |
| --- | --- | --- |
| `windows-latest` | Windows x64 | Pending remote execution |
| `macos-15` | macOS 15 Apple Silicon | Pending remote execution |
| `macos-15-intel` | macOS 15 Intel | Pending remote execution |
| `macos-26` | macOS 26 Apple Silicon | Pending remote execution |
| `macos-26-intel` | macOS 26 Intel | Pending remote execution |

The reviewed Task 4 commit and each exact GitHub Actions run URL must be added
to this table only after the controller pushes the reviewed branch and inspects
the completed matrix. A failed platform gate blocks native migration on that
platform; it is not replaced with plaintext storage or a CLI fallback.

Every matrix job uses Go `1.27.0` and runs `go mod verify`,
`go test -count=1 ./...`, `go vet ./...`, and a `go build -trimpath` candidate
build. Artifact names contain the actual GitHub runner OS and architecture. A
macOS artifact additionally contains `UNSIGNED-CANDIDATE.txt`: it is a CI test
candidate, not a stable/compatible release, and it contains no Gatekeeper or
quarantine-bypass instruction.

## macOS Keychain gate

`scripts/native-ci-macos.sh` refuses to run unless `CI=true` and `RUNNER_TEMP`
is nonempty. The refusal path exits with status 2 before it invokes
`/usr/bin/security`; local Windows validation exercised that refusal only, not
macOS Keychain behavior.

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

No phone credentials, user Keychain entries, real host configuration, or
end-user Keychain operation belongs to this workflow.

## Local source review evidence

The Task 4 source is reviewed locally on Windows before push. Local checks can
validate YAML syntax, Bash syntax, source allowlisting, Go tests, vetting, and
the legacy test suite. They cannot establish macOS compilation, Keychain
behavior, runner OS/architecture, signing, notarization, real desktop-Agent
hooks, or phone delivery. Historical Windows EOF timing failures remain
unclassified; later passing reruns do not establish that they were fixed.

Remote evidence: pending controller review, push, and completed matrix
inspection.
