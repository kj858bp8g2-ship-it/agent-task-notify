Set-StrictMode -Version Latest
Import-Module "$PSScriptRoot/Storage.psm1"
Import-Module "$PSScriptRoot/Settings.psm1"
Import-Module "$PSScriptRoot/Providers.psm1"

function Start-ATNWorkerProcess {
    param([string]$EntryPoint, [string]$DataDirectory, [string]$JobKey)
    if (-not [IO.File]::Exists($EntryPoint)) { throw 'Worker entry point unavailable.' }
    # Explicit argument list and no inherited handles: Hook stdout closes independently.
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = Join-Path $PSHOME 'pwsh.exe'
    $start.UseShellExecute = $true
    $start.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
    foreach ($arg in @('-NoLogo','-NoProfile','-NonInteractive','-File',[IO.Path]::GetFullPath($EntryPoint),'-Mode','Worker','-DataDirectory',[IO.Path]::GetFullPath($DataDirectory),'-JobKey',$JobKey)) { $start.ArgumentList.Add($arg) }
    $process = [Diagnostics.Process]::Start($start)
    $process.Dispose()
}

function New-ATNJob {
    param([string]$Agent, [hashtable]$Settings, [double]$DurationSeconds, [string]$Reason, [DateTimeOffset]$Now, [int]$RingSeconds = 0, [switch]$Preview)
    # Copy and resolve artwork now, so retries/extension never reread changed settings.
    $snapshot = $Settings | ConvertTo-Json -Depth 32 | ConvertFrom-Json -AsHashtable
    $snapshot.icons = @{$Agent = (Get-ATNIcon $Agent $Settings)}
    if ($RingSeconds -eq 0) { $RingSeconds = if ($DurationSeconds -ge $Settings.longTaskSeconds) { $Settings.longRingSeconds } else { $Settings.mediumRingSeconds } }
    return @{version=1;agentId=$Agent;settings=$snapshot;durationSeconds=$DurationSeconds;ringSeconds=$RingSeconds;reason=$Reason;preview=[bool]$Preview;createdAt=$Now.ToString('o');status='pending';attempts=0;diagnostic=$null;extensionStatus='none'}
}

function Remove-ATNExpiredJobs {
    param([string]$DataDirectory, [DateTimeOffset]$Now)
    $directory = Join-Path $DataDirectory 'jobs'
    if (-not [IO.Directory]::Exists($directory)) { return }
    foreach ($file in [IO.Directory]::EnumerateFiles($directory, '*.json')) {
        if ([IO.Path]::GetFileNameWithoutExtension($file) -cnotmatch '^[a-f0-9]{64}$') { continue }
        $lock = Enter-ATNLock ($file + '.lock')
        if ($null -eq $lock) { continue }
        try {
            $job = Read-ATNJson $file
            if ($job.status -in @('sent','failed') -and $job.ContainsKey('finishedAt') -and $job.extensionStatus -notin @('pending','sending') -and [DateTimeOffset]::Parse($job.finishedAt) -lt $Now.AddDays(-7)) { [IO.File]::Delete($file) }
        } catch { } finally { $lock.Dispose() }
    }
}

