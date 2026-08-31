# 原生候选安装 — 0.2.0-rc.2

本包是 Windows x64 与实验性 Mac Intel/Apple Silicon 的预发布候选，不是稳定兼容承诺。Mac **未签名、未公证**，普通 Agent 自动安装应停止并说明仅供明确授权的开发者实验路线；系统阻止执行时，不要移除 quarantine、绕过 Gatekeeper、关闭保护或请求管理员权限。编译/CI 通过不代表真实桌面首次授权、Agent 加载或手机送达已验证。包内 Skill 可协助本地安装，但阅读仓库、导入 Skill 不会自动安装、启用常驻服务或发送通知。发布须经过精确 tag 的原生/旧版/归档门禁，见[候选验证记录](https://github.com/kj858bp8g2-ship-it/agent-task-notify/blob/v0.2.0-rc.2/docs/native-validation.md)。

## 获取并校验

仅使用 `kj858bp8g2-ship-it/agent-task-notify` 已审查的精确 `v0.2.0-rc.2` prerelease（发布后），或精确候选 workflow run/source commit。发布资产是三个 Agent Task Notify（ATN）归档及合并的 `SHA256SUMS`：`atn-native-0.2.0-rc.2-windows-amd64.zip`、`atn-native-0.2.0-rc.2-darwin-amd64.tar.gz`、`atn-native-0.2.0-rc.2-darwin-arm64.tar.gz`。发布前的 `native-candidate-windows-amd64`、`native-candidate-darwin-amd64`、`native-candidate-darwin-arm64` artifact 各含一个归档及单条校验文件。先核对仓库、tag/运行、提交和 artifact 来源，再信任校验和。SHA-256 能检测损坏，但攻击者可同时替换归档与校验和，哈希本身不是发布者身份认证。不要使用下载后直接执行的 curl-to-shell 安装方式。

解压前核对 SHA256SUMS 中唯一对应文件名与实际哈希。Windows 可用系统 `certutil -hashfile ARCHIVE SHA256`，Mac 可用 `shasum -a 256 ARCHIVE`。先查看归档列表：须符合 `manifest.json` 固定清单，不能有绝对路径、`..`、重复项或链接。只解到新的用户自有目录，不覆盖旧安装。Mac 使用 tar.gz 保留执行权限，不用裸程序 ZIP 转运。保留两份 INSTALL、manifest、许可、Skill 与 integrations。默认设置及八个 Agent 图标已内嵌，不另安装可变配置/图标副本。

开发者可在已审查源码目录、已有 Go 的环境执行严格检查：

```text
go run ./cmd/package-native verify --archive ABS_ARCHIVE --checksums ABS_SHA256SUMS --platform PLATFORM --version 0.2.0-rc.2 --extract-to ABS_NEW_FOLDER
```

路径占位符须换成**经过清理规范化的 OS 原生绝对路径**，不能仅是 shell 认为可用的绝对字符串。开发验证器要求 `filepath.Clean(path) == path`：避免 `.`/`..`、多余分隔符及非根路径的末尾分隔符；Windows 使用原生反斜杠，不用正斜杠或混合分隔符。在 Windows PowerShell 中，已有归档/校验文件通过 `(Get-Item -LiteralPath 'ABS_ARCHIVE').FullName` 取得路径（校验文件同理）；已选定但仍不存在的新目标用 `[IO.Path]::GetFullPath('ABS_NEW_FOLDER')` 规范化。先将占位符换成所选绝对位置，传递所得值时加引号，尤其含空格时，例如 `--archive "$archive" --checksums "$checksums" --extract-to "$extract"`。这只是调用路径格式处理，不修复权限、不绕过其余检查。

工具先验证哈希、精确名称/清单、大小、权限、平台、架构、四份完全相同的程序和 manifest，再运行解出的程序；只接受当前 OS/架构。**调用者必须为新的 extract-to 目录选择源码外、已存在且自有的父目录**：隔离测试目录位于 extract-to 的同级，验证器本身不保证选址在源码外。它创建新的明确用户自有目录，隔离 HOME/用户数据/temp/CWD、清空 PATH，运行 version、doctor 和八个干预前预览，不发通知、不打开 Keychain。开发验证器需要对应 artifact 的单条 SHA256SUMS；若使用发布的合并校验文件，先将唯一匹配归档名的原始条目保存到独立本地文件，勿改哈希。它是开发工具，不是最终用户依赖。通知程序本身无需额外安装 PowerShell、Node、Python 或 Go；Agent 自身运行时及已验证的宿主命令 shell 仍由宿主提供。

## 先检查、配置，再显式安装

以下命令的 `agent-task-notify` 换成解压程序的绝对路径，Windows 文件名为 `agent-task-notify.exe`；按所在 shell 的规则加引号/调用操作符。选独立用户自有数据目录，不能在程序包、源码或旧版数据中。默认是 Windows `%LOCALAPPDATA%\AgentTaskNotifyNative` 或 Mac `$HOME/Library/Application Support/AgentTaskNotifyNative`，默认父目录必须存在。显式 `--data-directory ABS_DATA` 优先于 `ATN_DATA_DIRECTORY`，再到系统默认；后续所有步骤及 Hook 使用同一目录。

```text
agent-task-notify version
agent-task-notify doctor --data-directory ABS_DATA
agent-task-notify preview --agent codex --data-directory ABS_DATA
agent-task-notify configure --provider bark --data-directory ABS_DATA
```

`bark` 面向 iOS，`ntfy`/Android 仍为实验性。凭据仅在本地终端隐藏输入，不要把令牌、设备密钥、完整私有 URL 或配置文件贴到 Agent 聊天或命令参数。非终端输入必须显式加 `--credential-stdin`，从授权的本地来源提供；可选 `--settings-file ABS_JSON` 读取本地非秘密设置补丁。凭据与精确宿主原文备份用 Windows CurrentUser DPAPI/Mac Keychain 支持的加密保护，无明文或 security CLI 回退。仅显式配置或获授权应用安装/卸载时的前台 Vault 操作可创建/授权 Keychain 密钥；doctor/干预前 preview 只读设置和封套语法存在性，不解密、不打开 Keychain，不证明凭据有效、密钥已解锁或服务已接受。普通 preview 不发送；只有显式 `preview --agent ID --send` 才授权可选的真实响铃测试。排队/服务接受不等于手机送达，包验证不得使用 `--send`。ntfy 须核实服务端主题 ACL；随机名字或 token 本身不证明隐私，未认证 opt-in 表示接受暴露风险。

八个 ID：`codex`、`claude-code`、`cursor`、`gemini-cli`、`opencode`、`workbuddy`、`openclaw`、`hermes`。前五种支持收据自有的自动登记，应用前先预览：

```text
agent-task-notify install --agent codex --command-shell cmd --data-directory ABS_DATA
agent-task-notify install --agent codex --command-shell cmd --data-directory ABS_DATA --apply
```

Windows 示例仅适合确实使用 cmd 的宿主；只有验证相应宿主 shell 后才能选 `powershell` 或 `posix`。Mac 使用显式 `--command-shell posix`。不要把当前交互终端等同宿主 shell。`--config-path ABS_FILE` 可指定明确核实的配置：默认分别为用户 HOME 下 `.codex/hooks.json`、`.claude/settings.json`、`.cursor/hooks.json`、`.gemini/settings.json`；OpenCode 的 `$XDG_CONFIG_HOME/opencode/opencode.json` 或 `$HOME/.config/opencode/opencode.json` 仅为定位器，实际只写同级 `plugins/agent-task-notify.js`。宿主目标父目录（包括 OpenCode plugins）须已存在且用户自有，安装器不会创建或修复宿主目录。缺失时先停止，核实宿主位置后在本地明确创建/验证，不以提权或修改权限绕过。已有空白、空内容或损坏 JSON 不会被替换，须显式处理。检查打印的目标/计划，获得授权后才用 `--apply`。保留未知字段、其它 Hook 及 JSON 数字原始表示；受保护备份和收据是前提，冲突停止。宿主配置上限 4 MiB、加密封套 6 MiB。两秒锁限制只针对获取锁的等待，不是 OS 调用或整个安装的硬超时；前台 Vault 授权先于锁。Codex 原 `notify` 不覆盖，宿主信任/审批由用户完成。导入 Skill **不代表**后台 Hook 已启用；收据也不能证明没有其他插件路线或宿主已经加载。

OpenCode 在自身运行时加载 JS bridge，再直接启动原生程序。保留根程序旁的 `integrations/opencode`；原生安装器生成含显式程序/数据路径的 shim。不能直接导入旧默认 wrapper 替代原生安装。旧 PowerShell/plugin 路线保持独立，不与原生路线双重注册。本候选保持用户现用安装不变，不自动升级、不读取旧设备密钥。独立的旧版源码路线包含已窄范围验证的 worker 启动锁修正，不代表所有历史计时失败都已确定原因。

## WorkBuddy 手动实验包

只通过明确验证的宿主插件导入流程导入完整 `workbuddy` 子目录，不猜测或自动修改桌面配置。该目录自含 `.workbuddy-plugin/plugin.json`、`hooks/hooks.json`、`hooks/launch.sh` 和对应平台 `runtime` 程序。宿主 Bash 使用 `CODEBUDDY_PLUGIN_ROOT`，空缺时回退 `CLAUDE_PLUGIN_ROOT`。配置时运行 **WorkBuddy 子目录内**的程序副本，并保持同一数据目录；非默认目录须向宿主提供 `ATN_DATA_DIRECTORY`。OS 不匹配、程序缺失或执行失败均返回中性响应。桌面加载、取消和真实送达尚未验证；不要同时安装旧版和原生 WorkBuddy 包。

## OpenClaw 与 Hermes 手动实验包

OpenClaw 使用完整 `openclaw` 子目录及官方 typed `before_agent_run`/`agent_end` 插件 Hook。按 `openclaw/README.md` 审阅后通过 OpenClaw CLI 安装/链接并启用，显式授予 `hooks.allowConversationAccess`，再重启和检查 Gateway。桥接器丢弃提示词/消息，只转发生命周期、会话与运行字段，并忽略已观测到的子 Agent 会话。任务中途重启 Gateway 会丢失内存关联；本项目未做真实 OpenClaw 宿主测试。

Hermes 使用完整 `hermes` 子目录及官方 Shell Hooks。把 `hermes/config.example.yaml` 的两个已审查命令合并进现有 `~/.hermes/config.yaml`，不得覆盖其它 Hook 或开启全局自动接受。`pre_llm_call` 开始一次 turn，`on_session_end` 结束成功/失败/中断；按 `hermes/README.md` 使用绝对路径（原生 Windows 用正斜杠），运行 `hermes hooks doctor`，首次逐项批准精确的 `(event, command)`。本项目未做真实 Hermes 宿主测试。

## 默认行为与卸载

支持的任务开始事件启动计时；重复开始不重置，重复结束不创建第二份主任务。默认可调设置为 `minSeconds: 1800`、`longTaskSeconds: 3600`、`mediumRingSeconds: 45`、`longRingSeconds: 60`、`continuous: true`、`level: critical`、`volume: 7`、`sound: alarm`、`ntfyPriority: 4`、`enableAttention: false`、`icons: {}`。阈值须正数且有序，铃声目标 30–60 秒、音量 0–10、优先级 1–5。本地 JSON 补丁如 `{"minSeconds":300,"longTaskSeconds":1200,"sound":"alarm"}` 通过 configure 的 `--settings-file` 提供，凭据仍在本地输入，不编辑程序内嵌默认值。Bark 连续模式由主通知和一次续响组成，手机历史可能显示两条；45/60 秒只是近似目标，不保证实际响铃长度。普通单次由声音本身决定。ntfy 声音由手机控制，不发送 Call/X-Call 或 Bark 专属声音字段；都不是真实电话。

八种图标分别对应 Codex/ChatGPT、Claude Code/Claude、Cursor、Gemini CLI/Gemini、OpenCode、WorkBuddy、OpenClaw、Hermes Agent，内嵌的是远程图标元数据。它们仅装饰通知，不替换应用或系统小图标；远程图片可变化，引用不授予品牌许可。`icons` 仅可按八个已知 ID 指定 HTTPS 图片，空字符串或无效图片地址会省略。`sound` 值及每个独立图标覆盖值均最多 4096 UTF-8 字节。每个任务创建时冻结设置和图标。没有额外遥测；选定服务必然收到其凭据及通用通知内容，不含任务正文、路径或原生 ID。本地加密不等于推送端到端加密。

当前适配器不保证覆盖所有 `needs_attention`，没有原生 run ID 的来源无法完全消除延迟 Stop 歧义。仅明确可重试错误最多五次主发送、间隔 5/15/30/60 秒，续响只发送一次。无离线、常驻服务、重启恢复、恰好一次或手机送达保证；state → job → spawn 有崩溃缺口，不确定发送不重放，最后检查也可能与新事件竞态。240 秒 worker context 是协作预算，不是硬 OS 杀进程/进程树约束。

先运行 `uninstall --agent ID --data-directory ABS_DATA` 预览，审查授权后再加 `--apply`。只移除收据确认的原生自有项，不整份恢复配置或删除用户/数据目录。卸载完成前保留原程序包，删除原包可能令文件系统身份验证安全拒绝；自有条目被外部编辑时先审查，不能盲目恢复。WorkBuddy、OpenClaw、Hermes 通过各自已验证的手动宿主流程移除，CLI 会有意拒绝对它们自动卸载。回退旧版先停用/移除原生登记，保留受保护数据和备份，再显式启用一条已审查旧路线及原来的独立数据；不自动读取/迁移旧密钥、不覆盖现用 Hook、不递归删除目录或移除 Keychain 条目。

只读/不支持的文件系统、非私有根、链接/reparse、未知所有者/安全元数据及部分 OS 合法名称会被拒绝，不修复权限。源码隔离只检查祖先 `.git` 或本工具 `go.mod` 加 `config/native-source-files.json` 的精确标记，不能普遍识别无标记源码，也不是万能防泄露保证。允许受信任 OS 所有者祖先不等于接受任意服务共有目录，私有根仍须当前用户所有。详见[原生兼容性](https://github.com/kj858bp8g2-ship-it/agent-task-notify/blob/v0.2.0-rc.2/docs/native-compatibility.md)与[独立旧版指南](https://github.com/kj858bp8g2-ship-it/agent-task-notify/blob/v0.2.0-rc.2/README.zh-CN.md#旧版-windows-路线)。
