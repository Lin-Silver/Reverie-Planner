# Database performance benchmark

Reproducible smoke benchmark for the database records read path. Run:

```bash
go test ./src/backend -run TestDatabaseRecordsBenchmark10k -v
```

## Method
- Seeds 10,000 `db_records` rows in one transaction for a single database.
- Issues one paginated read: `GET /api/db/records?databaseId=…&limit=100&offset=5000`.
- Asserts the response `total` equals the seeded count and the page returns 100 rows.
- Logs seed time and paginated read time; fails only if a page read exceeds a generous 5s smoke bound (guards against a full-table scan per page, not a hardware-independent threshold).

## Reference result (this environment)
- Hardware: AMD EPYC 7713 64-Core (2 vCPU allocated), ~8.3 GiB RAM, Linux sandbox.
- Toolchain: Go 1.26.0, `modernc.org/sqlite`, WAL + `synchronous=FULL`, single connection.
- Seed 10,000 rows: ~224 ms.
- Paginated read (100 rows @ offset 5000, includes `COUNT(*)`): ~13 ms.

Numbers are indicative, not a fixed acceptance threshold; re-run on target hardware and record the output. Large-table virtualization in the UI is tracked separately and does not change these backend figures.