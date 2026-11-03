#!/usr/bin/env python3
"""Fail when scripts/benchmark.py regresses past a budget or a baseline.

Absolute budgets live in scripts/performance-budget.json so CI catches a
degradation even without a historical run. Supplying --baseline additionally
applies a relative check: any p50/p95 latency may grow by at most
--max-regression (default 30%), and throughput may drop by at most the same
fraction.

    python scripts/check_performance.py --report performance.json
    python scripts/check_performance.py --report performance.json \
        --baseline release/baseline.json --max-regression 0.30
"""

import argparse
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_BUDGET = ROOT / "scripts" / "performance-budget.json"


def load_json(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def metric(report, section, field):
    entry = report.get(section)
    if not entry:
        return None
    if field == "recordsPerSecond":
        return entry.get(field)
    stats = entry.get("latencyMs") or (entry.get("filter") or {}).get("latencyMs") or {}
    return stats.get(field)


def check_budget(report, budget, failures):
    write = budget.get("write", {})
    mode = write.get("mode")
    if mode and report.get("write", {}).get("mode") != mode:
        failures.append("write.mode is %r, budget expects %r" % (report.get("write", {}).get("mode"), mode))
    minimum = write.get("minRecordsPerSecond")
    actual = metric(report, "write", "recordsPerSecond")
    if minimum is not None:
        if actual is None:
            failures.append("write.recordsPerSecond missing")
        elif actual < minimum:
            failures.append("write.recordsPerSecond %.2f < %.2f" % (actual, minimum))
    for section, limits in budget.items():
        if section == "write":
            continue
        if not report.get(section):
            failures.append("%s missing from report" % section)
            continue
        for field, maximum in limits.items():
            action = "max" if field.startswith("max") else "min"
            mapped = {"maxP50Ms": "p50Ms", "maxP95Ms": "p95Ms",
                      "maxPayloadBytes": "payloadBytes",
                      "minRecordsPerSecond": "recordsPerSecond"}.get(field, field)
            actual = metric(report, section, mapped)
            if actual is None:
                failures.append("%s.%s missing" % (section, field))
            elif action == "max" and actual > maximum:
                failures.append("%s.%s %.2f > %.2f" % (section, field, actual, maximum))
            elif action == "min" and actual < maximum:
                failures.append("%s.%s %.2f < %.2f" % (section, field, actual, maximum))
    return failures


def check_baseline(report, baseline, max_regression, min_relative_ms, failures):
    for section in ("readRecords", "readRecordsFirstPage", "readRecordsDeepPage",
                    "readDatabaseSchema", "clientFilter", "uiEquivalentLoad",
                    "uiEquivalentLoadPaged"):
        for field in ("p50Ms", "p95Ms"):
            new = metric(report, section, field)
            old = metric(baseline, section, field)
            if new is None or old is None or old < min_relative_ms:
                continue
            if new > old * (1 + max_regression):
                failures.append("%s.%s %.2f -> %.2f (+%.0f%% > %.0f%%)" % (
                    section, field, old, new, (new / old - 1) * 100, max_regression * 100))
    old_rate = metric(baseline, "write", "recordsPerSecond")
    new_rate = metric(report, "write", "recordsPerSecond")
    if old_rate and new_rate and new_rate < old_rate * (1 - max_regression):
        failures.append("write.recordsPerSecond %.2f -> %.2f (-%.0f%% > %.0f%%)" % (
            old_rate, new_rate, (1 - new_rate / old_rate) * 100, max_regression * 100))
    return failures


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", required=True, help="benchmark.py JSON report to check")
    parser.add_argument("--budget", default=DEFAULT_BUDGET, help="absolute budget JSON")
    parser.add_argument("--baseline", help="optional prior benchmark JSON")
    parser.add_argument("--max-regression", type=float, default=0.30,
                        help="relative tolerance against --baseline (default 0.30 = 30%%)")
    parser.add_argument("--min-relative-ms", type=float, default=5.0,
                        help="skip relative latency checks on baselines below this p50 (default 5 ms)")
    args = parser.parse_args(argv)

    report = load_json(args.report)
    failures = []
    budget_path = pathlib.Path(args.budget)
    if budget_path.is_file():
        check_budget(report, load_json(budget_path), failures)
    else:
        print("warning: budget file %s not found, skipping absolute checks" % budget_path, file=sys.stderr)
    if args.baseline:
        check_baseline(report, load_json(args.baseline), args.max_regression, args.min_relative_ms, failures)

    if failures:
        print("performance gate FAILED:")
        for failure in failures:
            print("  - " + failure)
        return 1
    print("performance gate passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
