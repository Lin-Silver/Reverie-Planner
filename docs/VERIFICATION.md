# PR #2 Database Verification

This file records concrete, SHA-specific evidence. A successful CI run proves the code tested at **that** commit only; it does not certify full Notion parity or local-machine usability.

## Repository and merge boundaries

- Pull request: https://github.com/Lin-Silver/Reverie-Planner/pull/2 — **Open; do not merge without independent review**.
- Feature branch: `feature/notion-database-system`.
- Merged `main` baseline: `f46242329822170af2f811a685ed7b8f39756ec0` (PR #1).
- Living requirements and remaining work: [DATABASE_COMPLETION_PLAN.md](DATABASE_COMPLETION_PLAN.md).

## Most recent independently verified green baseline

- Code/test commit: `53a69d1bda63376e0e5f69d552b426029f05a7cf`.
- Windows GitHub Actions: [run 40659092333](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40659092333), **success on the exact SHA**.
- Windows steps: `go test ./...`, frontend Windows build/type-check, lint (17 warnings, 0 errors), Chromium Playwright **33/33**, service start/stop, launcher ownership check and portable package.
- Atomic initial record values: `db_record_initial_values.go` validates typed values, select membership, relation targets and file ownership in the record/page creation transaction; failed validation, SQLite INSERT or JSON response readback rolls back all rows. `db_record_initial_values_atomic_test.go` verifies valid text/select/relation values, invalid and read-only properties, fault rollback, retry and unchanged AI Provider bytes. The original fail-closed test continues to assert no partial writes for unknown properties. This is **not** evidence of complete typed property/schema lifecycle or reciprocal indexed relations.
- Calendar E2E polling: [run 40658460516](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40658460516) failed 32/33 because a polling callback dereferenced the optional `values` object before an asynchronous date save completed. The subsequent commit guards that read while retaining the exact persisted-date assertions; the full 33/33 suite passed on `53a69d1`.
- New SQLite recovery tests: `backup_snapshot_test.go` checks a real integrity-checked `VACUUM INTO` snapshot and isolation from subsequent live writes; `backup_restore_test.go` copies the snapshot to an independent temporary root, verifies restored data, reopens it through application initialization and confirms the source database and provider configuration were not changed.
- SQLite backup publication hardening: `957e876bd3771a1cef173f493de67177b5a0d248`, [Windows CI 40291519811](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40291519811), success. Pending snapshots are validated (SQLite integrity and foreign keys) before atomic publication; canceled or invalid snapshots are not left in the published backup directory, with source and provider preservation tests. This remains a SQLite-only backup.
- Database mutation request validation: `5eb8e633c1ef9237b3df3bab6323facb4f2a3d60`. Nine database/record/property/view/linked-view write endpoints reject trailing JSON values and requests larger than 4 MiB; 10 negative mutation regression cases check no data, version or provider changes. Verified by [Windows CI 40292259655](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40292259655), success (Go, build, lint 17 warnings, 28/28 Playwright, Windows launcher/package). Remaining API validation and query/performance requirements are open.
- Property schema ownership and title safeguards: `f4b17b7866b9bb4d561631be7699090a074004f5`, [Windows CI 40295378480](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40295378480), **success** (Go, frontend build/lint, 28/28 Playwright, Windows package). The Go regression suite rejects cross-database property update IDs, spoofed formula validation scope, blank property names, duplicate title properties and changes pointing to deleted relation targets without mutating existing property rows/versions, records or AI Provider settings. Legitimate property create/rename still passes. Full schema lifecycle/concurrency remains unverified.
- Earlier visual evidence: commit `01ebf1609477365588d0e71d92c7e2bfd264b9a6`, [Windows CI 40266939979](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40266939979), success, **28/28 Playwright**, five populated UI screenshots in artifact `Reverie-Database-UI-Evidence` (ID `12627116333`).
- **Scope limit:** isolated SQLite restoration is test coverage, not an exposed production restore feature. SQLite backup does not include separate attachment files or AI provider configuration; whole-workspace backup/recovery and historic migration stress testing remain incomplete.

## Representative regression evidence

The suite uses real SQLite-backed HTTP requests and browser interaction, not mocked local arrays:

1. Create records/properties; edit table cells and the attached BlockNote page; assert database persistence after reopening.
2. Move Board records between configured select groups; verify stored values and versions after reloading.
3. Move Calendar records between day cells, edit dates, schedule unscheduled records; verify persisted dates.
4. Create all five database views; save and restore view-local filters, sorting, grouping, date selection and table column layouts.
5. Evaluate typed nested AND/OR filters and numeric comparisons; reload saved filter definitions.
6. Use primary/secondary typed sort keys, ascending/descending order and stable null-last rules. Reopen, verify independent view settings.
7. Use Tab/Enter/Escape keyboard edits, avoid unchanged-write version increments; use Alt+Arrow cell navigation across rows and columns.
8. Create relation/formula properties through the visible UI; verify computed results in SQLite-backed records.
9. Restore trashed records and properties without losing values or attachments; reject access while a parent database is trashed.
10. Reproduce two-client stale-version conflict, preserve failed edits, require explicit retry or reload; prevent sidebar navigation during unresolved conflict.
11. Go tests cover migrations 8/9, invalid JSON and property types, stale version rollback, transaction failure injection on database/page rename, file ownership and deleted relation targets.

## Remaining gaps (not complete)

- Spreadsheet-like multi-cell selection, keyboard clipboard paste/copy, undo for batches and bulk editing with rollback.
- Database paging/virtualization and measured usability at 10,000+ records; current table loads all records.
- More complete bidirectional relation, person/file and rollup lifecycle, formula dependency semantics and deletion policy.
- Complete view arrangement, Gallery cover configuration, accessibility/responsive audits and linked-view independence.
- Backup/restore compatibility across every historic migration and full manual Windows user testing; CI only is insufficient.

The broader feature checklist and exact required exit criteria remain in [DATABASE_COMPLETION_PLAN.md](DATABASE_COMPLETION_PLAN.md). **PR #2 is not yet approved for merge.**
