. "$PSScriptRoot/TestHelpers.ps1"
Import-Module "$PSScriptRoot/../src/Settings.psm1" -Force
Import-Module "$PSScriptRoot/../src/Storage.psm1" -Force
Import-Module "$PSScriptRoot/../src/Providers.psm1" -Force
$temp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($temp) | Out-Null
try {
    $key = Get-ATNKey -Parts @('../secret','a|b')
    Assert-True ($key -cmatch '^[a-f0-9]{64}$') 'keys cannot traverse paths'
    Assert-True ((Get-ATNKey @('a','b|c')) -ne (Get-ATNKey @('a|b','c'))) 'array boundaries distinguish keys'
    $path = Join-Path $temp 'state.json'
    Assert-Null (Read-ATNJson $path) 'missing JSON returns null'
    Write-ATNJson $path @{stamp='2026-08-27T12:34:56Z';version=1}
    Assert-True ((Read-ATNJson $path).stamp -is [string]) 'timestamps remain strings'
    Write-ATNJson $path @{version=2}
    Assert-Equal (Read-ATNJson $path).version 2 'atomic replacement publishes latest value'
    Assert-Equal @(Get-ChildItem $temp).Count 1 'temporary writes are cleaned'
    $lockPath = Join-Path $temp 'state.lock'
    $lock = Enter-ATNLock $lockPath
    try {
        Assert-Null (Enter-ATNLock $lockPath -WaitMilliseconds 30) 'occupied lock cannot be acquired'
        # Replace only the nondeterministic clock dependency in the actual function
        # body: a scheduler pause advances it across the deadline between reads.
        $script:clockReads=0
        $script:boundaryClock=[pscustomobject]@{}
        $script:boundaryClock | Add-Member ScriptProperty ElapsedMilliseconds { $script:clockReads++; if ($script:clockReads -eq 1) {0L} else {2L} }
        $body=(Get-Command Enter-ATNLock).ScriptBlock.ToString().Replace('[Diagnostics.Stopwatch]::StartNew()','$script:boundaryClock')
        Assert-Null (& ([scriptblock]::Create($body)) -Path $lockPath -WaitMilliseconds 1) 'Deadline crossing never passes negative sleep'
        foreach ($wait in @(0,1,2)) {Assert-Null (Enter-ATNLock $lockPath -WaitMilliseconds $wait) 'Real short deadline returns safely'}
    } finally { $lock.Dispose() }
    $lock = Enter-ATNLock $lockPath
    Assert-True ($null -ne $lock) 'released stable lock can be reacquired'
    $lock.Dispose()
    Set-ATNCredential $temp bark @{endpoint='https://example.com/base/synthetic-key'}
    Assert-Equal (Get-ATNCredential $temp bark).endpoint 'https://example.com/base/synthetic-key' 'DPAPI round trip'
    foreach ($endpoint in @('http://example.com/key','https://user:pass@example.com/key','https://example.com/key?q=1','https://example.com/key#frag','https://example.com/','https://example.com/a/../key','https://example.com/a%2fb')) {
        Assert-Throws { Set-ATNCredential $temp bark @{endpoint=$endpoint} } 'unsafe endpoint rejected'
    }
    Assert-Equal (Get-ATNCredential $temp bark).endpoint 'https://example.com/base/synthetic-key' 'bad update preserves working credential'
    Assert-Throws { Set-ATNCredential $temp ntfy @{endpoint='https://example.com/topic';token='';allowUnauthenticated=$false} } 'unauthenticated topic needs opt in'
    Assert-Throws { Set-ATNCredential $temp ntfy @{endpoint='https://example.com/a/b';token='synthetic';allowUnauthenticated=$false} } 'ntfy needs exactly one topic'
    Set-ATNCredential $temp ntfy @{endpoint='http://127.0.0.1:12345/topic';token='synthetic-token';allowUnauthenticated=$false}
    Assert-Equal (Get-ATNCredential $temp ntfy).token 'synthetic-token' 'token round trip'
    foreach ($file in Get-ChildItem $temp -File) { Assert-True (-not ([IO.File]::ReadAllText($file.FullName).Contains('synthetic-token'))) 'no plaintext token on disk' }
    $settings = Get-ATNSettings $temp
    $settings.provider = 'ntfy'
    $payload = New-ATNPayload -Agent cursor -Settings $settings -DurationSeconds 2700 -Reason stopped
    Assert-Equal $payload.priority 4 'ntfy defaults to high priority'
    Assert-Equal ($payload.ContainsKey('call')) $false 'no actual telephone field'
    Assert-Equal ($payload.ContainsKey('sound')) $false 'no Bark sound leakage'
    Assert-Equal ($payload.ContainsKey('topic')) $false 'payload invents no topic'
    Assert-Equal $payload.title 'Cursor 长任务已停止' 'correct source and neutral state'
    $settings.provider = 'bark'
    $main = New-ATNPayload cursor $settings 2700 stopped
    $extension = New-ATNPayload cursor $settings 2700 stopped -Extension
    Assert-Equal $main.call 1 'continuous Bark enables call'
    Assert-Equal $main.icon $extension.icon 'extension preserves source icon'
    Assert-Equal $main.group $extension.group 'extension preserves source group'
    $settings.continuous = $false
    Assert-True (-not (New-ATNPayload cursor $settings 2700 stopped).ContainsKey('call')) 'single sound omits call'
    $preview = New-ATNPayload cursor $settings 2700 stopped -Preview
    Assert-True ($preview.body -notmatch '2700|45') 'preview is generic'
    Write-Host 'PASS StorageAndProviders'
} finally { Remove-Item -LiteralPath $temp -Recurse -Force }
