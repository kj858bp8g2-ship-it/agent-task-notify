Set-StrictMode -Version Latest

function Get-ATNRequiredString {
    param([hashtable]$InputEvent, [string]$Name)
    if (-not $InputEvent.ContainsKey($Name) -or $InputEvent[$Name] -isnot [string] -or [string]::IsNullOrWhiteSpace($InputEvent[$Name])) { return $null }
    return $InputEvent[$Name]
}

function New-ATNEvent {
    param([string]$AgentId, [string]$SessionId, [AllowNull()]$NativeRunId, [string]$EventType, [AllowNull()]$Reason, [bool]$IsChild = $false)
    return @{ agentId = $AgentId; sessionId = $SessionId; nativeRunId = $NativeRunId; eventType = $EventType; reason = $Reason; isChild = $IsChild }
}

function ConvertTo-ATNEvent {
    param([Parameter(Mandatory)][string]$Agent, [Parameter(Mandatory)][hashtable]$InputEvent)
    if ($Agent -notin @('codex','claude-code','cursor','gemini-cli','opencode','workbuddy')) { throw "Unknown agent source '$Agent'." }
    if ($InputEvent.ContainsKey('parent_session_id') -or $InputEvent.ContainsKey('parentSessionId') -or (Get-ATNRequiredString $InputEvent 'hook_event_name') -eq 'SubagentStop') { return $null }
    if ($Agent -eq 'claude-code' -and -not [string]::IsNullOrWhiteSpace((Get-ATNRequiredString $InputEvent 'agent_id'))) { return $null }

    switch ($Agent) {
        'codex' {
            $session = Get-ATNRequiredString $InputEvent 'session_id'; $turn = Get-ATNRequiredString $InputEvent 'turn_id'; $hook = Get-ATNRequiredString $InputEvent 'hook_event_name'
            if (-not $session -or -not $turn) { return $null }
            if ($hook -eq 'UserPromptSubmit') { return New-ATNEvent $Agent $session $turn 'started' $null }
            if ($hook -eq 'Stop') { return New-ATNEvent $Agent $session $turn 'stopped' 'stopped' }
            return $null
        }
        'claude-code' {
            $session = Get-ATNRequiredString $InputEvent 'session_id'; $hook = Get-ATNRequiredString $InputEvent 'hook_event_name'
            if (-not $session) { return $null }
            if ($hook -eq 'UserPromptSubmit') { return New-ATNEvent $Agent $session $null 'started' $null }
            if ($hook -eq 'Stop') { return New-ATNEvent $Agent $session $null 'stopped' 'stopped' }
            if ($hook -eq 'StopFailure') { return New-ATNEvent $Agent $session $null 'failed' 'failed' }
            return $null
        }
        'cursor' {
            $session = Get-ATNRequiredString $InputEvent 'conversation_id'; $run = Get-ATNRequiredString $InputEvent 'generation_id'; $hook = Get-ATNRequiredString $InputEvent 'hook_event_name'
            if (-not $session -or -not $run) { return $null }
            if ($hook -eq 'beforeSubmitPrompt') { return New-ATNEvent $Agent $session $run 'started' $null }
            if ($hook -ne 'stop') { return $null }
            $status = Get-ATNRequiredString $InputEvent 'status'
            if ($status -eq 'completed') { return New-ATNEvent $Agent $session $run 'stopped' 'completed' }
            if ($status -in @('error','aborted')) { return New-ATNEvent $Agent $session $run 'failed' 'failed' }
            return $null
        }
        'gemini-cli' {
            $session = Get-ATNRequiredString $InputEvent 'session_id'; $hook = Get-ATNRequiredString $InputEvent 'hook_event_name'
            if (-not $session) { return $null }
            if ($hook -eq 'BeforeAgent') { return New-ATNEvent $Agent $session $null 'started' $null }
            if ($hook -eq 'AfterAgent') { return New-ATNEvent $Agent $session $null 'stopped' 'stopped' }
            return $null
        }
        'opencode' {
            $version = $InputEvent['schemaVersion']; $eventName = Get-ATNRequiredString $InputEvent 'event'; $session = Get-ATNRequiredString $InputEvent 'sessionId'; $run = Get-ATNRequiredString $InputEvent 'runId'
            if ($version -ne 1 -or -not $eventName -or -not $session -or -not $run) { return $null }
            $child = -not [string]::IsNullOrWhiteSpace((Get-ATNRequiredString $InputEvent 'parentId'))
            if ($eventName -eq 'started') { return New-ATNEvent $Agent $session $run 'started' $null $child }
            if ($eventName -eq 'stopped') { return New-ATNEvent $Agent $session $run 'stopped' 'stopped' $child }
            if ($eventName -eq 'failed') { return New-ATNEvent $Agent $session $run 'failed' 'failed' $child }
            return $null
        }
        'workbuddy' {
            $session = Get-ATNRequiredString $InputEvent 'session_id'; $hook = Get-ATNRequiredString $InputEvent 'hook_event_name'
            if (-not $session) { return $null }
            if ($hook -eq 'UserPromptSubmit') { return New-ATNEvent $Agent $session $null 'started' $null }
            if ($hook -eq 'Stop') { return New-ATNEvent $Agent $session $null 'stopped' 'stopped' }
            return $null
        }
    }
}

function Test-ATNInputShape {
    param([string]$Agent, $InputEvent)
    if ($InputEvent -isnot [hashtable]) {return $false}
    $required=switch ($Agent) {
        codex {@('session_id','turn_id','hook_event_name')}
        cursor {@('conversation_id','generation_id','hook_event_name')}
        opencode {@('sessionId','runId','event')}
        {$_ -in @('claude-code','gemini-cli','workbuddy')} {@('session_id','hook_event_name')}
        default {return $false}
    }
    foreach ($field in $required) {if (-not (Get-ATNRequiredString $InputEvent $field)) {return $false}}
    if ($Agent -eq 'opencode' -and $InputEvent['schemaVersion'] -ne 1) {return $false}
    if ($Agent -eq 'cursor' -and $InputEvent.hook_event_name -eq 'stop' -and (Get-ATNRequiredString $InputEvent 'status') -notin @('completed','aborted','error')) {return $false}
    return $true
}

function Get-ATNHookOutput {
    param([Parameter(Mandatory)][string]$Agent, [Parameter(Mandatory)][string]$EventName)
    if ($Agent -notin @('codex','claude-code','cursor','gemini-cli','opencode','workbuddy')) { throw "Unknown agent source '$Agent'." }
    if ($Agent -in @('codex','workbuddy')) { return '{"continue":true}' }
    if ($Agent -eq 'cursor' -and $EventName -eq 'beforeSubmitPrompt') { return '{"continue":true}' }
    return '{}'
}

Export-ModuleMember -Function ConvertTo-ATNEvent, Get-ATNHookOutput, Test-ATNInputShape
