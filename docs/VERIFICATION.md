# Verification

This document separates **verified results** from **features not yet verified or delivered**.

## CI baseline

- Merged main baseline: `f46242329822170af2f811a685ed7b8f39756ec0` (PR #1).
- PR #2: https://github.com/Lin-Silver/Reverie-Planner/pull/2 — keep open until independently approved.
- Feature branch: `feature/notion-database-system`.
- Completed Windows CI for interactive database workflows: https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40255425287 — **success**, HEAD `ab8941c8e3b396aea71334a10a7f094b5b008c9c`.
- That run includes Go backend tests, frontend production build, TypeScript checks, lint (15 warnings, zero errors), Playwright **17/17**, launcher checks, graceful shutdown and Windows portable package upload.
- Property schema hardening and its new Go regressions were committed after the above run; use the latest SHA-matched PR workflow as their acceptance evidence. Do **not** infer latest CI success from an older green run.

## Verified database browser cases

New `src/frontend/e2e/database-workflows.spec.ts` executes four separate tests with real backend HTTP persistence assertions:

1. Drag a record from Board group `Todo` to `Done`; assert persisted select value and increasing version; reopen database and verify group.
2. Modify a Calendar record date; assert persisted date; reload and verify day cell plus persisted view date-field selection.
3. Open a List record's actual backing page in BlockNote, type content and poll the backend page content.
4. Create Board, Calendar, List, Gallery through the visible view selector and assert the five stored view types, including default Table.

Existing 13 Playwright scenarios remain enabled, including page conflict/retry and persistence regressions.

## Backend data consistency work

- Create database plus default property/view and record plus backing page in transactions.
- Version-check record updates and include property values and backing-page title in one transaction.
- Verify cross-database property ownership, property value type and configured select options, reject unknown types and invalid URLs/emails.
- Avoid holding open the only SQLite connection while querying nested properties/views.
- Regression tests check invalid-select rollback, missing linked record page rollback, nonempty database list termination, unsupported property definitions and protection of the required title property.

## Remaining requirements — NOT certified as complete

- Rich typed filters with AND/OR operators, column resize/reorder and advanced spreadsheet keyboard behavior.
- Relation, rollup, formula, person, file and other advanced property types.
- Database-record concurrency conflict UI and unsaved-draft recovery parity with the BlockNote page editor.
- Fully independent linked-view layout and configuration, complete record lifecycle/delete recovery, performance under very large datasets and UI polish.

No claim of complete Notion feature parity is made. Do not merge PR #2 until a separate review approves its final scope.
