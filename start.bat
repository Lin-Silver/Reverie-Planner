@echo off
setlocal enableextensions enabledelayedexpansion

set APP_ROOT=%~dp0
set EXE_PATH=%APP_ROOT%runtime\ReveriePlanner.exe
set LOG_PATH=%APP_ROOT%logs\service.log
set PID_PATH=%APP_ROOT%logs\service.pid

if not exist "%APP_ROOT%logs" mkdir "%APP_ROOT%logs"

if exist "%PID_PATH%" (
  set /p EXISTING_PID=<"%PID_PATH%"
  if defined EXISTING_PID (
    tasklist /FI "PID eq %EXISTING_PID%" /FO CSV /NH 2>nul | findstr /I "ReveriePlanner.exe" >nul
    if not errorlevel 1 (
      echo Reverie Planner is already running.
      exit /b 0
    )
  )
)

if not exist "%EXE_PATH%" (
  echo Building Reverie Planner...
  call "%APP_ROOT%build.bat"
  if errorlevel 1 (
    echo. Failed to build Reverie Planner. Please inspect the output above.
    exit /b 1
  )
)

if not exist "%APP_ROOT%config" mkdir "%APP_ROOT%config"
if not exist "%APP_ROOT%data" mkdir "%APP_ROOT%data"

set EXISTING_PID=
for /f "tokens=5" %%P in ('netstat -ano -p tcp 2^>nul ^| findstr /R /C:":8765 .*LISTENING"') do (
  set EXISTING_PID=%%P
)
if defined EXISTING_PID if not "%EXISTING_PID%"=="0" (
  echo Port 8765 is already in use by PID %EXISTING_PID%.
  echo Please stop the other process or use a free port.
  exit /b 1
)

powershell -NoProfile -Command "$p = Start-Process -FilePath '%EXE_PATH%' -ArgumentList '--root','%APP_ROOT%' -RedirectStandardOutput '%LOG_PATH%' -RedirectStandardError '%LOG_PATH%.err' -PassThru -WindowStyle Hidden; $p.Id | Out-File -FilePath '%PID_PATH%' -Encoding ascii; Write-Output ('Started PID ' + $p.Id)" >nul
if errorlevel 1 (
  echo Failed to launch Reverie Planner.
  exit /b 1
)

set /a COUNTER=0
:waitloop
set /a COUNTER+=1
if %COUNTER% GTR 60 (
  echo. Reverie Planner did not become ready in time.
  exit /b 1
)

powershell -NoProfile -Command "try { $resp = Invoke-WebRequest -Uri 'http://127.0.0.1:8765/api/health' -UseBasicParsing -TimeoutSec 2; if ($resp.StatusCode -eq 200) { exit 0 } } catch {} ; exit 1" >nul 2>&1
if not errorlevel 1 goto started

timeout /t 1 /nobreak >nul
goto waitloop

:started
echo Reverie Planner ready at http://127.0.0.1:8765
start "" http://127.0.0.1:8765
exit /b 0
