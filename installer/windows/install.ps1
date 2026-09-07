# Installs the QRX Agent as a Windows Service (scaffolding).
#
# STATUS: agent/platform.New() on windows returns Unsupported (see
# agent/platform/unsupported.go) -- Guardian's automatic restart and
# POST /api/v1/services/qrx/restart will fail loudly, not silently no-op,
# until a Windows Service Control Manager-backed ServiceManager is
# implemented. The Agent binary itself builds and runs fine on Windows
# (GOOS=windows go build ./cmd/agentd); only qrxd service management via
# the Agent is affected.
#
# This script uses sc.exe to register the AGENT ITSELF as a service (not
# qrxd) so it at least restarts automatically via the SCM's recovery
# options -- run as Administrator.
#
# Usage: .\install.ps1 -BinaryPath C:\path\to\agentd.exe

param(
    [Parameter(Mandatory = $true)]
    [string]$BinaryPath,
    [string]$ConfigPath = "$env:ProgramData\qrx-node-suite\agent.json",
    [string]$InstallDir = "$env:ProgramFiles\QRX Node Suite"
)

if (-not (Test-Path $BinaryPath)) {
    Write-Error "Binary not found: $BinaryPath (build it first: cd agent; go build -o agentd.exe .\cmd\agentd)"
    exit 1
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path $ConfigPath) | Out-Null
Copy-Item $BinaryPath (Join-Path $InstallDir "agentd.exe") -Force

if (-not (Test-Path $ConfigPath)) {
    @'
{
  "listen_addr": "127.0.0.1:8787",
  "data_dir": "C:\\ProgramData\\qrx-node-suite\\var",
  "adapter": { "name": "mock" }
}
'@ | Set-Content -Path $ConfigPath
    Write-Host "Wrote a Mock-mode default config to $ConfigPath -- edit it for a real deployment (see docs/configuration.md)."
}

$exePath = Join-Path $InstallDir "agentd.exe"
$binPath = "`"$exePath`" -config `"$ConfigPath`""

sc.exe create QRXAgent binPath= $binPath start= auto DisplayName= "QRX Node Suite Agent" | Out-Null
sc.exe failure QRXAgent reset= 86400 actions= restart/2000/restart/5000/restart/10000 | Out-Null

Write-Host "Installed. Start it with: Start-Service QRXAgent"
Write-Host "Then check:               Invoke-WebRequest http://127.0.0.1:8787/health"
