# Upstream Selection Report

Date: 2026-10-24
Repository: Reverie Planner (branch `refactor/ui-vertical-slice`)
Scope: Evaluate Plane CE and Vikunja as bases for the next product iteration without modifying `main`.

## Candidate validation

### 1. Plane CE (`makeplane/plane`)

- License: `AGPL-3.0` (`LICENSE.txt`)
- Stack: pnpm/turbo monorepo; frontend `apps/web` is React Router + React 19 + TypeScript; backend `apps/api` is Python 3.12 + Django 5.2 + DRF + PostgreSQL + Redis + Celery + channels.
- Build evidence:
  - `pnpm install` completed in the isolated clone.
  - `pnpm build` completed: `Tasks: 15 successful, 15 total`, including `apps/web`.
- Runtime evidence:
  - Full runtime was not started locally because the API requires PostgreSQL, Redis, and Celery in addition to Python dependencies. No Docker is available in this environment.
  - Therefore Plane CE is buildable, but not locally runnable as a single Windows process in this environment.
- Strengths: modern, polished project-management UI; rich domain features (issues, cycles, modules, views, editors); mature frontend component system.
- Risks for Reverie Planner: heavy server stack conflicts with local-first/Windows/portable goals; replacing the Go backend would discard the existing AI Agent, provider-secret handling, and SQLite persistence; long-term sync cost is high.

### 2. Vikunja (`go-vikunja/vikunja`)

- License: `AGPL-3.0-or-later` (`LICENSE`)
- Stack: Go backend (`go.mod`, `pkg/...`) + Vue 3/Vite frontend (`frontend/`), SQLite by default.
- Build evidence:
  - `pnpm install` and `pnpm build` completed in `frontend/`.
  - `go build -o vikunja-eval.exe .` completed; binary size 160,130,175 bytes.
- Runtime evidence:
  - Started with `VIKUNJA_SERVICE_PORT=3456`, `VIKUNJA_SERVICE_PUBLICURL=http://127.0.0.1:3456/`, SQLite database path.
  - `GET /` returned `200 OK`.
  - `GET /api/v1/info` returned valid JSON with version `dev` and frontend URL.
- Strengths: Go + SQLite matches the existing local-first architecture; single binary; mature task/project features; importers and CalDAV support.
- Risks: frontend is Vue 3 rather than the existing React frontend; integrating the existing AI Agent and provider UI requires substantial frontend work; maintaining a full fork increases upstream sync cost.

## Decision

Selected option: **Keep the existing Go backend + SQLite + AI Agent, and reuse legitimate open-source components/patterns instead of forking either upstream project.**

Rationale:

1. The existing backend already provides the differentiating capabilities that must be preserved: local SQLite storage, Windows DPAPI-backed provider secrets, OpenAI-compatible provider configuration, streaming agent chat, approvals, audit events, and registered project/task tools.
2. Plane CE's backend stack (Python + PostgreSQL + Redis + Celery) is incompatible with the stated local-first, portable, no-cloud-dependency goals and would force a large backend rewrite.
3. Vikunja is a strong Go/SQLite reference and was validated as buildable and runnable, but a full fork would replace the React frontend and require re-integrating all AI features, with higher ongoing merge cost.
4. A hybrid approach preserves user data and existing code while allowing selective reuse of Plane's UI/UX patterns and Vikunja's Go/SQLite implementation details under AGPL compliance.

## First vertical slice implemented

Branch: `refactor/ui-vertical-slice`

Implemented and verified:

- Collapsible sidebar with persisted state (`localStorage` key `reverie.sidebar.collapsed`).
- Sidebar navigation remains keyboard/assistive-tech accessible when collapsed.
- Escape closes the task editor dialog.
- Improved task drag-and-drop feedback with drag-over column highlighting.
- UI tests for sidebar collapse, dialog escape, and drag-and-drop.
- Existing project/task CRUD and persistence tests still pass.

Validation evidence:

- Frontend build: `npm run build` succeeded.
- Frontend lint: `npm run lint` passed with one pre-existing warning.
- Backend tests: `go test ./...` passed.
- Backend build: `go build -o runtime/ReveriePlanner.exe ./src/backend` succeeded.
- Playwright: 3 tests passed (`workspace.spec.ts` + 2 new UI interaction tests).

## Constraints honored

- No changes were made to `main`.
- No upstream commercial/proprietary code was copied.
- User data paths and existing backend behavior remain intact.
- Upstream projects were evaluated only in isolated directories under `G:\Reverie\upstream-eval`.
