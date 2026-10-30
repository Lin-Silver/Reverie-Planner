# Markdown package import (dev)

Settings → Local workspace → Bring in Markdown pages provides a bounded, offline
ZIP import with bounded local attachments. This is a **partial migration format**, not complete Notion migration.
Keep the original export and a Reverie workspace archive before any migration.

## Accepted input and preview

- ZIP containing 1–100 UTF-8 `.md` / `.markdown` files. Limit: 500 ZIP entries,
  1 MiB expanded per Markdown page, 8 MiB per attachment, 16 MiB compressed and expanded for the package.
- No extraction to user paths. Absolute paths, dot traversal, backslashes, drive
  paths, duplicate/case-ambiguous entries, symlinks, encrypted/corrupt ZIPs and
  invalid text fail validation. Every regular entry is read with a bounded reader.
- A directory named `Parent` uses `Parent.md` (or `.markdown`) as its page parent.
  Otherwise the nearest explicit ancestor page is used; unmatched directories
  do not generate invented content. Preview shows the exact source path and
  parent. Each import has its own `Imported Markdown` container.
- Filename titles lose the optional trailing 32-hex Notion ID. Original paths
  remain authoritative for links. Same titles and repeated independent imports
  create distinct pages; no existing document is overwritten or merged.
- Preview displays original Markdown as plain text. Conversion reuses BlockNote's
  installed parser locally. The server validates converted blocks/links before
  enabling import and repeats validation on commit. Preview writes no pages.
- Encoded relative links to other package Markdown pages are rewritten to local
  `/?page=...` links. Clicking them uses the current unsaved-writing/conflict
  guard. Direct links reopen their page; successful link navigation updates the
  current URL without reloading the editor.

## Explicit limitations

Supported attachments: PNG/JPEG/GIF/WebP images (matching detected image type),
PDF, TXT, DOCX, XLSX and PPTX downloads. Preview lists each path and byte size;
asset bytes are not returned in the preview JSON. Relative Markdown links and
embedded local image blocks are rewritten to local opaque asset URLs. All
supported files, including unreferenced attachments, are retained once per import
and owned by its `Imported Markdown` root. That root includes a selectable link
index for all imported attachments, including files not referenced by a document. Deleting/restoring a child page does not
delete shared bytes. Trashing the root hides its downloads; restoring the root
re-enables them. Assets are retained without automatic pruning for history/trash.

Unsupported files (including CSV, SVG, HTML, scripts, nested ZIPs and exporter
metadata) block the whole import and are listed. An image with the wrong detected
type is also a blocker. Missing local targets, root-relative links, heading
fragments and unsafe schemes block import. Embedded remote images/audio/video are
rejected; ordinary HTTP/HTTPS/mail links are retained. Nothing is fetched remotely.
Raster images may display inline; every other attachment downloads as an
`application/octet-stream` file with `nosniff` and a sandbox CSP. This is format
validation, not malware scanning; keep normal care when opening downloaded files.

Markdown conversion can simplify or omit editor-unsupported constructs, HTML,
Notion callouts/metadata and rich formatting. Source preview is not a guarantee
of lossless conversion. This does not yet migrate arbitrary attachment formats, CSV database types,
relations, independent views, comments or complete Notion packages. Do not delete
source exports. Complete reversible export is a separate open acceptance item.

## Transactions, retry and portability

All new pages, parent assignments, rewritten links, attachment bytes and the import receipt are
written in one SQLite transaction. Storage errors or cancellation before commit
roll back the whole batch. A cancelled/lost response after commit may mean the
batch completed; retry with the **same selected file and operation** to retrieve
the same page IDs, rather than creating duplicates. Selecting another file or
reloading starts a separate operation and may intentionally create another copy.

Receipts live in `app_state` with the documents, survive restart and travel through
workspace archive/CLI restoration. They are not expiring session state. A retry
of an old completed operation does not recreate pages later moved to trash.
Schema 12 stores the bounded imported assets inside SQLite (`page_assets`), so
pages and bytes commit together without a filesystem publication window. Larger
workspaces consequently have larger database snapshots; the 16 MiB per-package
limit remains. Keep a pre-upgrade archive: older binaries reject schema 12.
Assets intentionally use a root ID checked against live pages or page trash,
rather than a cascading page foreign key which would destroy recoverable bytes.
Archive export/restore verifies every stored asset's owner, size, content type and
SHA-256; snapshot checksums cover the BLOBs too. Database record attachments keep
their existing external-file storage. No provider or real user data is changed.

## Verification

Backend tests cover nested paths/Unicode/Notion titles, link resolution, malformed
and unsupported inputs, limits, unchanged existing pages, preview/digest checks,
transaction failure at both page and receipt writes, cancellation, restart and
idempotent retry. The native portable test imports synthetic pages, retries after
restart, archives them, restores via CLI and verifies pages/receipts on two further
starts. Browser regressions cover source preview, formatted text, navigation
with an unsaved draft, lost-success retry, explicit blockers and narrow keyboard
operation, attachment preview/render/download/reload, retry without duplicate
pages, and missing/remote image refusal. Added backend tests cover asset and
receipt failures, cancellation, v11 synthetic upgrade, restart, root trash/restore,
archive roundtrip, and corrupt bytes/owner rejection before restore publication.
The portable script verifies attachment downloads after source and restored-root
restarts. Existing browser assertions remain unchanged.

The development plan records exact local/native results; implementation or a
collected screenshot alone is not cross-platform/manual accessibility acceptance.
