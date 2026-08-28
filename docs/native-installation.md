# Native candidate installation — 0.2.0-rc.1

This is an experimental, unpublished test candidate for Windows x64, Mac Intel and Apple Silicon. Mac archives are **UNSIGNED and NOT NOTARIZED**. If macOS blocks opening the executable, stop: do not remove quarantine, bypass Gatekeeper, disable protection or request administrator access. A successful build or CI run is not evidence of first-use desktop authorization, real Agent loading or phone delivery. The packaged Skill is transitional legacy guidance: do not use it to install this candidate. Publication waits for the native Skill update and final package re-tests.

## Obtain and check the package

Use only the reviewed repository's exact candidate workflow run and source commit. Download the matching `native-candidate-windows-amd64`, `native-candidate-darwin-amd64` or `native-candidate-darwin-arm64` artifact. Each holds an Agent Task Notify (ATN) archive and `SHA256SUMS`: `atn-native-0.2.0-rc.1-windows-amd64.zip`, `atn-native-0.2.0-rc.1-darwin-amd64.tar.gz` or `atn-native-0.2.0-rc.1-darwin-arm64.tar.gz`. Confirm the repository, run, source commit and artifact provenance before trusting its checksum. SHA256SUMS detects corruption; an attacker can replace both archive and checksum, so hashes alone are not publisher authentication.

Compare the archive's SHA-256 with its one matching basename in SHA256SUMS before extracting. Windows has `certutil -hashfile ARCHIVE SHA256`; macOS has `shasum -a 256 ARCHIVE`. Inspect the archive list first: it must match `manifest.json`'s fixed list and contain no absolute paths, `..`, duplicates or links. Extract into a new user-owned folder, never over an old install. Mac must use the tar.gz, preserving executable permissions (do not transfer the bare binary through ZIP). Keep both INSTALL files, manifest, notices, Skill and integrations with the executable. Defaults and six Agent icons are embedded; there is no mutable config or icon copy to install.

For developer-level strict verification from the reviewed source checkout, with Go already installed, use:

```text
go run ./cmd/package-native verify --archive ABS_ARCHIVE --checksums ABS_SHA256SUMS --platform PLATFORM --version 0.2.0-rc.1 --extract-to ABS_NEW_FOLDER
```

Replace placeholders with absolute paths; quote paths containing spaces. This validates the hash, exact archive name/list, sizes, modes, platform, architecture, both binary copies and manifest before running the extracted program. It accepts only the current OS/architecture. It creates a new explicitly owned folder and isolated temporary user/data/CWD paths outside the source checkout, runs version, doctor and six previews with empty PATH, and never sends or opens Keychain. It is a developer tool, not an end-user runtime requirement. The program itself needs no separately installed PowerShell, Node, Python or Go; the Agent's own runtime and selected host command shell remain the Agent's prerequisites.

## Inspect, configure, then install explicitly

Use the absolute path to the extracted executable (`agent-task-notify.exe` on Windows, `agent-task-notify` on Mac). All commands below start with that path. In a shell that requires it, quote it or use the shell's invocation operator. Choose an independent user-owned data directory outside the package, source checkout and legacy data. New defaults are `%LOCALAPPDATA%\AgentTaskNotifyNative` (Windows) and `$HOME/Library/Application Support/AgentTaskNotifyNative` (Mac); the default's parent must exist. Explicit `--data-directory ABS_DATA` overrides `ATN_DATA_DIRECTORY`, which overrides these defaults. Keep the same data path for every step and future hook.

```text
agent-task-notify version
agent-task-notify doctor --data-directory ABS_DATA
agent-task-notify preview --agent codex --data-directory ABS_DATA
agent-task-notify configure --provider bark --data-directory ABS_DATA
```

Use `ntfy` instead of `bark` when appropriate. Configuration asks for credentials locally; never paste a token, device key, complete private URL or config file into Agent chat. Nonterminal input requires explicit `--credential-stdin`. Optional `--settings-file ABS_JSON` is a local nonsecret settings patch. Credentials use Windows DPAPI / Mac Keychain; no plaintext or CLI security fallback. Plain preview sends nothing; only explicit `preview --agent ID --send` authorizes a test notification. Do not use `--send` in package checks.

Agent IDs are `codex`, `claude-code`, `cursor`, `gemini-cli`, `opencode`, `workbuddy`. For the first five, preview before applying:

```text
agent-task-notify install --agent codex --command-shell cmd --data-directory ABS_DATA
agent-task-notify install --agent codex --command-shell cmd --data-directory ABS_DATA --apply
```

The Windows example assumes the host actually uses cmd; select `powershell` or `posix` only for a verified corresponding host shell. Mac hosts use explicit `--command-shell posix`. Do not infer the host shell from your interactive terminal. `--config-path ABS_FILE` selects an explicitly verified host config: default targets are `.codex/hooks.json`, `.claude/settings.json`, `.cursor/hooks.json`, `.gemini/settings.json` under the user's home; OpenCode uses `$XDG_CONFIG_HOME/opencode/opencode.json` or `$HOME/.config/opencode/opencode.json` and writes only its sibling `plugins/agent-task-notify.js`. The host target parent (including OpenCode's plugins directory) must already exist and be user-owned. Review the printed target/plan, then use `--apply` only with authorization. Unknown fields/hooks are preserved; a protected backup and receipt are required. Conflicts stop rather than overwriting. Codex's existing `notify` configuration is not replaced; finish any host trust/approval step yourself. Importing the Skill does **not** activate background hooks.

OpenCode loads the packaged JS bridge in its own runtime, then spawns the native executable directly. Keep the `integrations/opencode` folder beside the root binary; the native installer creates a shim with the explicit executable/data path. Do not import the legacy default wrapper as a substitute for that native installation. Never double-register the legacy PowerShell/plugin route with the native route. This candidate does not upgrade existing installations or read old device keys; legacy source scripts remain a separate, unchanged option.

## WorkBuddy manual experimental package

Manually import the complete `workbuddy` subfolder using an explicitly verified host plugin workflow; no desktop settings file is guessed or written automatically. It includes `.workbuddy-plugin/plugin.json`, `hooks/hooks.json`, `hooks/launch.sh` and the matching `runtime` binary. The host's Bash uses `CODEBUDDY_PLUGIN_ROOT`, falling back to `CLAUDE_PLUGIN_ROOT`; no separately installed notification runtime is needed. Configure the **WorkBuddy copy** of the binary with the same chosen data directory, and supply `ATN_DATA_DIRECTORY` to the host if using a nondefault path. Wrong OS, missing binary or execution failure produces a neutral response. Desktop loading, cancellation and actual delivery remain unverified; do not install legacy and native WorkBuddy packages together.

## Defaults and removal

The timer starts with a supported task start; repeats do not reset it. Default minimum is 1800 seconds, long-task threshold 3600; Bark targets 45/60 seconds with `alarm`. Delivery is not a real phone call and cannot promise exact audible duration. No adapter currently guarantees all `needs_attention` moments. Background execution and retries require host/OS support.

Preview `uninstall --agent ID --data-directory ABS_DATA`, then repeat with `--apply` only after review. It removes only receipt-confirmed native entries; it does not restore an entire config or delete user/data folders. WorkBuddy removal is manual through the verified host plugin workflow. Keep protected backups/data until their purpose is clear; do not recursively erase directories or delete Keychain items as an automated cleanup step.
