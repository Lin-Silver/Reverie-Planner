@echo off
setlocal
set ROOT=%~dp0
set FRONTEND_DIR=%ROOT%src\frontend
set BACKEND_DIR=%ROOT%src\backend
set OUTPUT_DIR=%ROOT%runtime

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

cd /d "%FRONTEND_DIR%"
if exist package-lock.json (
  call npm ci
) else (
  echo package-lock.json was not found; installing dependencies and generating a lockfile...
  call npm install --no-audit --no-fund
)
if errorlevel 1 exit /b 1
call npm run build
if errorlevel 1 exit /b 1

cd /d "%BACKEND_DIR%"
go build -o "%OUTPUT_DIR%\ReveriePlanner.exe" .
if errorlevel 1 exit /b 1

echo Build complete.
exit /b 0
