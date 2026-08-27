. "$PSScriptRoot/TestHelpers.ps1"
Import-Module "$PSScriptRoot/../src/Runtime.psm1" -Force
Import-Module "$PSScriptRoot/../src/Storage.psm1" -Force
$temp=Join-Path ([IO.Path]::GetTempPath()) ('atn-diagnostics-' + [guid]::NewGuid().ToString('N'))
try {
    foreach ($pair in @(@('failed','transport'),@('ambiguous','ambiguous-extension'))) {
        $job=New-ATNPreview codex $temp; $job.status='sent'; $job.attempts=3; $job.extensionStatus=$pair[0]; $job.extensionDiagnostic=$pair[1]
        Write-ATNJson (Join-Path $temp ('jobs/' + (Get-ATNKey @($pair[0])) + '.json')) $job
    }
    $job=New-ATNPreview codex $temp; $job.diagnostic='spawn-failed'
    Write-ATNJson (Join-Path $temp 'jobs/spawn.json') $job
    $job.diagnostic='ambiguous-send';$job.attempts=2
    Write-ATNJson (Join-Path $temp 'jobs/ambiguous.json') $job
    $job.diagnostic='PRIVATE https://secret.invalid/path';$job.extensionDiagnostic='PRIVATE';$job.attempts=999999
    Write-ATNJson (Join-Path $temp 'jobs/hostile.json') $job
    $result=Get-ATNDiagnostics $temp
    Assert-Equal $result.extensions.failed 1 'Doctor exposes extension failures'
    Assert-Equal $result.extensions.ambiguous 1 'Doctor exposes extension uncertainty'
    Assert-Equal $result.failures.'spawn-failed' 1 'Doctor exposes spawn failure'
    Assert-Equal $result.failures.'ambiguous-send' 1 'Doctor exposes ambiguous send'
    Assert-Equal $result.attempts 13 'Doctor sums bounded per-job attempts'
    Assert-True (($result | ConvertTo-Json -Depth 16) -notmatch 'PRIVATE|https|secret') 'Doctor never echoes arbitrary diagnostics'
    [IO.File]::WriteAllText((Join-Path $temp 'configuration-recovery-synthetic.dpapi'),'PRIVATE')
    Assert-Equal (Get-ATNDiagnostics $temp).configurationRecoveryCount 1 'Doctor flags retained configuration recovery without reading its contents or exposing a path'
    Write-ATNJson (Join-Path $temp 'input-diagnostics.json') @{'invalid-json'=999999;'invalid-utf8'=0;'invalid-shape'=0;private='PRIVATE'}
    Add-ATNInputDiagnostic $temp 'invalid-json'
    Assert-Equal (Get-ATNDiagnostics $temp).input.'invalid-json' 1000 'Input counters saturate safely'
    Assert-Equal (Read-ATNJson (Join-Path $temp 'input-diagnostics.json')).Count 3 'Persisted input counters retain fixed schema only'
    for ($i=0;$i -lt 1001;$i++) {Write-ATNJson (Join-Path $temp "jobs/bounded-$i.json") @{status='pending';attempts=99999;extensionStatus='none'}}
    $bounded=Get-ATNDiagnostics $temp
    Assert-True $bounded.truncated 'Doctor marks bounded scan limit'
    Assert-Equal (($bounded.jobs.Values | Measure-Object -Sum).Sum) 1000 'Doctor scans at most 1000 jobs'
    Assert-True ($bounded.attempts -le 5000) 'Attempt total is bounded'
    'PASS safe diagnostics'
} finally {if ([IO.Directory]::Exists($temp)) {Remove-Item -LiteralPath $temp -Recurse -Force}}
