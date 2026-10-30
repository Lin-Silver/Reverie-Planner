# Database packages (dev)

Settings → Local workspace → Transfer a database downloads a readable
`Reverie-database.reverie.json`. Select a package, preview its schema, views and
five sample records, then confirm an independent copy. No AI, account or network
conversion is required. Existing data is never matched, merged or overwritten.

## Supported contract

Version 1 (`format: reverie-database`) supports one live database, 1–32 properties,
1–32 independent views and 0–1,000 live records in at most 16 MiB of UTF-8 JSON.
Properties support Title, Text, Number, Checkbox, Date, URL and Email with empty
configuration. Names, types, visibility and strictly increasing positions travel.
Scalar validation follows the CSV subset; numbers use ordinary floating-point
precision. Null, absent, empty text, zero and false remain distinct where stored.

Database and record backing pages retain title, icon and supported editor blocks.
Links between included backing pages are rewritten to their new page IDs. Other
local dependencies block transfer. Supported view types retain name, type,
position, text search, sort/sortRules, filterTree, column order/width/visibility,
group/date selectors and Gallery property settings. Every property reference is
validated and rewritten structurally. A literal property ID inside free text is
never replaced. Unknown view settings are blockers rather than guessed extensions.

Relationships, formulas, rollups, select metadata, configured properties,
attachments, nested backing-page children, covers, linked backing pages and
outside-package links are refused with a reason. Record documents with images or
files require a full workspace archive. Trash, history, timestamps/versions,
project membership, external linked pages and application/provider settings are
outside this live-copy format. Keep a complete workspace archive for them.
A database with recoverable external attachment rows is also refused, even when
those files are currently trashed. Document-tree packages remain a separate format.
This increment does not claim full Notion ZIP or database/asset compatibility.

## API, consistency and recovery

- `GET /api/database-package?databaseId=…` reads database schema, records, values
  and backing pages in one SQLite read transaction; it validates the actual
  serialized file against the import limit before offering a download.
- `POST /api/database-package?preview=1` validates the exact file read-only and
  returns a SHA-256 digest, schema, views, record count and five-record sample.
- Confirm with the same file body and `?digest=…&operationKey=…`. Changed bytes
  require another preview. Duplicate JSON keys, unknown envelope fields, invalid
  types/references, unsupported formats and oversized files are rejected.
- New page, database, property, view and record IDs prevent shared editing.
  Page links and view property references are remapped. Creation and a durable
  receipt commit in one transaction, with page/value/schema/receipt readback.
  Storage/readback/cancellation failures before commit roll back the whole copy.
- If a successful response is interrupted, retry the same selected file without
  resetting the form. Its operation key retrieves the committed IDs, including
  after restart and full archive recovery. A new selection intentionally starts
  a different copy. Retrying an import later moved to trash does not recreate it.

Export uses explicit data structures and scoped database queries. It never reads
provider files, runtime tokens or unrelated app_state keys. This excludes provider
configuration, not arbitrary private text a person intentionally wrote in a page.
Synthetic tests seed unique fake provider/private-state canaries, check exports
exclude them and verify provider file bytes remain unchanged after import.

## Verification status

The increment adds inverse-ID roundtrip, independent-view, restart/archive,
invalid input/dependency, digest, provider-canary and fault-injected transaction
regressions. Browser tests cover actual download → preview → lost-response retry
→ rendered multiline cells → reload, plus malformed input and 390px keyboard
confirmation. These are implemented checks; see DEVELOPMENT_PLAN.md for actual
SHA-matched native results. Full Notion migration, assets/advanced properties,
10,000-record scale and manual accessibility/platform acceptance remain open.

Native acceptance: source `ba43477778c234f2e12538aa984d018b7bf7492c`,
[CI 41082127088](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41082127088).
Linux, Windows and macOS each passed 12/12 migration browser tests and 82/82 full
browser tests, backend/build checks and the extended native restart/archive/CLI
restore/two-restored-start/package workflow. Original snapshot assertions remain.
Manual platform and assistive-technology acceptance are separate open work.
