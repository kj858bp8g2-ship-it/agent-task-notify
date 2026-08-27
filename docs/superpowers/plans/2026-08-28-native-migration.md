# Native Notification Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the existing repository's native Windows/macOS notification tool, retaining six source adapters, both providers, configurable icons/sounds, safe installation, and evidence-qualified distribution.

**Architecture:** The approved native foundation supplies OS protection, private atomic files/locks, detached processes, and bounded delivery scheduling. Pure parsing/domain packages feed a persisted runtime; provider HTTP and installation transactions remain separate. Existing PowerShell code stays intact as the legacy release/reference, not a hidden native fallback.

**Tech Stack:** Go 1.27.0; pinned existing x/sys and go-keychain; standard-library JSON/HTTP/crypto; x/term v0.45.0 for local hidden input; host-loaded JavaScript only for OpenCode. Builds run on native Windows/macOS GitHub runners.

**Spec:** `docs/superpowers/specs/2026-08-28-native-runtime-design.md`

## Global Constraints

- Windows x64、Mac Intel/Apple Silicon；六个现有来源；两种现有推送通道；命令行安装、配置、检查、预览和卸载；平台发行包、测试及中英文说明。
- 通知工具本身不要求另装 PowerShell、Node.js、Python 或 Go；Agent 自身的依赖不在这个承诺内。
- 不擅自升级已安装的旧版，不读取旧设备密钥，不覆盖现用 Hook。Mac 初期仅按实际证据标注实验性支持，不以编译通过冒充完整兼容。
- 默认单次运行满 1800 秒触发提醒；满 3600 秒使用长任务档位。重复开始不重置同一运行，重复终止不创建第二份主任务。
- 默认 Bark 目标提醒长度为 45/60 秒，铃声 `alarm`；配置项继续可调。连续模式维持现有 30–60 秒范围，普通单次模式按声音本身播放。
- 主推送最多 5 次尝试，追加等待 5/15/30/60 秒。仅对明确可重试的传输/业务错误重试，续响保持一次发送。
- 严格验证 UTF-8、JSON 对象及必需字段；单个 Hook 输入限制为 4 MiB，超限以脱敏错误计数并返回中性响应。
- 原始输入、任务正文、原生会话标识不落盘。持久状态使用散列键。
- 任务创建时冻结设置和图标，重试与续响不读取中途修改的显示配置。
- 当前六个原生适配器均不保证发出 `needs_attention`，不将“所有需要用户配合的时刻必提醒”作为宣传文案。
- 新原生安装使用独立运行数据目录：Windows 用户本地应用数据下 `AgentTaskNotifyNative`；Mac 用户 `Library/Application Support/AgentTaskNotifyNative`。
- 禁止自行设计加密算法或把密钥和密文同放普通文件。后台读取不得弹出交互窗口。没有明文或 `security` CLI 生产回退。
- 完整 URL、令牌、响应正文与底层异常文本不进入日志。
- 安装前显示目标 Agent 和将修改的文件；先保存受保护的原始配置备份，再合并仅属本工具的 Hook。保留未知字段、其他 Hook 和权限。
- 卸载只移除收据确认的自有项，不整份恢复旧配置，不递归删除用户目录。
- WorkBuddy 发布自包含实验包，不猜测桌面配置文件。Codex 原 `notify` 配置不覆盖，Hook 信任由宿主/用户完成。
- 全部自动发送只面向本地假服务，不使用真实设备密钥；不接触现用安装/钥匙串/真实宿主配置。
- 没有获授权的签名身份时，Mac 产物只能明确标为未公证候选测试包；普通安装入口不自动启用，不移除 quarantine、不关闭 Gatekeeper、不购买服务。
- 本计划不实现跨项目累计计时、常驻重启恢复、GUI、Linux、真实电话、遥测、视频或自动发布短视频。

## Execution and verification contract

