# Troubleshooting

- **No notification:** check phone permissions, run safe Doctor, then use explicit real Preview only if you wish to test delivery. A receipt only records installer ownership; it does not prove hook loading.
- **Host did not load a hook:** verify the host version and its manual hook review/trust UI. Do not edit trust hashes or ask an Agent to bypass review.
- **Bark ring differs from expectation:** continuous mode targets 30–60 seconds with one extension; single sound is provider-controlled.
- **ntfy sound differs:** ntfy app settings control sound; adjust `ntfyPriority` only for priority.
- **Delayed end looked wrong:** an adapter without native run IDs cannot always distinguish delayed Stop events. Disable that adapter rather than assuming exact correlation.
- **WorkBuddy:** rebuild the complete manual package and review its documented host workflow. Desktop loading is experimental.

Doctor reports aggregate main/extension statuses, bounded attempt totals and fixed failure classes, including spawn failures and ambiguous sends. It scans at most 1,000 job files (`truncated` marks the limit), caps attempts at five per job and input-invalid counters at 1,000 per class. Invalid JSON, UTF-8 and required-field shapes are counted without saving input; Hook stdout remains neutral and exit code zero. No IDs, paths, URLs or raw error messages are reported.

OpenCode serializes metadata lookup and child completion per session with a five-second timeout per operation and bounded pending queues. Failed/timed-out Start is not followed by Stop; delivery may be missed, never replayed automatically. Its stderr emits only one fixed `bridge-*` code per failure class per plugin instance, without host error details.

Terminal-job cleanup is synchronous and may become slow with many retained pending/sending jobs; this release does not purge active state or add a scheduler. Keep this scale limit in mind; broad cleanup redesign is deferred.
