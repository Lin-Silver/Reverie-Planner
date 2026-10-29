# AGENTS.md

## Project context

Reverie Planner aims to replace Notion for personal local-first notes, structured databases and project planning. Linux, Windows and macOS have native portable delivery workflows; see docs/PROJECT_DIRECTION.md for priorities and release boundaries. The product is split into a Go backend and a Vite/React frontend, with both packaged for local deployment under one directory.

## Operating principles

- Prefer local, privacy-first behavior and SQLite persistence.
- Keep configuration and runtime data under the project root rather than AppData or Documents.
- Make the app functional without an LLM provider; AI capabilities must be optional.
- Preserve a polished developer experience for Windows users with `start.bat` and `stop.bat`.
- Keep the frontend and backend buildable from the project root.
- Work on dev; main holds stable releases. Keep only these two long-lived branches.
- Use docs/DESIGN_SYSTEM.md and App.css tokens for new UI, including reduced motion/transparency.

## Build commands

```powershell
build.bat
start.bat
stop.bat
```

## Validation

- Run the frontend build with `src/frontend/npm run build`.
- Run the backend build with `go build -o runtime/ReveriePlanner.exe ./src/backend`.
- Validate the service health endpoint at `http://127.0.0.1:8765/api/health`.

## Notes

- Do not add cloud-only dependencies to core flows.
- Store user data, secrets, and backups inside the app workspace folder.
- Prefer simple, maintainable Go + SQLite architecture over large framework overhead.
