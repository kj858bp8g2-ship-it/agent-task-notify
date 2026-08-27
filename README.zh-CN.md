# Agent Task Notify

为 Codex、Claude Code、Cursor、Gemini CLI、OpenCode 以及实验性的 WorkBuddy 本地长任务提供私有 Windows 手机通知。本项目不隶属于任何 Agent 或通知服务商。

## 快速开始

1. 从 GitHub 下载已审查的版本；不要使用 `curl | shell` 一类安装方式。
2. 请具备本机操作能力的 Agent 用 `scripts/Install-Notifications.ps1` 安装一个选定适配器。随附 Codex 插件 Hook 与脚本安装器是二选一，不能同时安装。
3. 在手机安装 Bark 或 ntfy 并开启通知；随后在本机运行 `scripts/Configure-Notifications.ps1`，只在本地隐藏输入框填写凭据。
4. 用 `scripts/agent-task-notify.ps1 -Mode Doctor` 查看非敏感诊断；它不表示宿主已经加载 Hook。
5. 如确实需要，请显式运行 `scripts/agent-task-notify.ps1 -Mode Preview -Agent codex -SendRealPush`；普通预览不会发送。

需要 Windows 与 PowerShell 7.4+，脚本会检查依赖。未支持的 Agent 版本不要启用，详见[兼容性](docs/compatibility.md)。不要把设备密钥、ntfy token 或服务端点发到聊天中。

Codex 插件在运行时使用宿主提供的 `PLUGIN_ROOT`。Hook 定义仍须经宿主 `/hooks` 流程人工审阅和信任；本项目不会自动修改信任设置。

参阅[配置](docs/configuration.md)、[隐私](docs/privacy.md)、[故障排除](docs/troubleshooting.md)与[贡献说明](CONTRIBUTING.md)。
