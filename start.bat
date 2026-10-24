@echo off
setlocal
set "APP_ROOT=%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%APP_ROOT%scripts\start.ps1"
set "RESULT=%ERRORLEVEL%"
if /i "%CI%"=="true" goto done
if /i "%REVERIE_NO_PAUSE%"=="1" goto done
echo.
pause
:done
exit /b %RESULT%
