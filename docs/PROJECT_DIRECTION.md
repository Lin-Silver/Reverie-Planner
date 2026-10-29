# Project direction

## Goal

Replace Notion for personal notes, knowledge organization and project planning with a dependable local-first workspace. Documents and structured databases are the main product; AI assistance and the existing Plane-inspired project tools support that product. A provider key, subscription or network connection must never be necessary to write or retrieve local content.

## Product boundaries

- Portable Go + React + SQLite application with data owned by the user, explicit backups and forward-only migrations.
- Nested pages, rich block editing, search, record pages and independent Table/Board/Calendar/List/Gallery views.
- Typed properties, relations and computed fields with clear errors and explicit conflict resolution.
- Preserve current Windows launchers and packaging. Linux automated tests do not establish macOS or Windows manual acceptance. macOS packaging is a tracked delivery gap.
- Cloud sync, public publishing, real-time collaborative editing and a complete copy of Notion are outside the initial local release.

## Priorities and acceptance

| Priority | Work | Acceptance |
|---|---|---|
| P0: trust existing data | Backups, migrations, errors, draft/conflict safety, attachment ownership | Regression tests plus restored disposable databases; never use production workspaces as fixtures |
| P1: daily replacement | Notion/Markdown import/export, templates, favorites/recent pages, complete property and view editors, gallery presentation | Create/import, edit, restart, recover and export without an AI account |
| P1: scale | Server pagination or virtualization and relation query costs | Reproducible 10,000-record benchmark with documented hardware, time, memory and query plans |
| P1: access | Keyboard/focus/screen readers, narrow windows, macOS source build/packaging | Browser keyboard regressions plus documented manual platform acceptance |
| P2: advanced data | Reciprocal relations, dependency lifecycle, advanced formulas, timeline | Versioned schema/API/UI tests for delete/restore, rollback and independent linked views |

## Integration versus release

Integrating an audited feature branch into the development main branch is not certification of every release gate. Keep the 35-item database checklist and its unchecked items visible. Merge only reviewed, tested behavior; label partial features honestly. A release advertised as a practical Notion replacement additionally needs the P0/P1 workflows above and manual acceptance. The original requirement for a separate review is satisfied by this repository-wide review requested by the owner; unverified release requirements remain open.

## Working agreements

Implement one complete user workflow at a time, with data/API/UI checks. Preserve existing regression assertions. Record exact commit/CI links for CI claims. Keep PR descriptions concise and place historical evidence in documentation rather than repeatedly appending it to PR bodies. Keep already integrated branches until the owner separately requests deletion.
