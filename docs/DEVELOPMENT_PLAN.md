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
- [x] Page recovery automatic acceptance on code commit `b9beea506f96d4b2a97f6aece4d1e5fcab4d3056`; native Linux/Windows/macOS [CI 40975261473](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40975261473) passed.
  - [x] Transactional pre-change revisions, bounded history browsing/plain-text preview/block download, version-checked restore and undo of restore.
  - [x] Preserve current parent/project/links and database-owned titles/properties; recover document writing without replaying stale structural metadata.
  - [x] Version-checked page trash, same-ID restore/newer version, parent ordering, owned-page guards and restart/rollback regressions.
  - [x] Native portable script now checks history/trash through restart, workspace archive, CLI restore and another restart.
  - [x] Final native CI evidence: **66/66 browser tests per platform**, Go tests/build, lifecycle, archive/CLI restore/restart and packaging passed. Manual platform/assistive-technology acceptance remains open under M4.
- [x] Supported-entity **synthetic** deletion/recovery matrix implemented: database/property/view/record rollback and restart; formula inputs; cross-database relation/rollup restore order and retarget rejection; safe self-reference recovery; record pages, view settings and attachment ownership; linked-page database/view recovery guards. See [DATABASE_RECOVERY.md](DATABASE_RECOVERY.md). Native acceptance is recorded separately below; reciprocal relations and exhaustive advanced dependency editing remain M3 work.
- [ ] Historical v6/v7 sample acceptance is recorded only when an authorized real sample exists; synthetic fixtures are labelled synthetic.

## M2 — Migration and daily organization

- [x] Initial text-only Markdown ZIP import: source/parent preview, local editor conversion, server preflight, nested explicit pages/internal links, separate-root collision policy and transactional/idempotent commit. Attachments/CSV/complete Notion package acceptance remains unchecked below.
- [x] Bounded local Markdown attachment subset: source paths/sizes, embedded raster images and local file downloads, atomic page/byte/receipt writes, root ownership/trash retention, verified workspace archive/CLI restore/restart. Accepted on code `8675a80106e58dd3b5f6ab7963189c60b6c4a02b`, [native CI 41007579249](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41007579249); supported formats/limits in MARKDOWN_PACKAGES.md. This does not close complete package compatibility.
- [ ] Markdown directory/Notion export-package import: preview, nested pages, links, assets, supported CSV database mapping, explicit unsupported-format reports.
- [ ] Transactional import with collision policy; rollback and restart consistency.
- [x] Bounded native document-tree transfer: editor block JSON, hierarchy, icons, local links/assets, separate-copy preview/import, rollback/receipt/restart/archive checks; accepted on `f9c34617fb48d3d2c58db40be20227cc73f5c37e`, native CI 41009774686. This is document-only; see DOCUMENT_PACKAGES.md.
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

Page recovery increment now implemented on top of `4f8446fe5e4fd19fe00878ae345aa7abfb49bde3` (previous dev/native CI 40970433174 successful). Schema 11 captures pre-change page data inside SQLite transactions and retains deleted pages. Fixed the broken frontend DELETE body/query mismatch and post-delete navigation save. New history/trash UI follows the existing native dialog/tokens, immediate keyboard interactions, explicit focus return and an opaque reading panel. See [PAGE_RECOVERY.md](PAGE_RECOVERY.md) for exact retention, backup/version compatibility and owner/parent boundaries.

Local validation complete: Go suite/vet, production frontend build/lint and extended native portable/CLI restore checks passed. Final full browser suite **66/66** passed, including 3 new regressions for history/restore/conflict, delete/recover/stale writes and retry/focus/narrow reduced-motion layout. Actual narrow/light and desktop/dark recovery screenshots reviewed; reading panels are opaque. Code commit `b9beea506f96d4b2a97f6aece4d1e5fcab4d3056` passed native [CI 40975261473](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40975261473): **66/66 on Linux, Windows and macOS**, plus backend, build, lint, native lifecycle/archive/CLI restore/restart and package steps. This follow-up checkpoint changes documentation only; its new CI run is not a new feature acceptance claim. Existing React lint warnings and editor chunk debt remain; this increment does not claim complete accessibility or full Notion replacement.

Next after this acceptance: incrementally complete bounded multi-page package migration, preserving explicit format limits and transactional rollback. The text-only subset below does not close the assets/CSV/export requirements. The supported-entity synthetic recovery matrix is now implemented; reciprocal relations and advanced dependency-editing combinations remain open under M3. Real v6/v7 samples are unavailable and remain unverified; do not invent historical acceptance or block synthetic regressions on private samples.

