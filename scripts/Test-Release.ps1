#requires -Version 7.4
param([string]$PackageRoot = (Split-Path $PSScriptRoot))
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath($PackageRoot)
$allowed = @(
    '.codex-plugin/plugin.json','.github/ISSUE_TEMPLATE/bug_report.md','.github/workflows/test.yml','.gitignore',
    'assets/agent-icons.json','config/defaults.json','docs/compatibility.md','docs/configuration.md','docs/privacy.md','docs/troubleshooting.md',
    'hooks/hooks.json','integrations/opencode/agent-task-notify.mjs','integrations/opencode/bridge.mjs','integrations/workbuddy/.gitattributes','integrations/workbuddy/.workbuddy-plugin/plugin.json','integrations/workbuddy/Build-Plugin.ps1','integrations/workbuddy/hooks/hooks.json','integrations/workbuddy/hooks/launch.sh','integrations/workbuddy/README.md',
    'scripts/agent-task-notify.ps1','scripts/Configure-Notifications.ps1','scripts/Install-Notifications.ps1','scripts/Test-Release.ps1','scripts/Uninstall-Notifications.ps1',
    'skills/agent-task-notify/SKILL.md','skills/agent-task-notify/agents/openai.yaml',
    'src/Adapters.psm1','src/Installation.psm1','src/Providers.psm1','src/Runtime.psm1','src/Settings.psm1','src/Storage.psm1',
    'tests/opencode-bridge.test.mjs','tests/provider-http.test.cjs','tests/Run.Tests.ps1','tests/runtime-process.test.cjs','tests/Test-ConfigAndEvents.ps1','tests/Test-Distribution.ps1','tests/TestHelpers.ps1','tests/Test-Installation.ps1','tests/Test-Runtime.ps1','tests/Test-StorageAndProviders.ps1',
    'tests/Test-Isolation.ps1','tests/Test-Diagnostics.ps1',
    'CHANGELOG.md','CONTRIBUTING.md','LICENSE','README.md','README.zh-CN.md','THIRD_PARTY_NOTICES.md'
)
$requiredRuntime = @('scripts/agent-task-notify.ps1','scripts/Configure-Notifications.ps1','scripts/Install-Notifications.ps1','scripts/Uninstall-Notifications.ps1','config/defaults.json','assets/agent-icons.json','src/Adapters.psm1','src/Installation.psm1','src/Providers.psm1','src/Runtime.psm1','src/Settings.psm1','src/Storage.psm1','integrations/opencode/agent-task-notify.mjs','integrations/opencode/bridge.mjs','integrations/workbuddy/Build-Plugin.ps1','integrations/workbuddy/hooks/hooks.json','integrations/workbuddy/hooks/launch.sh')
$all = @([IO.Directory]::EnumerateFiles($root, '*', [IO.SearchOption]::AllDirectories) | Where-Object { $_ -notmatch '[\\/](\.git|\.superpowers)([\\/]|$)' } | ForEach-Object { [IO.Path]::GetRelativePath($root, $_).Replace('\','/') } | Sort-Object)
foreach ($relative in $all) {
    if ($relative -match '(^|/)(credentials-(bark|ntfy)\.json|.*\.dpapi|.*\.log)$') { throw "Private runtime artifact is forbidden: $relative" }
    if ($relative -notin $allowed) { throw "Release file is not whitelisted: $relative" }
    $text = [IO.File]::ReadAllText((Join-Path $root $relative))
    $canary = 'PRIVATE' + '-CANARY'
    if ($text -match [regex]::Escape($canary)) { throw "Release privacy marker found: $relative" }
    if ($text -match '(?im)\b(?:task|turn|session|run)(?:[_-]?id)?[_-][A-Za-z0-9][A-Za-z0-9._:-]{11,}\b') { throw "Task identifier found: $relative" }
    # Native IDs need field context: artwork UUIDs and generic package IDs are legal.
    $uuid='[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}'
    $nativeField='(?:session[_-]?id|turn[_-]?id|conversation[_-]?id|generation[_-]?id|run[_-]?id|parent[_-]?id)'
    if ($text -match ('(?im)\b' + $nativeField + '["'']?\s*[:=]\s*["'']?' + $uuid + '\b') -or
        $text -match '(?im)\b(?:session[_-]?id|parent[_-]?id|id|run[_-]?id)["'']?\s*[:=]\s*["'']?(?:ses|msg)_[A-Za-z0-9]{12,}\b') { throw "Task identifier found: $relative" }
    if ($text -match '(?im)\b[A-Za-z]:[\\/]+Users[\\/][^\s"''`]+') { throw "Machine-specific user path found: $relative" }
    if ($text -match '(?im)\bauthorization\s*:\s*bearer\s+[A-Za-z0-9._~-]{12,}' -or $text -match '(?im)\b(?:api[_-]?key|device[_-]?key)\s*[:=]\s*["'']?[^\s"'']{12,}') { throw "Possible credential material found: $relative" }
}
foreach ($relative in @($allowed + $requiredRuntime | Select-Object -Unique)) { if (-not [IO.File]::Exists((Join-Path $root $relative))) { throw "Required release component is missing: $relative" } }
foreach ($relative in $all) { Write-Output $relative }