The foundation must have passed its final review before Task 1 starts. Accepted foundation source: `6039d4ade02b72130e766a7ac4ea0aace0dcca6d`, with the [five-platform native run](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33119263443) and [complete legacy regression run](https://github.com/kj858bp8g2-ship-it/agent-task-notify/actions/runs/33119263381) both successful. These are source/system/process gates, not final native-package or real-phone acceptance. Its exact source/OS/architecture evidence remains in `docs/native-validation.md`; this plan does not inherit end-user or phone claims from the legacy release. Use one implementation worker at a time and a task-scoped review. Reports record actual RED/GREEN, commands/output, deviations and unverified platform behavior. A Mac-only test written on Windows is not an executed RED: run it on the native matrix and label retrospective evidence honestly.

Each task updates the existing `config/native-source-files.json` exact inventory for its named new files only; `scripts/Test-Release.ps1` already consumes it. The public migration plan itself is added to that inventory at plan publication. Keep `tests/Test-Distribution.ps1` inventory/traversal/privacy regression checks intact, extending them only for a specific new packaging risk. No directory wildcards, private reports, local paths, runtime data, encrypted envelopes or receipt examples made from real data enter Git. The full legacy validation remains required before final publication, not after each tiny edit.

Development/test processes use isolated HOME/USERPROFILE/APPDATA/LOCALAPPDATA/ATN_DATA_DIRECTORY/TEMP and working directories. Build dependencies are not end-user dependencies. Runtime/error tests use synthetic values; no device endpoint is needed. `version` must remain side-effect free and launch with empty PATH. Mac system integration uses the disposable CI Keychain fixture; package tests run serially (`-p 1`) when they share that fixture.

The original repository/feature branch is retained. Publish one reviewed native candidate version in the existing repository; keep legacy installation available and unchanged. A foundation-only commit is not a product release.

## File and dependency map

| Task | Responsibility | Depends on |
| --- | --- | --- |
| 1 | Strict JSON, embedded resources, settings/events/keys and six adapters | Foundation |
| 2 | Whitelisted Bark/ntfy requests and bounded verified HTTP responses | 1 |
| 3 | Private reads/deletion/list checks, native data-path policy and one-bundle protected configuration | 1, 2, foundation secrets/store |
| 4 | Persisted lifecycle, worker execution, previews and bounded diagnostics | 1–3, foundation worker |
| 5 | Safe compare-before-replace host files preserving existing access metadata | Foundation store primitives, 3's final private-file lifecycle |
| 6 | Exact-owned hook/shim installation, protected backups and recoverable receipts | 1, 3, 5 |
| 7 | Native public/internal CLI, local hidden credential input, process integration | 1–4, 6 |
| 8 | OpenCode/WorkBuddy native packaging and cross-OS extracted-candidate gates | 1–7 |
| 9 | Native Skill/documentation and gated prerelease publication | 1–8 |

No circular imports: `core` imports only strictjson and embedded resources; adapters/providers consume core; configuration consumes providers/secrets/store; `runtime` composes core/configuration/providers/worker/store. `install` does not import runtime. CLI composes runtime/configuration/install; packages never import CLI.

---

### Task 1: Strict domain contracts and all six adapters

**Files:**
- Create: `internal/strictjson/object.go`, `internal/strictjson/object_test.go`.
- Create: `config/embed.go`, `assets/embed.go` (embed the existing JSON in the same directory, no copied defaults/catalog).
- Create: `internal/core/settings.go`, `internal/core/agents.go`, `internal/core/events.go`, `internal/core/keys.go`, `internal/core/core_test.go`.
- Create: `internal/adapters/adapters.go`, `internal/adapters/adapters_test.go`.
- Modify: `config/native-source-files.json`; do not change the legacy runtime/default JSON/catalog.
- Reference: `src/Settings.psm1`, `src/Adapters.psm1`, `tests/Test-ConfigAndEvents.ps1`.

**Interfaces:**

```go
// package strictjson
const MaxBytes = 4 << 20
var ErrInvalid error // fixed text, no input or wrapped parser error
func Object(data []byte) (map[string]json.RawMessage, error)
func String(value json.RawMessage) (string, error)
func Integer(value json.RawMessage) (int64, error)
func Boolean(value json.RawMessage) (bool, error)

// package config; package assets respectively; both return independent copies.
func DefaultsJSON() []byte
func AgentIconsJSON() []byte

// package core; JSON tags are the exact existing lower-camel keys.
type Settings struct {
    Provider string `json:"provider"`
    MinSeconds int64 `json:"minSeconds"`
    LongTaskSeconds int64 `json:"longTaskSeconds"`
    MediumRingSeconds int `json:"mediumRingSeconds"`
    LongRingSeconds int `json:"longRingSeconds"`
    Continuous bool `json:"continuous"`
    Level string `json:"level"`
    Volume int `json:"volume"`
    Sound string `json:"sound"`
    NtfyPriority int `json:"ntfyPriority"`
    EnableAttention bool `json:"enableAttention"`
    Icons map[string]string `json:"icons"`
}
type Agent struct {
    ID string `json:"id"`
    DisplayName string `json:"displayName"`
    IconURL string `json:"iconUrl"`
}
type Event struct {
    AgentID, SessionID, NativeRunID, EventType, Reason string
    IsChild bool
}
func Defaults() (Settings, error)
func ParseSettings(patch []byte, base Settings) (Settings, error)
func ValidateSettings(settings Settings) error
func AgentByID(id string) (Agent, error)
func Agents() []Agent
func Icon(agentID string, settings Settings) string
func RingSeconds(settings Settings, durationSeconds int64) int
func Key(parts ...string) string
func ValidKey(value string) bool

// package adapters: accepted=false,nil means a deliberately ignored event.
func Normalize(agentID string, data []byte) (event core.Event, accepted bool, err error)
func Neutral(agentID string, data []byte) []byte
```

- [ ] **Step 1: Add failing tests for strictness, exact resources and event contracts.**

```go
func TestThresholdsAndIndependentDefaults(t *testing.T) {
    s, err := core.Defaults()
    if err != nil { t.Fatal(err) }
    for _, tc := range []struct{ seconds int64; ring int }{
        {1799, 0}, {1800, 45}, {3599, 45}, {3600, 60},
    } {
        if got := core.RingSeconds(s, tc.seconds); got != tc.ring { t.Fatalf("%d: %d", tc.seconds, got) }
    }
    s.Icons["codex"] = ""
    next, _ := core.Defaults()
    if _, changed := next.Icons["codex"]; changed { t.Fatal("shared mutable defaults") }
}
func TestDuplicateKeysAtAnyDepthAreRejected(t *testing.T) {
    for _, raw := range []string{`{"x":1,"x":2}`, `{"x":{"y":1,"y":2}}`, `{"x":[{"y":1,"y":2}]}`} {
        if _, err := strictjson.Object([]byte(raw)); err == nil { t.Fatal("accepted duplicate") }
    }
}
```

Add table cases covering every existing `Test-ConfigAndEvents.ps1` positive/negative mapping, raw aliases, child fields, unknown events and neutral output. Include invalid UTF-8, trailing JSON, root array/null, 4 MiB+1, nesting >64, integer overflow/decimal/exponent/string/null, wrong-case setting keys, unknown settings/agent IDs and null icon maps. Include every catalog ID and icon override (valid HTTPS, empty, HTTP/invalid omitted). Mutating returned defaults/resources/agent slices must not affect future results.

- [ ] **Step 2: Run the new tests and record actual RED.**

Run `go test ./internal/strictjson ./internal/core ./internal/adapters`. Missing packages/interfaces are an honest first RED; once implemented, malformed-input tests must assert the actual invalid results rather than merely successful parsing.

- [ ] **Step 3: Implement bounded parsing, pure settings and exact normalization.**

`Object` checks size and UTF-8 before tokenizing; recursively tracks duplicate keys in objects (including inside arrays), depth ≤64, single root object and EOF. Use `json.RawMessage`/`json.Number` rather than permissive struct decoding or float64. Helpers reject null/wrong types; Integer accepts only JSON integer syntax and int64 range. Errors are fixed.

`ParseSettings` applies a validated partial object to a deep copy. Preserve all current validations: positive ordered thresholds; 30–60 ring targets; volume 0–10; ntfy priority 1–5; booleans exact; provider bark/ntfy; level critical/active/timeSensitive/passive; nonblank sound; icon override strings keyed only by six IDs. Bound sound and each icon override at4096UTF8bytes to keep valid settings/job records within their4MiB envelope; document this limit. A present empty/invalid-HTTPS icon disables it. No arbitrary map fields reach later layers.

```go
func RingSeconds(s Settings, seconds int64) int {
    if seconds < s.MinSeconds { return 0 }
    if seconds >= s.LongTaskSeconds { return s.LongRingSeconds }
    return s.MediumRingSeconds
}
func Key(parts ...string) string {
    if parts == nil { parts = []string{} }
    encoded, _ := json.Marshal(parts)
    sum := sha256.Sum256(encoded)
    return hex.EncodeToString(sum[:])
}
```

`Key` always encodes a string array (nil handled as empty array); ValidKey requires exactly 64 lowercase hex. Do not concatenate raw IDs with delimiters.

| Agent | Required IDs | Event mapping |
| --- | --- | --- |
| codex | session_id, turn_id | UserPromptSubmit→started; Stop→stopped |
| claude-code | session_id | UserPromptSubmit→started; Stop→stopped; StopFailure→failed |
| cursor | conversation_id, generation_id | beforeSubmitPrompt→started; stop+completed→stopped/completed; stop+error or aborted→failed |
| gemini-cli | session_id | BeforeAgent→started; AfterAgent→stopped |
| opencode | sessionId, runId, exact integer schemaVersion=1 | event started/stopped/failed; parentId means child |
| workbuddy | session_id | UserPromptSubmit→started; Stop→stopped (experimental) |

Unknown events/statuses and child events are ignored before state creation. Match legacy reason literals and aliases by reading `src/Adapters.psm1`; no task text is copied. `parent_session_id`/`parentSessionId` presence and SubagentStop are ignored; nonempty Claude agent_id is child. No adapter invents needs_attention. Neutral Codex/WorkBuddy is `{"continue":true}`; Cursor beforeSubmitPrompt likewise; every other neutral result is `{}`. Append exactly one newline at CLI layer, not here.

- [ ] **Step 4: Run focused tests, then all Go tests and vet; verify exact embedded bytes and source scan.**

Run `go test -count=1 ./internal/strictjson ./internal/core ./internal/adapters`, `go test -count=1 ./...`, `go vet ./...`, and the exact release scan. Record output. No HTTP, credentials or host files are used.

- [ ] **Step 5: Commit only this task's files with `feat: add native event and settings contracts`.**

### Task 2: Bark/ntfy protocol without credential leakage

**Files:**
- Create: `internal/providers/credential.go`, `internal/providers/payload.go`, `internal/providers/http.go`, `internal/providers/providers_test.go`.
- Modify: `config/native-source-files.json`.
- Reference: `src/Providers.psm1`, credential validation in `src/Storage.psm1`, `tests/Test-StorageAndProviders.ps1`, `tests/provider-http.test.cjs`.

**Interfaces:**

```go
type Credential struct {
    Endpoint string `json:"endpoint"`
    Token string `json:"token,omitempty"`
    AllowUnauthenticated bool `json:"allowUnauthenticated,omitempty"`
}
type Message struct {
    AgentID string `json:"agentId"`
    DurationSeconds int64 `json:"durationSeconds"`
    Reason string `json:"reason"`
    Preview bool `json:"preview"`
}
type Result struct { Accepted, Retryable bool; Diagnostic string }
func ParseCredential(provider string, data []byte) (Credential, error)
func ValidateCredential(provider string, credential Credential) error
func Send(ctx context.Context, settings core.Settings, credential Credential, message Message) Result
```

Credential input permits only endpoint/token/allowUnauthenticated with exact types (token/allowUnauthenticated are ntfy-only). Bound input by strictjson and endpoint/token lengths at 4096 bytes each; do not log input. `Send` creates only its provider's typed payload; callers cannot pass arbitrary HTTP fields or headers.

- [ ] **Step 1: Write failing loopback protocol tests.**

```go
func TestNtfyCannotRequestPhoneCall(t *testing.T) {
    var body map[string]json.RawMessage
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" { t.Error("wrong publish path") }
        if r.Header.Get("Call") != "" || r.Header.Get("X-Call") != "" { t.Error("phone header") }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Error(err) }
        io.WriteString(w, `{"id":"synthetic","event":"message","topic":"synthetic"}`)
    }))
    defer server.Close()
    s, _ := core.Defaults(); s.Provider = "ntfy"
    result := providers.Send(context.Background(), s, providers.Credential{Endpoint:server.URL+"/synthetic", AllowUnauthenticated:true}, providers.Message{AgentID:"codex",DurationSeconds:3600})
    if !result.Accepted { t.Fatal(result.Diagnostic) }
    for key := range body { if !map[string]bool{"topic":true,"title":true,"message":true,"priority":true,"icon":true}[key] { t.Fatal("unexpected field",key) } }
}
```

Cover six names/icons on both providers, continuous/single Bark (call only continuous), override/disable icons, generic preview/attention/terminal copy; 2xx with Bark code≠200 or wrong types; ntfy missing/wrong id/event/topic; duplicate/malformed/oversize responses; exact retry classification; no redirects (second server hit count remains 0); transport cancellation; credentials with userinfo/query/fragment/%/backslash/whitespace/dot segments; nonloopback HTTP; unauthenticated ntfy without explicit consent. Assert errors never contain planted endpoint/token/response markers. All servers bind loopback.

- [ ] **Step 2: Run `go test ./internal/providers` and record RED.**

- [ ] **Step 3: Implement whitelist payloads and bounded HTTP.**

Use HTTPS, or HTTP only for literal loopback/localhost (no arbitrary hostname resolution exception). Endpoint path segments are ASCII letters/digits/_/-, no empty or trailing segment; ntfy exactly one topic segment. Bark endpoint can have the server's allowed nonempty segments. No userinfo/query/fragment/escaped path. Token cannot contain CR/LF; no whitespace-only token. Empty token requires explicit AllowUnauthenticated.

Use `http.Client{Timeout:12*time.Second, CheckRedirect:...http.ErrUseLastResponse}` with standard system TLS trust. POST JSON UTF-8. Bound response reads to 64 KiB+1 and reject oversize; discard no raw bodies into diagnostics. Never use an insecure TLS bypass. Treat statuses 408/425/429/5xx retryable; other non2xx not retryable; malformed response/transport retryable unless context canceled. Bark requires exact integer code=200; use bounded numeric diagnostic class rather than arbitrary body text. ntfy requires nonblank string id, event exactly message, topic exactly requested.

```go
func retryableStatus(code int64) bool {
    return code == 408 || code == 425 || code == 429 || code >= 500 && code <= 599
}
```

Body text is generic, not agent output: `<name> 长任务已停止` and `任务已停止，请查看应用。耗时约 <floor minutes> 分钟。`; attention title `<name> 任务需要关注`; preview `<name> 通知预览` / `这是一条通用测试通知。`. Bark group/name, level, volume, sound, isArchive=0, optional icon and continuous call=1. ntfy topic, title/message, priority, optional icon only. Provider acceptance never means phone delivery. Extension uses the same frozen message/settings; timing belongs to worker.

- [ ] **Step 4: Run focused protocol tests, full Go/vet and exact source scan.**
- [ ] **Step 5: Commit `feat: add native Bark and ntfy providers`.**

### Task 3: Private read boundaries and atomic protected configuration

Carries foundation final-review M1: nativeOpen successful exclusive creation followed by initial handle/Fstat rejection can leave an empty file. Fix this lifecycle before configuration/runtime integration, with a controlled failure test proving cleanup of only this invocation's created name, never an existing lock/target. The prior state contains no written payload; do not report a historic secret leak. This task owns the necessary Windows/Darwin nativeOpen changes and focused tests in their existing store files, in addition to new readonly APIs. An unexported finish-open helper taking the actual handle and known-created flag may make initial validation/cleanup directly testable: a deliberately invalid/closed test-owned handle causes the real validation path to fail. No global production test switches or environment bypasses; exclusive creation owns cleanup, opening an existing lock never does.

**Files:**
- Create: `internal/store/read.go`, `internal/store/read_windows.go`, `internal/store/read_darwin.go`, `internal/store/read_test.go`, `internal/store/read_windows_test.go`, `internal/store/read_darwin_test.go`.
- Create: `internal/configuration/directory.go`, `internal/configuration/repository.go`, `internal/configuration/configuration_test.go`.
- Modify: `internal/store/files.go`, `internal/store/files_windows.go`, `internal/store/files_darwin.go` to reuse existing security checks for readonly handles and fix M1; `internal/store/files_windows_test.go`, `internal/store/files_darwin_test.go` for M1 controlled failure cases; `config/native-source-files.json`; `scripts/native-ci-macos.sh` and its exact command tests in `tests/native_ci_test.go` to serialise the full package suite with `-p 1` when using the shared disposable Keychain.
- Reference: existing `internal/secrets`, `internal/store`, `src/Storage.psm1`, `Save-ATNConfiguration` in `src/Installation.psm1`.

**Interfaces:**

```go
// store: fixed sentinels, no raw os.PathError escapes.
var ErrNotFound error
func ReadPrivate(path string, maxBytes int64) ([]byte, error)
func CheckPrivateDirectory(path string) error // read-only, never creates/repairs
func RemovePrivate(path string) error        // single validated regular file only

// configuration: Open validates/resolves only; Prepare creates fixed private dirs.
type Repository struct { /* unexported resolved directory/package root */ }
func Open(explicitDirectory, packageRoot string) (*Repository, error)
func (r *Repository) Directory() string
func (r *Repository) Prepare() error
func (r *Repository) Settings() (core.Settings, error)
func (r *Repository) Credential(provider string, mode secrets.AccessMode) (providers.Credential, error)
func (r *Repository) Configure(ctx context.Context, provider string, credential providers.Credential, settingsPatch []byte) error
func (r *Repository) Vault(ctx context.Context, mode secrets.AccessMode) (*secrets.Vault, error)
```

`Vault` provides the shared installation-scoped protection for installer backups. It creates installation identity only in Foreground under the configuration lock; Background with no identity/key fails. Identity is a random 16-byte lowercase hex string and never replaced when an existing file is malformed or unreadable. All errors are fixed categories. Configure already holds configuration.lock and must call an unexported lock-already-held helper, not recursively call the public locking Vault method. Background reads the stable identity without creating it. No path holds configuration.lock then attempts an installer lock.

- [ ] **Step 1: Write RED for private reads, path isolation and failed atomic commits.**

```go
func TestMissingReadDoesNotCreate(t *testing.T) {
    dir := filepath.Join(t.TempDir(),"private")
    if err := store.EnsurePrivateDirectory(dir); err != nil { t.Fatal(err) }
    path := filepath.Join(dir,"absent.json")
    _, err := store.ReadPrivate(path, 1024)
    if !errors.Is(err,store.ErrNotFound) { t.Fatal("wrong missing result") }
    if _, err := os.Lstat(path); !os.IsNotExist(err) { t.Fatal("read wrote a file") }
}
```

Cover nonprivate mode/DACL, extended ACL, symlink/reparse and ancestor links, nonregular file, max+1 bytes, read handle identity, no mutation of hostile file. Configuration tests use real synthetic DPAPI/CI Keychain: roundtrip both providers, same instance across saves/staging paths, second provider retained, corrupt envelope/identity safely rejected, Background does not create key, invalid patch leaves prior bundle byte-identical, deliberate commit failure preserves prior credential/settings together, plaintext markers absent in all files, independent source/package/old-runtime/home-root/other-owner rejection.

Tests must put state outside both source and executable/package root. Use private temp directories and clean only exact test-owned files. Any OS-specific failure fixtures use real access metadata, not a production bypass.

- [ ] **Step 2: Run focused store/configuration tests and record RED.**

- [ ] **Step 3: Implement safe existing-file reads and one-bundle transactions.**

Read with readonly/no-follow/noninheritable OS handles; validate regular type, private metadata and identity on opened handle, bound bytes, and check target consistency. Missing is distinct; no OPEN_ALWAYS/create for reads. Existing filesystem security is never repaired to force acceptance. Directory check is read-only. Single-file removal revalidates no links/private regular file; no recursion.

Directory resolution uses the explicit override, else ATN_DATA_DIRECTORY, else the spec's per-user default. Reject root/home directory itself, source/package root and descendants, known legacy AgentTaskNotify and `.codex/long-task-notify` directories and descendants, unsafe path components/foreign ownership. Reject invalid/control characters, relative paths and unresolved traversal; do not follow symlinks. Default base OS directory must exist. `Prepare` creates only root and fixed private `sessions`, `runs`, `jobs`, `locks`, `receipts`, `backups` leaf directories. No global path/PATH/config changes.

One private `configuration.json` object holds schemaVersion=1, the validated full settings snapshot, and at most bark/ntfy protected envelopes. A separate private `installation.json` holds schemaVersion=1 and the stable installationId. Use strict field/type/schema checks, bounded files, and never parse ciphertext as plaintext settings. Credential JSON is protected with purpose `credential:<provider>`; keep at most these two entries, preserve the other provider when updating. Configure holds configuration.lock with a 2-second context, validates everything first, protects with Foreground Vault, and performs ONE WriteAtomic to commit settings+credentials. A new instance ID can exist after a failed first configuration; it is not regenerated, leaked or treated as a successful config.

```go
release, err := store.Acquire(lockContext, filepath.Join(r.Directory(),"configuration.lock"))
if err != nil { return errConfigurationUnavailable }
defer release()
// Read/validate old bundle, protect new credential, marshal validated bundle.
return store.WriteAtomic(filepath.Join(r.Directory(),"configuration.json"), encoded)
```

Settings without a bundle return fresh defaults; invalid existing bundle fails, never silently resets. Credential() is readonly and Background for runtime. Clear sensitive byte buffers best-effort; document Go strings/GC are not a memory-erasure guarantee. Never print envelope/key/provider response. Installer plaintext backups go directly to Vault and private encrypted files, not temporary cleartext.

- [ ] **Step 4: Run Windows focused/full Go/vet/source scan; push reviewed task commits for actual Mac security tests before accepting this platform gate.**

Mac package serialisation must also apply to any corresponding CI counterfactual command only if needed for its shared fixture; preserve the exact counterfactual command assertion contract or update its focused tests together. Do not weaken locked-Keychain PASS/timeout checks.

- [ ] **Step 5: Commit `feat: add protected native configuration transactions`.**

### Task 4: Persisted lifecycle, delivery and safe diagnostics

**Files:**
- Create: `internal/runtime/state.go`, `internal/runtime/events.go`, `internal/runtime/delivery.go`, `internal/runtime/diagnostics.go`, `internal/runtime/runtime_test.go`, `internal/runtime/delivery_test.go`.
- Modify: `config/native-source-files.json`; any necessary foundation interface change must be proposed with its exact additional files before implementation, not silently expanded.
- Reference: `src/Runtime.psm1`, `tests/Test-Runtime.ps1`, `tests/runtime-process.test.cjs`, foundation `internal/worker/delivery.go`.

**Interfaces:**

```go
type Runtime struct {
    Repository *configuration.Repository
    Executable string
    Now func() time.Time
    Spawn func(executable, dataDirectory, jobKey string) error
    Send func(context.Context, core.Settings, providers.Credential, providers.Message) providers.Result
    Sleep worker.Sleep
}
type EventResult struct { JobKey, Diagnostic string; Queued bool }
type Diagnostic struct {
    SchemaVersion int `json:"schemaVersion"`
    Configured bool `json:"configured"`
    ActiveRuns int `json:"activeRuns"`
    PendingJobs int `json:"pendingJobs"`
    SendingJobs int `json:"sendingJobs"`
    SentJobs int `json:"sentJobs"`
    FailedJobs int `json:"failedJobs"`
    AmbiguousJobs int `json:"ambiguousJobs"`
    InputErrors int `json:"inputErrors"`
    Truncated bool `json:"truncated"`
    Receipts map[string]bool `json:"receipts"`
    DiagnosticCounts map[string]int `json:"diagnosticCounts"`
}
func New(repository *configuration.Repository, executable string) *Runtime
func (r *Runtime) Handle(ctx context.Context, event core.Event) (EventResult, error)
func (r *Runtime) RunJob(ctx context.Context, jobKey string) error
func (r *Runtime) Preview(ctx context.Context, agentID string, send bool) (EventResult, error)
func (r *Runtime) Inspect(ctx context.Context) (Diagnostic, error)
func (r *Runtime) RecordInputError(ctx context.Context) error
```

Seams are explicit function fields for deterministic clocks/loopback protocol tests, not environment-controlled production bypasses. `New` supplies time.Now, foundation SpawnWorker/Deliver-compatible Sleep, providers.Send; all normal production paths use those defaults.

- [ ] **Step 1: Write RED lifecycle/idempotence and durability assertions.**

```go
func TestNativeDuplicateStartDoesNotResetOrReopen(t *testing.T) {
    parent := t.TempDir()
    pkg := filepath.Join(parent,"package")
    if err := os.Mkdir(pkg,0700); err != nil { t.Fatal(err) }
    repository, err := configuration.Open(filepath.Join(parent,"state"),pkg)
    if err != nil { t.Fatal(err) }
    runner := notifier.New(repository,filepath.Join(pkg,"agent-task-notify"))
    now := time.Date(2026,1,1,0,0,0,0,time.UTC)
    runner.Now = func() time.Time { return now }
    var jobs []string
    runner.Spawn = func(_,_,key string) error { jobs=append(jobs,key); return nil }
    event := core.Event{AgentID:"codex",SessionID:"synthetic-session",NativeRunID:"synthetic-run",EventType:"started"}
    if _,err := runner.Handle(context.Background(),event); err != nil { t.Fatal(err) }
    now=now.Add(20*time.Minute)
    if _,err := runner.Handle(context.Background(),event); err != nil { t.Fatal(err) }
    now=now.Add(11*time.Minute)
    event.EventType="stopped"
    result, err := runner.Handle(context.Background(),event)
    if err != nil || !result.Queued { t.Fatal("long run missing") }
    event.EventType="started"
    if _,err := runner.Handle(context.Background(),event); err != nil { t.Fatal(err) }
    event.EventType="stopped"
    if _,err := runner.Handle(context.Background(),event); err != nil { t.Fatal(err) }
    if len(jobs)!=1 { t.Fatal("duplicate job") }
}
```

Add table cases for custom300/1200 thresholds299/300/1199/1200, every source, delayed/orphan stop, child/unknown ignore, same IDs across sources, nonnative new run, malformed/private state, wallclock reversal clamped0, invalid settings after start still ends timing, explicit attention disabled/one-shot/terminal independent. Verify raw native IDs/host input never persist. Settings/icons freeze even after reconfigure. Inject failure in each state/job/spawn boundary and assert fixed diagnostics and no false acceptance.

Delivery tests assert status `sending` and attempt count durable BEFORE Send, accepted state durable BEFORE extension, retries exactly five and waits5/15/30/60, permanent one attempt, no retries for accepted main after extension failure, no replay for sending or pending with attempts>0, concurrent held-lock no duplicate. Simulated persistence delay is subtracted from extension due time measured immediately after acceptance. ntfy/single/ring30 do not extend. Failed spawn remains pending/spawn-failed; no silent auto-recovery. Diagnostics leak checks plant secrets/paths/nativeIDs/body text; inspection caps1000 entries and reports truncation.

- [ ] **Step 2: Run `go test ./internal/runtime` and record RED.**

- [ ] **Step 3: Implement hashed records and the existing delivery scheduler composition.**

Preserve the legacy independent sessions/runs/jobs arrangement under the new private directory. Each strict schemaVersion=1 record contains only normalized enum/timestamp/hash/settings fields. Session key hashes agent+sessionID; native run key hashes source/session/nativeRunID; absent-native run key uses a fresh random nonce. Store only hashed correlation. Acquire per-session lock for ≤2seconds. Duplicate active starts preserve first timestamp; ended native IDs cannot restart; nonnative active duplicates preserve timestamp; later nonnative starts get a new key. Terminal changes run state before validating settings/threshold and never restarts it. Attention remains active and has its own deterministic job kind; only one per run. Job key hashes runKey+kind, avoiding terminal/attention duplicate collision. Reject unsupported record schema/types/statuses/keys.

State→job→spawn are individually atomic, NOT an all-or-nothing event transaction. A crash between records may lose a notification; retain diagnostic evidence where possible and document it. Do not invent replay/reconciliation service or pretend exactly-once guarantees. A failed spawn cannot be called sent.

At job creation deep-copy settings and persist the resolved selected icon as an explicit override (`frozen.Icons[event.AgentID]=core.Icon(event.AgentID,settings)`, including empty), so a later configuration/catalog change cannot select a different default. Persisted settings must contain the full schema's fields; partial patch semantics are for configure only, not for silently filling corrupt frozen jobs from new defaults.

RunJob validates a 64lowerhex key and takes an exclusive job lock with a 2-second acquisition context, separate from the longer HTTP delivery context. A busy/timeout lock returns a fixed unavailable category without mutating or replaying the job; there is no unbounded acquisition. Existing sent/failed/sending or pending+attempts>0 never replays. Load the job's frozen settings/message and Background credential. Reuse `worker.Deliver` unchanged; wrap its Attempt to commit sending/attempt count before network, persist accepted or failure after, and abort subsequent work on persistence failure. Capture monotonic time immediately after main HTTP acceptance, before persisting; the extension Sleep wrapper subtracts elapsed persistence time from ringSeconds-30. Report extension attempted/accepted separately; main accepted remains true if extension fails. Remove no job merely because send failed.

```go
// Shape of the composition; persistence is in the Attempt closure, not a copied retry loop.
report := worker.Deliver(ctx, job.RingSeconds, job.Settings.Provider=="bark" && job.Settings.Continuous, attempt, sleepUntilDue)
_ = report // persist only the schema's validated outcome fields
```

Inspect enumerates directory entries in bounded ReadDir batches (at most1001 seen, including invalid names), processes at most1000 across records and never returns full URLs, tokens, raw errors, receipt contents or paths. Do not call unbounded os.ReadDir and only truncate afterward. Receipt presence is only six known booleans. DiagnosticCounts uses fixed keys only: credential, transport, http-client, http-server, business-client, business-server, malformed-response, spawn-failed, state-write, invalid-record, ambiguous, other; unknown persisted text maps to other, never becomes an output key. Clamp every count1000. Configured means a syntactically valid bundle contains the selected provider envelope; it is not a phone-delivery or unlocked-Keychain assertion. Invalid input counter is a locked private schemaVersion1 integer, saturated1000. Cleanup removes only valid expired (>7days) sent/failed terminal records with no pending/sending extension; preserve active/pending/ambiguous, limit work1000 and recheck under the same run/job lock before single-file store removal. Run cleanup checks its derived terminal/attention jobs before removal. Native-ID duplicate suppression is bounded by the retained state, not a claim of permanent history after7day cleanup; document that inherited retention boundary. Preview dry run only reads the settings portion of the bundle, does not decrypt credentials or create durable jobs; send=true creates a synthetic preview job and queues it, reporting queued—not phone delivered.

Lock order is configuration independently, then session → its derived job keys in lexical order where both are needed. Delivery holds only its one job lock and never waits on a session lock. Retention runs after delivery releases its job lock, with a1second total cleanup context, and skips contention; no directory-wide retention on the synchronous Hook path, and Inspect is readonly. Run records store their hashed sessionKey for safe retention locking. Cleanup tests directly invoke the unexported retention method with synthetic expired records; no production clock/replay environment switch or service is introduced. Deadline limits waits, not a hard filesystem I/O wall-clock guarantee.

- [ ] **Step 4: Run focused runtime tests, all Go/vet and exact source scan; verify no extra default data-directory writes.**
- [ ] **Step 5: Commit `feat: add native persisted notification runtime`.**

### Task 5: Compare-before-replace host files without changing access rights

**Files:**
- Create: `internal/hostfile/hostfile.go`, `internal/hostfile/hostfile_windows.go`, `internal/hostfile/hostfile_darwin.go`, `internal/hostfile/hostfile_test.go`, `internal/hostfile/hostfile_windows_test.go`, `internal/hostfile/hostfile_darwin_test.go`.
- Create: `internal/winfile/rename_windows.go` with the permission-agnostic handle rename primitive used by both private and host writers.
- Modify: `internal/store/files_windows.go` to call the extracted primitive without changing its owner/DACL validation; existing `internal/store/files_windows_test.go` only to retain concrete replacement behavior coverage; `config/native-source-files.json`.

**Interfaces:**

```go
type Snapshot struct { Data []byte; Exists bool; /* opaque path/identity/access metadata/digest */ }
func Read(path string, maxBytes int64) (Snapshot,error)
func Replace(path string, before Snapshot, replacement []byte) error
func Remove(path string, before Snapshot) error

// package winfile (Windows-only): the caller already owns/validated the handle.
func Replace(handle windows.Handle, target string) error
```

Snapshot is bound to its original absolute path and cannot be forged through exported access metadata. Read is nofollow, bounded, owner-checked, and side-effect-free. Existing parent directory must be owned and nonlinked; ancestors cannot be symlink/reparse. The host directory is not required to be private0700/strictDACL because it belongs to the Agent. Reject unsafe target/metadata rather than normalizing its permissions. This is protection against accidental/observed external edits, not a sandbox against a malicious same-owner process continuously renaming ancestors.

- [ ] **Step 1: Write actual OS metadata and conflict RED tests.**

```go
func TestExternalEditIsNotOverwritten(t *testing.T) {
    path := filepath.Join(t.TempDir(),"host.json")
    if err := os.WriteFile(path,[]byte(`{"other":1}`),0600); err != nil { t.Fatal(err) }
    before, err := hostfile.Read(path, 4096)
    if err != nil { t.Fatal(err) }
    if err := os.WriteFile(path,[]byte(`{"external":2}`),0600); err != nil { t.Fatal(err) }
    if err := hostfile.Replace(path,before,[]byte(`{"ours":3}`)); err == nil { t.Fatal("overwrote external edit") }
    got, _ := os.ReadFile(path)
    if string(got)!=`{"external":2}` { t.Fatal("external data changed") }
}
```

Test absent target appearing after snapshot, present target disappearing, content same but metadata changed, a different bound path, symlink/reparse/nonregular/foreign owner, target read-only/DACL denial, temporary security BEFORE writing replacement, original ACL/mode/owner preserved after successful replacement, failure leaves original or unambiguous committed outcome and only exact-owned temp cleaned. Windows test a delete-sharing reader sees whole old/new file and nondelete-sharing handle safely blocks with unchanged budget. Mac real extended ACL (not just mode), owner and file flags require native tests; never clear an existing Agent ACL.

- [ ] **Step 2: Run focused hostfile tests and record RED.**

- [ ] **Step 3: Implement platform snapshot/replace/remove with access preservation.**

Use a same-directory exclusively created random temporary file with owner-only access, copy validated original metadata before writing any replacement bytes, write+sync+close, recheck original identity/content+access fingerprint, then use an atomic name replacement. Windows must preserve the original owner/group/DACL/protection semantics. Move only the existing FileRenameInfoEx buffer/flags/retry block into `winfile.Replace`, preserving bounded21attempt/20×10ms policy; both store and hostfile validate their own appropriate access metadata before calling it. No duplicated retry implementation or whole-writer refactor. Do not pass IGNORE_READONLY, alter ACLs for success, or use a fallback known to break the contract. Avoid ReplaceFileW assumptions: documented1176/1177 can move/remove the original on failure.

Darwin can copy original security metadata using native `fcopyfile` with COPYFILE_SECURITY=STAT|ACL and verify it; preserve mode, owner/group, extended ACL and relevant restrictive flags. Source metadata validation happens on the opened handle. Use nofollow exclusive temps and native rename after checks. Files with unsupported/uncopyable access metadata safely fail. New target defaults to owner-only and inherits no unexpected ACL; do not change the parent.

After replacement read back and confirm expected bytes and access metadata before reporting success. If atomic replacement succeeded but postverify fails, return a fixed ambiguous/verification category; installer must retain its pending receipt rather than claim rollback. Remove requires a matching live snapshot and deletes only that single regular file; never recursive. Third parties do not honor our locks, so document the remaining final-check/replace race and do not claim compare-and-swap atomicity against them.

- [ ] **Step 4: Run focused/full Windows tests; task acceptance requires actual4Mac metadata tests after reviewed push.**
- [ ] **Step 5: Commit `feat: preserve host configuration access metadata`.**

### Task 6: Safe installation and exact-owned uninstall

**Files:**
- Create: `internal/install/registry.go`, `internal/install/commands.go`, `internal/install/receipt.go`, `internal/install/install.go`, `internal/install/install_test.go`, `internal/install/commands_test.go`.
- Modify: `config/native-source-files.json`.
- Reference: `src/Installation.psm1`, `tests/Test-Installation.ps1`, official contract links in `docs/compatibility.md`, and existing OpenCode bridge.

**Interfaces:**

```go
type Options struct { AgentID, ConfigPath, Executable, PackageRoot, CommandShell string }
type Plan struct { AgentID, TargetPath, Action string; Experimental bool; /* opaque expected snapshot/owned entries */ }
func PlanInstall(ctx context.Context, repository *configuration.Repository, options Options) (Plan,error)
func ApplyInstall(ctx context.Context, repository *configuration.Repository, plan Plan) error
func PlanUninstall(ctx context.Context, repository *configuration.Repository, agentID string) (Plan,error)
func ApplyUninstall(ctx context.Context, repository *configuration.Repository, plan Plan) error
```

Plans are previews, not apply authorization. CLI requires explicit --apply. WorkBuddy returns a fixed manual-package-required error, never guessed .workbuddy/.codebuddy writes. Resolve existing known user-level paths (Codex .codex/hooks.json; Claude .claude/settings.json; Cursor .cursor/hooks.json; Gemini .gemini/settings.json; OpenCode XDG_CONFIG_HOME or .config/opencode). Explicit path is supported but must pass owner/path checks and be shown before apply. Validate executable as an absolute regular nonlinked owned file inside PackageRoot, without running an arbitrary supplied command. The production CLI supplies its own os.Executable(), not a user-controlled --executable flag. Distribution validation checks version/platform/archive identity before end users launch the tool; a local path alone is not publisher authentication.

- [ ] **Step 1: Write RED for non-destructive merges and receipts.**

```go
func TestPlanningWithoutShellDoesNotMutateHost(t *testing.T) {
    parent:=t.TempDir()
    pkg:=filepath.Join(parent,"package")
    if err:=os.Mkdir(pkg,0700);err!=nil { t.Fatal(err) }
    executable:=filepath.Join(pkg,"agent-task-notify")
    if err:=os.WriteFile(executable,[]byte("synthetic-not-executed"),0700);err!=nil { t.Fatal(err) }
    target:=filepath.Join(parent,"hooks.json")
    original:=[]byte(`{"version":1,"hooks":{"stop":[{"command":"echo synthetic-external"}]}}`)
    if err:=os.WriteFile(target,original,0600);err!=nil { t.Fatal(err) }
    repository,err:=configuration.Open(filepath.Join(parent,"state"),pkg)
    if err!=nil { t.Fatal(err) }
    _,err=install.PlanInstall(context.Background(),repository,install.Options{AgentID:"cursor",ConfigPath:target,Executable:executable,PackageRoot:pkg})
    if err==nil { t.Fatal("implicit unverified shell") }
    after,_:=os.ReadFile(target)
    if !bytes.Equal(after,original) { t.Fatal("plan changed host") }
}
```

Test all five automatic registries, refusal/manual result for WorkBuddy, other fields/arrays/permissions preserved, malformed/null/duplicate config rejected, exact unknown JSON numbers retained, duplicate registration, no receipt identical entries refused, edited owned entry refused, partial receipt transitions before/after host commit, external edits at each boundary, backup failure prevents activation, original exact bytes decrypt (including empty/nonexistent), no plaintext backups, uninstall no full restore, other files/backup/credentials retained, Cursor version1 validation, Codex notify/config.toml untouched. Bounded UTF16LE EncodedCommand legacy detection is decode-only, never execute. Include generated Windows and POSIX shell quoting tests with spaces/Chinese/apostrophe and reject metacharacter paths where safe quoting cannot be guaranteed.

- [ ] **Step 2: Run focused install tests and record RED.**

- [ ] **Step 3: Build registry/command rendering with explicit interpreter boundary.**

Official contract review must happen before registering a new native command. The current OpenAI hook page documents commandWindows but not the Windows command interpreter. Therefore CommandShell is explicit for automatic JSON-hook registration: `posix`, `cmd`, or `powershell`. It must match the actual host, verified by the installing Agent/user; no hidden automatic guess. On Mac `posix` can be shown as the documented shell contract only where the relevant host documentation supports it. Unknown shell produces a plan-only/manual instruction, not a mutation. A shell chosen by the host is an Agent dependency; the notifier does not install one. OpenCode uses direct `spawn(..., shell:false)` and needs no shell choice. Store the selected renderer in the receipt.

Renderer calls only the absolute packaged native executable with fixed `hook --agent <id> --data-directory <absolute>` arguments. POSIX single-quote escaping; PowerShell call operator with single-quote escaping; cmd quoted absolute arguments and conservative rejection of `%`, `!`, quotes, CR/LF and expansion/control characters. Do not interpolate event input or credentials. Rendered commands must be actually exercised under available Windows cmd/PowerShell/GitBash and CI Mac /bin/sh using a synthetic argv-printing helper, not just string assertions. Preserve legacy registration wrappers/matching shapes; do not add undocumented hook object keys.

Detect legacy script identifiers even inside bounded EncodedCommand; refuse mixing legacy/new/unreceipted identical hooks. Read adjacent Codex config.toml only for known legacy indicator, never mutate notify or print its content. Receipt confirmed ownership is not evidence of host load or plugin-route absence; installation docs require choosing plugin OR registration and checking host trust. OpenCode writes only exact `plugins/agent-task-notify.js` shim, importing the packaged bridge via JSON-escaped file URL and passing native executable/data directory options.

- [ ] **Step 4: Implement backup-before-activation and recoverable exact ownership.**

Hold a per-agent installation lock ≤2sec. Protect original exact host bytes using repository.Vault(Foreground), purpose `backup:<agentId>`, and store only encrypted envelope in private backups. Original absence is represented by an explicit existed=false plus protected empty bytes. No cleartext staging or log. Keep original backup for manual recovery, never blindly restore it on uninstall.

Use schemaVersion1 private receipts with known agent, validated absolute target/executable/package paths, renderer, original encrypted backup reference, exact owned entries or shim text, and state active/inactive/pending. Recompute owned command/shim shapes from receipt metadata rather than accepting arbitrary deletion entries. A pending transition contains beforeDigest, afterDigest, desired receipt and at most one previous committed receipt; no unbounded recursion. Write pending receipt BEFORE host mutation; compare/recheck host snapshot via hostfile; reread verify; then write committed receipt. If only first write happened, resolving a matching beforeDigest returns previous ownership. Matching afterDigest finalizes desired ownership. Any other bytes/access state is a conflict that preserves the pending record and host untouched; never restore stale full snapshots.

Uninstall removes only exact single matching owned entries or exact shim. Missing/edited/duplicate owned entries yield conflict and preserve them. Preserve unknown fields and every external hook. Use the same pending transition for safe host change, then inactive receipt; keep backups/credentials and no directory removal. Plan rendering may show the exact target paths, but diagnostic/error output must not include file contents, endpoints or raw lower errors.

- [ ] **Step 5: Run focused/full Go/vet/source scan; actual shell and native metadata evidence must be recorded, with untested host loading clearly separated.**
- [ ] **Step 6: Commit `feat: add reversible native agent registration`.**

### Task 7: Native CLI and actual isolated process workflow

**Files:**
- Modify: `internal/cli/app.go`, `internal/cli/app_test.go`, `tests/native_cli_test.go` for expanded safe usage text without relaxing version/no-side-effects checks; `cmd/agent-task-notify/main.go` only if required for input/dependency composition.
- Create: `internal/cli/commands.go`, `internal/cli/credentials.go`, `internal/cli/commands_test.go`, `internal/cli/credentials_test.go`, `tests/native_runtime_test.go`.
- Modify: `go.mod`, `go.sum`, `THIRD_PARTY_NOTICES.md` for reviewed x/term v0.45.0; `config/native-source-files.json`.
- Reference: existing CLI/version tests and `scripts/agent-task-notify.ps1` user behavior.

**Interfaces:**

Retain existing `cli.Run(args []string, stdout, stderr io.Writer) int` and Version semantics; introduce `RunWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int` for explicit synthetic input. Run uses os.Stdin and delegates; no tests accidentally consume actual terminal. Hidden terminal reader is a narrow function internally backed by x/term.ReadPassword; tests use synthetic terminals or an injected reader at that boundary, not secret production flags.

- [ ] **Step 1: Write RED for CLI argument contracts and process safety.**

```go
func TestConfigureRejectsSecretArgument(t *testing.T) {
    var out, errors bytes.Buffer
    code := cli.RunWithInput([]string{"configure","--provider","bark","--endpoint","planted-secret"},strings.NewReader(""),&out,&errors)
    if code != 2 || strings.Contains(out.String()+errors.String(),"planted-secret") { t.Fatal("unsafe argument handling") }
}
```

Assert exact version unchanged and zero filesystem writes; help/unknown/duplicate flags; malformed/4MiB hook bytes neutral exit0 with bounded error counter; no secret argument accepted/echoed; configure invalid patch/nonterminal without explicit stdin mode fails safely; dry preview no send/state writes/credential decryption; real preview queues with honest wording; install/uninstall default plan no mutations, --apply changes synthetic targets only; doctor fixed fields/no identifiers. Process tests build once into Chinese/space temp package directory and launch with empty PATH/isolated user/data/CWD, hook→worker→local server; require hook stdout EOF≤2sec, real child does not inherit pipes, retry and once-only extension observed, no unbounded subprocess or undisclosed services.

- [ ] **Step 2: Run focused CLI/process tests and record RED.**

- [ ] **Step 3: Implement the exact public and internal commands.**

Public:
`version`; `configure --provider bark|ntfy [--settings-file PATH] [--credential-stdin] [--data-directory PATH]`; `doctor [--data-directory PATH]`; `preview --agent ID [--send] [--data-directory PATH]`; `install --agent ID [--config-path PATH] [--command-shell posix|cmd|powershell] [--apply] [--data-directory PATH]`; `uninstall --agent ID [--apply] [--data-directory PATH]`.

Internal: `hook --agent ID [--data-directory PATH]`; `worker --data-directory PATH --job KEY`. Hook omitted directory follows the same validated environment/per-user default as other commands, for the self-contained WorkBuddy package. Installed registrations/shims always pass their explicit resolved directory. Worker requires both exact arguments. Reject all arbitrary commands, unknown/duplicate arguments and malformed keys. No endpoint/token/password CLI options. Executable/package root derives from os.Executable, not untrusted hook input; explicit paths validated by configuration/install.

No args/help return concise usage, version remains pure. Active commands return0success/2usage-or-invalidinput/1safeoperationalfailure; fixed diagnostic text and never echo user arguments. Hook always returns the appropriate neutral JSON+one newline and exit0 even on notify/input error; no stdout logs. Bound reading to4MiB+1, strictUTF8. Worker produces no output and cannot request interactive secrets. Installation plans show action/agent/target and experimental boundary; without--apply no mutations.

Configure prompts locally for hidden endpoint and, for ntfy, token; no terminal input echo. Explicit `--credential-stdin` accepts only the validated credential JSON from stdin; normal nonterminal input refuses rather than hanging. Settings file is nonsecret strict settings patch, limited4MiB and no raw parser errors. Keep unauthenticated ntfy privacy warning local and require its explicit boolean; no assumption that an obscure topic is private. Credentials stay on device; never ask users to paste them into agent chat. Use x/term v0.45.0, pinned hash/license recorded in notices; do not invent terminal masking.

Preview dry displays only provider/agent/ring target/continuous/experimental properties, not full private endpoint or arbitrary icon URL. Preview--send explicitly authorizes a queued synthetic notification; report queued, not phone delivered, and use runtime's frozen settings/icon/retry path. Doctor returns fixed safe structured counts, no full paths or receipt bodies. Runtime errors do not stop the Agent's work.

- [ ] **Step 4: Exercise full Windows and4Mac process matrix; record actual run/commit, no fake Host E2E claims.**

Test real process first transient send failure then success (5sec first retry), duplicate stop no second job, Bark ring31 extension about1sec after accepted main (bounded scheduling tolerance, not iOS claim), ntfy no continuation, Unicode events and source paths, malformed/oversize and absent credential safe behavior. Unit tests cover all5retry attempts without real105sec sleep; if full real cap isn't rerun, state that exact boundary. Include native CLI subprocess access tests when Keychain locked and no deadlock under shared fixture.

- [ ] **Step 5: Commit `feat: expose native notification commands`.**

### Task 8: Host bridges and verified native packages

**Files:**
- Modify: `integrations/opencode/bridge.mjs`, `tests/opencode-bridge.test.mjs`; preserve legacy mode and fail closed on invalid explicit native executable (no silent fallback). `integrations/opencode/agent-task-notify.mjs` remains its one-default-export wrapper; no extra distinct plugin export. Lifecycle order remains the existing tested contract.
- Create: `cmd/package-native/main.go`, `tests/native_package_test.go`; package verification is an explicit subcommand of cmd/package-native described below, not another script/runtime dependency.
- Create: `docs/native-installation.md`, `docs/native-installation.zh-CN.md` with complete minimum candidate install/safety instructions needed in the package; Task9 expands public guidance without deleting those boundaries.
- Create: `integrations/workbuddy/native/hooks.json`, `integrations/workbuddy/native/launch.sh`, `integrations/workbuddy/native/.gitattributes`; retain legacy launch/Build-Plugin.ps1 unchanged.
- Modify: `.github/workflows/native.yml`, `scripts/native-ci-macos.sh` only for full gates/package calls, `tests/native_ci_test.go` for the new exact canonical artifact naming/dependency checks while retaining all security/restore/guard regressions; `internal/cli/app.go`, `internal/cli/app_test.go`, `tests/native_cli_test.go` for candidate version0.2.0-rc.1; `config/native-source-files.json`.
- Reference: existing bridge/WorkBuddy contract and strict builder manifest. Task8 packages the existing Skill as a temporary transitional artifact; final distribution is gated until Task9 updates it to native guidance and re-tests final packages.

**Interfaces:**

OpenCode `options.executable` or host context `agentTaskNotifyExecutable` explicitly selects native mode (options wins); property presence with an invalid value is failure, not legacy fallback. Preserve the current dataDirectory option/context/env precedence and pass it as native --data-directory only when present; otherwise hook resolves its validated default. Use absolute validated executable and argument array `hook --agent opencode` with `shell:false`, preserve stdin UTF8 and bounded outputs/errors; source lifecycle bridge remains existing tested contract. Do not require separately installed Node; OpenCode loads JS in its own runtime. Legacy pwsh/hookPath mode is available only when native mode was not explicitly requested. Native invalid executable emits fixed `bridge-native-unavailable` and no legacy launch.

`go run ./cmd/package-native build --source-root ABS --binary ABS --platform windows-amd64|darwin-amd64|darwin-arm64 --version 0.2.0-rc.1 --output ABS` is a developer packaging command, not an end-user installer. It is a separate main package, not compiled into cmd/agent-task-notify. Output is a strict-list Windows ZIP or Mac tar.gz plus SHA256SUMS for the generated archive. Reject preexisting output/ambiguous version or platform mismatch; do not recursively copy source/data directories.

`go run ./cmd/package-native verify --archive ABS --checksums ABS --platform PLATFORM --version 0.2.0-rc.1 --extract-to ABS` verifies exact checksum/name/content/mode/architecture/manifest and extracts only into a new empty explicitly-owned directory. The developer verification then runs the extracted executable with empty PATH, isolated user/data/temp/CWD and expected exact version, doctor and dry preview. It never sends notifications, creates real Keychain entries, or accesses the user's default directories. Reject path traversal, absolute/duplicate archive entries, symlinks, extra files, oversized archive members (binary100MiB; every text1MiB), corrupt checksums/manifests, wrong platform and preexisting extraction directory. No ZIP permission assumption for Mac.

- [ ] **Step 1: Add RED bridge and package tests before implementation.**

```js
test('invalid explicit native executable never falls back to pwsh', async () => {
  const {createAgentTaskNotify}=await import('../integrations/opencode/bridge.mjs');
  const codes=[];let spawned=0;
  const plugin=await createAgentTaskNotify({executable:'relative-native',diagnostic:code=>codes.push(code),spawn(){spawned++;return completedChild(()=>{})}})({client:{session:{async get(){return {data:{id:'root'}}}}}});
  await plugin.event({event:{type:'message.updated',properties:{info:{sessionID:'root',id:'u',role:'user'}}}});
  assert.equal(spawned,0);
  assert.deepEqual(codes,['bridge-native-unavailable']);
});
```

`completedChild` is the existing helper in `tests/opencode-bridge.test.mjs`, not a new production API.

Use the existing bridge's real test harness/names rather than adding an unrelated competing harness; assert native spawn executable/args/shell:false/stdin and original ordering/error boundaries. Package tests inspect exact archive paths, version/platform manifest, executable architecture/mode, embeddeddefaults+sixicons behavior, no .git/.superpowers/agent_memory/credentials/receipts. Extract into Chinese/space path, run actual archived binary with empty PATH and isolated directories, compare full version and perform drydoctor/preview (no sends). Mac archive retains executable mode and contains conspicuous UNSIGNED-CANDIDATE.txt. WorkBuddy manual package is self-contained exactlist and native wrapper selects packaged architecture; no automatic settings writes.

- [ ] **Step 2: Run focused Node/Go tests and record RED.**

- [ ] **Step 3: Implement native packages and honest platform selection.**

Package only native binary, required thin host bridge/manual WorkBuddy resources, license/notices, nonsecret install instructions, native Skill, version/platform manifest and Mac unsigned marker. Default settings/icons are embedded; do not duplicate mutable runtime copies. Exact archive entries: binary at root (`agent-task-notify.exe` or `agent-task-notify`); `LICENSE`; `THIRD_PARTY_NOTICES.md`; `manifest.json`; `INSTALL.md`; `INSTALL.zh-CN.md`; `skills/agent-task-notify/SKILL.md`; `skills/agent-task-notify/agents/openai.yaml`; `integrations/opencode/agent-task-notify.mjs`; `integrations/opencode/bridge.mjs`; `workbuddy/.workbuddy-plugin/plugin.json`; `workbuddy/hooks/hooks.json`; `workbuddy/hooks/launch.sh`; `workbuddy/runtime/<binary>`; and Mac-only `UNSIGNED-CANDIDATE.txt`. WorkBuddy resources are copied from the new native directory, its manifest from the existing reviewed plugin descriptor with release version set; legacy source hooks/build script stay unchanged. The duplicated WorkBuddy native binary is deliberate for a self-contained manually importable subfolder. Mac launch.sh mode0755/LF; native hooks call it through host Bash using CODEBUDDY_PLUGIN_ROOT, fallback CLAUDE_PLUGIN_ROOT, as the existing experimental shared-runtime contract. It chooses the packaged current-platform binary, calls `hook --agent workbuddy`, and preserves neutral/error semantics. No guessed desktop config or extra runtime installation.

The native Skill is install guidance, not automatic Codex hook activation: no legacy .codex-plugin/hooks are copied into this candidate. Codex uses the native install command with explicit host shell and subsequent host trust. Keep the legacy source/plugin route documented separately; never double-register it with native hooks. This is a deliberate package boundary, not a claim that installing a Skill enables background notification.

Create same-release threeplatform archives; never ship local keys/encrypted backup/state/logs/tests/toolchains. Go builds use trimpath and pinned deps. Retain download source+hash verification and explain hashes alone are not publisher authentication. No curl|shell, global runtime installer, administrator request, certificate purchase or quarantine bypass. `manifest.json` schemaVersion1 stores exact version/platform/binary SHA256 and the fixed nonsecret file list; checksum file is outside the archive and names only its basename.

CI builds/tests on Windows x64 andMac15/26 botharches and runs bridge checks. Canonical release archives are produced by windows-latest, macos-15 and macos-15-intel (three assets), not arbitrarily selected from five independent builds. A dependent package-check matrix runs on all five platforms, downloads the corresponding canonical architecture artifact, verifies/extracts it, and launches that exact executable. Thus Mac26 checks the actual Mac15-built candidate, not only a second source build. Build/test/bridge jobs stay contents:read; no release is created in Task8. Preserve pinned checkout/setup-go/upload actions, pin any new action to a verified official commit and record source. Keep exact build failures visible; no artifact upload after failed build/test. Artifacts contain archives/checksums rather than bare Mac executable ZIPs. No downloaded executable is run until manifest/hash/platform checks pass.

- [ ] **Step 4: Run native/bridge/source/package tests, independent task review and actual5platform extracted-canonical-package checks.**

Windows controller additionally downloads the reviewed real archive, verifies exact GitHub artifact hash, then runs package verify in an isolated directory. Mac runtime evidence comes from the actual canonical artifact extracted on all4native runners. Record exact sourcecommit, artifactIDs, archivehashes, OS/arch and test scope; no realHost/phone or signing claim.

- [ ] **Step 5: Commit `feat: package and verify native notification candidates`.**

### Task 9: Native installation guidance and gated prerelease

**Files:**
- Create: `docs/native-compatibility.md`, `docs/native-configuration.md`, `.github/workflows/native-release.yml`.
- Modify: `docs/native-installation.md`, `docs/native-installation.zh-CN.md`, `integrations/workbuddy/README.md`, `skills/agent-task-notify/SKILL.md`, `skills/agent-task-notify/agents/openai.yaml`, `README.md`, `README.zh-CN.md`, `docs/configuration.md`, `docs/troubleshooting.md`, `docs/privacy.md`, `docs/compatibility.md`, `docs/native-validation.md`, `CHANGELOG.md`, `config/native-source-files.json`.
- Modify: `.github/workflows/native.yml` to add workflow_call and main-branch push coverage and expose canonical artifacts for the publication workflow; `.github/workflows/test.yml` to add workflow_call while preserving push/pull_request behavior, so publication can require the actual unchanged legacy suites.
- Modify: `tests/native_package_test.go` to require native Skill/candidate wording and exact release workflow safety; existing source scanner tests only for concrete new validation assertions.

**Interfaces:**

Candidate version is `0.2.0-rc.1`, tag `v0.2.0-rc.1`; tag lookup at planning found no existing repository tags. Recheck immediately before publishing and never overwrite a tag/release. Native release workflow triggers only on a push of that exact tag (or explicit manual dispatch at that tag), never ordinary branch pushes, and validates CLI/source version equality. The controller creates/pushes the tag only after whole-migration review, main integration and fresh successful checks; this is the authorized publication action. It calls the native full/package gates and legacy workflow, then a single publication job with contents:write after all needs succeeded. Other jobs remain contents:read. Published assets are the three verified canonical archives and a combined SHA256SUMS, and release is explicitly prerelease. Repository is the existing kj858bp8g2-ship-it/agent-task-notify; no new repo/account/service. No long-lived token handling; GitHub's job token is scoped to this repository/job and not logged. Native main pushes get the same read-only gates; only the exact-tag workflow can publish.

- [ ] **Step 1: Write failing source/package tests for documentation and publication gates.**

Add concrete assertions that all6IDs/two providers/native commands/configdefaults/experimentalMacAndroid/noattentionguarantee/no-secret-chat appear in installed guidance, but don't merely freeze paragraphs verbatim. Validate the known release YAML structure: exact-tag push/manual triggers only, exact ref/version gate, prerequisite native+legacy jobs, write permission only final publisher, prerelease true, no quarantine bypass/unknown asset glob. Packaged Skill must no longer instruct native users to install pwsh or run legacy entry as the default. Existing legacy guidance remains reachable.

```go
func TestNativeGuideNamesAllSources(t *testing.T) {
    _,file,_,ok:=runtime.Caller(0)
    if !ok { t.Fatal("test source location unavailable") }
    root:=filepath.Dir(filepath.Dir(file))
    if _,err:=os.Stat(filepath.Join(root,"go.mod"));err!=nil { t.Fatal(err) }
    data,err:=os.ReadFile(filepath.Join(root,"docs","native-compatibility.md"))
    if err!=nil { t.Fatal(err) }
    for _,agent:=range core.Agents() {
        if !bytes.Contains(data,[]byte(agent.ID)) { t.Fatalf("missing compatibility row: %s",agent.ID) }
    }
}
```

This test uses the concrete test file location and checks go.mod, not an invented harness or arbitrary current working directory. Existing package tests may extract this repeated location check into a test-only helper.

- [ ] **Step 2: Run focused package/guide/workflow tests and record RED.**

- [ ] **Step 3: Update the Skill and public documentation for native-first assistance without overclaiming.**

Read Skill creation instructions first, then write concise setup/diagnose/preview/uninstall routing. A repository link lets a capable local Agent read docs and assist; it does not auto-install without platform/security/host checks or make Skill a daemon. Setup checks Windows/Mac architecture, verified release/source, writable owned data directory, host command shell/trust/legacy duplication, then local hidden credential input and explicit optional audible preview. No secrets in chat; no automatic notifications from merely reading the repo. Mac unsigned candidate is an explicit developer/experimental route, never ordinary auto-install default. Stop and explain unsigned/firstlaunch gaps without disabling security.

Document all6agents including eachicon and WorkBuddy; Bark/iOS and experimentalntfy/Android; tunable1800/3600thresholds/45/60targets/alarm; ntfy soundcontrolledbyphone/noCall; doubleBarknotification/approximate timing; agent-specific command-shell prerequisite and knownpaths; noattentionguarantee/no-nativeIDambiguity; retriesbounded/noofflineguarantee; privateprotectedstorage/backups; exact-owneduninstall; noactivelegacyautomigration; noextratelemetry; legacyrollbackinstructionsnotblindrestore. Explain UI icon is notification decoration, not application/systemsmall-icon replacement; remote icons maychangeandnotbrandlicense.

Publish separate evidence columns implemented/contract/system/process/package/realHost/phone for the NEW candidate. Mark Mac and Android experimental; no realMac/Android/otherAgent tests available. Keep recorded sourcecommit/run/OS/arch/keychainwarning/unsignedboundary. List known crash gaps (state→job→spawn, uncertain sends no replay, no service/restart guarantee), final-check race, readonly unsupportedfilesystems and sender acceptancevsphonearrival. Do not borrow historical legacyiPhone tests. README links to native instructions with evidence level and legacy instructions retained.

- [ ] **Step 4: Implement the least-privileged exact-tag publication workflow.**

Use local reusable workflow references for native and legacy gates so the checked-out tag runs its own exact tests. Preserve their original triggers and test budgets. Publication downloads only the three known canonical artifact names from this run; verifies each existing SHA256SUMS and exact fixed archive names; creates a combined checksum listing; invokes the GitHub CLI available on the official runner with GH_TOKEN supplied only as the built-in job token. `gh release view` must confirm absent before `gh release create ... --prerelease --verify-tag`; never edit/clobber existing assets. Fixed release notes link native installation/compatibility/validation at the tag and explicitly describe unsigned Mac/untested phones. No external repository secrets or new permissions at account level. Unit tests check conditions/needs/permissions and a simulated publication command plan; real publication happens only after controller whole-migration review and exact tag push, not during source tests.

The Linux publisher does not execute foreign Windows/Mac binaries or import platform-only notifier packages. Use the runner's Python standard library only for bounded, read-only archive/checksum/manifest inspection and exact source `const Version` validation; require one matching constant, three known platform manifests, and all versions equal the exact tag without `v`. No extraction or arbitrary archive paths are needed there. Actual binary execution remains the prerequisite canonical package-check matrix on its matching OS. These build/CI tools are not end-user dependencies.

```yaml
on:
  push:
    tags: ['v0.2.0-rc.1']
  workflow_dispatch:
permissions:
  contents: read
jobs:
  native:
    if: github.ref == 'refs/tags/v0.2.0-rc.1'
    uses: ./.github/workflows/native.yml
  legacy:
    if: github.ref == 'refs/tags/v0.2.0-rc.1'
    uses: ./.github/workflows/test.yml
  publish:
    needs: [native, legacy]
    if: github.ref == 'refs/tags/v0.2.0-rc.1' && success()
    permissions:
      contents: write
    runs-on: ubuntu-latest
    # Steps inspect exact source/archive versions; prerequisite OS jobs execute binaries.
```

This shows the required gate/permission structure; the actual workflow includes pinned checkout/download actions, full archive/hash checks and exact gh command arguments. The already-authorized authenticated git tag push triggers publication without needing a new connector/token. If GitHub job-token publication is genuinely denied, report the exact remaining external permission instead of exporting credentials or claiming release. No trigger or write permission runs during ordinary feature-branch development.

- [ ] **Step 5: Run all native/legacy/bridge/source/package checks, independent task review, actual CI and whole-migration review before publishing in the SAME repository.**

All version strings/manifests/changelog use0.2.0-rc.1. If signature/realMacfirstlaunch missing, label Mac unsigned candidate and prevent ordinaryauto-install; no fake stableMacclaim. Publish reviewed commits/assets/checksums/evidence only; user’s live Windows notifier/hooks/keys remain byte-identical. Verify remote tag/branch/content and actual release asset list afterward. Final docs CI evidence may be a reviewed docs-only follow-up; never attach a test result to the wrong sourcecommit. Defer video to the separate requested follow-up.

- [ ] **Step 6: Commit `docs: guide native setup and gate candidate publication`.**

## Plan self-review and closure

The controller records one preflight row per task and every shared-file/interface pair in the private execution ledger before dispatch. New file paths are deliberate; modification/reference paths are checked against the repository. The public source inventory includes this plan. Task completion requires both specification and quality verdicts, and platform acceptance remains conditional on the actual gates specified above. All unresolved observations and rulings reach the final whole-migration review and user handoff.