The hourly scheduled development task reads this plan and current dev/CI state to resume. A scheduled wake-up is not proof that new code was produced or tests passed. `main` remains V0.0.1; these changes are on `dev` pending a separately versioned release.

## 2026-10-30 — Database recovery increment

Started from remote dev `5ccd720ed34b7b8908098aca33fae41ee2c386af`, whose [CI 40975939981](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40975939981) was completed/successful. Open issues #4–#7 and the main/dev-only branch list were checked before editing; no parallel head change was observed at that checkpoint.

Fixed restoration of formulas/rollups with deleted or retargeted dependencies, self-referencing records (without bypassing other target checks), and linked pages with deleted database/view targets. All restore checks run in the mutation transaction and retain tombstones/snapshots on conflict or storage failure. Database trash now uses native modal focus/Escape, load retry, abortable category requests and opaque token-based styling. Database background refresh no longer unmounts the recovery surface or resets its category/focus. No new motion was added to this frequent keyboard flow.

Validation checkpoint: targeted backend recovery tests, Go suite/vet, frontend production build/lint and extended native portable/archive/CLI restore/restart checks passed locally. Existing frontend lint warnings and editor chunk warning remain; no new DatabaseTrash lint warning was retained. Final full local browser suite **70/70 passed (3.8m)**, including all existing assertions and four added browser regressions. The final source/test hashes were checked unchanged against the validation snapshot. SHA-matched native [CI 40982918277](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40982918277) for `9b256e8032f8502cebea496c6a75384955dae7e2` completed successfully on Linux, Windows and macOS (backend, browser, lifecycle/CLI restore and packaging). No release/main update is authorized by these development checks alone.

Remaining priorities: complete bounded package import/export with preview/rollback, then scale/property/reciprocal relation and accessibility acceptance. Real historical v6/v7 samples are still unavailable; do not mark that acceptance done. Keep issue #4 open for historical/advanced lifecycle limits, and keep main at its current stable release.


## 2026-10-30 — Text-only package import increment

Started from remote dev `9b256e8032f8502cebea496c6a75384955dae7e2`; its native [CI 40982918277](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40982918277) was successful on all three platforms. Read AGENTS, direction/design documents, open issues #4–#7 and the main/dev-only list before editing. No concurrent head change was observed at the starting checkpoint; publication uses an expected-head lease.

Implemented the bounded offline workflow described in [MARKDOWN_PACKAGES.md](MARKDOWN_PACKAGES.md). Source preview is read-only; conversion and relative-link validation happen before confirmation and again on commit. A separate import root preserves existing titles/content. The SQLite transaction contains the entire page tree plus a persistent receipt, so a lost successful response can be retried without duplicate pages, including after restart and full workspace restore. Internal links use the current unsaved-writing guard. UI reuses system fonts, opaque preview/tokenized controls, immediate keyboard feedback and existing motion/material preferences; no new animation was added to the repeated preview/reading flow.

Local verification: 6 targeted backend tests (including 8 unsafe-input subcases) plus full `go test ./...`, `go vet ./...`, backend build, frontend production build/lint and extended native portable/archive/CLI restore/restart passed. Existing lint warnings and a 609 KB editor-related chunk warning remain. Local browser launch was blocked before any test step by the host's socket restriction (`Operation not permitted` in Chromium process singleton); the 3 added browser regressions and complete suite therefore require native CI. No local browser pass or screenshot review is claimed. First native CI [41004889761](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41004889761) on `72a70ec00ff78ee305b574b1b7582148899256b5` found the new narrow-window test attempting to click behind the correctly modal navigation drawer after resizing. The test now explicitly closes and asserts removal of the drawer; all import/data assertions are retained. The first two new browser workflows passed on Linux. Run 41005363482 retained the same 72/73 result with an unlocated timeout in the narrow test. Diagnostic run [41005936287](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41005936287) exposed a real mobile drawer defect: the sidebar inherited z-index 30 and intercepted pointer events over the close button. Reset the sidebar stacking level inside the native dialog, lift its close button, and provide an opaque material base so background writing does not show through navigation text. Added a hit-target assertion and retained the normal click and complete import assertions. Failure traces/screenshots and a focused native import gate precede the unchanged full suite. The corrected code commit is accepted in the final native verification below. The prior `9b256e8` acceptance is not evidence for this new implementation.

