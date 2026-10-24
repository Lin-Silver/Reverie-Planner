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
$appUrl = "http://127.0.0.1:$port/"
$healthUrl = "http://127.0.0.1:$port/api/health"
$httpHandler = [System.Net.Http.HttpClientHandler]::new()
$httpHandler.UseProxy = $false
$httpClient = [System.Net.Http.HttpClient]::new($httpHandler)
$httpClient.Timeout = [TimeSpan]::FromSeconds(2)

function Test-LocalHealth {
    $response = $null
    try {
        $response = $httpClient.GetAsync($healthUrl).GetAwaiter().GetResult()
        $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        return $response.IsSuccessStatusCode -and ((ConvertFrom-Json -InputObject $body).status -eq 'ok')
    } catch {
        return $false
    } finally {
        if ($response) { $response.Dispose() }
    }
}

function Test-FrontendReady {
    $response = $null
    try {
        $response = $httpClient.GetAsync("http://127.0.0.1:$port/").GetAwaiter().GetResult()
        $contentType = [string]$response.Content.Headers.ContentType.MediaType
        $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        return $response.IsSuccessStatusCode -and $contentType -eq 'text/html' -and $body.Contains('<div id="root">')
    } catch {
        return $false
    } finally {
        if ($response) { $response.Dispose() }
    }
}

function Test-PortOwnedByProcess([int]$processId) {
    $ownedListeners = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue |
        Where-Object { $_.OwningProcess -eq $processId })
    return $ownedListeners.Count -gt 0
}

if (Test-Path $serviceFile) {
    try {
        $metadata = Get-Content -Raw $serviceFile | ConvertFrom-Json
        $existing = Get-CimInstance Win32_Process -Filter "ProcessId=$([int]$metadata.pid)" -ErrorAction SilentlyContinue
        $sameRoot = $existing -and $existing.ExecutablePath -and
            ([System.IO.Path]::GetFullPath($existing.ExecutablePath) -ieq [System.IO.Path]::GetFullPath($exe)) -and
            $existing.CommandLine.Contains($root)
        if ($sameRoot) {
            if (-not [string]::IsNullOrWhiteSpace([string]$metadata.token) -and
                (Test-PortOwnedByProcess $existing.ProcessId) -and
                (Test-LocalHealth) -and (Test-FrontendReady)) {
                if ($env:REVERIE_NO_BROWSER -ne '1') {
                    Start-Process $appUrl
                }
                Write-Output 'Reverie Planner is already running.'
                exit 0
            }
            throw "The owned Reverie Planner process is not fully ready or its frontend is missing. Use stop.bat before restarting, then restore the complete app files."
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
if (-not (Test-Path (Join-Path $root 'src\frontend\dist\index.html'))) {
    throw 'The frontend build is missing (src\frontend\dist\index.html). Re-extract the complete Windows artifact, or run build.bat from the full source project.'
}

$listeners = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
if ($listeners.Count -gt 0) {
    $owners = ($listeners | Select-Object -ExpandProperty OwningProcess -Unique) -join ', '
    throw "Port $port is already in use by PID $owners. Stop the owning application or change config\app.json; this launcher will not claim another process as Reverie Planner."
}

$rootArg = $root.TrimEnd('\')
$process = Start-Process -FilePath $exe -ArgumentList @('--root', "`"$rootArg`"") -PassThru -NoNewWindow
$ready = $false
for ($attempt = 0; $attempt -lt 60; $attempt++) {
    if ($process.HasExited) {
        throw "Reverie Planner exited during startup with code $($process.ExitCode)."
    }
    if (Test-Path $serviceFile) {
        try {
            $startedMetadata = Get-Content -Raw $serviceFile | ConvertFrom-Json
        } catch {
            $startedMetadata = $null
        }
        if ($startedMetadata -and [int]$startedMetadata.pid -eq $process.Id -and
            (Test-PortOwnedByProcess $process.Id) -and
            -not [string]::IsNullOrWhiteSpace([string]$startedMetadata.token) -and
            (Test-LocalHealth) -and (Test-FrontendReady)) {
            $ready = $true
            break
        }
    }
    Start-Sleep -Seconds 1
}
if (-not $ready) {
    $shutdownRequested = $false
    if (-not $process.HasExited -and (Test-Path $serviceFile)) {
        try {
            $failedMetadata = Get-Content -Raw $serviceFile | ConvertFrom-Json
            if ([int]$failedMetadata.pid -eq $process.Id -and
                -not [string]::IsNullOrWhiteSpace([string]$failedMetadata.token)) {
                $request = [System.Net.Http.HttpRequestMessage]::new(
                    [System.Net.Http.HttpMethod]::Post,
                    "http://127.0.0.1:$port/api/shutdown"
                )
                $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new(
                    'Bearer',
                    [string]$failedMetadata.token
                )
                $response = $httpClient.SendAsync($request).GetAwaiter().GetResult()
                $shutdownRequested = $response.IsSuccessStatusCode
                $response.Dispose()
                $request.Dispose()
            }
        } catch {
            $shutdownRequested = $false
        }
    }
    if ($shutdownRequested) {
        $process.WaitForExit(10000) | Out-Null
    }
    if (-not $process.HasExited) {
        throw 'Reverie Planner did not become ready within 60 seconds. Graceful cleanup was unavailable; this launcher will not force-terminate the process.'
    }
    throw 'Reverie Planner did not become ready within 60 seconds; the launched process was stopped gracefully.'
}

Write-Output "Reverie Planner ready at $appUrl. This console owns the service lifetime."
if ($env:REVERIE_NO_BROWSER -ne '1') {
    Start-Process $appUrl
}
$process.WaitForExit()
exit $process.ExitCode
