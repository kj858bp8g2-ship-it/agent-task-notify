# Native compatibility — 0.2.0-rc.1

This NEW candidate is not validated by historical legacy Codex/iPhone use. Windows x64 is the local test platform; Mac Intel/Apple Silicon and ntfy/Android are experimental. No user-owned physical Mac, real Agent host, first Keychain authorization, Gatekeeper first launch, Android or iOS phone E2E has been tested for this native candidate. Mac packages are UNSIGNED and NOT NOTARIZED: ordinary auto-install stops; explicit developer evaluation must respect OS security without bypasses.

## Separate evidence levels

The table reports implementation and recorded source baseline `43488cca2cca774d0ec435e042ddf8158bdec3f1`, not a claim that later edits passed. Its [native run](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33159209594) passed five source suites but failed Windows canonical build; all five canonical consumers were skipped. Final candidate package acceptance remains pending fresh exact-source checks and downloaded Windows artifact verification. See [validation](native-validation.md) for commits, runs and OS/architecture scope.

| Source / ID | Notification icon | Implemented | Contract | System | Process | Package | Real host | Phone |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Codex `codex` | ChatGPT artwork | Yes | Fixture-tested | Five-OS source baseline | Synthetic CLI/hook/worker | Final gate pending | Untested | Untested |
| Claude Code `claude-code` | Claude artwork | Yes | Fixture-tested | Five-OS source baseline | Synthetic CLI/hook/worker | Final gate pending | Untested | Untested |
| Cursor `cursor` | Cursor artwork | Yes | Fixture-tested | Five-OS source baseline | Synthetic CLI/hook/worker | Final gate pending | Untested | Untested |
| Gemini CLI `gemini-cli` | Gemini artwork | Yes | Fixture-tested | Five-OS source baseline | Synthetic CLI/hook/worker | Final gate pending | Untested | Untested |
| OpenCode `opencode` | OpenCode artwork | Yes | JS/native bridge fixtures | Five-OS source baseline | Synthetic bridge/CLI | Final gate pending | Untested | Untested |
| WorkBuddy `workbuddy` | WorkBuddy artwork | Experimental manual package | Shared CodeBuddy shape only | Five-OS source baseline | Synthetic packaged launcher | Final gate pending | Untested | Untested |

System means native filesystem/protected-storage tests on Windows and four actual macOS CI runners, not physical-device authorization. Process means isolated child execution with synthetic inputs/local fake services, not real host E2E. In guarded Mac protected-process tests, inherited CI HOME is retained only after both exact child/inherited Keychain contexts are proven to contain the dedicated disposable fixture; app data/config/CWD remain synthetic. Other readonly/absent/unsafe cases use synthetic HOME. This is not proof of protected writes under arbitrary new HOME. Mac Security.framework deprecation warnings remain; some foreign-owner cases skip without available privilege. Locked-Keychain denial is a required explicit PASS, not a skip. Task terminal state/released lock proves logical completion, not hard OS-process exit or containment.

## Host prerequisites and paths

Native notifier packages require no separate PowerShell, Node, Python or Go installation. Host dependencies remain: use only a verified host command shell, not the current terminal. Windows selects explicit `--command-shell cmd`, `powershell` or `posix` according to the actual host. Mac shell-based hooks select `posix`. No supported host-version range is claimed by a documentation contract alone.