function Invoke-ATNEvent {
    param([Parameter(Mandatory)][hashtable]$Event, [Parameter(Mandatory)][string]$DataDirectory, [Parameter(Mandatory)][string]$EntryPoint, [DateTimeOffset]$Now = [DateTimeOffset]::UtcNow)
    if ($Event.agentId -cnotin @('codex','claude-code','cursor','gemini-cli','opencode','workbuddy') -or $Event.isChild -or [string]::IsNullOrWhiteSpace($Event.sessionId) -or $Event.eventType -cnotin @('started','stopped','failed','needs_attention')) { return $null }
    $sessionKey = Get-ATNKey @($Event.agentId, $Event.sessionId)
    $sessionPath = Join-Path $DataDirectory "sessions/$sessionKey.json"
    $lock = Enter-ATNLock ($sessionPath + '.lock') -WaitMilliseconds 2000
    if ($null -eq $lock) { return $null }
    $jobKey = $null
    try {
        $session = Read-ATNJson $sessionPath
        $native = -not [string]::IsNullOrWhiteSpace($Event.nativeRunId)
        $runKey = if ($native) { Get-ATNKey @($sessionKey, $Event.nativeRunId) } elseif ($null -ne $session) { $session.runKey } else { $null }
        $runPath = if ($runKey) { Join-Path $DataDirectory "runs/$runKey.json" } else { $null }
        $run = if ($runPath) { Read-ATNJson $runPath } else { $null }
        if ($Event.eventType -eq 'started') {
            if ($null -ne $run -and ($native -or $run.status -eq 'active')) { return $null }
            if (-not $native) { $runKey = Get-ATNKey @($sessionKey, [guid]::NewGuid().ToString('N')); $runPath = Join-Path $DataDirectory "runs/$runKey.json" }
            Write-ATNJson $runPath @{status='active';startedAt=$Now.ToString('o');attentionCreated=$false;terminalCreated=$false}
            Write-ATNJson $sessionPath @{runKey=$runKey}
            return $null
        }
        if ($null -eq $run -or $run.status -ne 'active') { return $null }
        $attention = $Event.eventType -eq 'needs_attention'
        if ($attention -and $run.attentionCreated) { return $null }
        $duration = [Math]::Max(0, ($Now - [DateTimeOffset]::Parse($run.startedAt)).TotalSeconds)
        # Even invalid settings or sub-threshold terminal events end the timer.
        if (-not $attention) { $run.status='stopped'; Write-ATNJson $runPath $run }
        $settings = Get-ATNSettings $DataDirectory
        if (($attention -and -not $settings.enableAttention) -or $duration -lt $settings.minSeconds) { return $null }
        $kind = if ($attention) { 'attention' } else { 'terminal' }
        $jobKey = Get-ATNKey @($runKey, $kind)
        $jobPath = Join-Path $DataDirectory "jobs/$jobKey.json"
        if ([IO.File]::Exists($jobPath)) { return $null }
        $reason = if ($attention) { 'attention' } elseif ($Event.eventType -eq 'failed') { 'failed' } else { 'stopped' }
        Write-ATNJson $jobPath (New-ATNJob $Event.agentId $settings $duration $reason $Now)
        if ($attention) { $run.attentionCreated=$true } else { $run.terminalCreated=$true }
        Write-ATNJson $runPath $run
        try { Start-ATNWorkerProcess $EntryPoint $DataDirectory $jobKey }
        catch { $job=Read-ATNJson $jobPath; $job.diagnostic='spawn-failed'; Write-ATNJson $jobPath $job }
    } finally { $lock.Dispose() }
    Remove-ATNExpiredJobs $DataDirectory $Now
    return $jobKey
}

function Get-ATNSafeFailure {
    param([Exception]$Exception)
    $diagnostic = [string]$Exception.Data['diagnostic']
    if ($diagnostic -notmatch '^(http:[1-5][0-9]{2}|business:[0-9]{3}|credential|transport|malformed-response)$') { $diagnostic = 'worker-error' }
    return @{diagnostic=$diagnostic;retryable=($Exception.Data['retryable'] -is [bool] -and $Exception.Data['retryable'])}
}

