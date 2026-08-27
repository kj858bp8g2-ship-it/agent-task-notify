# Agent Task Notify

Private Windows notifications for long local tasks in Codex, Claude Code, Cursor, Gemini CLI, OpenCode, and experimental WorkBuddy. This project is not affiliated with any Agent or notification provider.

## Quick start

1. Download a reviewed release from the GitHub repository; do not use curl-to-shell installers.
2. Ask a local-capable Agent to install one selected adapter with `scripts/Install-Notifications.ps1`. The shipped Codex plugin hooks and script installer are alternative paths—choose one, never both.
3. Install Bark or ntfy on your phone, enable its notifications, then run `scripts/Configure-Notifications.ps1` locally and enter the credential only at the local prompt.
4. Inspect `scripts/agent-task-notify.ps1 -Mode Doctor`. It does not prove that a host loaded the hook.
5. Only if desired, explicitly run `scripts/agent-task-notify.ps1 -Mode Preview -Agent codex -SendRealPush`; ordinary preview never sends.

Requires Windows and PowerShell 7.4+. The scripts detect missing dependencies. Unsupported Agent versions should stay disabled; see [compatibility](docs/compatibility.md). Never paste a device key, ntfy token, or provider endpoint into chat.

The Codex plugin uses the host-supplied `PLUGIN_ROOT` at runtime and its hook definitions must still be reviewed and trusted through the host's `/hooks` workflow. It makes no trust changes automatically.

See [configuration](docs/configuration.md), [privacy](docs/privacy.md), and [troubleshooting](docs/troubleshooting.md). Contributions: [CONTRIBUTING.md](CONTRIBUTING.md).