| ID | Existing default target / route | Contract basis checked 2026-08-28 |
| --- | --- | --- |
| `codex` | HOME `.codex/hooks.json`; preserve `notify`, complete user hook trust | [Codex hooks](https://learn.chatgpt.com/docs/hooks); multiple hook sources can coexist |
| `claude-code` | HOME `.claude/settings.json`; existing shell form | [Claude hooks](https://code.claude.com/docs/en/hooks); do not assume Windows shell from terminal |
| `cursor` | HOME `.cursor/hooks.json` | [Cursor hooks](https://cursor.com/docs/hooks); explicit shell verification required |
| `gemini-cli` | HOME `.gemini/settings.json` | [Gemini hooks](https://geminicli.com/docs/hooks/reference/); stdin and neutral JSON output |
| `opencode` | XDG_CONFIG_HOME `opencode/opencode.json`, otherwise HOME `.config/opencode/opencode.json`, locates sibling `plugins/agent-task-notify.js` | [OpenCode plugins](https://opencode.ai/docs/plugins/); retain packaged bridge; native direct spawn uses its host JS runtime |
| `workbuddy` | Manual complete `workbuddy` package; host Bash with CODEBUDDY_PLUGIN_ROOT or CLAUDE_PLUGIN_ROOT fallback | [Shared CodeBuddy plugin contract](https://www.codebuddy.cn/docs/cli/plugins-reference), not verified desktop loading |

`--config-path` is a verified explicit locator, not permission to guess desktop files. Host/plugin parents must already exist, be owned and validated; the installer never creates/repairs them. Existing empty/whitespace/malformed JSON is left untouched. Host JSON size limit is 4 MiB, backup envelope 6 MiB; unknown number lexemes/other hooks are preserved. A receipt records ownership, not loading, host trust or absence of another plugin route. Never double-register native and legacy paths. WorkBuddy imports its full self-contained subfolder, not the source wrapper, and has no automatic desktop config mutation or cancellation guarantee.

## Provider, timing and lifecycle limits

Bark/iOS uses configurable 1800/3600-second thresholds, 45/60-second targets and `alarm` by default. Continuous delivery may appear twice (main plus extension), with approximate timing; single mode plays the sound itself. Experimental ntfy/Android uses phone-controlled sound, priority only and no Call/X-Call. These are notifications, not calls. Sender acceptance is not phone arrival, sound or lock-screen evidence. Remote icons are notification decoration, not replacement system/application icons; URLs may change and grant no brand license.

No native adapter guarantees `needs_attention`. Without a native run ID, delayed Stop can be ambiguous. Frozen job settings/icons, duplicate handling and bounded retries do not imply exactly-once delivery. Main retry permits at most five attempts plus waits 5/15/30/60 seconds; extension once. Known gaps: state → job → spawn is not a crash transaction, uncertain sends are not replayed, final-check races remain, and there is no daemon/offline/reboot/restart recovery guarantee. A 240-second cooperative worker context and two-second lock acquisition wait are not hard OS-call/process deadlines. Retention is bounded and may not clear all accumulated state; there is no cleanup scheduler.

## Conservative filesystem/security boundary

Read-only, unsupported, linked/reparse, nonprivate or uncertain-metadata paths are refused, not repaired. Source checks only inspect exact ancestor `.git` or this tool's `go.mod` plus `config/native-source-files.json` markers; no directory enumeration/content scan finds every unmarked source tree. This is not a universal secret-leak guarantee.

Trusted Windows ancestors are only current user, SYSTEM, BUILTIN Administrators and the exactly resolved local NT SERVICE\TrustedInstaller SID; arbitrary service owners/All Services are not trusted. Mac trusted OS-owned ancestors are distinct from private current-user-only roots/files. Missing-parent checks do not accept existing nonprivate roots. Windows CI chooses its existing user TEMP for private output/extract parents; Mac retains RUNNER_TEMP. Selection never creates/repairs parents or changes global environment, and package validation still enforces every ancestor/private/no-reparse/no-preexisting-leaf check.

Windows host replacement preserves owner/group/ACL and DACL protection, with only the allowed automatic-inheritance-model `SE_DACL_AUTO_INHERITED` 0→1 transition on protected regular files. It does not promise all descriptor bytes identical. Readable mandatory-label/resource-attribute/CAP metadata are checked, not the full privileged audit SACL. A supported single mandatory label is preserved; unknown/complex ACE policy or inherited labels for new targets are refused. New targets use owner-only DACL and explicit current-process LOW/MEDIUM/MEDIUM_PLUS/HIGH integrity/NW; HIGH may prevent medium-integrity writes. Original absent-policy SACL_AUTO_INHERITED is retained unchanged, not a new transition exception. Real SCOPE fixtures may skip for privilege. No administrator/ACL repair workaround is recommended.

Reserved Windows device-name stems (even with extensions) are conservatively refused. Aliases are checked by validated filesystem identity as well as text, but no universal OS-valid path or race-free containment promise is made. Distinct 8.3 aliases are only tested where actually available, not inferred from ordinary identity tests. On Mac, in-memory zero-entry ACL encoding and actual on-disk replacement are separate evidence: CI normalized the synthetic zero-entry NO_INHERIT ACL away, so persisted-zero-ACL behavior was not observed.

Keep the original package until receipt-owned uninstall completes; a missing original package identity safely blocks removal. Never blindly restore a stale config or delete user folders/Keychain items. [Installation](native-installation.md), [configuration](native-configuration.md), [privacy](privacy.md) and [legacy rollback](../README.md#legacy-windows-route) explain the scoped operational steps.
