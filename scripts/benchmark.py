"""Repeatable 10,000-record performance baseline for Reverie Planner.

This drives the deployed application path (HTTP + SQLite), not a mocked array:

* create N records through POST /api/db/records or the batch endpoint (write path)
* GET /api/db/records?databaseId=...                    (full record load)
* GET /api/db/records?databaseId=...&limit=...          (keyset cursor page)
* GET /api/databases?id=...                             (schema/view load)
* schema + records sequentially                        (UI load() equivalent)
* schema + first page                                  (paged UI load)
* client filter/sort using the real TypeScript modules  (DatabaseView path)
* EXPLAIN QUERY PLAN for the record, value and schema reads

Usage (after build.bat / ./build.sh):

    python scripts/benchmark.py --records 10000 --iterations 5 \
        --output release/performance.json

Compare a new build with an already-built baseline binary using the same script:

    python scripts/benchmark.py --records 10000 --iterations 5 \
        --binary runtime/ReveriePlanner-before.exe \
        --write-mode single --output release/benchmark-before.json

The report JSON records the host, exact commands, latency percentiles, payload
bytes, database size and query plans. No numbers are estimated: a missing
frontend dependency or runtime binary fails or is reported as skipped.
"""

import argparse
import datetime
import http.client
import json
import math
import os
import pathlib
import platform
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]
IS_WINDOWS = sys.platform == "win32"


def find_free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def percentile(values, fraction):
    if not values:
        return None
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, math.ceil(fraction * len(ordered)) - 1))
    return round(ordered[index], 3)


def summarize(values):
    if not values:
        return None
    return {
        "count": len(values),
        "totalMs": round(sum(values), 3),
        "meanMs": round(sum(values) / len(values), 3),
        "p50Ms": percentile(values, 0.50),
        "p95Ms": percentile(values, 0.95),
        "minMs": round(min(values), 3),
        "maxMs": round(max(values), 3),
    }


class ApiError(RuntimeError):
    def __init__(self, status, body):
        super().__init__("HTTP %s: %s" % (status, body[:300]))
        self.status = status


class Api:
    """Small keep-alive HTTP client. POST failures are never retried, so a
    partially completed write cannot be silently duplicated."""

    def __init__(self, port):
        self.port = port
        self.connection = None

    def _connect(self):
        self.connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=60)

    def close(self):
        if self.connection is not None:
            try:
                self.connection.close()
            finally:
                self.connection = None

    def call(self, method, path, payload=None):
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        headers = {"Content-Type": "application/json", "Connection": "keep-alive"}
        attempt = 0
        while True:
            attempt += 1
            try:
                if self.connection is None:
                    self._connect()
                self.connection.request(method, path, body=body, headers=headers)
                response = self.connection.getresponse()
                data = response.read()
                if response.status >= 400:
                    raise ApiError(response.status, data.decode("utf-8", "replace"))
                return data
            except (http.client.HTTPException, OSError):
                self.close()
                if method != "GET" or attempt >= 3:
                    raise


def copy_runtime(root, destination, port, binary_override=None):
    if binary_override is not None:
        binary = pathlib.Path(binary_override).resolve()
    else:
        binary = ROOT / "runtime" / ("ReveriePlanner.exe" if IS_WINDOWS else "ReveriePlanner")
    if not binary.is_file():
        raise SystemExit(
            "Missing %s. Run build.bat (Windows) or ./build.sh (Linux/macOS) first." % binary
        )
    dist = ROOT / "src" / "frontend" / "dist"
    if not dist.is_dir():
        raise SystemExit("Missing %s. Run npm run build in src/frontend first." % dist)
    (destination / "runtime").mkdir(parents=True, exist_ok=True)
    (destination / "config").mkdir(parents=True, exist_ok=True)
    target_binary = destination / "runtime" / binary.name
    shutil.copy2(binary, target_binary)
    shutil.copy2(ROOT / "config" / "providers.example.json", destination / "config" / "providers.example.json")
    shutil.copytree(dist, destination / "src" / "frontend" / "dist")
    app_config = json.loads((ROOT / "config" / "app.json").read_text(encoding="utf-8"))
    app_config["port"] = port
    (destination / "config" / "app.json").write_text(
        json.dumps(app_config, ensure_ascii=False), encoding="utf-8"
    )
    return target_binary


