# Agent Task Notify

Phone notifications for long local tasks in Codex, Claude Code, Cursor, Gemini CLI, OpenCode, and experimental WorkBuddy, OpenClaw, and Hermes Agent integrations. Native `0.2.0-rc.2` targets Windows x64 and experimental Mac Intel/Apple Silicon; Bark/iOS and experimental ntfy/Android. This project is not affiliated with any Agent or notification provider.

## Native candidate quick start

Start with [native installation](docs/native-installation.md) ([中文](docs/native-installation.zh-CN.md)), [configuration](docs/native-configuration.md), and the separate [evidence levels](docs/native-compatibility.md). The notifier requires no extra PowerShell, Node, Python or Go; each Agent's own runtime and verified command shell remain prerequisites. Mac is **unsigned and not notarized**: ordinary auto-install stops; do not bypass OS security. [Validation](docs/native-validation.md) records exact source/run/package evidence and open gates, not a stable or real-phone guarantee.

1. Get the reviewed exact candidate package from this repository, verify provenance and SHA256SUMS, and extract into a new owned folder. Never overwrite a live installation.
2. Ask a capable local Agent to use the packaged Skill, or follow INSTALL yourself: confirm platform, separate owned data path, host shell, target and trust. A repository link/Skill does not auto-install or start a daemon.
3. Run the native executable's `version`, `doctor --data-directory ABS_DATA` and dry `preview --agent codex --data-directory ABS_DATA`. These do not authenticate credentials or prove host loading.
4. Configure the chosen provider with `configure --provider bark --data-directory ABS_DATA` (or `ntfy`) and enter credentials only at the local hidden prompt. Preview installation before authorized `--apply`; WorkBuddy, OpenClaw, and Hermes use their packaged manual experimental integrations.
5. Only when requested, use `preview --agent codex --send --data-directory ABS_DATA` for an optional audible test. Queued/accepted is not phone arrival. Enabled hooks also send after thresholds.

Default eligibility is 1800 seconds, long-task threshold 3600, Bark targets 45/60 seconds with `alarm`; all are tunable. Continuous Bark may produce two notifications, with approximate timing. ntfy sound is phone-controlled, not a call. No adapter guarantees every `needs_attention` moment. No extra telemetry, live legacy auto-migration, offline/reboot or exactly-once guarantee is provided. Never paste keys, tokens or private endpoints into chat. Native and legacy routes must not be enabled together.

## Legacy Windows route

Existing `0.1.0` script/plugin instructions remain here; they are not the native package default. Keep legacy data and credentials separate. For rollback, first disable/remove native registration with its receipt-owned uninstall (retain its original package until done), then explicitly re-enable one reviewed legacy route. Do not blindly restore a whole old host config, delete data or migrate/read old keys automatically.

1. Download a reviewed release from the GitHub repository; do not use curl-to-shell installers.
2. Ask a local-capable Agent to install one selected adapter with `scripts/Install-Notifications.ps1`. The shipped Codex plugin hooks and script installer are alternative paths—choose one, never both.
3. Install Bark or ntfy on your phone, enable its notifications, then run `scripts/Configure-Notifications.ps1` locally and enter the credential only at the local prompt.
4. Inspect `scripts/agent-task-notify.ps1 -Mode Doctor`. It does not prove that a host loaded the hook.
5. Only if desired, explicitly run `scripts/agent-task-notify.ps1 -Mode Preview -Agent codex -SendRealPush`; ordinary preview never sends.

Requires Windows and PowerShell 7.4+. The scripts detect missing dependencies. Unsupported Agent versions should stay disabled; see [compatibility](docs/compatibility.md). Never paste a device key, ntfy token, or provider endpoint into chat.

The Codex plugin uses the host-supplied `PLUGIN_ROOT` at runtime and its hook definitions must still be reviewed and trusted through the host's `/hooks` workflow. It makes no trust changes automatically.

See [configuration](docs/configuration.md), [privacy](docs/privacy.md), and [troubleshooting](docs/troubleshooting.md). Contributions: [CONTRIBUTING.md](CONTRIBUTING.md).
