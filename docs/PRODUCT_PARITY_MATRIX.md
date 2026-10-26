# Product Parity Matrix

Reverie Planner vs Notion (UI/UX) and Plane (project management). Baseline: merged PR #1 (`f462423`) plus the open PR #2 feature branch. This matrix describes implemented, tested behavior rather than promised parity.

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
| Databases/multi-view | Partial | E2E verified | SQLite records/properties/views, Table/Board/Calendar/List/Gallery creation, saved per-view filter/sort/group/date selection |
| Table/Board/Calendar/List/Gallery | Partial | E2E verified | Table edits, Board drag to persist select group, Calendar date edit, record page links and List/Gallery layouts; timeline not implemented |
| Properties/filter/sort/relations | Partial | Partially verified | Text/number/checkbox/select/multi-select/date/URL/email schema supported; text filter, sort and view settings persisted; relation/rollup/formula and typed AND/OR filters not completed |
| Search | Partial | Implemented | Global search across projects/tasks/pages |
| Shortcuts & templates | Partial | Verified | Ctrl+K command palette works; templates remain unavailable |

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
- Database formula, rollup, relation, file, people, and advanced typed filters are not complete.
- Views need column sizing/reordering, richer property management and gallery covers; no timeline view.
- Multi-client conflict resolution for database cells is not yet as mature as BlockNote page conflict handling.
- The database feature is not yet at full Notion parity; keep PR #2 open until separate acceptance review.
- No real-time collaboration
- No templates, favorites, recent pages
- Page cover image upload pending
- Mobile responsive polish pending
`