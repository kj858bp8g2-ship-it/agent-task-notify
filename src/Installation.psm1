Set-StrictMode -Version Latest
Import-Module "$PSScriptRoot/Storage.psm1"
Import-Module "$PSScriptRoot/Settings.psm1"

function ConvertTo-ATNCanonical($Value) {
    if ($Value -is [System.Collections.IDictionary]) {
        $ordered=[ordered]@{}; foreach ($key in @($Value.Keys | Sort-Object)) { $ordered[$key]=ConvertTo-ATNCanonical $Value[$key] }; return $ordered
    }
    if ($Value -is [array]) { return ,@($Value | ForEach-Object { ConvertTo-ATNCanonical $_ }) }
    return $Value
}
function Test-ATNEntryEqual($Left,$Right) {
    return (ConvertTo-Json -InputObject (ConvertTo-ATNCanonical $Left) -Depth 64 -Compress) -ceq (ConvertTo-Json -InputObject (ConvertTo-ATNCanonical $Right) -Depth 64 -Compress)
}
function Get-ATNInstallationFingerprint([string]$Path) {
    if (-not [IO.File]::Exists($Path)) {return 'absent'}
    return Get-ATNKey @([IO.File]::ReadAllText($Path))
}
function Resolve-ATNInstallationReceipt($Receipt) {
    if (-not $Receipt) {return $null}
    # A write-ahead receipt owns the intended exact entries even if the host write
    # succeeds but the process exits immediately afterwards. If no host change
    # happened, use the previous ownership; never restore a stale host snapshot.
    if ($Receipt.ContainsKey('recovery')) {
        if ((Get-ATNInstallationFingerprint $Receipt.recovery.targetPath) -ceq $Receipt.recovery.beforeFingerprint) {
            return $Receipt.recovery.previous
        }
        $Receipt.Remove('recovery')
    }
    return $Receipt
}
function Get-ATNConfigPath([string]$Agent) {
    $profilePath=[Environment]::GetFolderPath('UserProfile')
    switch ($Agent) {
        codex { return Join-Path $profilePath '.codex/hooks.json' }
        claude-code { return Join-Path $profilePath '.claude/settings.json' }
        cursor { return Join-Path $profilePath '.cursor/hooks.json' }
        gemini-cli { return Join-Path $profilePath '.gemini/settings.json' }
        opencode { $configRoot=if ($env:XDG_CONFIG_HOME) {$env:XDG_CONFIG_HOME} else {Join-Path $profilePath '.config'}; return Join-Path $configRoot 'opencode/opencode.json' }
    }
}
function Install-ATNIntegration {
    param([Parameter(Mandatory)][string]$Agent,[string]$ConfigPath,[Parameter(Mandatory)][string]$DataDirectory,[Parameter(Mandatory)][string]$PackageRoot,[switch]$AcceptExperimental)
    Get-ATNAgent $Agent | Out-Null
    if ($Agent -eq 'workbuddy') { throw 'WorkBuddy automatic settings mutation is unsupported. Build the experimental integrations/workbuddy package and manually review/install it; consumer desktop loading and cancel events are unverified.' }
    if (-not $ConfigPath) { $ConfigPath=Get-ATNConfigPath $Agent }
    $ConfigPath=[IO.Path]::GetFullPath($ConfigPath); $PackageRoot=[IO.Path]::GetFullPath($PackageRoot); $DataDirectory=[IO.Path]::GetFullPath($DataDirectory)
    $packagePrefix=$PackageRoot.TrimEnd([IO.Path]::DirectorySeparatorChar,[IO.Path]::AltDirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($DataDirectory.Equals($PackageRoot,[StringComparison]::OrdinalIgnoreCase) -or $DataDirectory.StartsWith($packagePrefix,[StringComparison]::OrdinalIgnoreCase)) {throw 'Runtime data and private backups must be outside the package.'}
    $hook=Join-Path $PackageRoot 'scripts/agent-task-notify.ps1'
    if (-not [IO.File]::Exists($hook)) { throw 'Package runtime is missing.' }
    if ($Agent -eq 'codex') {
        $toml=Join-Path (Split-Path $ConfigPath) 'config.toml'
        if ([IO.File]::Exists($toml) -and [IO.File]::ReadAllText($toml) -match 'CodexLongTaskNotify') {throw 'Legacy CodexLongTaskNotify detected in Codex config.toml; coexistence refused. Remove it manually first.'}
    }
    $receiptPath=Join-Path $DataDirectory "receipts/$Agent.json"
    $previous=Read-ATNJson $receiptPath
    if ($previous -and $previous.configPath -cne $ConfigPath) { throw 'Existing receipt uses another config path; uninstall it first.' }
    $previous=Resolve-ATNInstallationReceipt $previous
    $config=Read-ATNJson $ConfigPath; if ($null -eq $config) {$config=@{}}
    $original=Read-ATNJson $ConfigPath
    if (($config | ConvertTo-Json -Depth 64 -Compress) -match 'CodexLongTaskNotify') { throw 'Legacy CodexLongTaskNotify detected; coexistence refused. Remove it manually before installing.' }
    $receipt=@{schemaVersion=1;agentId=$Agent;configPath=$ConfigPath;entries=@();files=@()}
    if ($Agent -eq 'opencode') {
        $path=Join-Path (Split-Path $ConfigPath) 'plugins/agent-task-notify.js'
        $entry=([uri](Join-Path $PackageRoot 'integrations/opencode/agent-task-notify.mjs')).AbsoluteUri
        $content="import plugin from " + (ConvertTo-Json $entry -Compress) + ";`nexport default (context) => plugin({ ...context, agentTaskNotifyDataDirectory: " + (ConvertTo-Json $DataDirectory -Compress) + " });`n"
        if ([IO.File]::Exists($path)) {
            if (-not $previous -or $previous.files.Count -ne 1 -or $previous.files[0].path -cne $path -or [IO.File]::ReadAllText($path) -cne $previous.files[0].content) { throw 'OpenCode shim exists or was edited; refusing overwrite.' }
        }
        $receipt.files=@(@{path=$path;content=$content})
    } else {
        if (-not $config.ContainsKey('hooks')) {$config.hooks=@{}}
        if ($config.hooks -isnot [hashtable]) {throw 'Existing hooks must be an object.'}
        if ($Agent -eq 'cursor') {if ($config.ContainsKey('version') -and $config.version -ne 1) {throw 'Unsupported Cursor hook version.'}; $config.version=1}
        if ($previous) {
            foreach ($owned in $previous.entries) {
                if (-not $config.hooks.ContainsKey($owned.event) -or @($config.hooks[$owned.event] | Where-Object {Test-ATNEntryEqual $_ $owned.value}).Count -ne 1) {throw 'Owned hook was changed; review/uninstall before reinstalling.'}
                $config.hooks[$owned.event]=@($config.hooks[$owned.event] | Where-Object {-not (Test-ATNEntryEqual $_ $owned.value)})
            }
        }
        $literalHook=$hook.Replace("'","''"); $literalData=$DataDirectory.Replace("'","''")
        $script="& '$literalHook' -Mode Hook -Agent '$Agent' -DataDirectory '$literalData'"
        $command='pwsh -NoLogo -NoProfile -NonInteractive -EncodedCommand ' + [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($script))
        $events=switch ($Agent) {codex {@('UserPromptSubmit','Stop')}; claude-code {@('UserPromptSubmit','Stop','StopFailure')}; cursor {@('beforeSubmitPrompt','stop')}; gemini-cli {@('BeforeAgent','AfterAgent')}}
        foreach ($event in $events) {
            $value=if ($Agent -eq 'cursor') {@{command=$command}} else {@{hooks=@(@{type='command';command=$command})}}
            if ($config.hooks.ContainsKey($event) -and $config.hooks[$event] -isnot [array]) {throw 'Existing event hooks must be an array.'}
            $existing=if ($config.hooks.ContainsKey($event)) {@($config.hooks[$event])} else {@()}
            if (@($existing | Where-Object {Test-ATNEntryEqual $_ $value}).Count) {throw 'Unreceipted identical hook exists; refusing to claim ownership.'}
            $config.hooks[$event]=@($existing)+@($value)
            $receipt.entries+=@{event=$event;value=$value}
        }
    }
    $targetPath=if ($Agent -eq 'opencode') {$path} else {$ConfigPath}
    if ($previous -and $previous.ContainsKey('backup')) { $receipt.backup=$previous.backup }
    else {
        # Preserve exact bytes privately BEFORE activating any hook/shim. The
        # original host configuration may contain secrets: DPAPI, not plaintext.
        $backupDirectory=Join-Path $DataDirectory 'backups'
        [IO.Directory]::CreateDirectory($backupDirectory) | Out-Null
        $backupPath=Join-Path $backupDirectory ([guid]::NewGuid().ToString('N') + '.dpapi')
        $existed=[IO.File]::Exists($ConfigPath)
        $originalBytes=if ($existed) {,[IO.File]::ReadAllBytes($ConfigPath)} else {,[byte[]]::new(0)}
        try {
            $protected=[Security.Cryptography.ProtectedData]::Protect($originalBytes,$null,[Security.Cryptography.DataProtectionScope]::CurrentUser)
            [IO.File]::WriteAllBytes($backupPath,$protected)
        } finally {[Array]::Clear($originalBytes)}
        $receipt.backup=@{path=$backupPath;protection='DPAPI-CurrentUser';existed=$existed;purpose='Exact original host config for manual recovery only; never restored by uninstall.'}
    }
    $receipt.recovery=@{targetPath=$targetPath;beforeFingerprint=(Get-ATNInstallationFingerprint $targetPath);previous=$previous}
    # Failure here must not enable a hook without a recoverable ownership record.
    Write-ATNJson $receiptPath $receipt
    if ($Agent -eq 'opencode') {
        [IO.Directory]::CreateDirectory((Split-Path $path)) | Out-Null
        $temporary=Join-Path (Split-Path $path) ([guid]::NewGuid().ToString('N') + '.tmp')
        try {
            [IO.File]::WriteAllText($temporary,$content,[Text.UTF8Encoding]::new($false))
            [IO.File]::Move($temporary,$path,$true)
        } finally {if ([IO.File]::Exists($temporary)) {[IO.File]::Delete($temporary)}}
    } elseif (-not (Test-ATNEntryEqual $original $config)) {Write-ATNJson $ConfigPath $config}
    return $receipt
}
function Uninstall-ATNIntegration {
    param([Parameter(Mandatory)][string]$Agent,[Parameter(Mandatory)][string]$DataDirectory)
    Get-ATNAgent $Agent | Out-Null
    $path=Join-Path $DataDirectory "receipts/$Agent.json"; $receipt=Read-ATNJson $path
    $conflicts=@(); if (-not $receipt) {return @{conflicts=@();removed=$false}}
    $receipt=Resolve-ATNInstallationReceipt $receipt
    if (-not $receipt) {[IO.File]::Delete([IO.Path]::GetFullPath($path)); return @{conflicts=@();removed=$true}}
    $remaining=@(); $config=Read-ATNJson $receipt.configPath
    foreach ($entry in $receipt.entries) {
        if ($config -and $config.ContainsKey('hooks') -and $config.hooks.ContainsKey($entry.event)) {
            $matches=@($config.hooks[$entry.event] | Where-Object {Test-ATNEntryEqual $_ $entry.value})
            if ($matches.Count -eq 1) {
                $config.hooks[$entry.event]=@($config.hooks[$entry.event] | Where-Object {-not (Test-ATNEntryEqual $_ $entry.value)})
                if ($config.hooks[$entry.event].Count -eq 0) {$config.hooks.Remove($entry.event)}
                continue
            }
        }
        $conflicts+=$entry.event; $remaining+=$entry
    }
    if ($config -and $receipt.entries.Count) {Write-ATNJson $receipt.configPath $config}
    $files=@()
    foreach ($file in $receipt.files) {
        if (-not [IO.File]::Exists($file.path)) {continue}
        if ([IO.File]::ReadAllText($file.path) -ceq $file.content) {[IO.File]::Delete($file.path)} else {$files+=$file; $conflicts+='edited-owned-file'}
    }
    if ($conflicts.Count) {$receipt.entries=$remaining;$receipt.files=$files;Write-ATNJson $path $receipt} else {[IO.File]::Delete([IO.Path]::GetFullPath($path))}
    return @{conflicts=$conflicts;removed=($conflicts.Count -eq 0)}
}
function Save-ATNConfiguration {
    param([Parameter(Mandatory)][string]$Provider,[Parameter(Mandatory)][hashtable]$Credential,[Parameter(Mandatory)][string]$DataDirectory,[hashtable]$Settings=@{})
    # Validate settings in a private temporary staging directory before replacing either live file.
    $stage=Join-Path ([IO.Path]::GetTempPath()) ('atn-config-' + [guid]::NewGuid().ToString('N'))
    [IO.Directory]::CreateDirectory($stage) | Out-Null
    $commitLock=Enter-ATNLock (Join-Path $DataDirectory 'configuration.lock') -WaitMilliseconds 2000
    if ($null -eq $commitLock) {[IO.Directory]::Delete($stage); throw 'Configuration is busy.'}
    $recovery=$null; $retainRecovery=$false
    try {
        $merged=Get-ATNSettings $DataDirectory
        foreach ($key in $Settings.Keys) {$merged[$key]=$Settings[$key]}; $merged.provider=$Provider
        Write-ATNJson (Join-Path $stage 'settings.json') $merged
        Get-ATNSettings $stage | Out-Null
        Set-ATNCredential $stage $Provider $Credential
        Get-ATNCredential $stage $Provider | Out-Null
        $credentialPath=Join-Path $DataDirectory "credentials-$Provider.json"
        $before=if ([IO.File]::Exists($credentialPath)) {[IO.File]::ReadAllBytes($credentialPath)} else {$null}
        $replacement=[IO.File]::ReadAllBytes((Join-Path $stage "credentials-$Provider.json"))
        if ($null -ne $before) {
            # Save the old DPAPI envelope BEFORE replacing it; recovery must not
            # depend on a second disk write succeeding after the commit fails.
            $recovery=Join-Path $DataDirectory ('configuration-recovery-' + [guid]::NewGuid().ToString('N') + '.dpapi')
            [IO.File]::WriteAllBytes($recovery,$before)
        }
        Write-ATNJson $credentialPath (Read-ATNJson (Join-Path $stage "credentials-$Provider.json"))
        try { Write-ATNJson (Join-Path $DataDirectory 'settings.json') $merged }
        catch {
            # Compare and restore under the SAME exclusive handle. Never replace a
            # credential edited by another process between commit and recovery.
            $recovered=$false
            try {
                $handle=[IO.File]::Open($credentialPath,[IO.FileMode]::Open,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None)
                try {
                    $current=[byte[]]::new($handle.Length); $handle.ReadExactly($current)
                    if ([Convert]::ToBase64String($current) -ceq [Convert]::ToBase64String($replacement)) {
                        $handle.Position=0
                        if ($null -ne $before) {$handle.Write($before,0,$before.Length);$handle.SetLength($before.Length)} else {$handle.SetLength(0)}
                        $handle.Flush($true);$recovered=$true
                    }
                } finally {$handle.Dispose()}
            } catch { }
            if (-not $recovered -and $null -ne $before) {
                # Ciphertext only; retained for manual recovery without overwriting
                # an external edit. No paths or secret values in the error.
                $retainRecovery=$true
            }
            throw [InvalidOperationException]::new('Configuration commit failed; original credential restored or retained for private recovery.')
        }
    } finally {
        $commitLock.Dispose()
        if ($recovery -and -not $retainRecovery -and [IO.File]::Exists($recovery)) {[IO.File]::Delete($recovery)}
        foreach ($name in @('settings.json','credentials-bark.json','credentials-ntfy.json')) {$file=Join-Path $stage $name; if ([IO.File]::Exists($file)) {[IO.File]::Delete($file)}}
        [IO.Directory]::Delete($stage)
    }
}
Export-ModuleMember -Function Install-ATNIntegration,Uninstall-ATNIntegration,Save-ATNConfiguration
