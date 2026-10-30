# Text-only Markdown package import (dev)

Settings → Local workspace → Bring in Markdown pages provides a bounded, offline
ZIP import. This is an initial **text-only subset**, not complete Notion migration.
Keep the original export and a Reverie workspace archive before any migration.

## Accepted input and preview

- ZIP containing 1–100 UTF-8 `.md` / `.markdown` files. Limit: 500 ZIP entries,
  1 MiB expanded per entry, 16 MiB compressed and expanded for the package.
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

Any non-Markdown file (including CSV, images, PDFs, nested ZIPs and exporter
metadata) blocks the whole import and is listed. Embedded media, missing local
link targets, root-relative links, heading fragments and unsafe link schemes also
block import. HTTP/HTTPS/mail links are retained; nothing is fetched remotely.

Markdown conversion can simplify or omit editor-unsupported constructs, HTML,
Notion callouts/metadata and rich formatting. Source preview is not a guarantee
of lossless conversion. This does not yet migrate attachments, CSV database types,
relations, independent views, comments or complete Notion packages. Do not delete
source exports. Complete reversible export is a separate open acceptance item.

## Transactions, retry and portability

All new pages, parent assignments, rewritten links and the import receipt are
written in one SQLite transaction. Storage errors or cancellation before commit
roll back the whole batch. A cancelled/lost response after commit may mean the
batch completed; retry with the **same selected file and operation** to retrieve
the same page IDs, rather than creating duplicates. Selecting another file or
reloading starts a separate operation and may intentionally create another copy.

Receipts live in `app_state` with the documents, survive restart and travel through
workspace archive/CLI restoration. They are not expiring session state. A retry
of an old completed operation does not recreate pages later moved to trash.
No schema migration, filesystem attachment publication, provider changes or real
user-data fixtures are introduced by this increment.

## Verification

Backend tests cover nested paths/Unicode/Notion titles, link resolution, malformed
and unsupported inputs, limits, unchanged existing pages, preview/digest checks,
transaction failure at both page and receipt writes, cancellation, restart and
idempotent retry. The native portable test imports synthetic pages, retries after
restart, archives them, restores via CLI and verifies pages/receipts on two further
starts. Three new browser tests cover source preview, formatted text, navigation
with an unsaved draft, lost-success retry, explicit blockers and narrow keyboard
operation. Existing browser assertions remain unchanged.

The development plan records exact local/native results; implementation or a
collected screenshot alone is not cross-platform/manual accessibility acceptance.
