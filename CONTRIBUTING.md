# Contributing

Use PowerShell 7.4+ and Node for the existing implementation. Run `pwsh -NoProfile -File tests/Run.Tests.ps1`, then `pwsh -NoProfile -File scripts/Test-Release.ps1`. Keep tests offline and use synthetic data.

## Native Go development

Use Go 1.27.0, Git and Bash on Windows or macOS (Git Bash on Windows; Xcode Command Line Tools for macOS native tests). From the repository root, run:

```sh
go test -count=1 ./...
go vet ./...
go build ./...
```

The complete test suite includes CI orchestration tests that require a Git clone containing historical commit `0062d3b1ccc08c2b81112d8c843b8800f3af4df2` and its `internal/store/acl_darwin.go` blob. Use a full clone of the repository, including its native development history. For an existing shallow clone, run `git fetch --unshallow origin` (only when `git rev-parse --is-shallow-repository` returns `true`), then `git fetch origin`. Verify the required object with:

```sh
git cat-file -e 0062d3b1ccc08c2b81112d8c843b8800f3af4df2:internal/store/acl_darwin.go
```

If that object is still unavailable, obtain a full clone from a repository/ref retaining this history; do not skip the tests or treat missing history as success. A source ZIP can build the native executable, but without `.git` and the required history it cannot run the complete orchestration suite. Before squashing or deleting this history, preserve the object in a reachable ref or have a reviewed replacement for the historical counterfactual evidence mechanism.

For package native tests only, run `go test -count=1 ./internal/...`. On macOS, the secrets tests use the system Keychain API to create/delete synthetic test accounts and may need authorization; use an isolated development/CI test environment. Normal local macOS tests do not establish the CI-only locked-Keychain fixture gate. The real temporary Keychain fixture in `scripts/native-ci-macos.sh` is only for disposable, isolated GitHub-hosted CI runners: do not run it in an everyday user session or fabricate `CI=true` to bypass its guard. Cross-platform orchestration tests use fake security/Go commands and do not count as real macOS Keychain evidence.

Do not add any runtime data, logs, credentials (including encrypted credentials or backups), device endpoints, real task IDs, screenshots, or machine-specific paths to Git. Generated executables, private test reports and temporary fixtures are not source-release files; keep the exact release inventory check above passing.

For bugs, use the issue template and sanitise all reports. Changes to host hooks require documented source evidence and must preserve explicit user review/trust boundaries.