The bounded text-only workflow is implemented; the parent M2 package-compatibility/reversible-export acceptance is **not** complete. Assets, CSV typed database mapping, raw HTML/rich Markdown fidelity, local heading anchors, arbitrary directory container pages, complete Notion packages and reversible document/database export remain open. Real historical v6/v7 samples are still unavailable; do not mark their acceptance done. Next: first resolve any SHA-matched native regressions for this increment; then extend package preview/transactionality to attachment ownership, archive preservation and independent typed CSV databases. Main remains the published V0.0.1; do not release this development increment without native acceptance and a separate versioned release review.


### Final native verification — 2026-10-30

Code commit `977380f8bd59bdec4739c3871b24c01af956cdac` passed [CI 41006265669](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41006265669) on Linux, Windows and macOS. Each native job passed the focused 3/3 Markdown import checks and the unchanged complete **73/73 browser suite**, backend tests/build, frontend production build/lint, lifecycle/ownership checks, extended native import → retry → restart → workspace archive → CLI restore → two further restarts, and portable packaging. Windows also retained its unrelated-service/start/stop checks. Native Linux narrow-preview evidence was downloaded and visually reviewed: no horizontal overflow, opaque readable source preview and usable controls. The close action has a real hit-target assertion and normal pointer activation, not a forced click.

Local Chromium remains unavailable due to the host socket restriction; native CI supplies the browser evidence. This follow-up changes only this development plan, not the accepted source/tests/workflow. Its own new CI invocation is not a new feature-acceptance claim. The text-only import subset now has automated native acceptance; M2 complete Notion/asset/CSV/reversible-export compatibility and M4 manual platform/accessibility acceptance remain open. No main/release update was made. Next increment: attachment import ownership and retention through history/trash/archive, then typed CSV mapping and reversible package export. Continue preserving only main/dev and publishing with an expected-head lease.


## 2026-10-30 — Bounded Markdown attachment increment

Started from remote dev `977380f8bd59bdec4739c3871b24c01af956cdac`, matching the clean local checkout. Its [native CI 41006265669](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41006265669) completed successfully on Linux, Windows and macOS, including the focused import gate, full 73-test suite, portable lifecycle/archive/CLI restore and packaging. The preceding mobile drawer/import failure is therefore resolved on that SHA. Open issues #4–#7, no open PRs and the main/dev-only list were checked; publication uses an expected-head lease. Before publication, remote dev advanced to documentation-only `34ad81e8272e9f77cba0593d8635fea91d20422d`. Its diff was reviewed and its complete native-acceptance checkpoint retained; source/tests were unchanged. The new increment is based on that newer parent.

Implemented local attachment preview, relative asset links, embedded local images and download URLs. Supported formats and limits are documented in [MARKDOWN_PACKAGES.md](MARKDOWN_PACKAGES.md); CSV, arbitrary attachments, rich fidelity and full Notion compatibility remain open. The import root lists all included attachments as local links, keeping unreferenced files accessible. Schema 12 stores bounded imported asset bytes in SQLite, with the import root as shared owner. Pages, bytes and retry receipts share one transaction. Root trash hides downloads without losing history bytes; recovery and full workspace archives retain them. Export/restore verifies owner, size, detected content type and SHA-256. No new animation or material pattern was added to this frequent reading/confirmation flow; the existing tokenized preview remains opaque.

Local verification: all Go tests and `go vet ./...`, backend build, frontend production build/lint and the extended native portable/archive/CLI restore/restart script passed. Six additional backend tests cover supported assets, rewritten links, storage/receipt failure rollback, cancellation, retry, restart, root trash/restore, corrupted downloads/archives, missing owners and synthetic v11 upgrade; synthetic v6/v7 fixtures remove the new table when constructing their old schema, with original preservation assertions retained. Two new browser regressions cover local image rendering/download/reload and lost-success retry, plus missing/remote image preflight refusal. Chromium cannot launch locally because the host rejects its process-singleton socket (`Operation not permitted`); no local browser pass or screenshot review is claimed. Existing React lint warnings and the editor chunk warning remain. Native acceptance of this new increment is pending its own SHA-matched CI; the prior 73-test run only accepts the starting commit.

