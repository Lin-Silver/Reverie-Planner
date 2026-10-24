# Reverie Planner

Reverie Planner is a privacy-first, local-first project planner for developers and AI-assisted teams. It combines a polished project dashboard with local SQLite storage, configurable AI providers, and a single-click Windows startup flow.

## Features

- Local-first project planning with SQLite persistence
- Dashboard for project health, task flow, and team momentum
- Task creation, status tracking, due dates, priorities, and tags
- AI provider configuration for OpenAI-compatible endpoints
- Local AI command agent for project/task actions with audit logging
- Built-in project and workspace settings management
- Windows startup script and graceful stop flow
- Optional static frontend served by the Go backend

## Project structure

- `src/backend` – Go HTTP API and local database layer
- `src/frontend` – Vite + React frontend
- `config` – app configuration and provider settings
- `data` – SQLite database and exported assets
- `runtime` – compiled Windows executable output
- `scripts` – optional helper scripts

## Quick start

1. Open a terminal in the project root.
2. Run `build.bat` to build the frontend and backend.
3. Run `start.bat` to launch the Windows desktop app.
4. Browse to `http://127.0.0.1:8765`.

## Configuration

The backend reads configuration from `config/app.json` and `config/providers.json` relative to the app root. These files are created automatically when the service starts if they do not already exist.

## Development

### Frontend

```powershell
cd src/frontend
npm install
npm run dev
```

### Backend

```powershell
cd src/backend
go run .
```

## Production build

```powershell
build.bat
```

This compiles the Go service into `runtime/ReveriePlanner.exe` and builds the frontend bundle for static serving.

## License

MIT
