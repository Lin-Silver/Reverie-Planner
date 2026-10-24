$ErrorActionPreference = 'Stop'
$root = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot)).TrimEnd('\')
$serviceFile = Join-Path $root 'runtime\service.json'
$exe = Join-Path $root 'runtime\ReveriePlanner.exe'
$configFile = Join-Path $root 'config\app.json'
Add-Type -AssemblyName System.Net.Http

if (-not (Test-Path $serviceFile)) {
    Write-Output 'No Reverie Planner service metadata found; no process was stopped.'
    exit 0
}
$metadata = Get-Content -Raw $serviceFile | ConvertFrom-Json
$process = Get-CimInstance Win32_Process -Filter "ProcessId=$([int]$metadata.pid)" -ErrorAction SilentlyContinue
if (-not $process) {
    Write-Output 'The recorded process is no longer running; no other process was stopped.'
    exit 0
}
$sameRoot = $process.ExecutablePath -and
    ([System.IO.Path]::GetFullPath($process.ExecutablePath) -ieq [System.IO.Path]::GetFullPath($exe)) -and
    $process.CommandLine.Contains($root)
if (-not $sameRoot) {
    throw 'The recorded PID does not identify this Reverie Planner installation. Refusing to stop it.'
}
if ([string]::IsNullOrWhiteSpace([string]$metadata.token)) {
    throw 'The recorded service credential is missing. Refusing to stop the process.'
}

$config = Get-Content -Raw $configFile | ConvertFrom-Json
$uri = "http://127.0.0.1:$([int]$config.port)/api/shutdown"
$handler = [System.Net.Http.HttpClientHandler]::new()
$handler.UseProxy = $false
$client = [System.Net.Http.HttpClient]::new($handler)
$client.Timeout = [TimeSpan]::FromSeconds(5)
$request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, $uri)
$request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', [string]$metadata.token)
$response = $client.SendAsync($request).GetAwaiter().GetResult()
if (-not $response.IsSuccessStatusCode) {
    throw "Authenticated shutdown request failed with HTTP $([int]$response.StatusCode)."
}
$client.Dispose()
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    if (-not (Get-Process -Id ([int]$metadata.pid) -ErrorAction SilentlyContinue)) {
        Write-Output 'Reverie Planner stopped gracefully.'
        exit 0
    }
    Start-Sleep -Milliseconds 500
}
throw 'The authenticated shutdown request was accepted, but the process did not exit in time. No force-kill was attempted.'
