# 原生候选安装 — 0.2.0-rc.1

本包仅为尚未发布的实验测试候选，目标为 Windows x64、Mac Intel 和 Apple Silicon。Mac **未签名、未公证**；如果系统阻止执行，请停止，不要移除 quarantine、绕过 Gatekeeper、关闭保护或请求管理员权限。编译/CI 通过不代表真实桌面首次授权、Agent 加载或手机送达已验证。包内 Skill 仍是过渡旧版指南，请勿用它安装本候选；正式分发必须等原生 Skill 更新并重新验证最终归档。

## 获取并校验

仅从已审查仓库的精确候选 workflow run/source commit 获取对应 `native-candidate-windows-amd64`、`native-candidate-darwin-amd64` 或 `native-candidate-darwin-arm64` artifact。其中应只有 Agent Task Notify（ATN）归档与 `SHA256SUMS`，归档分别为 `atn-native-0.2.0-rc.1-windows-amd64.zip`、`atn-native-0.2.0-rc.1-darwin-amd64.tar.gz` 或 `atn-native-0.2.0-rc.1-darwin-arm64.tar.gz`。先核对仓库、运行、提交和 artifact 来源，再信任校验和。SHA-256 能检测损坏，但攻击者可同时替换归档与校验和，哈希本身不是发布者身份认证。

解压前核对 SHA256SUMS 中唯一对应文件名与实际哈希。Windows 可用系统 `certutil -hashfile ARCHIVE SHA256`，Mac 可用 `shasum -a 256 ARCHIVE`。先查看归档列表：须符合 `manifest.json` 固定清单，不能有绝对路径、`..`、重复项或链接。只解到新的用户自有目录，不覆盖旧安装。Mac 使用 tar.gz 保留执行权限，不用裸程序 ZIP 转运。保留两份 INSTALL、manifest、许可、Skill 与 integrations。默认设置及六个 Agent 图标已内嵌，不另安装可变配置/图标副本。

开发者可在已审查源码目录、已有 Go 的环境执行严格检查：

```text
go run ./cmd/package-native verify --archive ABS_ARCHIVE --checksums ABS_SHA256SUMS --platform PLATFORM --version 0.2.0-rc.1 --extract-to ABS_NEW_FOLDER
```

占位符换成绝对路径，空格路径需引号。工具先验证哈希、精确名称/清单、大小、权限、平台、架构、两份相同程序和 manifest，再运行解出的程序；只接受当前 OS/架构。它创建新的明确用户自有目录，在源码外隔离 HOME/用户数据/temp/CWD、清空 PATH，运行 version、doctor 和六个干预前预览，不发通知、不打开 Keychain。它是开发工具，不是最终用户依赖。通知程序本身无需额外安装 PowerShell、Node、Python 或 Go；Agent 自身运行时及已验证的宿主命令 shell 仍由宿主提供。

## 先检查、配置，再显式安装

以下命令的 `agent-task-notify` 换成解压程序的绝对路径，Windows 文件名为 `agent-task-notify.exe`；按所在 shell 的规则加引号/调用操作符。选独立用户自有数据目录，不能在程序包、源码或旧版数据中。默认是 Windows `%LOCALAPPDATA%\AgentTaskNotifyNative` 或 Mac `$HOME/Library/Application Support/AgentTaskNotifyNative`，默认父目录必须存在。显式 `--data-directory ABS_DATA` 优先于 `ATN_DATA_DIRECTORY`，再到系统默认；后续所有步骤及 Hook 使用同一目录。

```text
agent-task-notify version
agent-task-notify doctor --data-directory ABS_DATA
agent-task-notify preview --agent codex --data-directory ABS_DATA
agent-task-notify configure --provider bark --data-directory ABS_DATA
```

需要时把 `bark` 换成 `ntfy`。凭据仅在本地终端输入，不要把令牌、设备密钥、完整私有 URL 或配置文件贴到 Agent 聊天。非终端输入必须显式加 `--credential-stdin`；可选 `--settings-file ABS_JSON` 读取本地非秘密设置补丁。凭据用 Windows DPAPI/Mac Keychain 保护，无明文或 security CLI 回退。普通 preview 不发送；只有显式 `preview --agent ID --send` 才授权测试通知。包验证不得使用 `--send`。

六个 ID：`codex`、`claude-code`、`cursor`、`gemini-cli`、`opencode`、`workbuddy`。前五种先预览：

```text
agent-task-notify install --agent codex --command-shell cmd --data-directory ABS_DATA
agent-task-notify install --agent codex --command-shell cmd --data-directory ABS_DATA --apply
```

Windows 示例仅适合确实使用 cmd 的宿主；只有验证相应宿主 shell 后才能选 `powershell` 或 `posix`。Mac 使用显式 `--command-shell posix`。不要把当前交互终端等同宿主 shell。`--config-path ABS_FILE` 可指定明确核实的配置：默认分别为用户 HOME 下 `.codex/hooks.json`、`.claude/settings.json`、`.cursor/hooks.json`、`.gemini/settings.json`；OpenCode 为 `$XDG_CONFIG_HOME/opencode/opencode.json` 或 `$HOME/.config/opencode/opencode.json`，实际只写同级 `plugins/agent-task-notify.js`。宿主目标父目录（包括 OpenCode plugins）须已存在且用户自有。检查打印的目标/计划，获得授权后才用 `--apply`。保留未知字段、其它 Hook；受保护备份和收据是安装前提，冲突停止。Codex 原 `notify` 配置不覆盖，宿主信任/审批由用户完成。导入 Skill **不代表**后台 Hook 已启用。

OpenCode 在自身运行时加载 JS bridge，再直接启动原生程序。保留根程序旁的 `integrations/opencode`；原生安装器生成含显式程序/数据路径的 shim。不能直接导入旧默认 wrapper 替代原生安装。旧 PowerShell/plugin 路线保持独立，不与原生路线双重注册。本候选不升级现用安装、不读取旧设备密钥；旧源码脚本保持不变。

## WorkBuddy 手动实验包

只通过明确验证的宿主插件导入流程导入完整 `workbuddy` 子目录，不猜测或自动修改桌面配置。该目录自含 `.workbuddy-plugin/plugin.json`、`hooks/hooks.json`、`hooks/launch.sh` 和对应平台 `runtime` 程序。宿主 Bash 使用 `CODEBUDDY_PLUGIN_ROOT`，空缺时回退 `CLAUDE_PLUGIN_ROOT`。配置时运行 **WorkBuddy 子目录内**的程序副本，并保持同一数据目录；非默认目录须向宿主提供 `ATN_DATA_DIRECTORY`。OS 不匹配、程序缺失或执行失败均返回中性响应。桌面加载、取消和真实送达尚未验证；不要同时安装旧版和原生 WorkBuddy 包。

## 默认行为与卸载

支持的任务开始事件启动计时；重复开始不重置。默认 1800 秒触发、3600 秒进入长任务档位，Bark 目标提醒 45/60 秒，铃声 `alarm`。这不是电话，无法承诺精确实际响铃时长。当前适配器不保证覆盖所有 `needs_attention` 时刻；后台运行与重试依赖宿主/OS 支持。

先运行 `uninstall --agent ID --data-directory ABS_DATA` 预览，审查授权后再加 `--apply`。只移除收据确认的原生自有项，不整份恢复配置或删除用户/数据目录。WorkBuddy 通过已验证宿主插件流程手动移除。在用途明确前保留受保护备份和数据，不自动递归删除目录或移除 Keychain 条目。
