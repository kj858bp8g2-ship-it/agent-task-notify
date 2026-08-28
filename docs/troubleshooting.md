# Troubleshooting

## Native 0.2.0-rc.1

Use the absolute packaged executable and the same `--data-directory` used by the installed hook. Start with `version`, `doctor` and dry `preview --agent ID`; do not install/configure or send as a prerequisite to inspection. Doctor only checks settings/envelope presence, not decryption, credential authentication, an unlocked key, host trust/loading or phone arrival. Only explicit configuration or foreground Vault operations in authorized applied installation/removal may create/authorize Mac Keychain storage; do not use doctor/dry preview to obtain authorization. Mac is unsigned/experimental: if blocked, stop without quarantine/Gatekeeper bypass or elevation.

- Missing host parent: inspect the printed target and verify the host's intended existing user-owned directory; installer never creates/repairs it. OpenCode `--config-path` points to an opencode.json locator, but the actual target is sibling `plugins/agent-task-notify.js`. Empty/whitespace/malformed existing JSON remains untouched; fix it explicitly through the appropriate local workflow, not blind overwrite.
- Generic operation refusal: locally check selected data path, private ownership, writable supported filesystem, source/package separation and unchanged original package identity. Do not disclose private paths/credentials or repair permissions automatically. Unsupported metadata/aliases/reserved Windows names are conservative refusals; see [limits](native-compatibility.md).
- Nothing arrives: check the host's actual shell/trust and one active registration route, then phone permissions/provider settings. Only an explicitly requested `preview --agent ID --send` queues a real test. A receipt is ownership evidence, not proof of loading or duplicate absence; queued/accepted is not phone arrival. ntfy ACLs need independent verification.
- Timing/sound: defaults are 1800/3600-second thresholds and Bark 45/60-second targets with `alarm`. Continuous Bark sends main plus one extension; timing is approximate, and two phone entries can be expected. ntfy has phone-controlled sound and no Call/X-Call. Delayed Stop without native run IDs and missing `needs_attention` coverage are known boundaries.
- Uninstall conflict: keep the original package, backups and data; preview `uninstall --agent ID --data-directory ABS_DATA`, then explicitly apply only receipt-confirmed removal. Do not restore the whole stale config. WorkBuddy removal uses its verified manual host workflow. To return to legacy, remove/disable native first and explicitly choose one legacy route with separate data.

Main retries are bounded at five (5/15/30/60-second waits); extension once, no uncertain-send replay, no service/offline/reboot guarantee. Crash gaps between state/job/spawn and final-check races can miss notifications. 240 seconds is a cooperative worker budget; two seconds is installer lock acquisition wait, not a hard process/OS-call deadline. Retention is bounded and may leave accumulated state; do not recursively purge it as a fix.

Developer archive diagnostics use exactly `ATN_PACKAGE_DIAGNOSTICS=1` only with `package-native build`/`verify`; fixed stage/category stderr helps locate rejection without paths/argv/env/child output. They do not change runtime safety or prove a root cause/notification success. Windows CI4 at `43488cc` reported `ancestor-owner/rejected`; the precise ancestor/SID was not recorded. New caller TEMP selection needs fresh CI, not a claimed repair from local tests. Caller-selected extract parent must be outside source; the verifier cannot guarantee that choice. [Validation](native-validation.md) records gates and boundaries.

## Legacy Windows/shared provider troubleshooting

- **No notification:** check phone permissions, run safe Doctor, then use explicit real Preview only if you wish to test delivery. A receipt only records installer ownership; it does not prove hook loading.
- **Host did not load a hook:** verify the host version and its manual hook review/trust UI. Do not edit trust hashes or ask an Agent to bypass review.
- **Bark ring differs from expectation:** continuous mode targets 30–60 seconds with one extension; single sound is provider-controlled.
- **ntfy sound differs:** ntfy app settings control sound; adjust `ntfyPriority` only for priority.
- **Delayed end looked wrong:** an adapter without native run IDs cannot always distinguish delayed Stop events. Disable that adapter rather than assuming exact correlation.
- **WorkBuddy:** rebuild the complete manual package and review its documented host workflow. Desktop loading is experimental.

Doctor reports aggregate main/extension statuses, bounded attempt totals and fixed failure classes, including spawn failures and ambiguous sends. It scans at most 1,000 job files (`truncated` marks the limit), caps attempts at five per job and input-invalid counters at 1,000 per class. Invalid JSON, UTF-8 and required-field shapes are counted without saving input; Hook stdout remains neutral and exit code zero. No IDs, paths, URLs or raw error messages are reported.

OpenCode serializes metadata lookup and child completion per session with a five-second timeout per operation and bounded pending queues. Failed/timed-out Start is not followed by Stop; delivery may be missed, never replayed automatically. Its stderr emits only one fixed `bridge-*` code per failure class per plugin instance, without host error details.

Terminal-job cleanup is synchronous and may become slow with many retained pending/sending jobs; this release does not purge active state or add a scheduler. Keep this scale limit in mind; broad cleanup redesign is deferred.
