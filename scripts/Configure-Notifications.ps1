#requires -Version 7.4
param([Parameter(Mandatory)][ValidateSet('bark','ntfy')][string]$Provider,[string]$SettingsPath,[string]$DataDirectory=$(if ($env:ATN_DATA_DIRECTORY) { $env:ATN_DATA_DIRECTORY } else { Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentTaskNotify' }))
$ErrorActionPreference='Stop'
Import-Module "$PSScriptRoot/../src/Installation.psm1"
Import-Module "$PSScriptRoot/../src/Storage.psm1"
$settings=if ($SettingsPath) {Read-ATNJson $SettingsPath} else {@{}}
if ($null -eq $settings) {throw 'Settings file not found.'}
Write-Host 'Privacy: the selected push service receives its credential and generic notification content when a push is sent. DPAPI protects local storage; it is not end-to-end push encryption.'
if ($Provider -eq 'ntfy') {Write-Host 'ntfy: unrestricted topics may be read or published by others. Random topic names are not access control, and a bearer token alone does not prove topic ACLs. Configure and verify server topic ACLs before opting in; unauthenticated use accepts this risk.'}
$endpoint=Read-Host 'HTTPS provider endpoint (includes Bark key or ntfy topic; hidden)' -AsSecureString
$token=$null
try {
    $credential=@{endpoint=[Net.NetworkCredential]::new('',$endpoint).Password}
    if ($Provider -eq 'ntfy') {
        $token=Read-Host 'ntfy bearer token (hidden; empty only with explicit unauthenticated choice)' -AsSecureString
        $credential.token=[Net.NetworkCredential]::new('',$token).Password
        $credential.allowUnauthenticated=$false
        if (-not $credential.token) {$credential.allowUnauthenticated=((Read-Host 'Allow unauthenticated ntfy? Type YES') -ceq 'YES')}
    }
    Save-ATNConfiguration -Provider $Provider -Credential $credential -DataDirectory $DataDirectory -Settings $settings
    Write-Output 'Configuration saved. No notification sent.'
} finally {$endpoint.Dispose(); if ($token) {$token.Dispose()}; if ($credential) {$credential.Clear()}}
