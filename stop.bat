@echo off
setlocal
set "APP_ROOT=%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%APP_ROOT%scripts\stop.ps1"
exit /b %errorlevel%