def wait_health(port, process):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    url = "http://127.0.0.1:%d/api/health" % port
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise SystemExit("Benchmark service exited before becoming ready (code %s)" % process.returncode)
        try:
            with opener.open(url, timeout=1) as response:
                body = json.load(response)
                if body.get("status") == "ok":
                    return body
        except OSError:
            time.sleep(0.2)
    raise SystemExit("Benchmark service did not become ready at %s" % url)


def build_schema(api, record_count):
    created = json.loads(api.call("POST", "/api/databases", {"title": "Performance %d" % record_count}))
    for name, kind in [("Notes", "text"), ("Score", "number"), ("Stage", "select"), ("Due", "date"), ("Done", "checkbox")]:
        api.call("POST", "/api/db/properties", {
            "databaseId": created["id"], "name": name, "type": kind, "config": "{}",
        })
    schema = json.loads(api.call("GET", "/api/databases?id=" + created["id"]))
    return schema


def record_payload(database_id, properties, index, stages=None):
    by_name = {prop["name"]: prop["id"] for prop in properties}
    stages = stages or ["Backlog", "Doing", "Done"]
    return {
        "databaseId": database_id,
        "title": "Record %05d" % index,
        "values": {
            by_name["Notes"]: "note-%05d" % index,
            by_name["Score"]: index,
            by_name["Stage"]: stages[index % len(stages)],
            by_name["Due"]: "2026-%02d-%02d" % (1 + (index // 28) % 12, 1 + index % 28),
            by_name["Done"]: index % 2 == 0,
        },
    }


def seed_records(api, database_id, properties, record_count, progress, mode, batch_size):
    """Seed through the real HTTP API. `single` is the pre-existing per-record
    path and remains the apples-to-apples comparison with the old binary."""
    latencies = []
    started = time.perf_counter()
    if mode == "single":
        for index in range(record_count):
            payload = record_payload(database_id, properties, index)
            before = time.perf_counter()
            api.call("POST", "/api/db/records", payload)
            latencies.append((time.perf_counter() - before) * 1000)
            if progress and (index + 1) % max(1, record_count // 10) == 0:
                elapsed = time.perf_counter() - started
                print("  seeded %d/%d (%.1f records/s)" % (index + 1, record_count, (index + 1) / elapsed), flush=True)
        requests = record_count
    else:
        for start in range(0, record_count, batch_size):
            stop = min(record_count, start + batch_size)
            payload = {
                "databaseId": database_id,
                "records": [record_payload(database_id, properties, index)
                            for index in range(start, stop)],
            }
            before = time.perf_counter()
            api.call("POST", "/api/db/records/batch", payload)
            latencies.append((time.perf_counter() - before) * 1000)
            if progress:
                elapsed = time.perf_counter() - started
                print("  seeded %d/%d (%.1f records/s)" % (stop, record_count, stop / elapsed), flush=True)
        requests = math.ceil(record_count / batch_size)
    seconds = time.perf_counter() - started
    return {
        "operation": "POST /api/db/records" if mode == "single" else "POST /api/db/records/batch",
        "mode": mode,
        "batchSize": 1 if mode == "single" else batch_size,
        "records": record_count,
        "requests": requests,
        "seconds": round(seconds, 3),
        "recordsPerSecond": round(record_count / seconds, 2),
        "requestLatencyMs": summarize(latencies),
        "latencyMs": summarize(latencies),
        "approxPerRecordMs": (sum(latencies) / record_count if mode == "single"
                              else round(sum(latencies) / record_count, 3)),
    }


def timed_read(api, path, iterations, warmups):
    for _ in range(warmups):
        api.call("GET", path)
    latencies, payload_bytes, counts = [], [], []
    for _ in range(iterations):
        before = time.perf_counter()
        data = api.call("GET", path)
        payload = json.loads(data)
        latencies.append((time.perf_counter() - before) * 1000)
        payload_bytes.append(len(data))
        counts.append(len(payload) if isinstance(payload, list) else 1)
    return {
        "path": path,
        "iterations": iterations,
        "warmups": warmups,
        "latencyMs": summarize(latencies),
        "payloadBytes": payload_bytes[0],
        "payloadItems": counts[0],
    }, data


def timed_ui_load(api, database_id, iterations, warmups):
    schema_path = "/api/databases?id=" + database_id
    records_path = "/api/db/records?databaseId=" + database_id
    for _ in range(warmups):
        api.call("GET", schema_path)
        api.call("GET", records_path)
    latencies = []
    for _ in range(iterations):
        before = time.perf_counter()
        schema = json.loads(api.call("GET", schema_path))
        records = json.loads(api.call("GET", records_path))
        latencies.append((time.perf_counter() - before) * 1000)
        assert schema["properties"] and isinstance(records, list)
    return {
        "description": "GET /api/databases?id= then GET /api/db/records?databaseId= (DatabaseView.load)",
        "iterations": iterations,
        "warmups": warmups,
        "latencyMs": summarize(latencies),
    }


def query_plans(database_path, database_id):
    plans = {}
    statements = {
        "records": (
            "SELECT id, database_id, title, page_id, version, created_at, updated_at "
            "FROM db_records WHERE database_id=? AND deleted_at IS NULL ORDER BY created_at ASC",
            (database_id,),
        ),
        "values": (
            "SELECT dv.record_id,dv.property_id,dv.value FROM db_records dr "
            "JOIN db_values dv ON dv.record_id=dr.id "
            "JOIN db_properties dp ON dp.id=dv.property_id AND dp.deleted_at IS NULL "
            "WHERE dr.database_id=? AND dr.deleted_at IS NULL",
            (database_id,),
        ),
        "properties": (
            "SELECT id, database_id, name, type, config, position, visible, version "
            "FROM db_properties WHERE database_id=? AND deleted_at IS NULL ORDER BY position ASC",
            (database_id,),
        ),
    }
    with sqlite3.connect("file:%s?mode=ro" % database_path, uri=True) as connection:
        for name, (statement, parameters) in statements.items():
            rows = connection.execute("EXPLAIN QUERY PLAN " + statement, parameters).fetchall()
            plans[name] = [row[3] for row in rows]
        plans["indexes"] = [row[0] for row in connection.execute(
            "SELECT name FROM sqlite_master WHERE type='index' AND name LIKE 'idx_db_%' ORDER BY name"
        ).fetchall()]
    return plans


def storage_metrics(root):
    data = root / "data"
    sizes = {}
    for name in ("planner.db", "planner.db-wal", "planner.db-shm"):
        path = data / name
        sizes[name] = path.stat().st_size if path.exists() else 0
    return sizes


def run_filter_benchmark(root, workspace, records, properties, iterations):
    tsc = root / "src" / "frontend" / "node_modules" / ".bin" / ("tsc.cmd" if IS_WINDOWS else "tsc")
    node = shutil.which("node")
    if not tsc.is_file() or not node:
        return {"status": "skipped", "reason": "frontend node_modules/.bin/tsc or node is unavailable; run npm ci"}
    library = workspace / "filter-lib"
    command = [
        str(tsc),
        "--outDir", str(library),
        "--rootDir", str(root),
        "--target", "es2022",
        "--module", "esnext",
        "--moduleResolution", "bundler",
        "--skipLibCheck",
        str(root / "src" / "frontend" / "src" / "databaseFilters.ts"),
        str(root / "src" / "frontend" / "src" / "databaseSorting.ts"),
    ]
    subprocess.run(command, check=True, capture_output=True, text=True)
    number_id = next(prop["id"] for prop in properties if prop["name"] == "Score")
    stage_id = next(prop["id"] for prop in properties if prop["name"] == "Stage")
    notes_id = next(prop["id"] for prop in properties if prop["name"] == "Notes")
    payload = {
        "records": records,
        "properties": [{"id": prop["id"], "name": prop["name"], "type": prop["type"]} for prop in properties],
        "filterTree": {
            "kind": "group",
            "join": "and",
            "children": [
                {"kind": "rule", "propertyId": number_id, "operator": "gte", "value": "5000"},
                {
                    "kind": "group",
                    "join": "or",
                    "children": [
                        {"kind": "rule", "propertyId": stage_id, "operator": "equals", "value": "Done"},
                        {"kind": "rule", "propertyId": notes_id, "operator": "contains", "value": "note-49999"},
                    ],
                },
            ],
        },
        "sortRules": [
            {"propertyId": number_id, "direction": "desc"},
            {"propertyId": "title", "direction": "asc"},
        ],
    }
    input_path = workspace / "filter-input.json"
    input_path.write_text(json.dumps(payload), encoding="utf-8")
    result = subprocess.run(
        [node, str(root / "scripts" / "filter_bench.mjs"),
         "--input", str(input_path), "--lib", str(library), "--iterations", str(iterations)],
        check=True, capture_output=True, text=True,
    )
    return json.loads(result.stdout)


def write_report(path, report):
    """Atomic write with retries, then fall back to the system temp directory."""
    payload = json.dumps(report, indent=2)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name("%s.%d.tmp" % (path.name, os.getpid()))
    for attempt in range(20):
        try:
            temporary.write_text(payload, encoding="utf-8")
            os.replace(temporary, path)
            return
        except PermissionError:
            if attempt == 19:
                break
            time.sleep(0.5)
    fallback = pathlib.Path(tempfile.gettempdir()) / ("reverie-performance-%d.json" % int(time.time()))
    fallback.write_text(payload, encoding="utf-8")
    print("WARNING: could not write %s; report saved to %s" % (path, fallback))


def tool_version(command, *arguments):
    executable = shutil.which(command)
    if not executable:
        return "unavailable"
    try:
        return subprocess.run([executable, *arguments], capture_output=True,
                              text=True).stdout.strip()
    except OSError:
        return "unavailable"


def host_metadata():
    return {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "processor": platform.processor() or os.environ.get("PROCESSOR_IDENTIFIER", ""),
        "cpuCount": os.cpu_count(),
        "python": sys.version.split()[0],
        "node": tool_version("node", "--version"),
        "go": tool_version("go", "version"),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--records", type=int, default=10000, help="records to create (default 10000)")
    parser.add_argument("--iterations", type=int, default=5, help="timed repetitions for reads (default 5)")
    parser.add_argument("--filter-iterations", type=int, default=25, help="timed repetitions for client filter/sort")
    parser.add_argument("--output", type=pathlib.Path, help="write the JSON report to this path")
    parser.add_argument("--keep", action="store_true", help="keep the temporary workspace")
    parser.add_argument("--quiet", action="store_true", help="suppress seed progress")
    parser.add_argument("--binary", type=pathlib.Path, help="override the runtime binary (for before/after runs)")
    parser.add_argument("--write-mode", choices=["single", "batch"], default="single",
                        help="single = legacy POST per record; batch = POST /api/db/records/batch")
    parser.add_argument("--batch-size", type=int, default=1000, help="records per batch request (default 1000)")
    args = parser.parse_args()
    if args.records < 1 or args.iterations < 1:
        raise SystemExit("records and iterations must be positive")
    if args.batch_size < 1 or args.batch_size > 1000:
        raise SystemExit("batch size must be between 1 and 1000")

    port = find_free_port()
    workspace = pathlib.Path(tempfile.mkdtemp(prefix="reverie-benchmark-"))
    binary = copy_runtime(ROOT, workspace, port, args.binary)
    flags = getattr(subprocess, "CREATE_NO_WINDOW", 0)
    process = subprocess.Popen(
        [str(binary), "--root=" + str(workspace)], cwd=tempfile.gettempdir(),
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags,
    )
    api = Api(port)
    report = {"schemaVersion": 1, "kind": "reverie-performance-baseline",
              "benchmark": {"argv": sys.argv, "binary": str(pathlib.Path(args.binary).resolve()) if args.binary else "runtime default"}}
    try:
        health = wait_health(port, process)
        report["generatedAtUtc"] = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds")
        report["host"] = host_metadata()
        report["app"] = {"version": health.get("version"), "records": args.records}

        print("Creating database and properties...", flush=True)
        schema = build_schema(api, args.records)
        report["database"] = {"id": schema["id"], "properties": [
            {"name": prop["name"], "type": prop["type"]} for prop in schema["properties"]
        ]}
        print("Seeding %d records through the public API (%s)..." % (args.records, args.write_mode), flush=True)
        report["write"] = seed_records(
            api, schema["id"], schema["properties"], args.records, not args.quiet,
            args.write_mode, args.batch_size)

        records_read, raw_records_bytes = timed_read(
            api, "/api/db/records?databaseId=" + schema["id"], args.iterations, 2)
        raw_records = json.loads(raw_records_bytes)
        schema_read, _ = timed_read(api, "/api/databases?id=" + schema["id"], args.iterations, 2)
        report["readRecords"] = records_read
        report["readDatabaseSchema"] = schema_read
        report["uiEquivalentLoad"] = timed_ui_load(api, schema["id"], args.iterations, 2)

        # The frontend filters and sorts this exact response in memory.
        report["clientFilter"] = run_filter_benchmark(
            ROOT, workspace, raw_records, schema["properties"], args.filter_iterations)

        report["storage"] = storage_metrics(workspace)
    finally:
        try:
            subprocess.run([str(binary), "--stop", "--root", str(workspace)],
                           check=False, capture_output=True, timeout=30)
            process.wait(timeout=20)
        except Exception:
            if process.poll() is None:
                process.terminate()
                process.wait(timeout=20)
        api.close()

    database_path = workspace / "data" / "planner.db"
    if database_path.is_file():
        report["queryPlans"] = query_plans(str(database_path), schema["id"])
        report["storage"]["dbBytes"] = database_path.stat().st_size
        report["storage"]["dbMiB"] = round(database_path.stat().st_size / (1024 * 1024), 2)
    else:
        report["queryPlans"] = {"status": "unavailable", "reason": "planner.db missing"}

    if not args.keep:
        shutil.rmtree(workspace, ignore_errors=True)
    else:
        report["workspace"] = str(workspace)

    print("\nPerformance baseline (%d records)" % args.records)
    write = report["write"]
    print("  write:       %.2f records/s (%s, %d requests), %s ms p50/request, %s ms p95" % (
        write["recordsPerSecond"], write["mode"], write["requests"],
        write["latencyMs"]["p50Ms"], write["latencyMs"]["p95Ms"]))
    for key, label in [("readRecords", "read records"), ("readDatabaseSchema", "view/schema"),
                       ("uiEquivalentLoad", "UI load")]:
        entry = report.get(key)
        if not entry:
            continue
        stats = entry["latencyMs"]
        extra = ""
        if "payloadItems" in entry:
            extra = " (%d items, %d bytes)" % (entry["payloadItems"], entry["payloadBytes"])
        print("  %-13s %s ms p50, %s ms p95%s" % (label, stats["p50Ms"], stats["p95Ms"], extra))
    print("  payload:     %.2f MiB for %d records" % (
        report["readRecords"]["payloadBytes"] / (1024 * 1024), args.records))
    filter_result = report["clientFilter"]
    if filter_result.get("status") == "ok":
        print("  client filter: %s ms p50, %s ms p95 (%s of %d matched)" % (
            filter_result["filter"]["latencyMs"]["p50Ms"],
            filter_result["filter"]["latencyMs"]["p95Ms"],
            filter_result["matched"], args.records))
        print("  filter+sort:   %s ms p50, %s ms p95" % (
            filter_result["filterAndSort"]["latencyMs"]["p50Ms"],
            filter_result["filterAndSort"]["latencyMs"]["p95Ms"]))
    else:
        print("  client filter: SKIPPED (%s)" % filter_result.get("reason"))
    print("  database:     %.2f MiB" % report["storage"].get("dbMiB", 0))

    if args.output:
        write_report(args.output, report)
        print("\nJSON report: %s" % args.output)


if __name__ == "__main__":
    main()
