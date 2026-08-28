# Native configuration — 0.2.0-rc.1

Use the native executable and [installation guide](native-installation.md), not the legacy PowerShell commands. Windows x64 is the local validation platform; Mac Intel/Apple Silicon and ntfy/Android remain experimental. The notifier has no extra language-runtime dependency; host shells/runtimes are separate prerequisites.

`--data-directory ABS_DATA` overrides `ATN_DATA_DIRECTORY`, then Windows `%LOCALAPPDATA%\AgentTaskNotifyNative` or Mac `$HOME/Library/Application Support/AgentTaskNotifyNative`. Choose an existing owned parent and a new private data root outside source, package and legacy data. Existing unsafe roots are refused, never repaired. Keep the same path in every command and host registration.

## Local input and settings

Run `agent-task-notify configure --provider bark --data-directory ABS_DATA` (or `ntfy`) in a local terminal. Bark's endpoint includes its device key; ntfy uses a topic endpoint plus token. All credential prompts are hidden. Never share credentials, private endpoints or files in chat or command arguments. An explicit `--credential-stdin` accepts a provider-specific JSON object from an authorized local source, not an invitation to collect secrets in chat: Bark accepts `endpoint`; ntfy accepts `endpoint`, `token`, optional `allowUnauthenticated`. Unauthenticated topics require explicit consent; verify topic ACLs independently. An obscure name and bearer token are not themselves proof of ACLs.

Production endpoints require HTTPS. Loopback HTTP is for isolated local testing only. Endpoint query/fragment/userinfo/percent-encoding are unsupported; path segments use letters, numbers, `_` and `-`, with exactly one topic segment for ntfy. Validation refusals must not be worked around by weakening TLS or sending a real test during diagnosis.

Use `--settings-file ABS_JSON` for a strict UTF-8 JSON object patch (4 MiB input limit). Unknown keys, duplicate keys and wrong types are refused; selected `provider` must match `--provider`. Configuration re-prompts locally for the selected credential even for a settings patch. Example:

```json
{"minSeconds":300,"longTaskSeconds":1200,"mediumRingSeconds":30,"longRingSeconds":45,"sound":"alarm","icons":{"codex":""}}
```

| Setting | Default | Accepted meaning/range |
| --- | --- | --- |
| `provider` | `bark` | `bark` (iOS) or experimental `ntfy` (Android) |
| `minSeconds` | 1800 | Positive integer; minimum eligible run duration |
| `longTaskSeconds` | 3600 | Integer greater than `minSeconds` |
| `mediumRingSeconds`, `longRingSeconds` | 45, 60 | Integers 30–60; Bark approximate targets |
| `continuous` | true | Boolean; Bark main plus one extension when needed |
| `level` | `critical` | `critical`, `active`, `timeSensitive`, `passive` |
| `volume` | 7 | Integer 0–10 |
| `sound` | `alarm` | Nonblank UTF-8 string, at most 4096 bytes |
| `ntfyPriority` | 4 | Integer 1–5; phone app controls actual sound |
| `enableAttention` | false | Reserved normalized attention integration; no adapter guarantees it |
| `icons` | `{}` | Six known ID keys; each override at most 4096 UTF-8 bytes; HTTPS artwork, empty/invalid artwork omitted |

IDs are `codex`, `claude-code`, `cursor`, `gemini-cli`, `opencode`, `workbuddy`. Embedded metadata supplies their own icons; overrides do not replace the application/system small icon. Remote art may change and grants no brand license. Settings and resolved icon are frozen per job, including retries and extension. Continuous Bark can create two visible phone notifications; single mode follows the sound itself. ntfy sends no Call/X-Call or Bark-only sound fields. Neither channel is a telephone call or promises exact audible timing.

## Storage and checks

Credentials and original host backups are protected with Windows CurrentUser DPAPI or a non-synchronizing Mac Keychain key plus authenticated encryption. Private roots/files require current-user ownership; no plaintext or `security` CLI production fallback exists. Only explicit foreground configuration/Vault setup may create or authorize a Keychain key. Background reads, doctor and dry preview do not open interactive authorization.

`doctor` is a read-only syntactic settings/envelope-presence view, not decryption, authentication, key availability, host loading or phone evidence. `preview --agent ID` is dry; explicit `--send` queues an optional real test and does not confirm delivery. Enabled hooks can send after thresholds without another manual preview. No extra telemetry is added; the chosen provider receives its credential and generic notification content. Local storage encryption is not end-to-end push encryption.

Repeated starts preserve the same run clock, repeated ends do not duplicate the main job, and absent native run IDs leave delayed Stop ambiguity. Main sends retry at most five times with additional waits 5/15/30/60 seconds for retryable errors; extension is one attempt. There is no offline/restart service, exact-once guarantee or automatic replay of uncertain sends. See [compatibility](native-compatibility.md) for crash/race limits and [privacy](privacy.md) for backup handling. Legacy data remains separate; there is no active legacy auto-migration.
