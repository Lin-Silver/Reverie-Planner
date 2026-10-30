# Typed CSV database import (bounded subset)

Settings → Local workspace → Import a CSV database. No AI, account, upload service or network conversion is used. Choose a file and a new database title, preview, explicitly choose each column type, preview again, then import a separate copy. Preview is read-only. Changing the file/title/mapping invalidates confirmation. Existing pages/databases are never matched, merged or overwritten; duplicate titles are allowed in independent databases.

## Supported format

- A single UTF-8 comma-separated file, optionally with UTF-8 BOM. RFC-style quoted commas, escaped quotes and multiline fields; CRLF is normalized by the CSV parser. Empty physical lines are ignored. Preview row numbers are logical records with header = 1, not physical source lines.
- Up to 2 MiB source, 1,000 data records, 32 columns, 32 KiB per cell. At least one data record. All rows must have the header's column count. The JSON transport is independently bounded at 16 MiB to allow escaping expansion.
- Headers: unique ignoring case, 1–120 bytes, no NUL/line breaks/surrounding whitespace. Database title: non-blank, at most 250 bytes. Exactly one Title column: non-blank cell, at most 500 bytes.
- Initial mapping is first column Title, remaining columns Text. No automatic numeric/date/boolean inference. Type choices apply to the entire column. No skip-column or skip-error mode.

| Type | Accepted values |
| --- | --- |
| Title / Text | Literal strings; preserve whitespace and formula-looking text. Titles cannot be blank or multiline. NUL is refused in every cell. |
| Number | JSON decimal notation including exponents; finite IEEE-754 values in ±9,007,199,254,740,991. No separators/currency/hex/NaN. Keep large IDs and exact decimal/accounting strings as Text; numbers use the app's ordinary floating-point precision. |
| Checkbox | Exactly `true`, `false` or empty; no guessed translations or 0/1 coercion. |
| Date | YYYY-MM-DD or RFC3339, using the existing property validator. |
| URL / Email | Existing HTTP(S) URL / email validator. No fetching or link execution in preview. |

Empty non-text cells are absent, never coerced to zero/false. Non-empty typed cells with surrounding whitespace are refused; choose Text to preserve those bytes. Preview shows up to five unconverted source records, total record count, type choices and up to 100 row/column errors with the full error count. Any error blocks the whole commit. Multiline text uses a textarea in the database table; focusing/blurring without edits preserves the stored writing. The sample is escaped React text on an opaque, selectable, horizontally scrollable table with a keyboard focus target; no source HTML is rendered.

## Safety and retry

`POST /api/import/csv?preview=1` takes `{csv,title,types?}`. The server returns headers, effective types, sample, row count, problems and a SHA-256 digest binding the entire decoded source, title and exact type mapping. `POST /api/import/csv` takes the same payload plus `digest` and a 16–80 character `operationKey` (letters/digits/underscore/hyphen). Every commit reparses/revalidates the complete input and rejects a changed digest.

One SQLite transaction owns the backing database page, ordered properties, independent initial Table view, records, each record's own page, typed values and durable retry receipt. Schema/record/value readback is verified before commit. Any storage/readback/cancellation failure rolls back the transaction. A reused operation key with changed data/mapping is rejected. Retrying the same operation returns the original IDs, even after restart or complete workspace archive/CLI restore. If a completed import was subsequently deleted, a retry refers to that original copy; use recovery, rather than expecting retry to recreate it.

The browser keeps the retry key while the selection/mapping stays unchanged. If the import succeeds but its response is lost, retry without changing fields or reselecting the source. A new selection, title or mapping is a new operation and can intentionally create another copy. Keep the original source file; raw CSV bytes are not stored as a separate attachment. Receipts are not a reusable arbitrary-file duplicate detector.

## Explicitly not complete

Notion ZIP/database-to-document mapping, relations/rollups/formulas, select option metadata, attachments, record writing, formulas from spreadsheet files, reciprocal links and reversible database/view export remain separate tasks. A CSV has no independent view configuration to recover; each import receives its own Table view. This bounded standalone workflow does not close M2's complete Notion package or reversible-export acceptance. It also does not establish 10,000-record performance, exact accounting arithmetic or full manual accessibility/platform acceptance.

## Verification

Synthetic backend regressions cover types/literal preservation, all supported scalar validators, malformed/ambiguous/over-limit sources, bounded diagnostics, changed mapping/digest/key rejection, read-only preview, JSON transport expansion, transaction/storage/readback/cancellation rollback, independent IDs/views, retry/restart and archive restoration. The native portable script imports/retries and checks typed values through source restart, workspace archive, offline CLI restore and two restored starts. Browser tests cover mapping/reconfirmation, lost-success retry, actual database rendering/reload, invalid UTF-8/headers, all-or-nothing validation, keyboard correction and a narrow reduced-motion screenshot. See DEVELOPMENT_PLAN.md for actual SHA-matched execution results; test implementation alone is not acceptance evidence.
