# Reverie Planner — Database Completion Plan

**Mode:** execution plan / living checklist. **Branch:** `feature/notion-database-system`, **PR:** [#2](https://github.com/Lin-Silver/Reverie-Planner/pull/2). **Baseline:** `966e478b53298d65dd6f01227c10e7aff375b696` (Windows Actions `40257255601`, 19/19 E2E). **Merge policy:** no direct writes to `main`, do not merge without independent approval.

## Objective and definition of done

Ship a trustworthy, locally stored, long-lived Notion-style database subsystem with reliable records, pages, linked views, five functional layouts, typed properties, relations, formulas/rollups, editable presentation, backups and proven conflict/lifecycle behavior. Do not claim perfect Notion/Plane parity, cloud sync or real-time multi-user collaboration. Scope includes working APIs, UI, migration, documentation and tests; a rendered mock or CI-only happy path does not satisfy acceptance. Protect existing pages, project/task entities, AI providers and their keys.

**Global exit conditions:** every checkbox below is checked with code, regression evidence and a SHA-matched successful Windows CI; no skipped or weakened previous assertions; data survives restart, SQLite migration is forward-only, errors never silently discard drafts; PR stays Open pending independent review.

## Phase 0 — Baseline, invariants and safety

- [x] Verify PR #2 Open, clean mergeability and last green Windows CI (19/19 Playwright at `966e478`).
- [x] Review migration 7 / legacy linked-column handling, DB record/page coupling, five UI views and existing E2E coverage.
- [ ] Capture before/after compatibility tests for v6 and v7 user databases; preserve user content, attachments, providers and project/task tables.
- [x] Move lazy `ALTER TABLE pages` column setup to transactional numbered migration with safe version detection and rollback.
- [ ] Normalize API JSON decode/validation, SQLite error propagation, WAL/foreign key checks, stable IDs and index/query plans.
- [ ] Ensure backup and restore test uses a real SQLite copy and does not delete the original.

## Phase 1 — Data model and typed properties

- [ ] Implement typed property definitions and editors for text/title, number, boolean, select/multi-select, date/time, URL, email, person, relation, created/edited timestamps and file references. Read-only computed/system fields must not accept writes.
- [ ] Atomic schema edits: rename/reorder/hide/option edit/type migration or deliberate refusal, delete/restore safely, nonempty title invariant and optimistic versions.
- [ ] Typed validation: option membership, time zones/invalid dates, valid URL/email, file reference ownership, relational record existence and cycle-safe formula dependencies.
- [ ] Indexed relation storage using stable record IDs and explicit deletion semantics; reciprocal links and rollback on error.
- [x] Deterministic safe formula language (no eval/JS/SQL injection; null/error handling, dependency cycle detection) plus typed rollup aggregations.
- [ ] Persist/reconcile file attachments through the application's existing upload policy; no unsafe filesystem paths.

## Phase 2 — Views and interaction

- [x] Table/Board/Calendar/List/Gallery rendering and basic shared-record updates, date editing and navigation; E2E assertions for all five.
- [x] Persist one view's filter text, sort, Board group and Calendar field; verify reload.
- [x] Replace plain text filter with a typed nested AND/OR filter AST; validation + deterministic predicates, null/date/number/select semantics; view-local immutable config.
- [x] Implement multi-key typed sorting, hide/show/reorder/resize columns, Tab/Enter/Escape and Alt+Arrow cell navigation, and persisted per-view layouts.
- [ ] Add spreadsheet-style multi-cell clipboard copy/paste, range selection and bulk-edit confirmation with safe rollback.
- [ ] Improve Board empty and unexpected group behavior, keyboard alternatives, drag failure rollback, option management and conflict UX.
- [x] Calendar date drag/move/date picker, month navigation/timezone correctness, unspecified-date tray and reliable changes.
- [ ] Gallery cover and property selection, List record actions, quick-open and responsive/screen reader controls.
- [ ] Manage views via UI (rename, duplicate, delete with safe defaults, reorder, inline/linked views); preserve linked-view-specific settings.

## Phase 3 — Pages, conflict and lifecycle

- [x] Record pages open BlockNote, persist content; linked database page shares records; basic explicit Retry/Reload and sidebar navigation guard tested.
- [ ] Harden page title/content version coordination: record title edits must not silently conflict with BlockNote autosave; page editor/reload retains correct version snapshot.
- [ ] Explicit 409 conflict UI for property/version/linked-view config with draft preservation; concurrency tests across two browser contexts and rapid edit/navigation.
- [ ] Soft-delete/restore databases, properties, records, views and associated pages consistently; keep relational integrity and prevent orphan pages/links; document retention/cleanup.
- [ ] Backup/migration + restart recovery and transaction fault-injection tests.

## Phase 4 — Engineering and performance

- [ ] Split `DatabaseView.tsx` into typed API/model/view components, remove accidental `any`, centralize request status, loading, errors, conflict retries.
- [ ] Efficient pagination/virtualization for large tables and board groups, proper SQLite indexes, measurable benchmark with 10k+ records.
- [ ] Theme, WCAG keyboard/focus/ARIA basics, small-window responsive UI, no control hidden by layout.
- [ ] No third-party cloud requirement; no changes to AI Agent tools/config without separately reviewed compatibility.

## Phase 5 — Verification and delivery

- [ ] Go tests for fresh and migrated SQLite, properties/relations/formulas/rollups, filter semantics, rollback, delete/restore and consistency.
- [ ] New Playwright tests for filters, properties, relation editing, formulas, rollups, view layout, Calendar date changes, Linked View config, backups, 409 and keyboard access. Keep existing 19 E2E.
- [ ] Confirm `go test ./...`, `npm run build` (tsc), `npm run lint`, full Playwright, Windows start/stop and package using GitHub Actions. For each final claim capture exact head SHA and CI run.
- [ ] Update `docs/PRODUCT_PARITY_MATRIX.md` and `docs/VERIFICATION.md` to distinguish verified, partial and unavailable behavior; add migration/backup/API design.
- [ ] Independent review: inspect Git diff, schema and actual browser behavior, check no unexpected dependency/config/secret/user-data edits; **leave PR #2 Open**.

## Execution and progress discipline

Prefer correctness and irreversible-data safety before additional UI features. Implement vertical slices with a schema/API/UI/Go-test/Playwright-test chain. When a Windows CI fails, inspect its actual step/log and fix code/tests before moving on. Mark `[x]` only with reproducible proof; add a brief evidence entry containing commit and CI URL below; never weaken an assertion to make a run green. If local machine access is unavailable, explicitly say Windows tests are CI-only.

## Evidence log

- Baseline: `966e478b53298d65dd6f01227c10e7aff375b696`, [Windows CI 40257255601](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40257255601), Go tests, Windows package, 19/19 Playwright.

- Verified newer baseline: `5023e339b5a69711c57a2cc349ea3585f34e7dde`, [Windows CI 40263677658](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40263677658), success: backend Go regressions, TypeScript/build, Windows launcher/package, **25/25 Playwright** including calendar day drag/drop, typed AND/OR filters, UI relation+formula editor, attachment upload and soft-delete restore.
- In-progress after this green baseline: atomic database/page rename, preventing record access when parent database is trashed, rejecting tombstoned relation targets, and their Go regressions. These are **not yet** covered by the green CI above; verify against the latest PR head before marking further checkboxes.
- Verified table interaction milestone: `b0d34c68c4bc27cafa2389234d189819fc062814`, [Windows CI 40266607916](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40266607916), success: **27/27 Playwright**, including type-aware primary/secondary sort with null stability, per-view saved settings, column layout, Enter/Escape/Tab/Alt+Arrow navigation, and no redundant record-version updates.
- Visual evidence verified: `01ebf1609477365588d0e71d92c7e2bfd264b9a6`, [Windows CI 40266939979](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40266939979), success: **28/28 Playwright** and artifact `Reverie-Database-UI-Evidence` (five screenshots, artifact ID `12627116333`), plus the Windows package. Screenshots contain populated real test records; they are not proof of manual local-machine acceptance.
