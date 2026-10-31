# Reverie Planner V0.0.1

Reverie Planner aims to replace Notion for a personal, local-first workspace: nested documents, a block editor, structured databases, multiple views, search and project planning in one app. Core workflows must work offline without an AI provider or a cloud account. The Go service persists data in SQLite; the React interface uses a loopback-only HTTP API.

**Status: development preview, not complete Notion parity.** Pages, project/task planning and a substantial database subsystem are implemented. Complete Notion migration, custom templates, full property/view management, advanced relations, large-data performance and manual accessibility acceptance remain unfinished. Do not treat a green build as completion of the product.

See [product direction and release gates](docs/PROJECT_DIRECTION.md), [branch audit and fixes](docs/PROJECT_AUDIT.md), [feature matrix](docs/PRODUCT_PARITY_MATRIX.md) and [database completion checklist](docs/DATABASE_COMPLETION_PLAN.md).

## Linux and macOS quick start

Extract the portable archive for your OS/CPU to a writable folder. Run `./start.sh`, open the printed local address, and use `./stop.sh` to stop. Source builds: `./build.sh` with Go 1.26 and Node 22+. Runtime archives need neither toolchain nor Python. See [portable data and backup guide](docs/PORTABLE.md).

Settings → Local workspace shows exact data paths and creates consistent database snapshots. Stop the app before copying the entire workspace; snapshots alone do not include attachments or provider credentials. macOS builds are unsigned; platform acceptance is reported in the release PR checks.

## Windows quick start

1. Download and extract the `Reverie-Planner-Windows` artifact from a successful GitHub Actions run.
2. Keep the extracted folder together and double-click `start.bat`.
3. Use `stop.bat` for a graceful stop. The start console stays attached to the service; closing that console ends its process lifetime.

The portable artifact includes the built frontend and backend and does not require Node.js, Go, Docker, or Python. Keep the folder writable so it can store `data/planner.db`, local configuration, and logs. Moving the whole folder preserves its relative data/configuration paths.

## Development branch: transfer and recovery

The published V0.0.1 packages are the initial baseline. Newer `dev` builds add
built-in templates, favorites/recent pages, light/dark workspace materials,
page history/trash recovery and verified workspace archives with isolated restore.
Settings → Local workspace also contains bounded Markdown ZIP imports, typed CSV
imports, document-tree packages and database packages. Core transfers work without
an AI account. Each input is previewed; imports create independent copies rather
than overwriting matching titles.

Database packages preserve the supported scalar fields, record writing and
independent view settings. Attachments, relations, formulas, select metadata and
unknown settings require a workspace archive. These subsets do not establish
complete Notion compatibility. See [database package limits](docs/DATABASE_PACKAGES.md),
[Markdown packages](docs/MARKDOWN_PACKAGES.md), [CSV types](docs/CSV_IMPORT.md) and
[development acceptance](docs/DEVELOPMENT_PLAN.md) for exact scope and CI evidence.
Do not open newer-schema workspaces with the older release binary.

## Development branch: Glass v2

New dev builds default to a macOS-inspired Glass interface with a bundled original
pink/white sakura wallpaper. Toolbar → Appearance (also in Settings) switches to
Classic, chooses Sakura/Mist and adjusts material clarity. Navigation is lighter;
writing and tables use thicker reading materials. Dark mode, reduced motion and
solid-material system preferences are supported. Appearance choices travel with
the workspace archive. This is a cross-platform web implementation.

See [Glass design and wallpaper provenance](docs/GLASS_UI_V2.md),
[feature audit](docs/FEATURE_AUDIT_2026-10-30.md) and
[exact source acceptance](docs/DEVELOPMENT_PLAN.md). The audit separates unfinished
features from implemented workflows and pending manual release checks.

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
