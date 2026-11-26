# Reverie Planner V1.0.0 (release candidate)

A portable, local-first workspace for notes, structured databases and project planning. Core workflows run offline without an AI provider or cloud account. The Go service persists to SQLite; the React UI uses a loopback-only HTTP API.

## Included in 1.0.0
- Nested documents and BlockNote editing with conflict recovery.
- Table, Board, Calendar, List and Gallery database views; typed fields, nested AND/OR filters, multi-key sort, persisted per-view layout, and reviewed atomic TSV editing.
- Favorites and recently viewed pages in the sidebar.
- Templates: save a page as a template and create pages from it.
- Markdown import/export for pages (self-contained block converters).
- Server-side record pagination (`limit`/`offset` with `total`) and a reproducible 10,000-record benchmark (see docs/BENCHMARK.md).
- Local projects and tasks; optional AI providers with encrypted secrets.
- Visible workspace storage paths and consistent SQLite snapshot action.
- Native Linux, Windows and macOS build, browser, authenticated shutdown and restart checks. Runtime packages need no Go, Node, Python or cloud account.

## Data ownership
The whole extracted folder is the workspace. Stop before moving/copying it. SQLite snapshots alone exclude attachments and credentials. See docs/PORTABLE.md. Never point an older binary at a newer database schema.

## Known limitations (release gates still open)
- Real historical v6/v7 production backup migration acceptance is not yet verified (synthetic fixtures only).
- SHA-matched Windows CI (Go tests, build, lint, Playwright, launcher start/stop, packaging) must be recorded on the release commit.
- macOS builds are unsigned/not notarized; manual platform and accessibility acceptance is pending.
- Large-table UI virtualization, advanced reciprocal relation lifecycle, and full property/view management polish remain in progress.
- Existing lint warnings and the large lazy-loaded editor chunk remain.

Do not infer completeness from the version number. This is a release candidate; the items above are tracked before a final 1.0.0 sign-off.