function Invoke-ATNWorker {
    param(
        [Parameter(Mandatory)][ValidatePattern('^[a-f0-9]{64}$')][string]$JobKey,
        [Parameter(Mandatory)][string]$DataDirectory,
        [scriptblock]$Send = { param($settings,$credential,$payload) Send-ATNPush $settings $credential $payload },
        [scriptblock]$Wait = { param($seconds) Start-Sleep -Seconds $seconds }
    )
    $path = Join-Path $DataDirectory "jobs/$JobKey.json"
    $lock = Enter-ATNLock ($path + '.lock') -WaitMilliseconds 2000
    if ($null -eq $lock) { return }
    try {
        $job = Read-ATNJson $path
        if ($null -eq $job) { throw 'Job unavailable.' }
        if ($job.status -eq 'sending') { $job.diagnostic='ambiguous-send'; Write-ATNJson $path $job; return }
        if ($job.status -ne 'pending') {
            if ($job.extensionStatus -in @('pending','sending')) { $job.extensionStatus='ambiguous'; $job.extensionDiagnostic='ambiguous-extension'; Write-ATNJson $path $job }
            return
        }
        # A partially attempted pending job is not a restart-replay queue.
        if ($job.attempts -ne 0) { $job.diagnostic='ambiguous-send'; Write-ATNJson $path $job; return }
        try { $credential = Get-ATNCredential $DataDirectory $job.settings.provider }
        catch { $job.status='failed';$job.diagnostic='credential';$job.finishedAt=[DateTimeOffset]::UtcNow.ToString('o');Write-ATNJson $path $job;return }
        $payload = New-ATNPayload $job.agentId $job.settings $job.durationSeconds $job.reason -Preview:$job.preview
        $delays = @(0,5,15,30,60)
        foreach ($delay in $delays) {
            if ($delay -gt 0) { & $Wait $delay }
            $job.status='sending';$job.attempts++;$job.diagnostic=$null
            Write-ATNJson $path $job
            try {
                $result = & $Send $job.settings $credential $payload
                if ($null -eq $result -or $result.accepted -ne $true) { throw 'Unaccepted result.' }
                $acceptedClock = [Diagnostics.Stopwatch]::StartNew()
                $job.status='sent';$job.finishedAt=[DateTimeOffset]::UtcNow.ToString('o')
                if ($job.settings.provider -eq 'bark' -and $job.settings.continuous -and $job.ringSeconds -gt 30) { $job.extensionStatus='pending' }
                Write-ATNJson $path $job
                break
            } catch {
                $failure = Get-ATNSafeFailure $_.Exception
                $job.diagnostic=$failure.diagnostic
                if (-not $failure.retryable -or $job.attempts -ge 5) { $job.status='failed';$job.finishedAt=[DateTimeOffset]::UtcNow.ToString('o') }
                # Between attempts retain sending: a crash is ambiguous, never replayed.
                Write-ATNJson $path $job
                if ($job.status -eq 'failed') { return }
            }
        }
        if ($job.extensionStatus -eq 'pending') {
            & $Wait ([Math]::Max(0, $job.ringSeconds - 30 - $acceptedClock.Elapsed.TotalSeconds))
            $job.extensionStatus='sending';Write-ATNJson $path $job
            try {
                $extension = New-ATNPayload $job.agentId $job.settings $job.durationSeconds $job.reason -Extension -Preview:$job.preview
                $result = & $Send $job.settings $credential $extension
                if ($null -eq $result -or $result.accepted -ne $true) { throw 'Unaccepted result.' }
                $job.extensionStatus='sent'
            } catch { $job.extensionStatus='failed';$job.extensionDiagnostic=(Get-ATNSafeFailure $_.Exception).diagnostic }
            Write-ATNJson $path $job
        }
    } finally { $lock.Dispose() }
}

function New-ATNPreview {
    param([Parameter(Mandatory)][string]$Agent, [Parameter(Mandatory)][string]$DataDirectory, [ValidateRange(30,60)][int]$RingSeconds = 45)
    Get-ATNAgent $Agent | Out-Null
    $settings = Get-ATNSettings $DataDirectory
    return New-ATNJob $Agent $settings 0 'stopped' ([DateTimeOffset]::UtcNow) -RingSeconds $RingSeconds -Preview
}

function Add-ATNInputDiagnostic {
    param([string]$DataDirectory,[ValidateSet('invalid-json','invalid-utf8','invalid-shape')][string]$Code)
    $path=Join-Path $DataDirectory 'input-diagnostics.json'
    $lock=Enter-ATNLock ($path + '.lock') -WaitMilliseconds 100
    if ($null -eq $lock) {return}
    try {
        $counts=@{'invalid-json'=0;'invalid-utf8'=0;'invalid-shape'=0}
        try {$old=Read-ATNJson $path} catch {$old=$null}
        foreach ($key in @($counts.Keys)) {if ($old -and $old[$key] -is [long]) {$counts[$key]=[Math]::Clamp($old[$key],0,1000)}}
        $counts[$Code]=[Math]::Min(1000,$counts[$Code]+1)
        Write-ATNJson $path $counts
    } finally {$lock.Dispose()}
}

