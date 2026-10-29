# Reverie Planner V0.0.1

A portable local-first foundation for notes, structured databases and project planning. This is an initial release, not full Notion parity.

## Included

- Nested documents and BlockNote editing with conflict recovery.
- Table, Board, Calendar, List and Gallery database views, typed fields, filters, sorting and reviewed atomic TSV editing.
- Local projects and tasks; optional AI providers.
- Visible workspace storage paths and consistent SQLite snapshot action.
- Native Linux, Windows and macOS build, browser, authenticated shutdown and restart checks.
- Runtime packages need no Go, Node, Python or cloud account.

## Download and run

Choose the archive matching your OS and CPU (architecture is in the filename). Extract into a writable folder. Windows: start.bat / stop.bat. Linux/macOS: start.sh / stop.sh. macOS also has Terminal .command launchers. Keep the start terminal open and open the loopback URL. macOS builds are unsigned and not notarized. Native packages cover the architectures listed in assets, not every CPU architecture.

## Data ownership

The whole extracted folder is the workspace. Stop before moving/copying it. SQLite snapshots alone exclude attachments and credentials. See docs/PORTABLE.md before backup/restore or moving credentials between accounts/operating systems. Never point an older binary at a newer database schema.

## Known scope limits

Notion import/export, templates/favorites, advanced reciprocal relationship lifecycle, 10,000-record performance acceptance, full accessibility review and signed macOS distribution remain unfinished. Existing lint warnings and the large editor chunk remain. Do not infer completeness from the version number. Track issues #4–#7.
