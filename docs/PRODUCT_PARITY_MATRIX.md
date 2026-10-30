# Product Parity Matrix

Reverie Planner vs Notion (UI/UX) and Plane (project management). Baseline: integrated page-tree and database feature work. See PROJECT_AUDIT.md for reviewed branch heads. This matrix describes implemented, tested behavior rather than promised parity.

## Notion

| Feature | Reverie | Status | Notes |
|---|---|---|---|
| Workspace & page tree | Partial | Verified | Nested page tree, page move validation and sidebar drag supported; additional UX polish remains |
| Nested pages | Implemented | Verified | Recursive tree with cycle prevention, atomic moves and backend tests |
| Block editor | Implemented | Verified | BlockNote (MPL-2.0) with slash menu, drag handles, markdown shortcuts |
| Slash command | Implemented | Verified | BlockNote built-in |
| Rich text | Implemented | Verified | BlockNote |
| Drag & reorder blocks | Implemented | Verified | BlockNote built-in |
| Page icon & cover | Partial | Implemented | icon editable; cover field exists, UI pending |
| Auto-save | Implemented | Verified | 1s debounce + status indicator |
| Databases/multi-view | Partial | E2E verified | Persistent SQLite records/properties/views, five working layouts, typed filters, two-key sorts, independent view configuration, linked pages and Trash/Restore |
| Table/Board/Calendar/List/Gallery | Partial | E2E verified | Board and Calendar drag/drop persist data; table column sizing/reordering/hiding, keyboard cell editing; List/Gallery record links. Gallery property selection and local attachment cover selection; no timeline view |
| Properties/filter/sort/relations | Partial | Partially verified | Typed field schema, nested AND/OR filters, configurable select/date/number, formulas and relation rollups, scoped file attachment and version validation; more complex relation/formula behaviors and spreadsheet editing remain |
| Search | Partial | Implemented | Global search across projects/tasks/pages |
| Shortcuts & templates | Partial | In development | Ctrl/Cmd+K native focus-contained palette; five built-in editable page templates; custom templates pending |
| Import/export & portability | Partial | In development | V0.0.1 native portable packages released. dev includes workspace archive/isolated restore, page history/trash recovery, bounded Markdown ZIP + local assets, typed standalone CSV, document-tree packages and scalar database packages with independent views; full Notion/advanced database/asset compatibility remains open |

## Plane

| Feature | Reverie | Status | Notes |
|---|---|---|---|
| Project management | Implemented | Verified | CRUD + archive |
| Issues | Implemented | Verified | Kanban board, CRUD, status/priority |
| Custom states | Partial | Implemented | API + DB; UI labels editor pending |
| Modules | Partial | Implemented | CRUD API + list UI |
| Cycles | Partial | Implemented | CRUD API + list UI |
| Labels | Partial | Implemented | API + DB; UI tagging pending |
| Intake | Partial | Implemented | API + list UI |
| Views | Partial | Implemented | API + DB; filter UI pending |
| Comments | Partial | Implemented | API + DB; UI pending |
| Activity | Partial | Implemented | API + DB; UI pending |
| Analytics | Partial | Implemented | Counts by status |

## Known gaps
- Basic formula, rollup, relation, file, people and AND/OR filter implementations exist, but advanced lifecycle, bidirectional links, dependency handling and large-scale performance are not fully proven.
- Table column sizing/reordering/hiding now works; reviewed TSV copy/paste and rectangular selection exist; still missing native spreadsheet shortcuts/undo and richer property management, richer gallery cover handling and a timeline view.
- Database record conflicts have Retry/Reload tests, but full multi-client schema and linked-view conflict UX needs further validation.
- The database feature is not yet at full Notion parity; track the remaining release gates in PROJECT_DIRECTION.md and DATABASE_COMPLETION_PLAN.md.
- No real-time collaboration
- Built-in templates, SQLite-backed favorites and recently visited pages are implemented on dev; custom templates remain open
- Page cover image upload pending
- Responsive shell, native mobile navigation and theme/material tokens added on dev; full mobile and accessibility acceptance remain open

Verified database interaction baseline: [Windows CI 40266607916](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40266607916), commit `b0d34c68c4bc27cafa2389234d189819fc062814`, **27/27 Chromium E2E**. Newer commits require their own SHA-matched verification.
