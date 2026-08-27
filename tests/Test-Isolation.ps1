# Safety gate: evaluate parameter defaults without executing any entry point.
. "$PSScriptRoot/TestHelpers.ps1"
$temp = Join-Path ([IO.Path]::GetTempPath()) ('atn-isolation-' + [guid]::NewGuid().ToString('N'))
$previous = $env:ATN_DATA_DIRECTORY
try {
    $env:ATN_DATA_DIRECTORY = $temp
    foreach ($name in @('agent-task-notify','Configure-Notifications','Install-Notifications','Uninstall-Notifications')) {
        $ast = [Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot/../scripts/$name.ps1", [ref]$null, [ref]$null)
        $parameter = $ast.ParamBlock.Parameters | Where-Object { $_.Name.VariablePath.UserPath -eq 'DataDirectory' }
        $value = & ([scriptblock]::Create($parameter.DefaultValue.Extent.Text))
        Assert-Equal $value $temp "$name honors explicit environment override before any data access"
    }
    $config=Join-Path $temp 'host/config.json'
    & "$PSScriptRoot/../scripts/Install-Notifications.ps1" -Agent claude-code -ConfigPath $config | Out-Null
    Assert-True ([IO.File]::Exists((Join-Path $temp 'receipts/claude-code.json'))) 'Install shares environment data'
    & "$PSScriptRoot/../scripts/Uninstall-Notifications.ps1" -Agent claude-code | Out-Null
    Assert-True (-not [IO.File]::Exists((Join-Path $temp 'receipts/claude-code.json'))) 'Uninstall shares environment data'
    function Read-Host { param($Prompt,[switch]$AsSecureString); return ConvertTo-SecureString 'https://example.invalid/synthetic' -AsPlainText -Force }
    & "$PSScriptRoot/../scripts/Configure-Notifications.ps1" -Provider bark | Out-Null
    Assert-True ([IO.File]::Exists((Join-Path $temp 'credentials-bark.json'))) 'Configure shares environment data using synthetic credential'
    $explicit=Join-Path $temp 'explicit'
    $result='{"hook_event_name":"UserPromptSubmit","session_id":"synthetic","turn_id":"synthetic"}' | & pwsh -NoProfile -File "$PSScriptRoot/../scripts/agent-task-notify.ps1" -DataDirectory $explicit
    Assert-Equal $LASTEXITCODE 0 'Explicit data hook exits successfully'
    Assert-Equal @(Get-ChildItem (Join-Path $explicit 'runs') -Filter '*.json').Count 1 'Explicit parameter overrides environment'
    Assert-True (-not [IO.Directory]::Exists((Join-Path $temp 'runs'))) 'Environment data was not used when explicit parameter supplied'
    'PASS isolation safety gate'
} finally { $env:ATN_DATA_DIRECTORY = $previous; if ([IO.Directory]::Exists($temp)) {Remove-Item -LiteralPath $temp -Recurse -Force} }
