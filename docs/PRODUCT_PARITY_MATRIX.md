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
| Table/Board/Calendar/List/Gallery | Partial | E2E verified | Board and Calendar drag/drop persist data; table column sizing/reordering/hiding, keyboard cell editing; List/Gallery record links. No timeline view or Gallery cover editor |
| Properties/filter/sort/relations | Partial | Partially verified | Typed field schema, nested AND/OR filters, configurable select/date/number, formulas and relation rollups, scoped file attachment and version validation; more complex relation/formula behaviors and spreadsheet editing remain |
| Search | Partial | Implemented | Global search across projects/tasks/pages |
| Shortcuts & templates | Partial | Verified | Ctrl+K command palette works; templates remain unavailable |
| Import/export & portability | Missing | Planned | Notion/Markdown import/export and macOS packaging need implementation and acceptance |

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
- Table column sizing/reordering/hiding now works; reviewed TSV copy/paste and rectangular selection exist; still missing native spreadsheet shortcuts/undo and richer property management, gallery covers and a timeline view.
- Database record conflicts have Retry/Reload tests, but full multi-client schema and linked-view conflict UX needs further validation.
- The database feature is not yet at full Notion parity; track the remaining release gates in PROJECT_DIRECTION.md and DATABASE_COMPLETION_PLAN.md.
- No real-time collaboration
- No templates, favorites, recent pages
- Page cover image upload pending
- Mobile responsive polish pending

Verified database interaction baseline: [Windows CI 40266607916](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40266607916), commit `b0d34c68c4bc27cafa2389234d189819fc062814`, **27/27 Chromium E2E**. Newer commits require their own SHA-matched verification.
