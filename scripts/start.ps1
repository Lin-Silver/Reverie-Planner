$ErrorActionPreference = 'Stop'
$root = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot)).TrimEnd('\')
$exe = Join-Path $root 'runtime\ReveriePlanner.exe'
$serviceFile = Join-Path $root 'runtime\service.json'
$configFile = Join-Path $root 'config\app.json'
Add-Type -AssemblyName System.Net.Http

if (-not (Test-Path $configFile)) {
    throw "Missing app config: $configFile"
}
$config = Get-Content -Raw $configFile | ConvertFrom-Json
$port = [int]$config.port
$healthUrl = "http://127.0.0.1:$port/api/health"
$httpHandler = [System.Net.Http.HttpClientHandler]::new()
$httpHandler.UseProxy = $false
$httpClient = [System.Net.Http.HttpClient]::new($httpHandler)
$httpClient.Timeout = [TimeSpan]::FromSeconds(2)

function Test-LocalHealth {
    try {
        $response = $httpClient.GetAsync($healthUrl).GetAwaiter().GetResult()
        $healthy = $response.IsSuccessStatusCode
        $response.Dispose()
        return $healthy
    } catch {
        return $false
    }
}

if (Test-Path $serviceFile) {
    try {
        $metadata = Get-Content -Raw $serviceFile | ConvertFrom-Json
        $existing = Get-CimInstance Win32_Process -Filter "ProcessId=$([int]$metadata.pid)" -ErrorAction SilentlyContinue
        $sameRoot = $existing -and $existing.ExecutablePath -and
            ([System.IO.Path]::GetFullPath($existing.ExecutablePath) -ieq [System.IO.Path]::GetFullPath($exe)) -and
            $existing.CommandLine.Contains($root)
        if ($sameRoot) {
            if (Test-LocalHealth) {
                Start-Process $healthUrl
                Write-Output 'Reverie Planner is already running.'
                exit 0
            }
            throw "The owned Reverie Planner process exists but is not responding. Use stop.bat before restarting."
        }
        if (-not $existing) {
            Remove-Item -LiteralPath $serviceFile -Force
        }
    } catch {
        if ($_.Exception.Message -like 'The owned Reverie Planner process exists*') { throw }
        Write-Output "Ignoring invalid stale service metadata."
    }
}
if (-not (Test-Path $exe)) {
    throw 'The portable backend executable is missing. Download and extract the Windows CI artifact, or build the app with the development prerequisites.'
}

$rootArg = $root.TrimEnd('\')
$process = Start-Process -FilePath $exe -ArgumentList @('--root', "`"$rootArg`"") -PassThru -NoNewWindow
$ready = $false
for ($attempt = 0; $attempt -lt 60; $attempt++) {
    if ($process.HasExited) {
        throw "Reverie Planner exited during startup with code $($process.ExitCode)."
    }
    try {
        if (Test-LocalHealth) {
            $ready = $true
            break
        }
    } catch {
        Start-Sleep -Seconds 1
    }
    Start-Sleep -Seconds 1
}
if (-not $ready) {
    throw 'Reverie Planner did not become ready within 60 seconds.'
}

Write-Output "Reverie Planner ready at $healthUrl. This console owns the service lifetime."
if ($env:REVERIE_NO_BROWSER -ne '1') {
    Start-Process $healthUrl
}
$process.WaitForExit()
exit $process.ExitCode