function Get-ATNDiagnostics {
    param([Parameter(Mandatory)][string]$DataDirectory)
    $settings = Get-ATNSettings $DataDirectory
    $safeSettings = @{}
    foreach ($key in @('provider','minSeconds','longTaskSeconds','mediumRingSeconds','longRingSeconds','continuous','level','volume','ntfyPriority','enableAttention')) { $safeSettings[$key]=$settings[$key] }
    $safeSettings.soundConfigured = -not [string]::IsNullOrWhiteSpace($settings.sound)
    $safeSettings.iconOverrideCount = $settings.icons.Count
    $counts = @{pending=0;sending=0;sent=0;failed=0;invalid=0}
    $extensions=@{none=0;pending=0;sending=0;sent=0;failed=0;ambiguous=0;invalid=0}
    $failures=@{'spawn-failed'=0;'ambiguous-send'=0;'ambiguous-extension'=0;credential=0;transport=0;'malformed-response'=0;'worker-error'=0;'http-4xx'=0;'http-5xx'=0;business=0;other=0}
    $attempts=0; $scanned=0; $truncated=$false
    $directory = Join-Path $DataDirectory 'jobs'
    if ([IO.Directory]::Exists($directory)) {
        foreach ($path in [IO.Directory]::EnumerateFiles($directory,'*.json')) {
            if ($scanned -ge 1000) {$truncated=$true;break}; $scanned++
            try {
                $job=Read-ATNJson $path
                if ($job.status -in @('pending','sending','sent','failed')) { $counts[$job.status]++ } else { $counts.invalid++ }
                $extension=$job['extensionStatus']; if ($extension -in @('none','pending','sending','sent','failed','ambiguous')) {$extensions[$extension]++} else {$extensions.invalid++}
                if ($job['attempts'] -is [long]) {$attempts += [Math]::Clamp($job.attempts,0,5)}
                foreach ($field in @('diagnostic','extensionDiagnostic')) {
                    $code=$job[$field]; if (-not $code) {continue}
                    $category=if ($code -in @('spawn-failed','ambiguous-send','ambiguous-extension','credential','transport','malformed-response','worker-error')) {$code} elseif ($code -match '^http:4[0-9]{2}$') {'http-4xx'} elseif ($code -match '^http:5[0-9]{2}$') {'http-5xx'} elseif ($code -match '^business:[0-9]{3}$') {'business'} else {'other'}
                    $failures[$category]++
                }
            }
            catch { $counts.invalid++ }
        }
    }
    $receipts = @{}
    foreach ($agent in @('codex','claude-code','cursor','gemini-cli','opencode','workbuddy')) { $receipts[$agent]=@{receiptPresent=[IO.File]::Exists((Join-Path $DataDirectory "receipts/$agent.json"))} }
    $inputCounts=@{'invalid-json'=0;'invalid-utf8'=0;'invalid-shape'=0}
    try {$saved=Read-ATNJson (Join-Path $DataDirectory 'input-diagnostics.json');foreach ($key in @($inputCounts.Keys)) {if ($saved -and $saved[$key] -is [long]) {$inputCounts[$key]=[Math]::Clamp($saved[$key],0,1000)}}} catch {}
    $recoveryCount=0
    if ([IO.Directory]::Exists($DataDirectory)) {foreach ($file in [IO.Directory]::EnumerateFiles($DataDirectory,'configuration-recovery-*.dpapi')) {$recoveryCount++;if($recoveryCount -ge 1000){break}}}
    return @{settings=$safeSettings;jobs=$counts;extensions=$extensions;failures=$failures;attempts=$attempts;truncated=$truncated;input=$inputCounts;configurationRecoveryCount=$recoveryCount;installation=$receipts;credentialPresent=[IO.File]::Exists((Join-Path $DataDirectory "credentials-$($settings.provider).json"))}
}

Export-ModuleMember -Function Invoke-ATNEvent, Invoke-ATNWorker, New-ATNPreview, Get-ATNDiagnostics, Add-ATNInputDiagnostic
