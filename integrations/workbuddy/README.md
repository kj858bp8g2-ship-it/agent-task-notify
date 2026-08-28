# WorkBuddy experimental wrapper

## Native 0.2.0-rc.1 candidate

Use the **complete `workbuddy` subfolder from the matching native archive**, not this source wrapper or `Build-Plugin.ps1`. The package has `.workbuddy-plugin/plugin.json`, `hooks/hooks.json`, `hooks/launch.sh` and its matching `runtime/agent-task-notify` (Windows `.exe`). No separate notifier PowerShell/Node/Python/Go dependency exists; the host still supplies Bash and its verified plugin workflow. Mac is unsigned, not notarized and experimental: ordinary auto-install must stop; never bypass OS protections.

Configure the WorkBuddy runtime copy locally with `configure --provider bark --data-directory ABS_DATA` (or experimental ntfy/Android); keep credentials out of chat. Use the same independent owned data path, and give the host `ATN_DATA_DIRECTORY` when nondefault. The host supplies `CODEBUDDY_PLUGIN_ROOT`, with `CLAUDE_PLUGIN_ROOT` fallback. Manually review/import the entire folder only through a verified workflow; no desktop configuration path is guessed, and native `install --agent workbuddy` intentionally refuses automatic registration. Importing the Skill alone enables nothing.

Desktop loading, cancellation, real-host and phone behavior are untested for this candidate. Synthetic packaged launcher tests do not establish them. No native run ID or all-`needs_attention` guarantee is assumed. Icons decorate notifications, not system app icons; no brand license is granted. Remove through the verified host plugin workflow, preserving protected data; never register native and legacy WorkBuddy together. See [native installation](../../docs/native-installation.md), [compatibility](../../docs/native-compatibility.md) and [validation](../../docs/native-validation.md).

## Legacy Windows build route

This is a **manual, experimental** package based on the documented shared CodeBuddy runtime compatibility layout (`.workbuddy-plugin/plugin.json`, root `hooks/`, `CODEBUDDY_PLUGIN_ROOT` with `CLAUDE_PLUGIN_ROOT` fallback). Consumer WorkBuddy desktop loading has **not** been verified. Stop does not guarantee cancellation events, and no native run ID is assumed.

From this directory, run `pwsh -NoProfile -File ./Build-Plugin.ps1 -OutputDirectory <new-output-directory>`. Only the explicit runtime/wrapper allowlist is copied. The resulting folder is self-contained; the source template itself is not an installable package. Keep settings and DPAPI credentials outside it in the default local application data directory. Configure notifications separately before opting in.

Review the generated hooks and use the host's documented manual local-plugin/marketplace workflow if your version supports it. We do not guess desktop settings paths or automatically enable/trust hooks. Requires PowerShell 7.4+ on PATH and the shared Windows runtime's Git Bash. Nothing is sent by the build; installing/enabling hooks opts into task notifications.

References: [plugin manifest compatibility](https://www.codebuddy.cn/docs/cli/plugins-reference), [hook contract](https://www.codebuddy.cn/docs/cli/hooks), [WorkBuddy plugins](https://www.codebuddy.cn/docs/workbuddy/Plugins).
