Set-StrictMode -Version Latest

function Get-ATNKey {
    param([Parameter(Mandatory)][AllowEmptyCollection()][string[]]$Parts)
    $json = ConvertTo-Json -InputObject @($Parts) -Compress
    return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($json))).ToLowerInvariant()
}

# System.Text.Json avoids ConvertFrom-Json's pre-7.5 automatic date conversion.
function ConvertFrom-ATNElement {
    param([System.Text.Json.JsonElement]$Element)
    switch ($Element.ValueKind.ToString()) {
        Object { $value = @{}; foreach ($property in $Element.EnumerateObject()) { $value[$property.Name] = ConvertFrom-ATNElement $property.Value }; return $value }
        Array { $value = @(); foreach ($item in $Element.EnumerateArray()) { $value += ,(ConvertFrom-ATNElement $item) }; return ,$value }
        String { return $Element.GetString() }
        Number { $number = 0L; if ($Element.TryGetInt64([ref]$number)) { return $number }; return $Element.GetDouble() }
        True { return $true }
        False { return $false }
        Null { return $null }
    }
}

function Read-ATNJson {
    param([Parameter(Mandatory)][string]$Path)
    if (-not [IO.File]::Exists($Path)) { return $null }
    $document = [System.Text.Json.JsonDocument]::Parse([IO.File]::ReadAllText($Path, [Text.UTF8Encoding]::new($false, $true)))
    try {
        if ($document.RootElement.ValueKind -ne [System.Text.Json.JsonValueKind]::Object) { throw 'Stored JSON must be an object.' }
        return ConvertFrom-ATNElement $document.RootElement
    } finally { $document.Dispose() }
}

function Write-ATNJson {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][hashtable]$Value)
    $absolute = [IO.Path]::GetFullPath($Path)
    $directory = [IO.Path]::GetDirectoryName($absolute)
    [IO.Directory]::CreateDirectory($directory) | Out-Null
    $temporary = Join-Path $directory ([guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllText($temporary, (ConvertTo-Json -InputObject $Value -Depth 64 -Compress), [Text.UTF8Encoding]::new($false))
        [IO.File]::Move($temporary, $absolute, $true)
    } finally { if ([IO.File]::Exists($temporary)) { [IO.File]::Delete($temporary) } }
}

function Enter-ATNLock {
    param([Parameter(Mandatory)][string]$Path, [ValidateRange(0,60000)][int]$WaitMilliseconds = 0)
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($Path))) | Out-Null
    $clock = [Diagnostics.Stopwatch]::StartNew()
    do {
        try { return [IO.File]::Open($Path, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None) }
        catch [IO.IOException] {
            $remaining=$WaitMilliseconds - $clock.ElapsedMilliseconds
            if ($remaining -le 0) { return $null }
            Start-Sleep -Milliseconds ([Math]::Min(20, $remaining))
        }
    } while ($true)
}

function Test-ATNCredential {
    param([string]$Provider, [hashtable]$Credential)
    try {
        if ($Provider -cnotin @('bark','ntfy') -or $Credential.endpoint -isnot [string]) { throw 'invalid' }
        $raw = $Credential.endpoint
        $uri = [uri]$raw
        if (-not $uri.IsAbsoluteUri -or ($uri.Scheme -ne 'https' -and -not ($uri.Scheme -eq 'http' -and $uri.IsLoopback))) { throw 'invalid' }
        if ($uri.UserInfo -or $uri.Query -or $uri.Fragment -or $raw -match '[\\\s]' -or $raw -match '%|/\.{1,2}(/|$)') { throw 'invalid' }
        $segments = $uri.AbsolutePath.Substring(1).Split('/')
        if (@($segments | Where-Object { $_ -notmatch '^[A-Za-z0-9_-]+$' }).Count) { throw 'invalid' }
        if ($Provider -eq 'ntfy') {
            if ($segments.Count -ne 1 -or $Credential.token -isnot [string] -or $Credential.allowUnauthenticated -isnot [bool]) { throw 'invalid' }
            if ($Credential.token -match '[\r\n]' -or ([string]::IsNullOrWhiteSpace($Credential.token) -and -not $Credential.allowUnauthenticated)) { throw 'invalid' }
        }
        return $uri
    } catch { throw [ArgumentException]::new('Invalid provider credential.') }
}

function Set-ATNCredential {
    param([Parameter(Mandatory)][string]$DataDirectory, [Parameter(Mandatory)][string]$Provider, [Parameter(Mandatory)][hashtable]$Credential)
    try {
        Test-ATNCredential $Provider $Credential | Out-Null
        $plain = [Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -InputObject $Credential -Compress))
        try {
            $cipher = [Security.Cryptography.ProtectedData]::Protect($plain, $null, [Security.Cryptography.DataProtectionScope]::CurrentUser)
            $verified = [Security.Cryptography.ProtectedData]::Unprotect($cipher, $null, [Security.Cryptography.DataProtectionScope]::CurrentUser)
            try { if ([Convert]::ToBase64String($plain) -cne [Convert]::ToBase64String($verified)) { throw 'verification' } } finally { [Array]::Clear($verified) }
            Write-ATNJson (Join-Path $DataDirectory "credentials-$Provider.json") @{protected=[Convert]::ToBase64String($cipher)}
        } finally { [Array]::Clear($plain) }
    } catch { throw [InvalidOperationException]::new('Credential could not be saved.') }
}

function Get-ATNCredential {
    param([Parameter(Mandatory)][string]$DataDirectory, [Parameter(Mandatory)][string]$Provider)
    try {
        if ($Provider -cnotin @('bark','ntfy')) { throw 'invalid' }
        $stored = Read-ATNJson (Join-Path $DataDirectory "credentials-$Provider.json")
        $plain = [Security.Cryptography.ProtectedData]::Unprotect([Convert]::FromBase64String($stored.protected), $null, [Security.Cryptography.DataProtectionScope]::CurrentUser)
        try { $credential = [Text.Encoding]::UTF8.GetString($plain) | ConvertFrom-Json -AsHashtable } finally { [Array]::Clear($plain) }
        Test-ATNCredential $Provider $credential | Out-Null
        return $credential
    } catch { throw [InvalidOperationException]::new('Credential could not be read.') }
}

Export-ModuleMember -Function Get-ATNKey, Write-ATNJson, Read-ATNJson, Enter-ATNLock, Set-ATNCredential, Get-ATNCredential, Test-ATNCredential
