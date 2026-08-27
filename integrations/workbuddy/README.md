# WorkBuddy experimental wrapper

This is a **manual, experimental** package based on the documented shared CodeBuddy runtime compatibility layout (`.workbuddy-plugin/plugin.json`, root `hooks/`, `CODEBUDDY_PLUGIN_ROOT` with `CLAUDE_PLUGIN_ROOT` fallback). Consumer WorkBuddy desktop loading has **not** been verified. Stop does not guarantee cancellation events, and no native run ID is assumed.

From this directory, run `pwsh -NoProfile -File ./Build-Plugin.ps1 -OutputDirectory <new-output-directory>`. Only the explicit runtime/wrapper allowlist is copied. The resulting folder is self-contained; the source template itself is not an installable package. Keep settings and DPAPI credentials outside it in the default local application data directory. Configure notifications separately before opting in.

Review the generated hooks and use the host's documented manual local-plugin/marketplace workflow if your version supports it. We do not guess desktop settings paths or automatically enable/trust hooks. Requires PowerShell 7.4+ on PATH and the shared Windows runtime's Git Bash. Nothing is sent by the build; installing/enabling hooks opts into task notifications.

References: [plugin manifest compatibility](https://www.codebuddy.cn/docs/cli/plugins-reference), [hook contract](https://www.codebuddy.cn/docs/cli/hooks), [WorkBuddy plugins](https://www.codebuddy.cn/docs/workbuddy/Plugins).
