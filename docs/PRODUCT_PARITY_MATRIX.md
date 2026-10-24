# Product Parity Matrix

Reverie Planner vs Notion (UI/UX) and Plane (project management). Baseline: `1f5a900`.

## Notion

| Feature | Reverie | Status | Notes |
|---|---|---|---|
| Workspace & page tree | Partial | Implemented | Sidebar lists workspace pages; nested via parentId supported in DB/API, UI tree drag not yet |
| Nested pages | Partial | Implemented | parent_id column + API; sidebar nesting UI pending |
| Block editor | Implemented | Verified | BlockNote (MPL-2.0) with slash menu, drag handles, markdown shortcuts |
| Slash command | Implemented | Verified | BlockNote built-in |
| Rich text | Implemented | Verified | BlockNote |
| Drag & reorder blocks | Implemented | Verified | BlockNote built-in |
| Page icon & cover | Partial | Implemented | icon editable; cover field exists, UI pending |
| Auto-save | Implemented | Verified | 1s debounce + status indicator |
| Databases/multi-view | Missing | Planned | Not yet |
| Table/kanban/calendar/timeline views | Missing | Planned | |
| Properties/filter/sort/relations | Missing | Planned | |
| Search | Partial | Implemented | Global search across projects/tasks/pages |
| Shortcuts & templates | Partial | Planned | Ctrl+K search pending |

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
- No full database views (table/kanban/calendar)
- No real-time collaboration
- No templates, favorites, recent pages
- Page cover image upload pending
- Drag-to-reorder pages in sidebar pending
- Keyboard command palette (Ctrl+K) pending
- Mobile responsive polish pending
`