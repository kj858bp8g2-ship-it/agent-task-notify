. "$PSScriptRoot/TestHelpers.ps1"
Import-Module "$PSScriptRoot/../src/Storage.psm1"
Import-Module "$PSScriptRoot/../src/Settings.psm1"
$runtimePath = "$PSScriptRoot/../src/Runtime.psm1"
Assert-True (Test-Path $runtimePath) 'Lifecycle runtime must exist'
Import-Module $runtimePath
$root = Join-Path ([IO.Path]::GetTempPath()) ('atn-runtime-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($root) | Out-Null
$now = [DateTimeOffset]'2026-01-01T00:00:00Z'
function New-TestEvent($session, $run, $type, $agent = 'codex') {
    return @{agentId=$agent;sessionId=$session;nativeRunId=$run;eventType=$type;reason='stopped';isChild=$false}
}
function Invoke-TestEvent($event, $at = $now) {
    # Missing entry point deliberately exercises retained-pending spawn failures.
    Invoke-ATNEvent -Event $event -DataDirectory $root -EntryPoint (Join-Path $root 'missing.ps1') -Now $at
}
function Read-TestJob($key) { Read-ATNJson (Join-Path $root "jobs/$key.json") }
try {
    Write-ATNJson (Join-Path $root 'settings.json') @{minSeconds=300;longTaskSeconds=1200}
    # Catches off-by-one duration classification and failure to freeze settings.
    foreach ($case in @(@{s=299;ring=0},@{s=300;ring=45},@{s=1199;ring=45},@{s=1200;ring=60})) {
        $session = "private-session-$($case.s)"
        Assert-Null (Invoke-TestEvent (New-TestEvent $session 'private-run' 'started')) 'Start does not notify'
        $key = Invoke-TestEvent (New-TestEvent $session 'private-run' 'stopped') $now.AddSeconds($case.s)
        if ($case.ring -eq 0) { Assert-Null $key 'Below threshold stays silent' }
        else {
            $job = Read-TestJob $key
            Assert-Equal $job.durationSeconds $case.s 'Duration frozen at terminal event'
            Assert-Equal $job.ringSeconds $case.ring 'Hand-derived ring boundary'
            Assert-Equal $job.status 'pending' 'Failed spawn retains pending job'
            Assert-Equal $job.diagnostic 'spawn-failed' 'Spawn failure is safe'
            Assert-Equal $job.settings.minSeconds 300 'Snapshot contains thresholds'
        }
        Assert-Null (Invoke-TestEvent (New-TestEvent $session 'private-run' 'failed') $now.AddHours(2)) 'Terminal variants cannot create two groups'
        Assert-Null (Invoke-TestEvent (New-TestEvent $session 'private-run' 'started') $now.AddHours(3)) 'Old native ID cannot restart'
        Assert-Null (Invoke-TestEvent (New-TestEvent $session 'private-run' 'stopped') $now.AddHours(4)) 'Old native ID cannot notify again'
    }
    Invoke-TestEvent (New-TestEvent 'repeat' 'r' 'started') | Out-Null
    Invoke-TestEvent (New-TestEvent 'repeat' 'r' 'started') $now.AddSeconds(250) | Out-Null
    $key = Invoke-TestEvent (New-TestEvent 'repeat' 'r' 'stopped') $now.AddSeconds(300)
    Assert-Equal (Read-TestJob $key).durationSeconds 300 'Repeated start preserves original timer'
    $keys = @()
    foreach ($agent in @('claude-code','gemini-cli')) {
        foreach ($session in @('same-session','other-session')) {
            foreach ($turn in @(0,1)) {
                $at=$now.AddHours($turn)
                Invoke-TestEvent (New-TestEvent $session $null 'started' $agent) $at | Out-Null
                Invoke-TestEvent (New-TestEvent $session $null 'started' $agent) $at.AddSeconds(1) | Out-Null
                $keys += Invoke-TestEvent (New-TestEvent $session $null 'stopped' $agent) $at.AddSeconds(300)
            }
        }
    }
    Assert-Equal @($keys | Sort-Object -Unique).Count 8 'Local runs, sources and sessions never collide'
    $child=New-TestEvent 'child' 'r' 'started';$child.isChild=$true
    Assert-Null (Invoke-TestEvent $child) 'Child ignored'
    Assert-Null (Invoke-TestEvent (New-TestEvent 'unknown' 'r' 'started' 'unknown')) 'Unknown source ignored'
    Assert-Null (Invoke-TestEvent (New-TestEvent 'orphan' 'r' 'stopped')) 'Orphan stop ignored'
    $serialized=(Get-ChildItem $root -Recurse -Filter *.json | ForEach-Object { Get-Content -Raw $_.FullName }) -join ''
    Assert-True ($serialized -notmatch 'private-session|private-run|same-session|other-session') 'Raw source identities never persisted'
    'PASS Runtime lifecycle'
    # Catches replay of ambiguous sending, unbounded retries, and loss of frozen settings.
    Assert-True ($null -ne (Get-Command Invoke-ATNWorker -ErrorAction SilentlyContinue)) 'Worker must exist'
    Set-ATNCredential $root 'bark' @{endpoint='http://127.0.0.1/synthetic-key'}
    # Missing bounded startup waiting can lose a worker during a cleanup lock.
    $startupKey=Get-ATNKey @('synthetic-startup-lock')
    $startupPath=Join-Path $root "jobs/$startupKey.json"
    Write-ATNJson $startupPath (New-ATNPreview codex $root -RingSeconds 30)
    try {
        $observed=& (Get-Module Runtime) {
            param($directory,$jobKey)
            $originalLock=Get-Command Enter-ATNLock
            $observation=@{waitMilliseconds=-1;sends=0;restored=$false}
            try {
                function Enter-ATNLock {
                    param([string]$Path,[int]$WaitMilliseconds=0)
                    $observation.waitMilliseconds=$WaitMilliseconds
                    & $originalLock -Path $Path -WaitMilliseconds $WaitMilliseconds
                }
                Invoke-ATNWorker $jobKey $directory -Send {param($s,$c,$p) $observation.sends++;@{accepted=$true}}
            } finally {
                Set-Item Function:Enter-ATNLock -Value $originalLock.ScriptBlock
                $observation.restored=[object]::ReferenceEquals((Get-Command Enter-ATNLock).ScriptBlock,$originalLock.ScriptBlock)
            }
            return $observation
        } $root $startupKey
        Assert-True $observed.restored 'Startup lock observer restores the original function'
        Assert-Equal $observed.waitMilliseconds 2000 'Worker waits briefly for a startup maintenance lock'
        Assert-Equal $observed.sends 1 'Observed worker still invokes the real lock and sends once'
        Assert-Equal (Read-TestJob $startupKey).status 'sent' 'Observed worker persists accepted state'
        Assert-Equal (Read-TestJob $startupKey).attempts 1 'Observed worker persists one attempt'
        'PASS Runtime startup lock wait'
    } finally { [IO.File]::Delete($startupPath) }
    $script:sends=0; $script:waits=@()
    $sendFailure={ param($settings,$credential,$payload)
        $script:sends++
        Assert-Equal $settings.minSeconds 300 'Retry uses frozen configuration'
        $error=[InvalidOperationException]::new('raw-secret-error')
        $error.Data['retryable']=$true; $error.Data['diagnostic']='http:503'; throw $error
    }
    $wait={param($seconds) $script:waits += $seconds}
    Write-ATNJson (Join-Path $root 'settings.json') @{minSeconds=999;longTaskSeconds=2000}
    Invoke-ATNWorker $key $root -Send $sendFailure -Wait $wait
    $job=Read-TestJob $key
    Assert-Equal $job.status 'failed' 'Failure exhaustion persisted'
    Assert-Equal $job.attempts 5 'Five total attempts maximum'
    Assert-Equal ($script:waits -join ',') '5,15,30,60' 'Four additional backoff delays'
    Assert-Equal $job.diagnostic 'http:503' 'Only classified diagnostic persisted'
    Assert-Equal $script:sends 5 'No sixth request'
    Invoke-ATNWorker $key $root -Send $sendFailure -Wait $wait
    Assert-Equal $script:sends 5 'Failure tombstone never automatically replayed'
    $key=$keys[0];$script:sends=0;$script:waits=@()
    $sendExtensionFailure={param($settings,$credential,$payload)
        $script:sends++
        $persisted=Read-TestJob $key
        if ($script:sends -eq 1) {
            Assert-Equal $persisted.status 'sending' 'Sending intent durable before network'
            Assert-Equal $persisted.attempts 1 'Attempt durable before network'
            return @{accepted=$true}
        }
        Assert-Equal $persisted.status 'sent' 'Main accepted before extension'
        throw 'private exception'
    }
    Invoke-ATNWorker $key $root -Send $sendExtensionFailure -Wait $wait
    $job=Read-TestJob $key
    Assert-Equal $job.status 'sent' 'Extension failure does not undo main acknowledgment'
    Assert-Equal $job.extensionStatus 'failed' 'Separate extension failure'
    Assert-Equal $job.attempts 1 'Main not retried after extension failure'
    Assert-Equal $script:sends 2 'Only one extension'
    Assert-True ($script:waits.Count -eq 1 -and $script:waits[0] -gt 14 -and $script:waits[0] -le 15) 'Extension scheduled target minus thirty after main'
    Invoke-ATNWorker $key $root -Send $sendExtensionFailure -Wait $wait
    Assert-Equal $script:sends 2 'Accepted main never replayed'
    $key=$keys[1]; $job=Read-TestJob $key;$job.status='sending';$job.attempts=1;Write-ATNJson (Join-Path $root "jobs/$key.json") $job
    Invoke-ATNWorker $key $root -Send $sendFailure -Wait $wait
    Assert-Equal (Read-TestJob $key).diagnostic 'ambiguous-send' 'Crashed sending is diagnosed without replay'
    Assert-Equal $script:sends 2 'Ambiguous send not retried'
    $key=$keys[2];$script:sends=0
    Invoke-ATNWorker $key $root -Send {param($s,$c,$p) $script:sends++;$e=[Exception]::new('secret');$e.Data['retryable']=$false;$e.Data['diagnostic']='http:401';throw $e} -Wait $wait
    Assert-Equal (Read-TestJob $key).attempts 1 'Permanent rejection stops after first attempt'
    $key=$keys[3];$held=Enter-ATNLock (Join-Path $root "jobs/$key.json.lock")
    try { Invoke-ATNWorker $key $root -Send $sendFailure -Wait $wait } finally { $held.Dispose() }
    Assert-Equal (Read-TestJob $key).status 'pending' 'Concurrent worker cannot touch locked job'
    Assert-Throws { Invoke-ATNWorker '../escape' $root } 'Job keys reject path traversal'
    $key=$keys[4];$job=Read-TestJob $key;$job.settings.continuous=$false;Write-ATNJson (Join-Path $root "jobs/$key.json") $job
    $script:sends=0;$script:waits=@()
    Invoke-ATNWorker $key $root -Send {param($s,$c,$p) $script:sends++;@{accepted=$true}} -Wait $wait
    Assert-Equal $script:sends 1 'Single sound sends once'
    Assert-Equal $script:waits.Count 0 'Single sound schedules no extension'
    'PASS Runtime worker'
    Assert-True ($null -ne (Get-Command New-ATNPreview -ErrorAction SilentlyContinue)) 'Preview must exist'
    $before=@(Get-ChildItem (Join-Path $root 'jobs') -Filter *.json).Count
    $preview=New-ATNPreview -Agent 'cursor' -DataDirectory $root -RingSeconds 31
    Assert-Equal $preview.ringSeconds 31 'Preview honors validated ring target'
    Assert-Equal $preview.preview $true 'Preview is synthetic'
    Assert-Equal @(Get-ChildItem (Join-Path $root 'jobs') -Filter *.json).Count $before 'Constructing preview has no durable or network side effect'
    Assert-Throws { New-ATNPreview 'cursor' $root -RingSeconds 29 } 'Preview rejects out of range ring'
    $diag=Get-ATNDiagnostics $root
    Assert-Equal $diag.jobs.failed 2 'Doctor counts exhausted and permanent failures'
    Assert-Equal $diag.jobs.pending 7 'Doctor counts pending without replay'
    Assert-True (($diag | ConvertTo-Json -Depth 16) -notmatch 'https?://|synthetic|private|raw-secret') 'Doctor output excludes endpoints and artwork URL details (fixed HTTP status classes are legal)'
    Write-ATNJson (Join-Path $root 'settings.json') @{minSeconds=300;longTaskSeconds=1200}
    Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'started') | Out-Null
    Assert-Null (Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'needs_attention') $now.AddSeconds(300)) 'Attention disabled by default'
    Write-ATNJson (Join-Path $root 'settings.json') @{minSeconds=300;longTaskSeconds=1200;enableAttention=$true}
    $attentionKey=Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'needs_attention') $now.AddSeconds(300)
    Assert-Equal (Read-TestJob $attentionKey).reason 'attention' 'Explicit recognized domain attention'
    Assert-Null (Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'needs_attention') $now.AddSeconds(301)) 'Attention deduplication'
    Assert-Null (Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'attention') $now.AddSeconds(301)) 'Unknown attention spelling ignored'
    $terminalKey=Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'stopped') $now.AddSeconds(302)
    Assert-True ($terminalKey -ne $attentionKey) 'Terminal has independent single notification group'
    Assert-Null (Invoke-TestEvent (New-TestEvent 'attention-session' 'r' 'needs_attention') $now.AddSeconds(303)) 'Attention after stop ignored'
    # Expiry never touches active timers, pending jobs or ambiguous sends.
    $expired=Read-TestJob $keys[0];$expired.finishedAt=$now.AddDays(-8).ToString('o');Write-ATNJson (Join-Path $root "jobs/$($keys[0]).json") $expired
    Invoke-TestEvent (New-TestEvent 'cleanup' 'r' 'started') | Out-Null
    Invoke-TestEvent (New-TestEvent 'cleanup' 'r' 'stopped') $now.AddSeconds(300) | Out-Null
    Assert-Null (Read-TestJob $keys[0]) 'Expired completed job removed'
    Assert-Equal (Read-TestJob $keys[1]).status 'sending' 'Ambiguous sending never expires automatically'
    Assert-Equal (Read-TestJob $keys[3]).status 'pending' 'Pending never expires automatically'
    foreach ($provider in @('bark','ntfy')) {
        Set-ATNCredential $root 'ntfy' @{endpoint='http://127.0.0.1/synthetic-topic';token='';allowUnauthenticated=$true}
        $candidate=New-ATNPreview 'cursor' $root -RingSeconds 30
        $candidate.settings.provider=$provider
        if ($provider -eq 'ntfy') { $candidate.ringSeconds=60 }
        $candidateKey=Get-ATNKey @('test-short',$provider)
        Write-ATNJson (Join-Path $root "jobs/$candidateKey.json") $candidate
        $script:sends=0;$script:waits=@()
        Invoke-ATNWorker $candidateKey $root -Send {param($s,$c,$p) $script:sends++;@{accepted=$true}} -Wait $wait
        Assert-Equal $script:sends 1 'Thirty-second Bark or sixty-second ntfy has exactly one notification'
        Assert-Equal $script:waits.Count 0 'Thirty-second Bark or sixty-second ntfy has no extension wait'
    }
    Write-ATNJson (Join-Path $root 'receipts/cursor.json') @{private='synthetic-secret-path'}
    $diag=Get-ATNDiagnostics $root
    Assert-True $diag.installation.cursor.receiptPresent 'Doctor exposes per-source receipt presence'
    Assert-True (-not $diag.installation.codex.receiptPresent) 'Doctor does not invent other installed sources'
    Assert-True (($diag | ConvertTo-Json -Depth 16) -notmatch 'synthetic-secret-path') 'Receipt contents not disclosed'
    'PASS Runtime preview diagnostics attention cleanup'
} finally { Remove-Item -LiteralPath $root -Recurse -Force }
