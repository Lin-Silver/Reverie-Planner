# Performance baseline — 10,000 records

Measured on 2026-11-03 with the re-runnable harness in `scripts/benchmark.py`. This is **data-layer evidence only**: it does not measure DOM rendering, virtual scrolling or the five layouts at 10k rows, so it does not close the Phase 4 "pagination/virtualization + 10k benchmark" checklist item by itself.

**Scope warning — the batch endpoint is an API capability, not a user path.** `POST /api/db/records/batch` is exercised only by `scripts/benchmark.py`. Nothing in the UI calls it today: `+ New` still sends one `POST /api/db/records`, TSV paste uses `/api/db/records/bulk` (edits existing rows), CSV and database-package imports already commit their whole import in a single transaction with an `operationKey`, and Markdown import only writes pages. Its throughput therefore describes what an external bulk writer can achieve against the HTTP API; it does **not** describe the import experience in the product, and it does not make any existing import path faster.

## Environment and scope

| Item | Value |
| --- | --- |
| Host | Windows 10 `10.0.26300`, AMD64, 32 logical CPUs (`AMD64 Family 25 Model 97 Stepping 2`) |
| Toolchain | Go `1.26.0 windows/amd64`, Python `3.10.11`, Node `v20.19.5` locally (CI uses Node 22) |
| App version | `0.0.1`, built via `build.bat` (`go build -trimpath -buildvcs=false`) |
| Source | `lane/release` based on `dev` `6e1d2e65b65cd33e825f83d45cf5972c6e67454e` |
| Dataset | 6 properties: title `Name`, text `Notes`, number `Score`, select `Stage`, date `Due`, checkbox `Done`; **no** relations, formulas or rollups |
| Records | 10,000, created one-by-one through the public `POST /api/db/records` API (each record also creates a page) |
| Transport | loopback HTTP keep-alive; server and client on the same machine |

Not covered: first-load DOM render of 10k rows, Board grouping, Calendar placement, Gallery/List render, relations/formulas/rollups, concurrent clients, migrations, backups, macOS/Linux runs. Latency below is machine-specific; the method and dataset are what should be compared across runs.

## Reproduce

```powershell
build.bat   # npm ci + npm run build + Go build
python scripts/benchmark.py --records 10000 --iterations 5 --filter-iterations 25 `
  --output release/benchmark-10000.json
```

Prerequisites: `src/frontend/node_modules` (the script compiles the real `databaseFilters.ts` and `databaseSorting.ts` with the local `tsc`). If node_modules/tsc is missing, the filter section is reported as `skipped`, never estimated. The same command runs in `.github/workflows/benchmark.yml` (manual or weekly) and uploads the JSON report.

## Results — reference run `2026-11-03T06:33:01Z`

Raw report: `release/benchmark-10000.json` (gitignored). Percentiles are nearest-rank over the stated repetitions.

| Measurement | Result |
| --- | --- |
| Create 10,000 records (`POST /api/db/records`) | 67.503 s total, **148.14 records/s**; p50 **4.796 ms**, p95 **9.210 ms**, max 797.107 ms |
| Read full record set (`GET /api/db/records?databaseId=`) | p50 **161.821 ms**, p95 **169.359 ms**, min 142.336 ms; payload **4,256,157 B (4.06 MiB)** |
| Load schema/views (`GET /api/databases?id=`) | p50 **0.331 ms**, p95 **0.428 ms**; payload 1,321 B |
| UI-equivalent load (schema then records, as `DatabaseView.load`) | p50 **169.874 ms**, p95 **182.716 ms** |
| Client filter, real `matchesFilters` (nested AND/OR, 3 rules) | p50 **1.362 ms**, p95 **1.508 ms**; 1,667 of 10,000 matched |
| Client sort, real `sortDatabaseRecords` (`Score` desc, title asc) | p50 **0.281 ms**, p95 **0.412 ms** |
| Filter + sort (actual view pipeline) | p50 **1.410 ms**, p95 **1.432 ms** |
| SQLite storage | `planner.db` 15,237,120 B (14.53 MiB) + WAL 4,227,152 B at measurement; 15,269,888 B (14.56 MiB) after shutdown |

Observed spread over four repeated 10k runs on the same machine (same script, 5 read iterations): create **148–336 records/s**, full record read p50 **136–175 ms**, schema load p50 **0.295–0.442 ms**, filter p50 **1.348–1.383 ms**. Seed throughput is the most environment-sensitive metric (antivirus/disk activity moves it by more than 2×); treat it as a range, not a fixed capacity claim. The 148 rec/s figure was recorded while three parallel lanes were building and packaging on the same disk; a later clean protocol (one discarded warm-up, three fresh-root runs) measured **242–268 rec/s** on the same commit.

### Batch write path — `POST /api/db/records/batch` (API only)

Measured with `--write-mode batch --batch-size 1000` on the same machine, same protocol (warm-up discarded, 3 runs, fresh root/DB/port each):

| Measurement | Result |
| --- | --- |
| Create 10,000 records (10 batch requests) | median **0.750 s**, **13,336 records/s**; min/max 0.737–0.877 s |
| Per-request latency | p50 **64.6 ms** per 1,000-record batch |
| Full record read (unchanged) | p50 **126.6–128.8 ms**; client filter p50 1.40–1.43 ms |

Two honest qualifications: (1) this is **not** an import speed-up in the product — no UI or import path calls the endpoint, so no user-visible flow gets faster; (2) the earlier framing that "importing 10k records takes ~35 s" was an artifact of the benchmark seeding one record per POST, not a property of the import features. Batch creation is also not a substitute for the package/CSV import paths, which already commit once per import.

### Query plans (from the same report)

```
records:    SEARCH db_records USING INDEX idx_db_records_live (database_id=? AND deleted_at=?)
values:     SEARCH dr USING INDEX idx_db_records_live (database_id=? AND deleted_at=?)
            SEARCH dv USING INDEX sqlite_autoindex_db_values_1 (record_id=?)
            SEARCH dp USING INDEX sqlite_autoindex_db_properties_1 (id=?)
properties: SEARCH db_properties USING INDEX idx_db_properties_live (database_id=? AND deleted_at=?)
```

The record, value and property reads use the intended live/unique indexes; there is no full table scan for these queries.

## Interpretation

- The **view/schema load is cheap** (0.33 ms, 1.3 KB), but every database open also downloads the **entire record set** (4.06 MiB at 10k) in one JSON response. That is the dominant data-layer cost (~162 ms locally) and it grows linearly with records.
- Client-side filter and sort are **not the bottleneck** at 10k (≈1.4 ms combined for the tested nested filter + two-key sort). Network payload and DOM rendering dominate; UI render time is unmeasured, and no pagination/virtualization exists yet.
- Record creation through the legacy endpoint is one HTTP request and one SQLite `synchronous=FULL` commit per record (page + record + values), so it is fsync-bound at roughly 150–340 records/s depending on disk contention. That is why the batch endpoint exists. Neither number describes a product import: CSV/package imports commit once per import, and the batch endpoint is currently not wired into any UI path.
- Storage is roughly **1.5 KB/record** for 5 scalar values plus a page, before attachments.

**Checklist effect:** the harness satisfies the "measurable benchmark with 10k+ records" evidence requirement, but [DATABASE_COMPLETION_PLAN.md](DATABASE_COMPLETION_PLAN.md) Phase 4 still stays unchecked because its item also requires efficient pagination/virtualization and the UI at that scale is unverified.
