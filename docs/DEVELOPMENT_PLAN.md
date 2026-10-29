# Reverie Planner ongoing development plan

This is the durable checkpoint for human and scheduled development. The target is a reliable local-first personal Notion replacement. “Perfect” means meeting the acceptance items below and maintaining them; do not declare full Notion parity from UI polish or passing tests alone.

## Working rules

- Read AGENTS.md and the current remote dev head before changing code; preserve other work with an expected-head lease.
- Keep only main/dev. Develop on dev. Promote reviewed changes only with SHA-matched native Linux/Windows/macOS checks and a versioned release plan.
- Keep user data local. No AI account is required for core flows. Never use real user data as a destructive test fixture.
- Finish a complete user workflow, record exact tests and commit/CI evidence, then choose the next unmet item.
- If a scheduled run overlaps an in-progress change, inspect its checkpoint/CI first rather than duplicating the work. Do not invent a successful run when tools are unavailable.
- Preserve original regression assertions. Distinguish code, automated verification and manual platform/assistive-technology acceptance.

## Baseline already delivered

- [x] Native portable V0.0.1: Linux x64, Windows x64, macOS ARM64; authenticated stop/restart and owned data paths.
- [x] dev workspace redesign, tokenized light/dark materials, native application dialogs, mobile navigation, focus and reduced-motion/transparency support.
- [x] Five built-in page templates; SQLite favorites/recent pages; single-page Markdown append/export and block JSON export.
- [x] Baseline dev native CI: commit 131cafd793223c0e422d75bbd09a1fa40b5e876f, run 40909181555; 61/61 browser tests on each platform.

## M1 — Data ownership and recovery (highest priority)

- [x] One portable archive contains a verified SQLite snapshot and all referenced attachment bytes, including recoverable trash; app settings are sanitized and provider files/keys/runtime tokens are excluded.
- [x] Manifest lists format/schema version, sizes and hashes; missing or changed attachments fail the archive instead of silently producing an incomplete backup.
- [x] Preview and restore validate archive limits, names, duplicates, checksums, database integrity, foreign keys and supported schema.
- [x] Restore publishes only to a new directory; failures/cancellation leave the current workspace unchanged. Reopen the restored workspace and verify contents and attachments.
- [x] Settings has download, preview, restore, status and error/retry controls; offline CLI restoration is documented and tested.
- [ ] Page revision history and restore preserve current/newer content through version checks; page delete/trash/restart matrix is tested.
- [ ] Historical v6/v7 sample acceptance is recorded only when an authorized real sample exists; synthetic fixtures are labelled synthetic.

## M2 — Migration and daily organization

- [ ] Markdown directory/Notion export-package import: preview, nested pages, links, assets, supported CSV database mapping, explicit unsupported-format reports.
- [ ] Transactional import with collision policy; rollback and restart consistency.
- [ ] Reversible document/database export; asset paths, typed values and independent views retained.
- [ ] User-created templates, duplication, covers and complete page organization; offline browser acceptance.

## M3 — Database scale and completeness

- [ ] Pagination/virtualization with stable sort, filter and selection semantics; 10,000+ record benchmark with hardware, time, memory and query plans.
- [ ] Complete property editing, view ordering and independent linked-view settings with concurrency guards.
- [ ] Reciprocal relations, formula/rollup dependency lifecycle, delete/restore and rollback matrix.
- [ ] Native clipboard shortcuts, spreadsheet undo/safe rollback, timeline and keyboard-equivalent interactions.

## M4 — Accessible polished product

- [ ] Consolidate all remaining dialogs and menus; test names, focus, keyboard, screen readers and touch.
- [ ] Verify long titles, empty/error/loading states, small windows, large tables and all themes/material preferences.
- [ ] Eliminate React lint debt; evaluate editor chunk and real responsiveness without weakening data safety.
- [ ] Manual Linux/Windows/macOS acceptance and documented macOS signing/notarization status.

## M5 — Release and maintenance

- [ ] Update version/package/release workflows for the next release; immutable published assets.
- [ ] Final end-to-end create → import → edit → restart → recover → export without AI.
- [ ] All required native checks and review pass; publish documented limitations and update issues.
- [ ] Continue regression/security/data integrity maintenance after the checklist is complete.

## Current checkpoint

M1 workspace archive and isolated restore accepted on code commit `a6ab2efab816b38b303d44ba298d5e5326c67117`: [native CI 40969661631](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40969661631), all Linux/Windows/macOS jobs successful, **63/63 browser tests on each platform**, Go tests/build, lifecycle and offline CLI restore/restart checks passed. Local Go tests, go vet, production frontend build/lint and 63 browser tests also passed; existing lint warnings/editor chunk debt remain. Synthetic fixtures do not establish real historical backup acceptance.

Next active item: **page revision history and trash recovery**. Add version-checked restore, preserve current/newer content, retain a recoverable copy of deletion, and test restart plus multi-window conflicts. Do not redo the accepted archive workflow unless a regression or new requirement justifies it.

The hourly scheduled development task reads this plan and current dev/CI state to resume. A scheduled wake-up is not proof that new code was produced or tests passed. `main` remains V0.0.1; these changes are on `dev` pending a separately versioned release.
