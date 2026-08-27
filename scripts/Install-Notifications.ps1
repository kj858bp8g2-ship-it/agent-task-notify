#requires -Version 7.4
param([Parameter(Mandatory)][string]$Agent,[string]$ConfigPath,[string]$DataDirectory=$(if ($env:ATN_DATA_DIRECTORY) { $env:ATN_DATA_DIRECTORY } else { Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentTaskNotify' }),[switch]$AcceptExperimental)
$ErrorActionPreference='Stop'
Import-Module "$PSScriptRoot/../src/Installation.psm1"
Install-ATNIntegration -Agent $Agent -ConfigPath $ConfigPath -DataDirectory $DataDirectory -PackageRoot (Split-Path $PSScriptRoot) -AcceptExperimental:$AcceptExperimental
