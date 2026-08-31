# Native candidate validation — 0.2.0-rc.2

## Current gate status

This candidate adds experimental manual OpenClaw and Hermes Agent integrations and expands the native package from six to eight Agent identities. The implementation uses the official lifecycle-hook contracts, but no real OpenClaw or Hermes host has been available to this project. Real-host loading, long-run completion, phone arrival and sound remain untested for both new integrations.

Local source tests, exact-source GitHub Actions, canonical archive consumers, tag publication and downloaded release-asset verification are separate gates. This document is intentionally updated with exact commit/run/artifact evidence only after each gate succeeds. Until then, the compatibility table marks the corresponding rc.2 rows as required rather than borrowing results from `0.2.0-rc.1`.

| Gate | Status |
| --- | --- |
| OpenClaw bridge fixtures and native adapter fixtures | Passed locally: 5 JS bridge cases plus Go normalization cases |
| Hermes native adapter fixtures | Passed locally: lifecycle, child and malformed-shape cases |
| Full Go, Node, PowerShell and release-scanner suites | Passed locally on Windows x64 and on exact pre-tag source CI |
| Windows x64 canonical archive build/verify with eight dry previews | Passed locally through black-box package tests |
| Fresh five-platform native CI and legacy CI | Passed for pre-tag source commit `3a1f833` |
| Canonical Windows/macOS consumer jobs | Passed for pre-tag source commit `3a1f833` |
| Exact `v0.2.0-rc.2` tag publication | Passed; tag peels to `3d88a88` |
| Downloaded four-asset release verification | Passed; all release checksums matched |
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

## Exact implementation-source CI completed 2026-08-31

- Implementation source commit: [`27f7e6e6d06542d9b02e72475d3e127fd31c6f1d`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/commit/27f7e6e6d06542d9b02e72475d3e127fd31c6f1d).
- Legacy/offline workflow: [run `33407634834`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33407634834), passed in 3m24s.
- Native candidate workflow: [run `33407634833`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33407634833), passed in 6m44s. Its five build/test jobs and five canonical package-consumer jobs all passed, covering Windows x64 plus the workflow's macOS and Linux matrices; Windows, Darwin amd64 and Darwin arm64 candidate artifacts were produced.

The macOS jobs emitted the already-known compiler deprecation warning for the Keychain interaction API, and GitHub emitted Node.js action-runtime deprecation warnings. Neither warning changed the successful job conclusions.

## Pre-tag source CI and flaky-gate correction completed 2026-09-01

- The first documentation evidence commit `d6dc43a` passed its [legacy run `33408451127`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33408451127), but [native run `33408451139`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33408451139) exposed an existing Windows-only flaky assertion in `TestRetentionBoundariesAndReadOnlyInspection`. One of two eligible records sometimes remained after a single 25 ms best-effort cleanup opportunity.
- Commit [`3a1f8332404fed11debe3ddbba0af4dee9c7788e`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/commit/3a1f8332404fed11debe3ddbba0af4dee9c7788e) corrected the test to exercise the documented repeated-cleanup contract while still failing immediately if an ineligible record disappears. It did not loosen retention eligibility or change runtime delivery behavior.
- The corrected test passed 200 consecutive local runs. A clean full serialized Go suite, `go mod verify`, `go vet`, the 12 OpenCode/OpenClaw bridge tests and the release scanner also passed locally.
- Exact corrected-source legacy workflow: [run `33411330046`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33411330046), passed in 3m49s.
- Exact corrected-source native workflow: [run `33411330068`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33411330068), passed in 8m00s; all five build/test and five package-consumer jobs passed.

The final pre-tag evidence commit [`3d88a88`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/commit/3d88a88b71226fcb5e026f8f33928c0203152f5b) then passed [legacy run `33422783886`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33422783886) in 3m27s and [native run `33422783832`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33422783832) in 5m17s, including both five-job matrices. The immutable tag therefore contains the OpenClaw/Hermes implementation, the corrected retention gate and their pre-publication evidence.

## Publication and independent download verification completed 2026-09-01

- Annotated tag [`v0.2.0-rc.2`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/releases/tag/v0.2.0-rc.2) peels to `3d88a88`. Its independent tag-push [legacy run `33423401404`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33423401404) passed in 3m26s.
- The [native prerelease workflow `33423405193`](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33423405193) passed in 7m56s. From tag source it reran legacy, all five native build/test jobs, all five package-consumer jobs, exact archive/source inspection and the guarded publish job.
- All four authored release assets were downloaded again from the public Release. Their SHA-256 values matched the downloaded `SHA256SUMS`: Darwin amd64 `a43025a421e325886a73aec10a77b05ffe099b78ed9af939997453b4df3e6a70`, Darwin arm64 `a7fb60cdae31a50004f7c4188b23f130c30b1d78ca814757a507d9dfdb7515fd`, and Windows amd64 `06ba9a14f2689e1fa433a20790ed684efa25ae6cc9b33f34116677de2fafa46e`. The checksum asset itself matched the Release page digest `9ca34b30b49dae4791863acc5cd260220346bc4025cfdf0dd29c177f78cbd080`.
- The freshly downloaded Windows archive additionally passed the developer verifier from this checkout: exact inventory/manifest, four identical binaries, isolated `version`, `doctor` and all eight dry previews. No provider was configured and no phone notification was sent. The downloaded Mac archives were hash-verified locally but not executed on this Windows machine; matching-system execution evidence is the workflow's macOS consumer jobs.

## Evidence boundaries

Synthetic bridge/adapter tests can prove payload minimization, child filtering, deterministic spawn arguments, conservative malformed-input handling and package inventory. They cannot prove a future host version loads the integration, a task survives a host restart, a provider accepts a push, iOS/Android displays it, or the requested sound duration is honored. Sender acceptance remains different from phone arrival. Mac artifacts remain unsigned and not notarized; do not bypass Gatekeeper or remove quarantine.

Historical `0.2.0-rc.1` CI and release evidence belongs only to that tag. See [compatibility](native-compatibility.md), [installation](native-installation.md), [privacy](privacy.md) and [troubleshooting](troubleshooting.md) for the operational boundaries.
