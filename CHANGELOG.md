# Changelog

## 0.2.0-rc.2 — OpenClaw and Hermes candidate

- Adds experimental manual OpenClaw and Hermes Agent integrations based on their official lifecycle-hook contracts, plus pinned official remote icon metadata for both Agents.
- OpenClaw ships as a self-contained linked plugin with minimal direct-spawn payloads, child-session filtering and fail-open notification handling. Hermes ships a merge-only Shell Hooks example with per-command consent guidance and minimal native normalization.
- Native packages now contain four identical notifier binaries (root, WorkBuddy, OpenClaw and Hermes), eight Agent dry previews and expanded archive safety checks. No real OpenClaw/Hermes host validation is claimed.
- Keeps default timing, sounds and per-Agent artwork configurable. Prompts, messages, tool input/output and model responses are not retained or forwarded by the new adapters.

## 0.2.0-rc.1 — native candidate

- Native Windows x64 and experimental unsigned/not-notarized Mac Intel/Apple Silicon archives, embedded defaults and six Agent icon mappings; no extra notifier language runtime.
- Local configure/doctor/dry preview/explicit send/install/receipt-owned uninstall; protected DPAPI/Keychain credentials and backups, independent native data, no active legacy auto-migration.
- Six adapter contracts including a manual self-contained WorkBuddy package; Bark/iOS and experimental ntfy/Android. Real hosts/phones, first Mac authorization and Gatekeeper remain untested; timing, attention, crash/restart and delivery limitations are explicit.
- Native-first bilingual installation and packaged Skill; exact-tag least-privileged prerelease publication requires five source jobs, five canonical consumers and unchanged legacy suites. Windows private CI output/extraction selects existing user TEMP without relaxing package validation. Fresh exact-source CI and final downloaded artifact verification remain release gates; older evidence is not attributed to this edit.

## 0.1.0

- Initial reviewed local-task notification packaging, documentation, skill, release scanner, and offline CI.
