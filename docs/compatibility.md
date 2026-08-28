# Compatibility routes

The NEW native `0.2.0-rc.1` candidate has its own [compatibility/evidence matrix](native-compatibility.md) and [exact-source validation record](native-validation.md). Mac and Android are experimental, and historical legacy phone/host evidence below does not validate the native candidate.

## Legacy Windows evidence

Contract sources below were checked on 2026-08-27. They establish adapter shape only; they do not establish a supported host version or real-host loading unless noted.

Local release tests ran on Windows with PowerShell 7.6.4 and Node.js 24.15.0. PowerShell 7.4 is the declared minimum, not an executed version matrix. Complete system plugin/skill validators ran locally; CI runs project contract checks, not those formal system validators. Reproducible formal-validator CI integration is deferred.

| Source | Implemented | Contract-tested | Actual Agent host E2E | Phone E2E | Reproducible contract basis |
|---|---:|---:|---:|---:|---|
| Codex | Yes | Yes | Historical local evidence only | Historical Bark evidence only | [OpenAI hooks docs](https://learn.chatgpt.com/docs/hooks), checked 2026-08-27; `PLUGIN_ROOT` and hook review/trust boundary. Host version not verified. |
| Claude Code | Yes | Yes | No | No | [Claude Code hooks](https://code.claude.com/docs/en/hooks), checked 2026-08-27; UserPromptSubmit/Stop/StopFailure. Host version not verified. |
| Cursor | Yes | Yes | No | No | [Cursor hooks](https://cursor.com/docs/hooks), checked 2026-08-27; conversation/generation IDs and stop status. Host version not verified. |
| Gemini CLI | Yes | Yes | No | No | [Gemini CLI hooks reference](https://geminicli.com/docs/hooks/reference/), checked 2026-08-27; BeforeAgent/AfterAgent. Host version not verified. |
| OpenCode | Yes | Yes | No | No | [Plugin docs](https://opencode.ai/docs/plugins/), [SDK docs](https://opencode.ai/docs/sdk/), and linked `dev` types/loader checked 2026-08-27; upstream branch was not release-pinned, so re-review before claiming a supported version. |
| WorkBuddy | Experimental package | Build contract tested | No | No | [CodeBuddy plugin reference](https://www.codebuddy.cn/docs/cli/plugins-reference) and [hooks](https://www.codebuddy.cn/docs/cli/hooks), checked 2026-08-27; shared runtime contract, not a WorkBuddy desktop version guarantee. |

Android and all other real Agent hosts are experimental until independently tested. No adapter promises native `needs_attention`; sources without a native run ID have ambiguous delayed Stop correlation. Current local Codex/Bark history is not new release regression evidence.

WorkBuddy’s `Build-Plugin.ps1` creates a strict-whitelist self-contained manual package. Do not copy its thin source wrapper alone, and do not expect automatic desktop settings mutation, cancellation events, or confirmed loading.

Choose either the Codex plugin route or script registration, never both. Automatic plugin-plus-script double-registration detection is not implemented. A receipt is ownership metadata, not evidence that a host loaded the hook.
