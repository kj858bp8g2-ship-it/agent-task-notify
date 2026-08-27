Set-StrictMode -Version Latest
Import-Module "$PSScriptRoot/Settings.psm1"
Import-Module "$PSScriptRoot/Storage.psm1"

function New-ATNPayload {
    param([Parameter(Mandatory)][string]$Agent, [Parameter(Mandatory)][hashtable]$Settings, [Parameter(Mandatory)][double]$DurationSeconds, [Parameter(Mandatory)][string]$Reason, [switch]$Extension, [switch]$Preview)
    $source = Get-ATNAgent $Agent
    $title = "$($source.displayName) 长任务已停止"
    $body = '任务已停止，请查看应用。耗时约 ' + [Math]::Floor($DurationSeconds / 60) + ' 分钟。'
    if ($Reason -eq 'attention') { $title = "$($source.displayName) 任务需要关注" }
    if ($Preview) { $title = "$($source.displayName) 通知预览"; $body = '这是一条通用测试通知。' }
    if ($Settings.provider -eq 'ntfy') { $payload = @{title=$title;message=$body;priority=$Settings.ntfyPriority} }
    elseif ($Settings.provider -eq 'bark') {
        $payload = @{title=$title;body=$body;group=$source.displayName;level=$Settings.level;volume=$Settings.volume;sound=$Settings.sound;isArchive=0}
        if ($Settings.continuous) { $payload.call = 1 }
    } else { throw 'Unknown provider.' }
    $icon = Get-ATNIcon $Agent $Settings
    if ($icon) { $payload.icon = $icon }
    return $payload
}

function New-ATNPushFailure {
    param([bool]$Retryable, [string]$Diagnostic)
    $exception = [InvalidOperationException]::new('Push was not accepted.')
    $exception.Data['retryable'] = $Retryable
    $exception.Data['diagnostic'] = $Diagnostic
    return $exception
}

function Send-ATNPush {
    param([Parameter(Mandatory)][hashtable]$Settings, [Parameter(Mandatory)][hashtable]$Credential, [Parameter(Mandatory)][hashtable]$Payload)
    try { $uri = Test-ATNCredential $Settings.provider $Credential } catch { throw (New-ATNPushFailure $false 'credential') }
    $body = @{}
    $allowed = if ($Settings.provider -eq 'bark') { @('title','body','group','level','volume','sound','icon','isArchive') } else { @('title','message','priority','icon') }
    if ($Settings.provider -eq 'bark' -and $Settings.continuous) { $allowed += 'call' }
    foreach ($key in $allowed) { if ($Payload.ContainsKey($key)) { $body[$key] = $Payload[$key] } }
    if ($Settings.provider -eq 'ntfy') { $body.topic = $uri.AbsolutePath.Substring(1); $uri = [uri]($uri.GetLeftPart([UriPartial]::Authority) + '/') }
    $handler = [Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $client = [Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromSeconds(12)
    $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Post, $uri)
    $response = $null
    try {
        if ($Settings.provider -eq 'ntfy' -and -not [string]::IsNullOrWhiteSpace($Credential.token)) { $request.Headers.Authorization = [Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $Credential.token) }
        $request.Content = [Net.Http.ByteArrayContent]::new([Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -InputObject $body -Depth 16 -Compress)))
        $request.Content.Headers.ContentType = [Net.Http.Headers.MediaTypeHeaderValue]::Parse('application/json; charset=utf-8')
        $response = $client.SendAsync($request).GetAwaiter().GetResult()
        $status = [int]$response.StatusCode
        if ($status -lt 200 -or $status -ge 300) { throw (New-ATNPushFailure ($status -in @(408,425,429) -or $status -ge 500) "http:$status") }
        try { $result = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult() | ConvertFrom-Json -AsHashtable -ErrorAction Stop } catch { throw (New-ATNPushFailure $true 'malformed-response') }
        if ($result -isnot [hashtable]) { throw (New-ATNPushFailure $true 'malformed-response') }
        if ($Settings.provider -eq 'bark') {
            if (-not $result.ContainsKey('code') -or ($result.code -isnot [long] -and $result.code -isnot [int])) { throw (New-ATNPushFailure $true 'malformed-response') }
            if ($result.code -ne 200) { throw (New-ATNPushFailure ($result.code -in @(408,425,429) -or $result.code -ge 500) "business:$($result.code)") }
        } elseif (-not $result.ContainsKey('id') -or -not $result.ContainsKey('event') -or -not $result.ContainsKey('topic') -or $result.id -isnot [string] -or $result.event -isnot [string] -or $result.topic -isnot [string] -or [string]::IsNullOrWhiteSpace($result.id) -or $result.event -cne 'message' -or $result.topic -cne $body.topic) { throw (New-ATNPushFailure $true 'malformed-response') }
        return @{accepted=$true}
    } catch {
        if ($_.Exception.Data.Contains('retryable')) { throw $_.Exception }
        throw (New-ATNPushFailure $true 'transport')
    } finally { if ($null -ne $response) { $response.Dispose() }; $request.Dispose(); $client.Dispose() }
}

Export-ModuleMember -Function New-ATNPayload, Send-ATNPush
