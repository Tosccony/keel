# Installs the latest Keel release on Windows.
#   irm https://raw.githubusercontent.com/Tosccony/keel/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$url = 'https://github.com/Tosccony/keel/releases/latest/download/Keel-Setup-x64.exe'
$setup = Join-Path $env:TEMP 'Keel-Setup-x64.exe'

Write-Host 'Downloading Keel...'
Invoke-WebRequest -Uri $url -OutFile $setup -UseBasicParsing

Write-Host 'Running the installer (Windows will ask for permission)...'
Start-Process -FilePath $setup -ArgumentList '/SILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait
Remove-Item $setup -Force

Write-Host 'Keel is installed. Open it from the Start menu.'
