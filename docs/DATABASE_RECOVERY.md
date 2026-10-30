# Database recovery (dev)

Database trash retains soft-deleted databases, properties, records and views in SQLite, without expiry or permanent erasure. Restore keeps the original ID, increments that entity's version once and preserves stored values, view configuration/order, backing-page writing and attachment ownership. Deleting a database hides its children rather than deleting them; explicitly deleted children remain in trash after their database is restored.

## Recovery order

1. Restore the parent database before its properties, records or views.
2. Restore referenced databases and records before restoring a relation property or a record with active relation values. A record may reference itself: restoring that record also makes its own reference valid in the same transaction. Other referenced records must already exist and be active.
3. Restore input properties before formulas. Restore the relation property and target database/property before rollups. A changed relation target cannot silently redirect a saved rollup.
4. Restore a linked database and its saved view before restoring a deleted linked page. The page snapshot stays in trash until both targets are available; successful recovery reactivates the existing link rather than creating a replacement view.

An unavailable dependency or invalid saved configuration returns HTTP 409 without consuming the tombstone/snapshot or changing versions/values. SQLite read/write failures return HTTP 500 and roll back. Repeating an already successful restore returns 404 and does not increment the version again. No repair writes to raw SQLite are part of the user workflow.

Deletion guards also apply while dependent databases are trashed: live formula/rollup definitions protect their inputs, and incoming live relation values protect referenced records/databases. Delete computed fields before inputs, clear incoming live references before deleting their targets, and keep a verified full workspace archive before reorganizing data. See [WORKSPACE_ARCHIVES.md](WORKSPACE_ARCHIVES.md) for archive and offline CLI restore instructions.

## Automated acceptance matrix

All fixtures are disposable and synthetic. These checks do not operate on user workspaces.

| Entity or dependency | Checks and evidence source |
| --- | --- |
| Database, property, record, view | Delete, rejected UPDATE rollback, same-ID restore, single version increment, repeat-restore rejection and restart: `db_recovery_lifecycle_test.go` |
| Formula | Input-first restore, preserved numeric inputs/output, malformed/duplicate-key config rejection and SQL fault rollback: `db_recovery_lifecycle_test.go` |
| Relation and rollup | Database/record/property ordering, changed target rejection, persisted relation values and recomputed result after restart: `db_recovery_lifecycle_test.go` and existing relation guard tests |
| Self relation | Self-only and mixed self/other targets; missing other targets still conflict; same relation values after restore/restart: `db_recovery_lifecycle_test.go` |
| Record page and files | Original writing/ID, identical attachment bytes, hidden file access while deleted, foreign-record file assignment rejection and unchanged provider configuration: `db_recovery_lifecycle_test.go` |
| Linked page | Database/view-first recovery, failed lookup leaves snapshot intact, saved link and writing after restart: `db_recovery_lifecycle_test.go`, `page-recovery.spec.ts` |
| Trash UI | Native modal, Escape/focus return, failed-load retry, input-first restore, category request races, failed category isolation, narrow reduced-motion layout: `database-workflows.spec.ts` |
| Portable workspace | Entity trash through process restart; database/inputs/formula/view/record recovery; archive, CLI restore and two more restored-workspace launches: `scripts/test-portable.py` |

The UI currently lists up to 250 deleted items per category. All older items remain in SQLite and the complete workspace archive. Arbitrary cyclic multi-record relation recovery, reciprocal relation editing, bulk dependency recovery, historical real v6/v7 workspaces and manual platform/screen-reader acceptance remain open. For cyclic legacy data, retain an archive and do not delete or edit raw values to bypass a conflict. This matrix certifies the stated cases only; broader product and release gates remain in [DEVELOPMENT_PLAN.md](DEVELOPMENT_PLAN.md).
