Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/TestHelpers.ps1"
Import-Module "$PSScriptRoot/../src/Settings.psm1" -Force
Import-Module "$PSScriptRoot/../src/Adapters.psm1" -Force

$temp = Join-Path ([IO.Path]::GetTempPath()) ("atn-test-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temp | Out-Null
try {
    $settings = Get-ATNSettings -DataDirectory $temp
    Assert-Equal $settings.minSeconds 1800 'fresh install uses default threshold'
    Assert-Equal $settings.provider 'bark' 'fresh install uses Bark'
    Assert-Equal $settings.enableAttention $false 'attention is disabled by default'
    Assert-Equal $settings.continuous $true 'continuous alerting defaults on'
    Assert-Equal $settings.level 'critical' 'default level is critical'
    Assert-Equal $settings.volume 7 'default volume survives loading'
    Assert-Equal $settings.ntfyPriority 4 'default ntfy priority survives loading'

    @{minSeconds=300;longTaskSeconds=1200;mediumRingSeconds=30;longRingSeconds=45;sound='minuet';icons=@{codex='https://example.test/codex.png'}} | ConvertTo-Json -Depth 3 | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    $custom = Get-ATNSettings -DataDirectory $temp
    Assert-Equal $custom.minSeconds 300 'custom threshold survives loading'
    Assert-Equal $custom.sound 'minuet' 'custom sound survives loading'
    Assert-Equal (Get-ATNIcon -Agent codex -Settings $custom) 'https://example.test/codex.png' 'valid configured icon overrides catalog'
    $custom.icons.codex = ''
    Assert-Equal (Get-ATNIcon -Agent codex -Settings $custom) '' 'empty configured icon disables artwork'
    $custom.icons.codex = 'http://invalid.example/codex.png'
    Assert-Equal (Get-ATNIcon -Agent codex -Settings $custom) '' 'invalid configured icon does not leak artwork URL'

    @{minSeconds=0} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'zero threshold is rejected'
    @{unknown='value'} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'unknown configuration key is rejected'
    @{continuous='true'} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'string boolean is rejected'
    @{minSeconds=12.5} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'fractional threshold is rejected'
    @{minSeconds=1800;longTaskSeconds=1800} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'inverted task tiers are rejected'
    @{mediumRingSeconds=29} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'out-of-range ring target is rejected'
    @{provider='other'} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'invalid provider is rejected'
    @{ntfyPriority=6} | ConvertTo-Json | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'invalid ntfy priority is rejected'
    '{ not-json }' | Set-Content (Join-Path $temp 'settings.json') -Encoding utf8
    Assert-Throws { Get-ATNSettings -DataDirectory $temp } 'malformed JSON is rejected'

    $agents = @('codex','claude-code','cursor','gemini-cli','opencode','workbuddy')
    $names = @()
    foreach ($agentId in $agents) {
        $agent = Get-ATNAgent -Agent $agentId
        $names += $agent.displayName
        Assert-Equal $agent.id $agentId "$agentId catalog record keeps its identity"
        Assert-True ($agent.iconUrl -like 'https://*') "$agentId artwork uses HTTPS"
        Assert-True ($agent.sourceUrl -like 'https://*') "$agentId provenance uses HTTPS"
    }
    Assert-Equal (@($names | Select-Object -Unique).Count) 6 'catalog source names remain distinct'
    Assert-Throws { Get-ATNAgent -Agent 'Codex' } 'catalog does not guess aliases'
    Assert-Equal (Get-ATNAgent -Agent 'gemini-cli').iconUrl 'https://is1-ssl.mzstatic.com/image/thumb/Purple211/v4/8d/61/d1/8d61d164-af8a-6cf5-13d1-eb063334436b/AppIcon-0-0-1x_U007epad-0-0-0-1-0-0-sRGB-0-0-85-220.png/512x512bb.jpg' 'Gemini catalog resolves the supplied official artwork rendition'

    $event = ConvertTo-ATNEvent -Agent codex -InputEvent @{hook_event_name='Stop';session_id='synthetic-session';turn_id='synthetic-turn';prompt='SYNTHETIC-FIXTURE';cwd='C:\private';output='PRIVATE-OUTPUT'}
    Assert-Equal $event.eventType 'stopped' 'Codex Stop is not project success'
    Assert-Equal $event.reason 'stopped' 'Codex Stop remains neutral'
    Assert-Equal ($event.ContainsKey('prompt')) $false 'adapter drops task content'
    Assert-Equal ($event.ContainsKey('cwd')) $false 'adapter drops working directory'
    Assert-Equal ($event.ContainsKey('output')) $false 'adapter drops output'
    Assert-Null (ConvertTo-ATNEvent -Agent codex -InputEvent @{hook_event_name='Stop';session_id='synthetic-session'}) 'Codex requires native turn identity'
    Assert-Null (ConvertTo-ATNEvent -Agent codex -InputEvent @{hook_event_name='SubagentStop';session_id='synthetic-session';turn_id='synthetic-turn'}) 'subagent events are ignored'
    Assert-Equal (ConvertTo-ATNEvent -Agent codex -InputEvent @{hook_event_name='UserPromptSubmit';session_id='s';turn_id='t'}).eventType 'started' 'Codex prompt starts a turn'
    Assert-Null (ConvertTo-ATNEvent -Agent codex -InputEvent @{hook_event_name='Unknown';session_id='s';turn_id='t'}) 'Codex unsupported event is ignored'

    $claude = ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='StopFailure';session_id='claude-session'}
    Assert-Equal $claude.eventType 'failed' 'Claude StopFailure maps to failed'
    Assert-Null $claude.nativeRunId 'Claude does not fabricate native run identity'
    Assert-Equal (ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='UserPromptSubmit';session_id='claude-session'}).eventType 'started' 'Claude prompt starts a turn'
    Assert-Equal (ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='Stop';session_id='claude-session'}).reason 'stopped' 'Claude Stop is neutral'
    Assert-Null (ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='Stop'}) 'Claude requires session identity'
    Assert-Null (ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='UserPromptSubmit';session_id='claude-session';agent_id='child-agent'}) 'Claude child prompt is ignored'
    Assert-Null (ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='StopFailure';session_id='claude-session';agent_id='child-agent'}) 'Claude child failure is ignored'
    Assert-Null (ConvertTo-ATNEvent -Agent claude-code -InputEvent @{hook_event_name='Unknown';session_id='claude-session'}) 'Claude unsupported event is ignored'

    $cursor = ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='stop';conversation_id='cursor-conversation';generation_id='cursor-generation';status='completed'}
    Assert-Equal $cursor.reason 'completed' 'Cursor completed stop carries completed reason'
    Assert-Equal (ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='stop';conversation_id='cursor-conversation';generation_id='cursor-generation';status='error'}).eventType 'failed' 'Cursor error maps to failed'
    Assert-Equal (ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='stop';conversation_id='cursor-conversation';generation_id='cursor-generation';status='aborted'}).eventType 'failed' 'Cursor aborted maps to failed'
    Assert-Null (ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='stop';conversation_id='cursor-conversation';generation_id='cursor-generation';status='unknown'}) 'Cursor unknown status stays unsupported'
    Assert-Equal (ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='beforeSubmitPrompt';conversation_id='cursor-conversation';generation_id='cursor-generation'}).eventType 'started' 'Cursor prompt starts generation'
    Assert-Null (ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='stop';conversation_id='cursor-conversation';status='completed'}) 'Cursor requires generation identity'
    Assert-Null (ConvertTo-ATNEvent -Agent cursor -InputEvent @{hook_event_name='Unknown';conversation_id='cursor-conversation';generation_id='cursor-generation'}) 'Cursor unsupported event is ignored'

    Assert-Equal (ConvertTo-ATNEvent -Agent gemini-cli -InputEvent @{hook_event_name='BeforeAgent';session_id='gemini-session'}).eventType 'started' 'Gemini BeforeAgent starts a turn'
    Assert-Equal (ConvertTo-ATNEvent -Agent gemini-cli -InputEvent @{hook_event_name='AfterAgent';session_id='gemini-session'}).reason 'stopped' 'Gemini AfterAgent remains neutral'
    Assert-Null (ConvertTo-ATNEvent -Agent gemini-cli -InputEvent @{hook_event_name='AfterModel';session_id='gemini-session'}) 'Gemini model responses do not end tasks'
    Assert-Null (ConvertTo-ATNEvent -Agent gemini-cli -InputEvent @{hook_event_name='BeforeAgent'}) 'Gemini requires session identity'
    Assert-Null (ConvertTo-ATNEvent -Agent gemini-cli -InputEvent @{hook_event_name='Unknown';session_id='gemini-session'}) 'Gemini unsupported event is ignored'

    $openCode = ConvertTo-ATNEvent -Agent opencode -InputEvent @{schemaVersion=1;event='failed';sessionId='open-session';runId='open-run';parentId='parent-run';reason='failed';prompt='SYNTHETIC-FIXTURE'}
    Assert-Equal $openCode.eventType 'failed' 'OpenCode bridge forwards supported failure'
    Assert-Equal $openCode.isChild $true 'OpenCode parent identity marks child event'
    Assert-Equal ($openCode.ContainsKey('prompt')) $false 'OpenCode bridge drops raw prompt'
    Assert-Equal (ConvertTo-ATNEvent -Agent opencode -InputEvent @{schemaVersion=1;event='started';sessionId='open-session';runId='open-run'}).eventType 'started' 'OpenCode bridge forwards started'
    Assert-Equal (ConvertTo-ATNEvent -Agent opencode -InputEvent @{schemaVersion=1;event='stopped';sessionId='open-session';runId='open-run'}).reason 'stopped' 'OpenCode bridge forwards stopped'
    Assert-Null (ConvertTo-ATNEvent -Agent opencode -InputEvent @{schemaVersion=2;event='started';sessionId='open-session';runId='open-run'}) 'OpenCode rejects unknown bridge version'
    Assert-Null (ConvertTo-ATNEvent -Agent opencode -InputEvent @{schemaVersion=1;event='started';sessionId='open-session'}) 'OpenCode requires native run identity'
    Assert-Null (ConvertTo-ATNEvent -Agent opencode -InputEvent @{schemaVersion=1;event='unknown';sessionId='open-session';runId='open-run'}) 'OpenCode unsupported event is ignored'

    Assert-Equal (ConvertTo-ATNEvent -Agent workbuddy -InputEvent @{hook_event_name='UserPromptSubmit';session_id='wb-session'}).eventType 'started' 'WorkBuddy experimental prompt starts a task'
    Assert-Equal (ConvertTo-ATNEvent -Agent workbuddy -InputEvent @{hook_event_name='Stop';session_id='wb-session'}).reason 'stopped' 'WorkBuddy Stop stays neutral'
    Assert-Null (ConvertTo-ATNEvent -Agent workbuddy -InputEvent @{hook_event_name='Stop'}) 'WorkBuddy requires session identity'
    Assert-Null (ConvertTo-ATNEvent -Agent workbuddy -InputEvent @{hook_event_name='Unknown';session_id='wb-session'}) 'unsupported events are ignored'

    Assert-Equal (Get-ATNHookOutput -Agent codex -EventName UserPromptSubmit) '{"continue":true}' 'Codex hook output is neutral'
    Assert-Equal (Get-ATNHookOutput -Agent cursor -EventName beforeSubmitPrompt) '{"continue":true}' 'Cursor prompt hook output is neutral'
    Assert-Equal (Get-ATNHookOutput -Agent cursor -EventName stop) '{}' 'Cursor stop hook has no decision'
    Assert-Equal (Get-ATNHookOutput -Agent gemini-cli -EventName AfterAgent) '{}' 'Gemini hook output has no decision'
    Assert-Equal (Get-ATNHookOutput -Agent claude-code -EventName Stop) '{}' 'Claude hook output has no decision'
    Assert-Throws { Get-ATNHookOutput -Agent 'unknown' -EventName Stop } 'unknown hook source is rejected'

    Write-Host 'PASS Test-ConfigAndEvents'
}
finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
