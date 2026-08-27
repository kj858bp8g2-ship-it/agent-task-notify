Set-StrictMode -Version Latest

function Test-ATNHttpsUrl {
    param([string]$Value)
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    $uri = $null
    return [uri]::TryCreate($Value, [UriKind]::Absolute, [ref]$uri) -and $uri.Scheme -eq 'https' -and [string]::IsNullOrEmpty($uri.UserInfo)
}

function Get-ATNAgent {
    param([Parameter(Mandatory)][string]$Agent)
    $catalogPath = Join-Path $PSScriptRoot '..\assets\agent-icons.json'
    $record = @(Get-Content -Raw $catalogPath | ConvertFrom-Json -AsHashtable | Where-Object { $_.id -ceq $Agent })
    if ($record.Count -ne 1) { throw "Unknown agent source '$Agent'." }
    return $record[0]
}

function Get-ATNIcon {
    param([Parameter(Mandatory)][string]$Agent, [Parameter(Mandatory)][hashtable]$Settings)
    $record = Get-ATNAgent -Agent $Agent
    if ($Settings.ContainsKey('icons') -and $Settings.icons -is [System.Collections.IDictionary] -and $Settings.icons.Contains($Agent)) {
        $override = $Settings.icons[$Agent]
        if ($override -isnot [string] -or [string]::IsNullOrEmpty($override)) { return '' }
        if (-not (Test-ATNHttpsUrl -Value $override)) { return '' }
        return $override
    }
    if (-not (Test-ATNHttpsUrl -Value $record.iconUrl)) { return '' }
    return $record.iconUrl
}

function Get-ATNSettings {
    param([Parameter(Mandatory)][string]$DataDirectory)
    $defaultsPath = Join-Path $PSScriptRoot '..\config\defaults.json'
    $settings = Get-Content -Raw $defaultsPath | ConvertFrom-Json -AsHashtable
    $settingsPath = Join-Path $DataDirectory 'settings.json'
    if (-not (Test-Path -LiteralPath $settingsPath)) { return $settings }
    $raw = Get-Content -Raw -LiteralPath $settingsPath | ConvertFrom-Json -AsHashtable
    if ($raw -isnot [System.Collections.IDictionary]) { throw 'settings.json must contain an object.' }
    foreach ($key in $raw.Keys) {
        if (-not $settings.ContainsKey($key)) { throw "Unknown setting '$key'." }
        $settings[$key] = $raw[$key]
    }

    foreach ($key in @('minSeconds','longTaskSeconds','mediumRingSeconds','longRingSeconds','volume','ntfyPriority')) {
        if ($settings[$key] -isnot [long] -and $settings[$key] -isnot [int]) { throw "Setting '$key' must be an integer." }
    }
    foreach ($key in @('continuous','enableAttention')) {
        if ($settings[$key] -isnot [bool]) { throw "Setting '$key' must be a boolean." }
    }
    if ($settings.provider -isnot [string] -or $settings.provider -notin @('bark','ntfy')) { throw 'provider must be bark or ntfy.' }
    if ($settings.level -isnot [string] -or $settings.level -notin @('critical','active','timeSensitive','passive')) { throw 'Invalid Bark level.' }
    if ($settings.sound -isnot [string] -or [string]::IsNullOrWhiteSpace($settings.sound)) { throw 'sound must be a non-empty string.' }
    if ($settings.minSeconds -le 0 -or $settings.longTaskSeconds -le $settings.minSeconds) { throw 'Task thresholds must be positive and ordered.' }
    if ($settings.mediumRingSeconds -lt 30 -or $settings.mediumRingSeconds -gt 60 -or $settings.longRingSeconds -lt 30 -or $settings.longRingSeconds -gt 60) { throw 'Bark ring targets must be between 30 and 60 seconds.' }
    if ($settings.volume -lt 0 -or $settings.volume -gt 10) { throw 'volume must be between 0 and 10.' }
    if ($settings.ntfyPriority -lt 1 -or $settings.ntfyPriority -gt 5) { throw 'ntfyPriority must be between 1 and 5.' }
    if ($settings.icons -isnot [System.Collections.IDictionary]) { throw 'icons must be an object.' }
    foreach ($iconAgent in $settings.icons.Keys) {
        Get-ATNAgent -Agent $iconAgent | Out-Null
        if ($settings.icons[$iconAgent] -isnot [string]) { throw "Icon override '$iconAgent' must be a string." }
    }
    return $settings
}

Export-ModuleMember -Function Get-ATNSettings, Get-ATNAgent, Get-ATNIcon
