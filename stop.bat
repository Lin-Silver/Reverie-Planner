@echo off
setlocal enableextensions
set ROOT=%~dp0
set PID_FILE=%ROOT%logs\service.pid

if exist "%PID_FILE%" (
  set /p PID=<"%PID_FILE%"
  if defined PID (
    taskkill /PID %PID% /F >nul 2>&1
    del "%PID_FILE%" >nul 2>&1
  )
)

for /f "skip=1 tokens=1 delims= " %%P in ('wmic process where "name='ReveriePlanner.exe'" get processid 2^>nul') do (
  taskkill /PID %%P /F >nul 2>&1
)

echo Reverie Planner stopped.
exit /b 0
