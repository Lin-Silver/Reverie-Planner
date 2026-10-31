# Reverie Planner feature audit — 2026-10-30

Audited baseline: `5d2c8013b614b9f66ca2d4c677db5e849efd6193` on dev.
That head's CI 41083433422 succeeded; the feature acceptance before the documentation
checkpoint was `ba43477`, CI 41082127088 (82/82 browsers per native platform).
This is a code/documentation audit, not a new manual platform certification.
New Glass v2 work is recorded separately in GLASS_UI_V2.md and DEVELOPMENT_PLAN.md.

## What is already present

Nested pages and BlockNote writing, autosave/conflict handling, favorites/recent
pages, five built-in templates, five database layouts, typed fields, nested
filters, multi-key sorts, column layouts, reviewed atomic TSV editing/selection,
page history/trash, synthetic entity recovery, portable workspace archives and
isolated CLI/UI restore are present. Bounded Markdown ZIP/local-asset imports,
standalone typed CSV, document-tree packages and scalar database packages are
implemented and have native automated acceptance. None should be reported as
entirely missing because an older broad checklist remains unchecked.

## Missing or partial product workflows

| Priority | Capability | Actual state and remaining gap | Code/evidence |
| --- | --- | --- | --- |
| P1 | Complete Notion migration | Partial. Markdown ZIP and separate CSV work; integrated Notion ZIP database/page/asset mapping, rich exporter metadata and complete unsupported-format handling remain. | `markdown_import.go`, `csv_import.go`, `MARKDOWN_PACKAGES.md`, issue #5 |
| P1 | Reversible database transfer | Partial. Seven scalar types and independent views work; configured/select properties, relationships/formulas/rollups, database attachments, nested record pages and complete asset round trips block the bounded format. | `database_package.go`, `DATABASE_PACKAGES.md` |
| P1 | Large database usability | Not implemented/accepted. Records GET loads the full live set; frontend filters/sorts/renders in memory. Server pagination/virtualization and reproducible 10k+ time/memory/query-plan benchmarks remain. | `db_database.go:handleDBRecords`, `DatabaseView.tsx`, issue #6 |
| P1 | Property and view management | Partial. Add/edit validation, safe type-change refusal, rename/duplicate/delete and column presentation exist. Full property/option editing UX, deliberate type migration, view reordering and independent linked-view editing remain. | `db_database.go` (type changes deliberately refused), `DatabasePropertyDialog.tsx`, `DatabaseView.tsx` |
| P1 | Spreadsheet behavior | Partial. Rectangular selection and reviewed atomic TSV writes exist. Complete native clipboard shortcuts and spreadsheet undo/safe reversal of batches remain. | `DatabaseBulkPaste.tsx`, `databaseClipboard.ts`, `db_bulk_edit.go` |
| P1 | Daily page organization | Partial. Built-in templates, icons, favorites, recents and nesting work. User-created templates, full page duplication/organization and cover upload/management are unfinished. | `pageTemplates.ts`, `App.tsx`, `PRODUCT_PARITY_MATRIX.md` |
| P1 | Provider setup screen | Missing complete UI. The API/client exist, but Settings has a provider selector and explanatory placeholder; provider creation, endpoint/model/key editing and connection testing have no complete settings form. | baseline `App.tsx` Settings section; `main.go`/provider endpoints |
| P1/P2 | Planning configuration | Partial. Projects/tasks CRUD and basic cycles/modules/intake/stickies/drafts exist. Labels/custom states editors, saved-view filtering, comment/activity UI and richer editing/organization flows are not complete. API/schema existence is not UI completion. | `App.tsx` render branches and TaskDraft form, `handlers_plane.go`, parity matrix |
| P2 | Advanced database dependencies | Partial. Basic formulas, relations, rollups, cycle checks and synthetic delete/recovery protections exist. Reciprocal/indexed relations and exhaustive dependency/schema/concurrency lifecycle remain. | `db_computed*.go`, `db_relation*.go`, `db_recovery_lifecycle_test.go`, issue #6 |
| P2 | Timeline | Not implemented. Available layouts are Table, Board, Calendar, List and Gallery. | `DatabaseView.tsx`, `validateDBViewConfig` |

## Implemented code whose release acceptance is still incomplete

| Gate | What remains | What this does not mean |
| --- | --- | --- |
| Historical data | Authorized, sanitized real v6/v7 samples and migration comparison; current fixtures are synthetic. | It does not establish known data loss or absence of synthetic migration tests. |
| Accessibility | Screen-reader/manual keyboard/touch review, complete dialog consistency, contrast across all states, long labels/large tables. | Existing focus/reduced-motion/native-dialog regressions are real, but do not certify full WCAG compliance. |
| Platform delivery | Manual Linux/Windows/macOS use and macOS signing/notarization. | Native automated portable packages already exist. |
| Engineering | 14 baseline lint warnings, ~609 KB editor dependency chunk, large App/DatabaseView files and several inline component styles. | A passing build does not resolve these debts. |
| Release | New dev features have not been promoted into a new versioned stable release. | Published V0.0.1 remains the earlier baseline. |

## Explicit initial-scope exclusions

Cloud synchronization, public publishing, real-time collaboration and a complete
copy of every Notion feature are outside the current personal/local initial
release boundary in PROJECT_DIRECTION.md. Member/workspace records are not proof
of complete authentication, collaborative permissions or cloud functionality.

## Recommended order

1. Finish database assets/configured fields and integrated Notion package migration.
2. Deliver pagination/virtualization with a documented 10k benchmark.
3. Complete provider/property/view configuration and daily page organization UX.
4. Complete manual accessibility/platform gates and prepare a reviewed next release.

Glass v2 is a separate, owner-requested design increment; it must preserve all
existing data/conflict/restore checks and must not close these functional gates.
