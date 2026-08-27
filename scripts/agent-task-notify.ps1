#requires -Version 7.4
param(
    [string]$Mode = 'Hook',
    [string]$Agent = 'codex',
    [string]$DataDirectory = $(if ($env:ATN_DATA_DIRECTORY) { $env:ATN_DATA_DIRECTORY } else { Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentTaskNotify' }),
    [string]$JobKey,
    [switch]$SendRealPush,
    [int]$RingSeconds = 45
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$neutral = if ($Agent -in @('codex','workbuddy')) { '{"continue":true}' } else { '{}' }
try {
    Import-Module "$PSScriptRoot/../src/Runtime.psm1"
    Import-Module "$PSScriptRoot/../src/Adapters.psm1"
    Import-Module "$PSScriptRoot/../src/Storage.psm1"
    Import-Module "$PSScriptRoot/../src/Providers.psm1"
    switch -CaseSensitive ($Mode) {
        'Hook' {
            # Decode bytes directly, independently of the Agent's console code page.
            $reader = [IO.StreamReader]::new([Console]::OpenStandardInput(), [Text.UTF8Encoding]::new($false,$true), $false)
            try { $raw = $reader.ReadToEnd() }
            catch { Add-ATNInputDiagnostic $DataDirectory 'invalid-utf8'; break }
            finally { $reader.Dispose() }
            try {
                if ([string]::IsNullOrWhiteSpace($raw)) {throw 'Empty JSON input.'}
                $inputEvent = ConvertFrom-Json -InputObject $raw -AsHashtable
            }
            catch { Add-ATNInputDiagnostic $DataDirectory 'invalid-json'; break }
            if (Test-ATNInputShape $Agent $inputEvent) {
                $name = if ($inputEvent.ContainsKey('hook_event_name')) { [string]$inputEvent.hook_event_name } else { '' }
                if ($name) { $neutral = Get-ATNHookOutput $Agent $name }
                $event = ConvertTo-ATNEvent $Agent $inputEvent
                if ($null -ne $event) { Invoke-ATNEvent $event $DataDirectory $PSCommandPath | Out-Null }
            } else { Add-ATNInputDiagnostic $DataDirectory 'invalid-shape' }
        }
        'Worker' {
            Invoke-ATNWorker -JobKey $JobKey -DataDirectory $DataDirectory
            $result = Read-ATNJson (Join-Path $DataDirectory "jobs/$JobKey.json")
            if ($result.status -eq 'failed' -or $result.diagnostic -eq 'ambiguous-send' -or $result.extensionStatus -in @('failed','ambiguous')) { throw 'Worker failed.' }
        }
        'Preview' {
            $job = New-ATNPreview $Agent $DataDirectory -RingSeconds $RingSeconds
            if ($SendRealPush) {
                $key = Get-ATNKey @('preview',[guid]::NewGuid().ToString('N'))
                Write-ATNJson (Join-Path $DataDirectory "jobs/$key.json") $job
                Invoke-ATNWorker $key $DataDirectory
                $result = Read-ATNJson (Join-Path $DataDirectory "jobs/$key.json")
                if ($result.status -ne 'sent' -or $result.extensionStatus -in @('failed','ambiguous')) { throw 'Preview failed.' }
                [Console]::Out.WriteLine('{"accepted":true}')
            } else {
                $payload = New-ATNPayload $job.agentId $job.settings 0 'stopped' -Preview
                [Console]::Out.WriteLine((ConvertTo-Json -InputObject $payload -Depth 16 -Compress))
            }
        }
        'Doctor' { [Console]::Out.WriteLine((Get-ATNDiagnostics $DataDirectory | ConvertTo-Json -Depth 16 -Compress)) }
        default { throw 'Unsupported mode.' }
    }
} catch {
    if ($Mode -cne 'Hook') { [Console]::Error.WriteLine('AgentTaskNotify operation failed; check local configuration and safe Doctor status.'); exit 1 }
}
if ($Mode -ceq 'Hook') { [Console]::Out.WriteLine($neutral) }
exit 0
