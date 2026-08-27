#requires -Version 7.4
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$previousData=$env:ATN_DATA_DIRECTORY
$testData=Join-Path ([IO.Path]::GetTempPath()) ('atn-suite-' + [guid]::NewGuid().ToString('N'))
try {
    $env:ATN_DATA_DIRECTORY=$testData
    & (Join-Path $PSScriptRoot 'Test-Isolation.ps1')
    & (Join-Path $PSScriptRoot 'Test-Diagnostics.ps1')
    foreach ($test in @('Test-ConfigAndEvents.ps1','Test-StorageAndProviders.ps1','Test-Runtime.ps1','Test-Installation.ps1','Test-Distribution.ps1')) { & (Join-Path $PSScriptRoot $test) }
    foreach ($test in @('provider-http.test.cjs','runtime-process.test.cjs','opencode-bridge.test.mjs')) { & node (Join-Path $PSScriptRoot $test); if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }
    # All behavior tests supply their own directory; the suite fallback must stay unused.
    if ([IO.Directory]::Exists($testData)) {throw 'A test used the suite fallback instead of explicit isolated data.'}
} finally {
    $env:ATN_DATA_DIRECTORY=$previousData
    if ([IO.Directory]::Exists($testData)) {Remove-Item -LiteralPath $testData -Recurse -Force}
}
