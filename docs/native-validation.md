# Native candidate validation — 0.2.0-rc.2

## Current gate status

This candidate adds experimental manual OpenClaw and Hermes Agent integrations and expands the native package from six to eight Agent identities. The implementation uses the official lifecycle-hook contracts, but no real OpenClaw or Hermes host has been available to this project. Real-host loading, long-run completion, phone arrival and sound remain untested for both new integrations.

Local source tests, exact-source GitHub Actions, canonical archive consumers, tag publication and downloaded release-asset verification are separate gates. This document is intentionally updated with exact commit/run/artifact evidence only after each gate succeeds. Until then, the compatibility table marks the corresponding rc.2 rows as required rather than borrowing results from `0.2.0-rc.1`.

| Gate | Status before publication |
| --- | --- |
| OpenClaw bridge fixtures and native adapter fixtures | Passed locally: 5 JS bridge cases plus Go normalization cases |
| Hermes native adapter fixtures | Passed locally: lifecycle, child and malformed-shape cases |
| Full Go, Node, PowerShell and release-scanner suites | Passed locally on Windows x64; exact-source CI still required |
| Windows x64 canonical archive build/verify with eight dry previews | Passed locally through black-box package tests |
| Fresh five-platform native CI and legacy CI | Pending push |
| Canonical Windows/macOS consumer jobs | Pending push |
| Exact `v0.2.0-rc.2` tag publication | Pending authorization gates |
| Downloaded four-asset release verification | Pending publication |
| Real OpenClaw/Hermes host and phone E2E | Untested |

## Contract and privacy evidence

- OpenClaw integration basis: official typed plugin hooks `before_agent_run`, `agent_end` and subagent lifecycle hooks. The packaged plugin requires the host's explicit conversation-hook permission, then discards conversation/tool/model fields and directly spawns the packaged native runtime with a minimal schema. The pinned official artwork source commit is recorded in `THIRD_PARTY_NOTICES.md`.
- Hermes integration basis: official Shell Hooks `pre_llm_call` and `on_session_end`. The packaged example is merged manually, uses absolute paths and preserves Hermes' first-use consent for each exact command. Native normalization discards all fields except lifecycle/session/turn/terminal state. The pinned official artwork source commit is recorded in `THIRD_PARTY_NOTICES.md`.
- Both integrations are manual because this project does not own the host's full plugin/YAML configuration. `install` and `uninstall` therefore refuse to imply automatic ownership.

## Local verification completed 2026-08-31

- The final serialized `go test -p 1 -count=1 -timeout 20m ./...` run passed every package; its complete `./tests` black-box package took 159.499 seconds. An earlier run had exposed and driven a fix for the old 14-member publisher list before this clean rerun.
- `go mod verify` and `go vet ./...` passed.
- `tests/Run.Tests.ps1` passed the legacy PowerShell suites, real local HTTP/retry process tests, OpenCode bridge tests and all five OpenClaw bridge tests. The release scanner also passed with both new integration directories.
- The Windows package test built the actual native binary/archive, required four byte-identical executable copies, checked the fixed manifest/list/modes/limits, extracted into an isolated root, ran version and doctor, and ran dry previews for all eight Agent IDs. It did not configure a provider or send a phone notification.

These are local Windows results from the working tree, not immutable commit, CI, tag, Mac, release-asset, real-host or phone evidence. The table above remains explicit about those pending gates.

## Evidence boundaries

Synthetic bridge/adapter tests can prove payload minimization, child filtering, deterministic spawn arguments, conservative malformed-input handling and package inventory. They cannot prove a future host version loads the integration, a task survives a host restart, a provider accepts a push, iOS/Android displays it, or the requested sound duration is honored. Sender acceptance remains different from phone arrival. Mac artifacts remain unsigned and not notarized; do not bypass Gatekeeper or remove quarantine.

Historical `0.2.0-rc.1` CI and release evidence belongs only to that tag. See [compatibility](native-compatibility.md), [installation](native-installation.md), [privacy](privacy.md) and [troubleshooting](troubleshooting.md) for the operational boundaries.
