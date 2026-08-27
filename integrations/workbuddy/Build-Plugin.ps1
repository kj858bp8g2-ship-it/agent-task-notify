#requires -Version 7.4
param([Parameter(Mandatory)][string]$OutputDirectory)
$ErrorActionPreference='Stop'
$destination=[IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $destination) {throw 'Output directory must not exist; choose a new explicit directory.'}
$package=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$runtimeFiles=@('src/Adapters.psm1','src/Providers.psm1','src/Runtime.psm1','src/Settings.psm1','src/Storage.psm1','scripts/agent-task-notify.ps1','config/defaults.json','assets/agent-icons.json')
$wrapperFiles=@('.workbuddy-plugin/plugin.json','hooks/hooks.json','hooks/launch.sh','README.md')
$noticeFiles=@('LICENSE','THIRD_PARTY_NOTICES.md')
foreach ($file in $runtimeFiles) {if (-not [IO.File]::Exists((Join-Path $package $file))) {throw 'Required runtime file missing.'}}
foreach ($file in $wrapperFiles) {if (-not [IO.File]::Exists((Join-Path $PSScriptRoot $file))) {throw 'Required wrapper file missing.'}}
foreach ($file in $noticeFiles) {if (-not [IO.File]::Exists((Join-Path $package $file))) {throw 'Required public notice missing.'}}
foreach ($file in $runtimeFiles) {
    $target=Join-Path $destination "runtime/$file"
    [IO.Directory]::CreateDirectory((Split-Path $target)) | Out-Null
    [IO.File]::Copy((Join-Path $package $file),$target,$false)
}
foreach ($file in $wrapperFiles) {
    $target=Join-Path $destination $file
    [IO.Directory]::CreateDirectory((Split-Path $target)) | Out-Null
    [IO.File]::Copy((Join-Path $PSScriptRoot $file),$target,$false)
}
foreach ($file in $noticeFiles) {[IO.File]::Copy((Join-Path $package $file),(Join-Path $destination $file),$false)}
Write-Output 'Experimental package built; no host configuration changed.'
