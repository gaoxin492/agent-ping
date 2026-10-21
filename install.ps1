# agent-ping installer for Windows (PowerShell):
#   irm https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$repo = if ($env:AGENT_PING_REPO) { $env:AGENT_PING_REPO } else { 'gaoxin492/agent-ping' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$dir  = Join-Path $env:LOCALAPPDATA 'agent-ping'
$exe  = Join-Path $dir 'agent-ping.exe'
$url  = "https://github.com/$repo/releases/latest/download/agent-ping_windows_$arch.exe"

New-Item -ItemType Directory -Force -Path $dir | Out-Null
Write-Host "Downloading $url"
Invoke-WebRequest -Uri $url -OutFile "$exe.tmp" -UseBasicParsing
Move-Item -Force "$exe.tmp" $exe
Write-Host "Installed to $exe"

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $dir)) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir"), 'User')
    $env:Path += ";$dir"
    Write-Host "Added $dir to your PATH (new terminals will see it)."
}

& $exe _postinstall
