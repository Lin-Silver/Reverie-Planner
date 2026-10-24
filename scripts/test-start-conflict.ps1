$ErrorActionPreference = 'Stop'
$workspace = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot)).TrimEnd('\')

if (-not ('StartupConflictHealthServer' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Net;
using System.Net.Sockets;
using System.Text;
using System.Threading;

public sealed class StartupConflictHealthServer : IDisposable
{
    private readonly TcpListener listener;
    private readonly Thread worker;

    public StartupConflictHealthServer(int port)
    {
        listener = new TcpListener(IPAddress.Loopback, port);
        listener.Start();
        worker = new Thread(Serve) { IsBackground = true };
        worker.Start();
    }

    private void Serve()
    {
        while (true)
        {
            TcpClient client;
            try { client = listener.AcceptTcpClient(); }
            catch (SocketException) { return; }
            using (client)
            using (NetworkStream stream = client.GetStream())
            {
                byte[] request = new byte[2048];
                try
                {
                    stream.Read(request, 0, request.Length);
                    byte[] body = Encoding.ASCII.GetBytes("{\"status\":\"ok\"}");
                    byte[] header = Encoding.ASCII.GetBytes(
                        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " +
                        body.Length + "\r\nConnection: close\r\n\r\n");
                    stream.Write(header, 0, header.Length);
                    stream.Write(body, 0, body.Length);
                    stream.Flush();
                }
                catch (IOException) { }
            }
        }
    }

    public void Dispose()
    {
        listener.Stop();
        worker.Join(1000);
    }
}
'@
}

$probe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$probe.Start()
$port = ([System.Net.IPEndPoint]$probe.LocalEndpoint).Port
$probe.Stop()

$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("ReverieStartConflict-" + [guid]::NewGuid().ToString('N'))
$server = $null
try {
    New-Item -ItemType Directory -Path (Join-Path $testRoot 'runtime') -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $testRoot 'scripts') -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $testRoot 'config') -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $testRoot 'src\frontend\dist') -Force | Out-Null
    Copy-Item (Join-Path $workspace 'runtime\ReveriePlanner.exe') (Join-Path $testRoot 'runtime\ReveriePlanner.exe')
    Copy-Item (Join-Path $PSScriptRoot 'start.ps1') (Join-Path $testRoot 'scripts\start.ps1')
    Set-Content -Path (Join-Path $testRoot 'src\frontend\dist\index.html') -Value '<div id="root"></div>'
    Set-Content -Path (Join-Path $testRoot 'config\app.json') -Value (@{
        appName = 'Reverie Planner startup test'
        version = 'test'
        port = $port
        host = '127.0.0.1'
    } | ConvertTo-Json)

    $server = [StartupConflictHealthServer]::new($port)

    $responderReady = $false
    for ($attempt = 0; $attempt -lt 40; $attempt++) {
        try {
            $handler = [System.Net.Http.HttpClientHandler]::new()
            $handler.UseProxy = $false
            $client = [System.Net.Http.HttpClient]::new($handler)
            $response = $client.GetAsync("http://127.0.0.1:$port/api/health").GetAwaiter().GetResult()
            $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            $response.Dispose()
            $client.Dispose()
            if ($response.StatusCode -eq [System.Net.HttpStatusCode]::OK -and $body -eq '{"status":"ok"}') {
                $responderReady = $true
                break
            }
        } catch {}
        Start-Sleep -Milliseconds 250
    }
    if (-not $responderReady) { throw 'The simulated healthy service did not become ready.' }

    $startScript = Join-Path $testRoot 'scripts\start.ps1'
    $output = ''
    try {
        $output = (& $startScript 2>&1 | Out-String)
    } catch {
        $output += $_.Exception.Message
    }
    if ($output -notmatch "Port $port is already in use by PID") {
        throw "The launcher did not clearly reject the unrelated service. Output: $output"
    }
    if ($output -match 'Reverie Planner ready|is already running') {
        throw "The launcher incorrectly claimed the unrelated healthy service. Output: $output"
    }
    if (Test-Path (Join-Path $testRoot 'runtime\service.json')) {
        throw 'The launcher wrote service metadata for a process it did not start.'
    }
    $server.Dispose()
    $server = $null

    Remove-Item -LiteralPath (Join-Path $testRoot 'src\frontend\dist\index.html')
    $output = ''
    try {
        $output = (& $startScript 2>&1 | Out-String)
    } catch {
        $output += $_.Exception.Message
    }
    if ($output -notmatch 'The frontend build is missing') {
        throw "The launcher did not reject a missing frontend build. Output: $output"
    }
    if (Test-Path (Join-Path $testRoot 'runtime\service.json')) {
        throw 'The launcher started a backend even though the frontend build was missing.'
    }
    Write-Output 'PASS: unrelated healthy services and missing frontend builds are rejected without false readiness.'
} finally {
    if ($server) { $server.Dispose() }
    if (Test-Path $testRoot) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force
    }
}
