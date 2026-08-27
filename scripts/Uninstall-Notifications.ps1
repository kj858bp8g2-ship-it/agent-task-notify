#requires -Version 7.4
param([Parameter(Mandatory)][string]$Agent,[string]$DataDirectory=$(if ($env:ATN_DATA_DIRECTORY) { $env:ATN_DATA_DIRECTORY } else { Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentTaskNotify' }))
$ErrorActionPreference='Stop'
Import-Module "$PSScriptRoot/../src/Installation.psm1"
Uninstall-ATNIntegration -Agent $Agent -DataDirectory $DataDirectory
