# Document tree packages (dev)

Settings → Local workspace → Transfer a document tree exports saved personal
writing into `Reverie-documents.reverie.json`. This is an offline transfer format
for a bounded document subset, **not a complete backup or Notion/database format**.
Use a workspace archive for history, trash, databases, settings and favorites.

## What travels

- One selected saved page and all its descendants, titles and icons.
- Supported editor block JSON, including styled text, lists, headings, tables,
  code, quotes, local raster images and file blocks. JSON numeric precision is
  retained while references are rewritten. No Markdown conversion is involved.
- Page links within the selected tree and exact structured references to local
  page assets. A literal URL in ordinary text is not treated as an attachment.
- Referenced page assets, including shared bytes owned by an ancestor outside the
  tree, and all unreferenced assets owned by included pages. The source owner must
  be live; unavailable/corrupt bytes fail export. PNG/JPEG/GIF/WebP, PDF, TXT and
  Office downloads follow the Markdown package format and content-type policy.

The version-1 JSON envelope declares `format: "reverie-documents"`, `version: 1`,
`rootId`, `pages` and `assets`. Pages contain source IDs, parent IDs, title, icon
and the editor content string. Assets contain source IDs, names, content types,
SHA-256 and base64 bytes. No paths are extracted and no URLs are fetched. An
export reads one SQLite transaction so concurrent writes cannot mix snapshots.

## Boundaries and confirmation

Limits: 100 pages; 2 MiB per document, 16 MiB aggregate document content; 500
assets, 8 MiB per asset and 16 MiB aggregate asset bytes; 48 MiB JSON request.
Limits also apply after block/reference serialization; export checks the actual
encoded JSON/base64 download size so it cannot publish a file above the import
request cap. An over-limit tree is refused without changing source data.
Export and import validate limits, UTF-8, the parent tree, duplicate IDs/JSON keys,
known format/version, supported types/URLs, asset format and checksum. Unknown
package fields, cycles, outside-tree page links, covers, legacy non-block text,
database-owned/linked pages, audio/video and remote embedded media are refused
with a reason. Ordinary HTTP/HTTPS/mail links remain links.

Preview is read-only: page titles, hierarchy, raw editor blocks and asset
counts/sizes are shown; attachment payloads are not echoed. Confirming creates
an `Imported documents` wrapper and new page/asset IDs. Existing documents,
matching titles, projects and providers are never overwritten. Project
membership, versions, timestamps, histories and favorites are intentionally not
imported. All imported bytes are owned by the copied tree root; trash hides
its downloads, restore recovers them, and the full workspace archive retains them.
Keep the original package. It contains private writing and attachment bytes.

Pages, attachments and the persistent operation receipt share a transaction.
Failure/cancellation before commit rolls back everything. A lost response after
commit may still mean success: retry with the **same selected file/operation**
to recover the result, including after backend restart or full workspace restore.
A different selection or reloaded UI starts a fresh intentional copy. The digest
binds confirmation to the previewed bytes; a stale digest returns HTTP 409.

## Acceptance

Backend tests cover inverse-ID round trips of blocks/typed numbers, hierarchy,
icons, shared and unreferenced bytes, restart/receipt/archive restoration,
transaction failures at page/asset/receipt writes, cancellation, scoped asset
collection, unsupported dependencies, malformed packages and serialization/base64 expansion
that would otherwise produce an unimportable file. Browser regressions
exercise the real download → preview → lost-response retry → independent linked
copy → reload flow and narrow/reduced-motion keyboard confirmation. Portable
native checks repeat export/import/retry/download after restart, CLI workspace
restore and two restored starts. See DEVELOPMENT_PLAN.md for exact native CI
results; implemented tests alone are not an acceptance claim.

Typed CSV databases, reversible database/view exports, complete Notion migration,
cover support and broader manual accessibility remain open. This document-only
increment does not close the broader M2 migration/export checklist.
