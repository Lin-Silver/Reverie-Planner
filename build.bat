@echo off
setlocal
set "RESULT=0"
set ROOT=%~dp0
set FRONTEND_DIR=%ROOT%src\frontend
set BACKEND_DIR=%ROOT%src\backend
set OUTPUT_DIR=%ROOT%runtime

if not exist "%FRONTEND_DIR%\package.json" goto missing_source
if not exist "%BACKEND_DIR%\main.go" goto missing_source
if not exist "%ROOT%go.mod" goto missing_source

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

cd /d "%FRONTEND_DIR%"
if exist package-lock.json (
  call npm ci
) else (
  echo package-lock.json was not found; installing dependencies and generating a lockfile...
  call npm install --no-audit --no-fund
)
if errorlevel 1 goto command_failed
call npm run build
if errorlevel 1 goto command_failed

cd /d "%BACKEND_DIR%"
go build -o "%OUTPUT_DIR%\ReveriePlanner.exe" .
if errorlevel 1 goto command_failed

echo Build complete.
goto finish

:missing_source
echo.
echo Cannot build: source files are missing from "%ROOT%".
echo This portable app folder already contains a built executable. Run start.bat to use it.
echo To rebuild from source, download or clone the full source project, including:
echo   go.mod
echo   src\backend\*.go
echo   src\frontend\package.json
set "RESULT=2"
goto finish

:command_failed
set "RESULT=%ERRORLEVEL%"

:finish
if /i "%CI%"=="true" goto done
if /i "%REVERIE_NO_PAUSE%"=="1" goto done
echo.
pause

:done
exit /b %RESULT%
