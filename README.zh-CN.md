# Agent Task Notify

为 Codex、Claude Code、Cursor、Gemini CLI、OpenCode，以及实验性的 WorkBuddy、OpenClaw、Hermes Agent 本地长任务提供手机通知。原生 `0.2.0-rc.2` 面向 Windows x64 与实验性 Mac Intel/Apple Silicon；支持 Bark/iOS 及实验性 ntfy/Android。本项目不隶属于任何 Agent 或通知服务商。

## 原生候选快速开始

先读[原生安装](docs/native-installation.zh-CN.md)、[配置](docs/native-configuration.md)及[分级兼容证据](docs/native-compatibility.md)。通知程序不要求另装 PowerShell、Node、Python 或 Go，Agent 自身运行时和已核实的命令 shell 仍是宿主前提。Mac **未签名、未公证**，普通自动安装须停止，不得绕过系统保护。[验证记录](docs/native-validation.md)区分精确源码/run/归档实测及未完成门禁，不承诺稳定兼容或手机送达。

1. 从同仓库获取已审查的精确候选包，核对来源与 SHA256SUMS，解压到新的自有目录，不覆盖现用安装。
2. 让具备本机操作能力的 Agent 使用包内 Skill，或自行按 INSTALL 操作：确认平台、独立自有数据目录、宿主 shell、修改目标和信任流程。给出仓库链接/导入 Skill 不会自动安装或启动常驻服务。
3. 运行原生程序的 `version`、`doctor --data-directory ABS_DATA` 和干预前 `preview --agent codex --data-directory ABS_DATA`；这些不验证凭据或证明宿主已加载。
4. 用 `configure --provider bark --data-directory ABS_DATA`（或 `ntfy`）配置，只在本地隐藏提示输入凭据。先预览安装，再经授权加 `--apply`；WorkBuddy、OpenClaw、Hermes 使用包内各自的手动实验适配。
5. 仅明确需要真实响铃测试时使用 `preview --agent codex --send --data-directory ABS_DATA`。排队/服务接受不等于手机送达；启用的 Hook 达到阈值后也会自动发送。

默认 1800 秒起提醒、3600 秒长任务档位，Bark 目标 45/60 秒、铃声 `alarm`，均可调。连续 Bark 可能显示两条通知，时间为近似；ntfy 声音由手机控制，不是真电话。不保证每个 `needs_attention` 时刻，无额外遥测、现用旧版自动迁移、离线/重启恢复或恰好一次保证。不要在聊天中分享密钥、token 或私有端点，不要同时启用原生和旧版路线。

## 旧版 Windows 路线

以下保留 `0.1.0` 脚本/插件说明，不是原生包默认入口。旧数据与凭据保持独立。回退时先通过收据自有项卸载停用/移除原生登记（完成前保留原包），再显式启用一条已审查旧路线；不盲目恢复整份旧配置、不删数据、不自动读取或迁移旧密钥。

1. 从 GitHub 下载已审查的版本；不要使用 `curl | shell` 一类安装方式。
2. 请具备本机操作能力的 Agent 用 `scripts/Install-Notifications.ps1` 安装一个选定适配器。随附 Codex 插件 Hook 与脚本安装器是二选一，不能同时安装。
3. 在手机安装 Bark 或 ntfy 并开启通知；随后在本机运行 `scripts/Configure-Notifications.ps1`，只在本地隐藏输入框填写凭据。
4. 用 `scripts/agent-task-notify.ps1 -Mode Doctor` 查看非敏感诊断；它不表示宿主已经加载 Hook。
5. 如确实需要，请显式运行 `scripts/agent-task-notify.ps1 -Mode Preview -Agent codex -SendRealPush`；普通预览不会发送。

需要 Windows 与 PowerShell 7.4+，脚本会检查依赖。未支持的 Agent 版本不要启用，详见[兼容性](docs/compatibility.md)。不要把设备密钥、ntfy token 或服务端点发到聊天中。

Codex 插件在运行时使用宿主提供的 `PLUGIN_ROOT`。Hook 定义仍须经宿主 `/hooks` 流程人工审阅和信任；本项目不会自动修改信任设置。

参阅[配置](docs/configuration.md)、[隐私](docs/privacy.md)、[故障排除](docs/troubleshooting.md)与[贡献说明](CONTRIBUTING.md)。
