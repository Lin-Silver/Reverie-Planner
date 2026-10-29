# Reverie Planner

Reverie Planner is a local-first project planner. Its Go service stores projects, tasks, AI sessions, approvals, and audit events in SQLite; the React interface talks to that service over a loopback-only HTTP API.

## Windows quick start

1. Download and extract the `Reverie-Planner-Windows` artifact from a successful GitHub Actions run.
2. Keep the extracted folder together and double-click `start.bat`.
3. Use `stop.bat` for a graceful stop. The start console stays attached to the service; closing that console ends its process lifetime.

The portable artifact includes the built frontend and backend and does not require Node.js, Go, Docker, or Python. Keep the folder writable so it can store `data/planner.db`, local configuration, and logs. Moving the whole folder preserves its relative data/configuration paths.

## AI providers and secrets

On first start, `config/providers.example.json` is copied to the private `config/providers.json` runtime configuration. Existing provider files containing the legacy `apiKey` field are migrated to encrypted storage. Provider API responses never return the key; editing a provider with an empty key retains the saved value.

Windows uses DPAPI tied to the current Windows user. Keep the configuration and data folder private. A DPAPI-protected provider file may not be usable under a different Windows account. Remote provider URLs must use HTTPS; loopback URLs may use HTTP for local services such as Ollama.

The AI Agent uses registered project/task tools rather than SQL. Write operations require approval; project archiving also requires typing `ARCHIVE`. Chat history and operation audit events are stored in the local SQLite database. Provider tests send an actual short chat completion request; automated tests use a simulated provider and do not verify a real external model.

## Development and tests

Development builds require Go 1.26 and Node.js 22:

```powershell
build.bat
```

Backend tests:

```powershell
go test ./...
```

Frontend checks and Playwright browser tests:

```powershell
cd src/frontend
npm ci
npm run lint
npm run build
npx playwright install chromium
npm run test:e2e
```

The browser test starts an isolated backend and disposable SQLite workspace; it exercises project/task changes and reload persistence without touching user data.

## Project structure

- `src/backend` — Go API, SQLite migrations, provider client, and AI tools
- `src/frontend` — React/Vite application and Playwright end-to-end tests
- `config` — public app settings and provider example; private provider config is ignored by Git
- `data` — SQLite database and consistent backups
- `runtime` — built Windows executable and private lifecycle metadata
- `scripts` — Windows startup and authenticated graceful-stop helpers
