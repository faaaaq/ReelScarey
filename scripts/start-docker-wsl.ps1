param([string]$Distribution = 'Ubuntu-22.04')

$ErrorActionPreference = 'Stop'
if ($Distribution -notmatch '^[A-Za-z0-9._-]+$') {
    throw 'Choose the name of the WSL distribution containing Docker.'
}
$projectRoot = Split-Path -Parent $PSScriptRoot
$stateDirectory = Join-Path $projectRoot 'data'
$statePath = Join-Path $stateDirectory 'docker-wsl-host.json'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot 'compose.yaml'))) {
    throw 'Run this launcher from a ReelScarey checkout.'
}
if (Test-Path -LiteralPath $statePath) {
    try {
        $saved = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
        $existing = Get-Process -Id $saved.process_id -ErrorAction Stop
        $savedStart = [datetime]$saved.started_at
        if ($existing.ProcessName -eq 'wsl' -and
            $existing.StartTime.ToUniversalTime().Ticks -eq $savedStart.ToUniversalTime().Ticks -and
            $saved.distribution -eq $Distribution) {
            Write-Output "The Docker host launcher is already running (process $($existing.Id))."
            exit 0
        }
    } catch { # A stopped host can be launched again; never stop an unrelated PID.
    }
}
# Systemd services alone do not keep a WSL distribution alive. This foreground
# Linux process keeps the Docker host available after the Windows command exits.
$hostProcess = Start-Process -FilePath 'wsl.exe' -ArgumentList @(
    '--distribution', $Distribution, '--user', 'root',
    '--cd', ('"' + $projectRoot + '"'), '--exec', 'sh', '-c',
    '"docker compose up -d --no-build reelscarey && exec sleep infinity"'
) -WindowStyle Hidden -PassThru
if ($hostProcess.WaitForExit(3000)) {
    throw "The Docker host launcher exited with code $($hostProcess.ExitCode). Check Docker in $Distribution."
}
New-Item -ItemType Directory -Path $stateDirectory -Force | Out-Null
@{
    process_id = $hostProcess.Id
    started_at = $hostProcess.StartTime.ToUniversalTime().ToString('o')
    distribution = $Distribution
} | ConvertTo-Json | Set-Content -LiteralPath $statePath
Write-Output "Docker host is running in the background (process $($hostProcess.Id)). App: http://127.0.0.1:8787"