Remaining/next: resolve any native attachment-import regression first, then typed CSV/database mapping with independent view semantics and reversible document/database export. The broader M2 migration/export checkboxes stay open, as do real historical v6/v7 sample acceptance, database scale/advanced relations and manual accessibility/platform acceptance. Main remains the released V0.0.1; this increment does not authorize a new stable release or establish full Notion parity.


### Attachment native verification — 2026-10-30

Code commit `8675a80106e58dd3b5f6ab7963189c60b6c4a02b` passed [native CI 41007579249](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41007579249): Linux, Windows and macOS each passed **5/5 focused import checks** and **75/75 complete browser tests**, Go tests/build, frontend build/lint, portable lifecycle and extended attachment/import → retry → restart → archive → CLI restore → two further restarts, plus packaging. Windows start/stop/unrelated-service ownership checks passed too. The new local images and download URLs are exercised in a real native browser; no manual hardware/screen-reader or new screenshot review is claimed.

This acceptance checkpoint changes only this plan. Source/tests/workflow remain the exact tree verified above; the documentation follow-up's own CI is separate. The bounded attachment subset is now checked off under M2, while full CSV/Notion compatibility, typed database mapping, reversible exports and other release gates remain open. Main is unchanged and no release was made. Next: implement previewed typed CSV databases transactionally, then reversible page/database/asset export; preserve the import receipt, original regression assertions and source archive throughout.


## 2026-10-30 — Reversible document-tree transfer increment

Began after checking current dev, open issues #4–#7 and the main/dev-only branch list. The shared checkout held uncommitted attachment work that was simultaneously published as `8675a80106e58dd3b5f6ab7963189c60b6c4a02b`; it was left untouched and this increment uses a detached isolated worktree. That code's [CI 41007579249](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41007579249) subsequently passed all three platforms (75/75 complete browser tests each). Before publication, dev advanced to documentation-only `a91b362d3e99b8b651bf61edeb191fba4fab5299`; reviewed and retained its entire checkpoint, and based this increment on that newer head. Publication uses an expected-head lease.

Implemented the document-only transfer subset in [DOCUMENT_PACKAGES.md](DOCUMENT_PACKAGES.md) before extending external CSV mapping: it provides a native, reversible reference format for nested writing and attachment ownership. A saved tree exports block JSON, titles/icons, local page links and exact referenced/root-owned asset bytes. Import has read-only preview, strict limits/format/tree/checksum validation, independent IDs and one transaction containing pages, attachments and durable retry receipt. Unsupported database pages, covers, outside-tree references, legacy text and media are explicit blockers rather than silent downgrades. No schema change, cloud dependency or real user data is introduced. Full Notion/CSV/database export acceptance remains open; document-only behavior must not close the broader M2 checkbox.

Local verification: six new backend tests (including page/asset/receipt rollback and cancellation subcases), full Go suite/vet, production frontend build/lint and backend build passed. The build used `-buildvcs=false` locally because VCS stamping in this isolated worktree failed; native CI retains the normal build commands. Extended portable checks passed document export/import/receipt retry and local attachment downloads through source restart, full archive, CLI restore and two restored starts. Existing React lint warnings/editor chunk debt remain; no new component lint warnings were introduced. Two new browser workflows are implemented, and the focused CI gate retains all five existing Markdown tests plus these two, before the unchanged full suite. Local Playwright's bundled browser is absent; using the existing Chrome executable also failed at process-singleton socket creation (`Operation not permitted`) before any test assertion. No local browser pass or new visual review is claimed. This increment requires its own SHA-matched native CI; the previous attachment CI is only baseline evidence.

Remaining/next: resolve any native document-transfer regressions first, then transactional typed CSV/database mapping and reversible database/view export; keep legacy v6/v7 real-sample acceptance, advanced relations/scale and manual accessibility/platform checks open. Main remains V0.0.1. The only long-term branches remain main/dev; no release is made from this development acceptance.


### Document preview correction before acceptance

Initial source commit `62fbf08494a1631af7c35c2032d90955dc7d79b1` ran [native CI 41008616098](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41008616098); the focused 7-test gate and full 77-test browser suite passed on each platform. Reviewing the actual native Linux narrow screenshot caught an uncovered UI defect: `.markdown-source` wrapping/max-height/readable panel rules were scoped to the Markdown importer, so the new document preview could clip long block JSON. Generalized the existing rules to archive panels, retained all data assertions and added explicit computed wrapping/no-inner-overflow checks with long synthetic content. Capture the actual narrow viewport after scrolling the preview into view. Also made singular document feedback natural and showed the real parent title in the preview. Final acceptance must use the new corrected source SHA and its own complete native run; the initial 77-test pass does not establish this fix.


