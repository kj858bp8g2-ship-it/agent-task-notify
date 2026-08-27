#requires -Version 7.4
. "$PSScriptRoot/TestHelpers.ps1"
Assert-True (Test-Path "$PSScriptRoot/../src/Installation.psm1") 'Installation module exists'
Import-Module "$PSScriptRoot/../src/Installation.psm1" -Force
Import-Module "$PSScriptRoot/../src/Storage.psm1" -Force
$temp = Join-Path ([IO.Path]::GetTempPath()) ('atn-install-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($temp) | Out-Null
try {
    $unsafePackage=Join-Path $temp 'package'
    [IO.Directory]::CreateDirectory((Join-Path $unsafePackage 'scripts')) | Out-Null
    [IO.File]::WriteAllText((Join-Path $unsafePackage 'scripts/agent-task-notify.ps1'),'# synthetic package')
    $unsafeData=Join-Path $unsafePackage 'data'
    Assert-Throws {Install-ATNIntegration claude-code (Join-Path $temp 'unsafe-host/config.json') $unsafeData $unsafePackage | Out-Null} 'Private backup/data must never live inside release package'
    Assert-True (-not [IO.Directory]::Exists($unsafeData)) 'Unsafe backup destination rejected before writing'
    foreach ($failureAgent in @('claude-code','opencode')) {
        $failureRoot=Join-Path $temp "failure-$failureAgent"
        $failureConfig=Join-Path $failureRoot 'config.json'
        $failureData=Join-Path $failureRoot 'data'
        Write-ATNJson $failureConfig @{keep='original';hooks=@{}}
        $before=[IO.File]::ReadAllText($failureConfig)
        $blockedReceipt=Join-Path $failureData "receipts/$failureAgent.json"
        [IO.Directory]::CreateDirectory($blockedReceipt) | Out-Null
        Assert-Throws {Install-ATNIntegration $failureAgent $failureConfig $failureData (Split-Path $PSScriptRoot) | Out-Null} 'Receipt failure surfaces'
        Assert-Equal ([IO.File]::ReadAllText($failureConfig)) $before 'Receipt failure must not change host JSON'
        Assert-True (-not (Test-Path (Join-Path $failureRoot 'plugins/agent-task-notify.js'))) 'Receipt failure must not activate shim'
        [IO.Directory]::Delete($blockedReceipt)
        Install-ATNIntegration $failureAgent $failureConfig $failureData (Split-Path $PSScriptRoot) | Out-Null
        $hostFile=if ($failureAgent -eq 'opencode') {Join-Path $failureRoot 'plugins/agent-task-notify.js'} else {$failureConfig}
        $installed=[IO.File]::ReadAllText($hostFile)
        $upgrade=Join-Path $failureRoot 'upgrade'
        [IO.Directory]::CreateDirectory((Join-Path $upgrade 'scripts')) | Out-Null
        Copy-Item "$PSScriptRoot/../scripts/agent-task-notify.ps1" (Join-Path $upgrade 'scripts/agent-task-notify.ps1')
        $lock=[IO.File]::Open($hostFile,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
        try {
            Assert-Throws {Install-ATNIntegration $failureAgent $failureConfig $failureData $upgrade | Out-Null} 'Host write failure surfaces'
            Assert-Equal ([IO.File]::ReadAllText($hostFile)) $installed 'Host failure preserves exact bytes'
            Assert-True (Test-Path $blockedReceipt -PathType Leaf) 'Recoverable ownership remains after host failure'
        } finally {$lock.Dispose()}
        Install-ATNIntegration $failureAgent $failureConfig $failureData $upgrade | Out-Null
        Assert-True ([IO.File]::ReadAllText($hostFile) -cne $installed) 'Host failure retry installs upgraded command'
        $removed=Uninstall-ATNIntegration $failureAgent $failureData
        Assert-Equal $removed.conflicts.Count 0 'Recovered upgrade can uninstall'
        Assert-Equal (Read-ATNJson $failureConfig).keep 'original' 'Recovery preserves unrelated config'
    }
    foreach ($failureAgent in @('claude-code','opencode')) {
        $failureRoot=Join-Path $temp "first-host-failure-$failureAgent"
        $failureConfig=Join-Path $failureRoot 'config.json';$failureData=Join-Path $failureRoot 'data'
        Write-ATNJson $failureConfig @{keep='untouched';hooks=@{}}
        $before=[IO.File]::ReadAllText($failureConfig)
        $blocker=Join-Path $failureRoot 'plugins'
        if ($failureAgent -eq 'opencode') {[IO.File]::WriteAllText($blocker,'blocked directory')}
        else {$lock=[IO.File]::Open($failureConfig,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)}
        try {
            Assert-Throws {Install-ATNIntegration $failureAgent $failureConfig $failureData (Split-Path $PSScriptRoot) | Out-Null} 'Initial host write failure surfaces'
            Assert-Equal ([IO.File]::ReadAllText($failureConfig)) $before 'Initial host failure preserves original JSON'
            Assert-True (Test-Path (Join-Path $failureData "receipts/$failureAgent.json") -PathType Leaf) 'Initial host failure retains recovery receipt'
        } finally {if ($failureAgent -eq 'opencode') {[IO.File]::Delete($blocker)} else {$lock.Dispose()}}
        $removed=Uninstall-ATNIntegration $failureAgent $failureData
        Assert-Equal $removed.conflicts.Count 0 'Uninstall clears uncommitted initial ownership'
        Assert-Equal ([IO.File]::ReadAllText($failureConfig)) $before 'Uninstall of failed install preserves exact original bytes'
        Install-ATNIntegration $failureAgent $failureConfig $failureData (Split-Path $PSScriptRoot) | Out-Null
        Assert-Equal (Uninstall-ATNIntegration $failureAgent $failureData).conflicts.Count 0 'Fresh install succeeds after recovery'
    }
    foreach ($agent in @('codex','claude-code','cursor','gemini-cli')) {
        $config = Join-Path $temp "$agent/config.json"
        $data = Join-Path $temp "$agent/data"
        Write-ATNJson $config @{hooks=@{unrelated=@(@{command='keep'})}; mcpServers=@{keep=@{command='other'}}; notify=@('existing')}
        Write-ATNJson (Join-Path $data 'settings.json') @{minSeconds=1900}
        Set-ATNCredential $data bark @{endpoint='https://example.invalid/synthetic'}
        $cipher = [IO.File]::ReadAllText((Join-Path $data 'credentials-bark.json'))
        $receipt = Install-ATNIntegration $agent $config $data (Split-Path $PSScriptRoot)
        Assert-True (Test-Path (Join-Path $data "receipts/$agent.json")) 'Per-source receipt saved'
        $first = [IO.File]::ReadAllText($config)
        Install-ATNIntegration $agent $config $data (Split-Path $PSScriptRoot) | Out-Null
        Assert-Equal ([IO.File]::ReadAllText($config)) $first 'Repeat does not duplicate'
        Assert-Equal ([IO.File]::ReadAllText((Join-Path $data 'credentials-bark.json'))) $cipher 'Upgrade preserves credentials'
        Assert-Equal (Read-ATNJson (Join-Path $data 'settings.json')).minSeconds 1900 'Upgrade preserves settings'
        $current=Read-ATNJson $config
        Assert-Equal $current.mcpServers.keep.command 'other' 'MCP survives'
        Assert-Equal $current.notify[0] 'existing' 'Notify survives'
        $current.later='keep'; Write-ATNJson $config $current
        Uninstall-ATNIntegration $agent $data | Out-Null
        $current=Read-ATNJson $config
        Assert-Equal $current.later 'keep' 'Later edit survives'
        Assert-Equal $current.hooks.unrelated[0].command 'keep' 'Other hook survives'
        foreach ($owned in $receipt.entries) { Assert-True (-not $current.hooks.ContainsKey($owned.event)) 'Owned hook removed' }
        Install-ATNIntegration $agent $config $data (Split-Path $PSScriptRoot) | Out-Null
        $current=Read-ATNJson $config
        $event=$receipt.entries[0].event
        $current.hooks[$event][0].custom='edited'; Write-ATNJson $config $current
        $result=Uninstall-ATNIntegration $agent $data
        Assert-True ($result.conflicts.Count -gt 0) 'Changed entry reported'
        Assert-Equal (Read-ATNJson $config).hooks[$event][0].custom 'edited' 'Changed owned entry preserved'
    }
    $legacy=Join-Path $temp 'legacy.json'; Write-ATNJson $legacy @{notify=@('pwsh','CodexLongTaskNotify.ps1')}
    Assert-Throws { Install-ATNIntegration codex $legacy (Join-Path $temp 'legacy-data') (Split-Path $PSScriptRoot) } 'Legacy coexistence refused'
    Assert-Throws { Install-ATNIntegration workbuddy $legacy $temp (Split-Path $PSScriptRoot) -AcceptExperimental } 'WorkBuddy automatic mutation refused'
    $legacyToml=Join-Path $temp 'codex-legacy/config.toml'
    [IO.Directory]::CreateDirectory((Split-Path $legacyToml)) | Out-Null
    [IO.File]::WriteAllText($legacyToml,'notify = ["pwsh", "CodexLongTaskNotify.ps1"]')
    Assert-Throws { Install-ATNIntegration codex (Join-Path (Split-Path $legacyToml) 'hooks.json') (Join-Path $temp 'legacy-data') (Split-Path $PSScriptRoot) } 'Legacy Codex TOML notifier refused'
    $data=Join-Path $temp 'configure'
    Save-ATNConfiguration -Provider bark -Credential @{endpoint='https://example.invalid/synthetic'} -DataDirectory $data -Settings @{minSeconds=1801}
    $bytes=[IO.File]::ReadAllText((Join-Path $data 'credentials-bark.json'))
    Assert-Equal (Get-ATNCredential $data bark).endpoint 'https://example.invalid/synthetic' 'DPAPI readback'
    Assert-Throws { Save-ATNConfiguration bark @{endpoint='http://remote.invalid/key'} $data @{} } 'Bad credential rejected'
    Assert-Throws { Save-ATNConfiguration bark @{endpoint='https://example.invalid/new'} $data @{minSeconds=-1} } 'Bad settings rejected'
    Assert-Equal ([IO.File]::ReadAllText((Join-Path $data 'credentials-bark.json'))) $bytes 'Failed configuration preserves cipher'
    $settingsFile=Join-Path $data 'settings.json'
    $settingsBefore=[IO.File]::ReadAllText($settingsFile)
    $settingsLock=[IO.File]::Open($settingsFile,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
    try {
        Assert-Throws { Save-ATNConfiguration bark @{endpoint='https://example.invalid/replacement'} $data @{minSeconds=1802} } 'Real second-file replacement failure surfaces'
        Assert-Equal ([IO.File]::ReadAllText((Join-Path $data 'credentials-bark.json'))) $bytes 'Second-write failure restores original credential bytes'
        Assert-Equal ([IO.File]::ReadAllText($settingsFile)) $settingsBefore 'Second-write failure preserves original settings'
    } finally {$settingsLock.Dispose()}
    # Fault at the real second-write boundary after an unrelated process edits
    # credentials. The storage writes stay real; only the failed operation is injected.
    & (Get-Module Installation) {
        param($directory)
        $script:failureDirectory=$directory
        function script:Write-ATNJson {
            param($Path,$Value)
            if ($Path -eq (Join-Path $script:failureDirectory 'settings.json')) {
                Storage\Write-ATNJson (Join-Path $script:failureDirectory 'credentials-bark.json') @{protected='foreign-edit'}
                throw 'synthetic second-write failure'
            }
            Storage\Write-ATNJson $Path $Value
        }
    } $data
    try {
        Assert-Throws { Save-ATNConfiguration bark @{endpoint='https://example.invalid/replacement'} $data @{} } 'Concurrent second-write failure surfaces'
        Assert-Equal (Read-ATNJson (Join-Path $data 'credentials-bark.json')).protected 'foreign-edit' 'Rollback never overwrites concurrent foreign credential edit'
        $recoveries=@(Get-ChildItem $data -Filter 'configuration-recovery-*.dpapi')
        Assert-Equal $recoveries.Count 1 'Original cipher retained for private manual recovery after conflict'
        Assert-Equal ([IO.File]::ReadAllText($recoveries[0].FullName)) $bytes 'Recovery retains original valid credential bytes'
        Assert-Equal ([IO.File]::ReadAllText($settingsFile)) $settingsBefore 'Concurrent failure preserves settings'
    } finally {Import-Module "$PSScriptRoot/../src/Installation.psm1" -Force}
    foreach ($backupAgent in @('claude-code','opencode')) {
        $backupRoot=Join-Path $temp "backup-$backupAgent"
        $backupConfig=Join-Path $backupRoot 'config.json'; $backupData=Join-Path $backupRoot 'data'
        [IO.Directory]::CreateDirectory($backupRoot) | Out-Null
        $raw=[Text.UTF8Encoding]::new($true).GetPreamble()+[Text.Encoding]::UTF8.GetBytes("{ `r`n `"secret`": `"synthetic-private`" }`r`n")
        [IO.File]::WriteAllBytes($backupConfig,$raw)
        $backupBlock=Join-Path $backupData 'backups'; [IO.Directory]::CreateDirectory($backupData) | Out-Null
        [IO.File]::WriteAllText($backupBlock,'block')
        Assert-Throws { Install-ATNIntegration $backupAgent $backupConfig $backupData (Split-Path $PSScriptRoot) | Out-Null } 'Backup failure prevents host activation'
        Assert-Equal ([Convert]::ToBase64String([IO.File]::ReadAllBytes($backupConfig))) ([Convert]::ToBase64String($raw)) 'Backup failure preserves exact host bytes'
        Assert-True (-not [IO.File]::Exists((Join-Path $backupRoot 'plugins/agent-task-notify.js'))) 'Backup failure never activates shim'
        [IO.File]::Delete($backupBlock)
        $saved=Install-ATNIntegration $backupAgent $backupConfig $backupData (Split-Path $PSScriptRoot)
        $protected=[IO.File]::ReadAllBytes($saved.backup.path)
        Assert-True (-not [Text.Encoding]::UTF8.GetString($protected).Contains('synthetic-private')) 'Backup protects host secrets'
        $restored=[Security.Cryptography.ProtectedData]::Unprotect($protected,$null,[Security.Cryptography.DataProtectionScope]::CurrentUser)
        Assert-Equal ([Convert]::ToBase64String($restored)) ([Convert]::ToBase64String($raw)) 'Private backup preserves original bytes including BOM/newlines'
        $edited=Read-ATNJson $backupConfig; $edited.later='keep'; Write-ATNJson $backupConfig $edited
        Uninstall-ATNIntegration $backupAgent $backupData | Out-Null
        Assert-Equal (Read-ATNJson $backupConfig).later 'keep' 'Uninstall never restores stale whole backup'
    }
    $config=Join-Path $temp 'opencode/opencode.json'
    $data=Join-Path $temp 'opencode/data'
    Install-ATNIntegration opencode $config $data (Split-Path $PSScriptRoot) | Out-Null
    $shim=Join-Path (Split-Path $config) 'plugins/agent-task-notify.js'
    Assert-True (Test-Path $shim) 'Discoverable JS shim exists'
    Assert-Equal @(Get-ChildItem (Split-Path $shim) -File).Count 1 'Only one scanned plugin'
    Install-ATNIntegration opencode $config $data (Split-Path $PSScriptRoot) | Out-Null
    Uninstall-ATNIntegration opencode $data | Out-Null
    Assert-True (-not (Test-Path $shim)) 'Owned shim removed'
    $output=Join-Path $temp '插件 package'
    Assert-True (Test-Path "$PSScriptRoot/../integrations/workbuddy/Build-Plugin.ps1") 'WorkBuddy builder exists'
    & "$PSScriptRoot/../integrations/workbuddy/Build-Plugin.ps1" -OutputDirectory $output
    Assert-True (Test-Path (Join-Path $output '.workbuddy-plugin/plugin.json')) 'Native manifest packaged'
    Assert-True (Test-Path (Join-Path $output 'runtime/assets/agent-icons.json')) 'Artwork catalog packaged'
    foreach ($notice in @('LICENSE','THIRD_PARTY_NOTICES.md')) {
        Assert-True (Test-Path (Join-Path $output $notice)) 'Standalone WorkBuddy package includes public license/brand notice'
        Assert-Equal ([IO.File]::ReadAllText((Join-Path $output $notice))) ([IO.File]::ReadAllText((Join-Path (Split-Path $PSScriptRoot) $notice))) 'Packaged notice keeps complete reviewed text'
    }
    Assert-Throws { & "$PSScriptRoot/../integrations/workbuddy/Build-Plugin.ps1" -OutputDirectory $output } 'Build refuses overwrite'
    $isolated=Join-Path $temp 'encoded data'
    $config=Join-Path $temp 'encoded/config.json'
    $receipt=Install-ATNIntegration claude-code $config $isolated (Join-Path $output 'runtime')
    $command=$receipt.entries[0].value.hooks[0].command
    $encoded=$command.Split(' ')[-1]
    $psi=[Diagnostics.ProcessStartInfo]::new('pwsh'); $psi.UseShellExecute=$false; $psi.RedirectStandardInput=$true; $psi.RedirectStandardOutput=$true
    foreach ($arg in @('-NoProfile','-NonInteractive','-EncodedCommand',$encoded)) {$psi.ArgumentList.Add($arg)}
    $process=[Diagnostics.Process]::Start($psi)
    $process.StandardInput.WriteLine('{"session_id":"synthetic","hook_event_name":"UserPromptSubmit"}');$process.StandardInput.Close()
    $stdout=$process.StandardOutput.ReadToEnd();$process.WaitForExit()
    Assert-Equal $process.ExitCode 0 'Encoded command executes through spaces/Chinese'
    Assert-Equal $stdout.Trim() '{}' 'Claude hook output remains neutral'
    Assert-Equal @(Get-ChildItem (Join-Path $isolated 'runs') -Filter '*.json').Count 1 'Copied runtime starts isolated timer'
    $process.Dispose()
    if ($env:ATN_TEST_BASH) {
        $psi=[Diagnostics.ProcessStartInfo]::new($env:ATN_TEST_BASH)
        $psi.UseShellExecute=$false;$psi.RedirectStandardInput=$true;$psi.RedirectStandardOutput=$true;$psi.RedirectStandardError=$true
        $psi.ArgumentList.Add((Join-Path $output 'hooks/launch.sh').Replace('\','/'))
        $psi.Environment['CODEBUDDY_PLUGIN_ROOT']=$output.Replace('\','/')
        $psi.Environment['ATN_DATA_DIRECTORY']=Join-Path $temp 'workbuddy-data'
        $process=[Diagnostics.Process]::Start($psi)
        # Invalid/no-op event exercises launcher and copied runtime without reading real settings or starting any run.
        $process.StandardInput.WriteLine('{}');$process.StandardInput.Close()
        $stdout=$process.StandardOutput.ReadToEnd();$stderr=$process.StandardError.ReadToEnd();$process.WaitForExit()
        Assert-Equal $process.ExitCode 0 'Git Bash launcher runs copied package'
        Assert-Equal $stderr '' 'Launcher has no stderr'
        Assert-Equal $stdout.Trim() '{"continue":true}' 'WorkBuddy no-op output'
        $process.Dispose()
    }
    'PASS installation/configuration'
} finally { if (([IO.Path]::GetFullPath($temp)).StartsWith([IO.Path]::GetTempPath())) { Remove-Item -LiteralPath $temp -Recurse -Force } }
