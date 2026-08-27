Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-Equal {
    param($Actual, $Expected, [string]$Description)
    if ($Actual -ne $Expected) {
        throw "$Description. Expected '$Expected', got '$Actual'."
    }
}

function Assert-True {
    param([bool]$Condition, [string]$Description)
    if (-not $Condition) { throw $Description }
}

function Assert-Null {
    param($Actual, [string]$Description)
    if ($null -ne $Actual) { throw "$Description. Expected null." }
}

function Assert-Throws {
    param([scriptblock]$Action, [string]$Description)
    try { & $Action } catch { return }
    throw "$Description. Expected an exception."
}
