Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/TestHelpers.ps1"

function Assert-ReleaseRejects {
    param([scriptblock]$Action, [string]$Expected, [string]$Description)
    try { & $Action } catch {
        if ($_.Exception.Message -notmatch $Expected) { throw "$Description. Expected '$Expected', got '$($_.Exception.Message)'." }
        return
    }
    throw "$Description. Expected an exception."
}

function Copy-ATNReleasePackage {
    param([string]$Source, [string]$Destination, [string[]]$Files)
    foreach ($relative in $Files) {
        $target = Join-Path $Destination $relative
        [IO.Directory]::CreateDirectory((Split-Path $target)) | Out-Null
        [IO.File]::Copy((Join-Path $Source $relative), $target, $false)
    }
}

$packageRoot = Split-Path $PSScriptRoot
$release = Join-Path $packageRoot 'scripts/Test-Release.ps1'
$agents = @('codex','claude-code','cursor','gemini-cli','opencode','workbuddy')
$temp = Join-Path ([IO.Path]::GetTempPath()) ("atn-distribution-" + [guid]::NewGuid())
$staging = Join-Path $temp 'Agent Task Notify 中文 space'
try {
    $manifest = Get-Content -Raw -LiteralPath (Join-Path $packageRoot '.codex-plugin/plugin.json') | ConvertFrom-Json -AsHashtable
    Assert-Equal $manifest.name 'agent-task-notify' 'plugin manifest keeps the package identity'
    Assert-True (-not $manifest.ContainsKey('hooks')) 'plugin manifest leaves hook discovery to hooks/hooks.json'
    Assert-True ([IO.File]::Exists((Join-Path $packageRoot 'hooks/hooks.json'))) 'plugin hook configuration ships separately'
    Assert-True ([IO.File]::Exists((Join-Path $packageRoot 'skills/agent-task-notify/SKILL.md'))) 'plugin skill ships with package'

    $catalog = Get-Content -Raw -LiteralPath (Join-Path $packageRoot 'assets/agent-icons.json') | ConvertFrom-Json -AsHashtable
    Assert-Equal $catalog.Count 6 'catalog retains six source records'
    Import-Module (Join-Path $packageRoot 'src/Settings.psm1') -Force
    Import-Module (Join-Path $packageRoot 'src/Providers.psm1') -Force
    foreach ($agent in $agents) {
        $record = @($catalog | Where-Object { $_.id -ceq $agent })
        Assert-Equal $record.Count 1 "$agent has exactly one catalog record"
        foreach ($field in @('sourceUrl','iconUrl','mimeType','width','height','sha256','usage')) { Assert-True ($record[0].ContainsKey($field)) "$agent includes reviewed $field metadata" }
        $settings = @{provider='bark';continuous=$true;level='critical';volume=7;sound='alarm';ntfyPriority=4;icons=@{}}
        $main = New-ATNPayload $agent $settings 3600 stopped
        $extension = New-ATNPayload $agent $settings 3600 stopped -Extension
        $preview = New-ATNPayload $agent $settings 0 stopped -Preview
        Assert-Equal $main.icon $record[0].iconUrl "$agent main payload keeps its icon"
        Assert-Equal $extension.icon $record[0].iconUrl "$agent extension payload keeps its icon"
        Assert-Equal $preview.icon $record[0].iconUrl "$agent preview payload keeps its icon"
    }

    $safeFiles = @(& $release -PackageRoot $packageRoot)
    [IO.Directory]::CreateDirectory($staging) | Out-Null
    Copy-ATNReleasePackage $packageRoot $staging $safeFiles
    & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null

    $config = Join-Path $temp 'host/config.json'
    $data = Join-Path $temp 'isolated-data'
    & (Join-Path $staging 'scripts/Install-Notifications.ps1') -Agent claude-code -ConfigPath $config -DataDirectory $data | Out-Null
    Assert-True ([IO.File]::Exists((Join-Path $data 'receipts/claude-code.json'))) 'copied package installer writes an isolated receipt'
    Assert-True ([IO.File]::Exists($config)) 'copied package installer writes only the supplied host config'

    $hookConfig = Get-Content -Raw -LiteralPath (Join-Path $staging 'hooks/hooks.json') | ConvertFrom-Json -AsHashtable
    $command = $hookConfig.hooks.UserPromptSubmit[0].hooks[0].commandWindows
    $encoded = [regex]::Match($command, 'EncodedCommand\s+([A-Za-z0-9+/=]+)').Groups[1].Value
    $decoded = [Text.Encoding]::Unicode.GetString([Convert]::FromBase64String($encoded))
    Assert-True ($decoded -match 'PLUGIN_ROOT') 'portable command resolves PLUGIN_ROOT at execution'
    $oldRoot = $env:PLUGIN_ROOT; $oldData = $env:ATN_DATA_DIRECTORY
    try {
        $env:PLUGIN_ROOT = $staging
        $env:ATN_DATA_DIRECTORY = $data
        $result = '{"hook_event_name":"UserPromptSubmit","session_id":"synthetic-session","turn_id":"synthetic-turn"}' | & pwsh -NoLogo -NoProfile -NonInteractive -EncodedCommand $encoded
        Assert-Equal $LASTEXITCODE 0 'copied portable command exits successfully'
        Assert-Equal ($result | Out-String).Trim() '{"continue":true}' 'copied portable command emits neutral hook response'
        Assert-Equal @(Get-ChildItem (Join-Path $data 'runs') -Filter '*.json').Count 1 'portable command writes run only to explicit isolated data'
        Assert-Equal @(Get-ChildItem (Join-Path $data 'sessions') -Filter '*.json').Count 1 'portable command writes isolated session'
    } finally { $env:PLUGIN_ROOT = $oldRoot; $env:ATN_DATA_DIRECTORY = $oldData }

    $unlistedSource = Join-Path $staging 'src/private-notes.md'
    [IO.File]::WriteAllText($unlistedSource, 'synthetic', [Text.UTF8Encoding]::new($false))
    Assert-ReleaseRejects { & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null } 'not whitelisted: src/private-notes.md' 'release scanner rejects unlisted src descendant'
    [IO.File]::Delete($unlistedSource)
    $unlistedDoc = Join-Path $staging 'docs/screenshot.png'
    [IO.File]::WriteAllBytes($unlistedDoc, @(0,1,2))
    Assert-ReleaseRejects { & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null } 'not whitelisted: docs/screenshot.png' 'release scanner rejects unlisted docs descendant'
    [IO.File]::Delete($unlistedDoc)
    $fakeCredential = Join-Path $staging 'docs/credentials-bark.json'
    [IO.File]::WriteAllText($fakeCredential, '{"endpoint":"not-a-real-key"}', [Text.UTF8Encoding]::new($false))
    Assert-ReleaseRejects { & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null } 'Private runtime artifact' 'release scanner rejects fake credential artifact before generic whitelist failure'
    [IO.File]::Delete($fakeCredential)

    $legal = Join-Path $staging 'docs/privacy.md'
    $original = [IO.File]::ReadAllText($legal)
    $nativeUuid=(@('01234567','89ab','4cde','8fab','0123456789ab') -join '-')
    foreach ($field in @('session_id','turn_id','conversation_id','generation_id','sessionId','runId')) {
        [IO.File]::WriteAllText($legal, $original + "`n" + ('{"' + $field + '":"' + $nativeUuid + '"}'))
        Assert-ReleaseRejects { & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null } 'Task identifier' "native $field is rejected"
    }
    foreach ($pair in @(@('sessionID','ses_'),@('id','msg_'))) {
        [IO.File]::WriteAllText($legal, $original + "`n" + ($pair[0] + ': ' + $pair[1] + '0123456789abcdefghijkl'))
        Assert-ReleaseRejects { & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null } 'Task identifier' 'OpenCode native ID is rejected'
    }
    [IO.File]::WriteAllText($legal, $original + "`n" + 'session_id is a field name; id: package-name; sessionID: synthetic; docs/session-guide.md; https://example.invalid/artwork/' + $nativeUuid + '/icon.png')
    & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null
    foreach ($case in @(
        @{content=('task_' + '0123456789abcdef'); expected='Task identifier'; description='task identifier'},
        @{content=('D:' + '/Users/synthetic-user/release'); expected='Machine-specific user path'; description='forward-slash user path'},
        @{content=('Authorization:' + ' Bearer ' + 'synthetictoken0123456789'); expected='Possible credential material'; description='Bearer header credential'}
    )) {
        [IO.File]::WriteAllText($legal, $original + "`n" + $case.content, [Text.UTF8Encoding]::new($false))
        Assert-ReleaseRejects { & (Join-Path $staging 'scripts/Test-Release.ps1') -PackageRoot $staging | Out-Null } $case.expected "release scanner rejects $($case.description)"
    }
    [IO.File]::WriteAllText($legal, $original, [Text.UTF8Encoding]::new($false))
    Write-Host 'PASS Test-Distribution'
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
