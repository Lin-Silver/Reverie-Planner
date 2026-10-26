# PR #2 Database Verification

This file records concrete, SHA-specific evidence. A successful CI run proves the code tested at **that** commit only; it does not certify full Notion parity or local-machine usability.

## Repository and merge boundaries

- Pull request: https://github.com/Lin-Silver/Reverie-Planner/pull/2 — **Open; do not merge without independent review**.
- Feature branch: `feature/notion-database-system`.
- Merged `main` baseline: `f46242329822170af2f811a685ed7b8f39756ec0` (PR #1).
- Living requirements and remaining work: [DATABASE_COMPLETION_PLAN.md](DATABASE_COMPLETION_PLAN.md).

## Most recent independently verified green baseline

- Commit: `b0d34c68c4bc27cafa2389234d189819fc062814`.
- Windows GitHub Actions: [run 40266607916](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40266607916), **success**.
- Real Windows steps: Go backend tests, frontend build/type-check, lint (**17 warnings, 0 errors**), Chromium Playwright **27/27**, service start/stop, launcher ownership check and portable package.
- Fresh work after this baseline: populated screenshots of Table, Board, Calendar, List and Gallery, uploaded as a separate CI artifact if the screenshot test and upload step succeed. **Do not claim this artifact exists before its SHA-matched CI run completes**.

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