### Encoded-size safety correction before acceptance

A boundary review found that raw document/asset byte limits alone could permit a JSON/base64 download larger than the 48 MiB importer cap, and JSON escaping could make stored normalized blocks exceed document limits. Added normalized per-document/aggregate and post-reference-rewrite aggregate guards, plus an actual encoded download-size check before response publication. The seventh backend regression covers per-page and aggregate escaping expansion, and an eight-page/two-asset synthetic workspace whose legal logical sizes encode above the transport cap; export is refused with a clear error and no source mutation. This is an additional safety fix on top of the preview correction; final acceptance requires its own SHA-matched native run. All earlier assertions remain in place.


### Final document-transfer acceptance — 2026-10-30

Final code commit `f9c34617fb48d3d2c58db40be20227cc73f5c37e` passed [native CI 41009774686](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41009774686) on Linux, Windows and macOS: **7/7 focused migration tests and 77/77 complete browser tests per platform**, backend tests/build, frontend build/lint, document export/import and idempotent retry, native lifecycle, attachment downloads after restart, workspace archive/CLI restore/two restored starts, and packaging. Windows also passed unrelated-service ownership and graceful start/stop checks. Local full Go suite/vet and the extended portable script passed after the final serialization fix. The native Linux screenshot from this exact commit was downloaded and matched byte-for-byte the visually reviewed corrected preview: bounded, wrapping, selectable text on an opaque panel and usable narrow controls. This is automated/native plus screenshot evidence; it does not establish manual hardware or screen-reader acceptance.

Only the explicitly bounded native document subset is checked off. CSV typed database mapping, reversible databases/independent views, complete Notion migration, covers/legacy media, real authorized v6/v7 samples, scale/advanced relations and manual accessibility remain open. Next: transactional previewed CSV mapping with explicit types/validation and independent views, then reversible database/asset exports. Main remains the published V0.0.1; only main/dev branches were present at final verification. No release or stable-branch update was made.

This follow-up modifies this plan only. The accepted source/tests/workflow are the exact commit above; a new documentation-only CI invocation is separate and is not a new feature-acceptance claim. Publish this checkpoint using an expected-head lease and preserve newer concurrent work.

## 2026-10-30 — Transactional typed CSV increment

Started from remote dev `88d8b61d0e771283ed456345f82715caac3411fa`, matching a clean isolated worktree. Its [CI 41010350965](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41010350965) completed successfully; the preceding document source `f9c34617fb48d3d2c58db40be20227cc73f5c37e` retains its own 77/77 native acceptance. Read AGENTS/direction/design/plan and open issues #4–#7, and verified only main/dev before editing. No new long-lived branch or stable release is created. Publication uses an expected-head lease.

Implemented the bounded standalone workflow in [CSV_IMPORT.md](CSV_IMPORT.md): read-only CSV sample, explicit seven scalar type mappings, whole-file validation with bounded row/column diagnostics, digest-bound confirmation and a separate database/Table view. Database page, properties, records/backing pages, values and a durable operation receipt commit in one transaction with readback checks. Storage/readback/cancellation failures roll back everything. Changed mappings invalidate preview; lost successful responses can be retried through restart and full archive recovery. Existing user data is never a fixture. The UI reuses system fonts, opaque archive panels, responsive labelled controls and immediate feedback, with no new reading/keyboard animation.

Local verification: targeted CSV regressions, Go suite/vet, backend build, frontend production build/lint and extended portable/archive/CLI restore/two-restored-start checks passed before publication. Existing React lint warnings and the 609 KB editor chunk warning remain; no new CSV component lint warnings. Local Chrome failed before assertions at its process-singleton socket (`Operation not permitted`); no local browser pass or screenshot review is claimed. Three CSV browser regressions are added to the focused migration gate and complete suite. This increment requires its own SHA-matched native CI; baseline evidence does not accept new code. Native results are pending until explicitly recorded below.

Remaining/next: first resolve any CSV native/browser regression, then reversible typed database/property/view/record export and Notion ZIP integration. The seven scalar types/1,000-row standalone CSV subset does not close full Notion package mapping, attachments/relations/select metadata, reversible database/view export, 10,000-row scale or manual platform/accessibility acceptance. Real authorized v6/v7 samples remain unavailable. Main remains the published V0.0.1.
