# Configuration routes

For native `0.2.0-rc.1`, use [native configuration](native-configuration.md) and [native installation](native-installation.md). Native data is separate (`AgentTaskNotifyNative`), flags use `--data-directory`, and Mac is an unsigned experimental route. Do not use the legacy commands below for a native package or automatically migrate its credentials.

## Legacy Windows configuration

Runtime data defaults to Windows LocalApplicationData/AgentTaskNotify. Set `ATN_DATA_DIRECTORY` for a shared override used by runtime, configure, install, uninstall, and the OpenCode bridge; an explicit `-DataDirectory` (or bridge option/installed shim path) takes precedence. The unset default is unchanged; changing `LOCALAPPDATA` alone does not redirect Windows Known Folder resolution. Use a private directory outside the package. Install, upgrade, and uninstall preserve user settings and credentials; only receipt-owned hook entries are removed.

Configuration is validated and DPAPI-tested before commit. A failed settings replacement restores the previous credential under an exclusive compare-and-restore handle; concurrent foreign credential edits are never overwritten. If automatic recovery conflicts, the old protected bytes are retained as `configuration-recovery-*.dpapi` for private manual recovery. These fault-tested replacements are not a power-loss or crash-injection guarantee.

Doctor's `configurationRecoveryCount` (capped at 1,000) flags retained recovery files without reading them or showing paths. Find them locally in the data directory you selected. They contain the original encrypted credential JSON envelope, not plaintext and not an automatically replayed configuration. For manual recovery, first disable hooks and stop configuration edits, privately preserve the current settings/credential files, then explicitly choose whether to keep the foreign edit or restore the selected recovery bytes to the matching provider's credential file. Do not replace current settings or another provider's credential blindly, do not paste/decrypt the envelope into chat, and do not restore while another process is editing. The original Windows account is needed for DPAPI. Recovery may require operator judgment; this is not a concurrent-editor or power-loss transaction service.

The editable defaults are `provider: 'bark'`, `minSeconds: 1800`, `longTaskSeconds: 3600`, `mediumRingSeconds: 45`, `longRingSeconds: 60`, `continuous: true`, `level: 'critical'`, `volume: 7`, `sound: 'alarm'`, `ntfyPriority: 4`, `enableAttention: false`, and `icons: {}`. Thresholds are positive integers and `longTaskSeconds` must be larger than `minSeconds`; ring targets are 30–60 seconds. `provider` is `bark` or `ntfy`; Bark level is `critical`, `active`, `timeSensitive`, or `passive`; volume is 0–10; and ntfy priority is 1–5.

Create a local settings file, then provide it to the interactive local configuration command. This complete non-default example keeps credentials out of the file and chat:

```json
{"provider":"bark","minSeconds":300,"longTaskSeconds":1200,"mediumRingSeconds":30,"longRingSeconds":45,"continuous":true,"level":"critical","volume":7,"sound":"minuet","ntfyPriority":4,"enableAttention":false,"icons":{}}
```

```powershell
pwsh -NoProfile -File .\scripts\Configure-Notifications.ps1 -Provider bark -SettingsPath .\my-settings.json
```

The command prompts locally for the provider credential. Bark’s continuous call targets 30–60 seconds. Single-sound mode does not promise an exact duration. ntfy sends one notification; its application controls sound, while `ntfyPriority` controls priority. No ntfy Call/X-Call or Bark-only fields are sent.

`icons` may override any of the six IDs with an HTTPS URL. Set an icon to an empty string to use provider/default artwork. Invalid artwork also falls back rather than substituting another Agent’s logo. Frozen job settings retain the selected artwork across retries.

Production credentials require HTTPS endpoints. HTTP is rejected for arbitrary remote endpoints; `http://127.0.0.1` or `http://localhost` is allowed only for local provider-development/testing, never for a remote service. `enableAttention` is reserved for explicit normalized integrations. Native adapters currently do not emit `needs_attention` events.
