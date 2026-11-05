#!/usr/bin/env python3
"""Join local synthetic load traces with test-only mock reception logs."""
import argparse
import collections
import csv
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("run", type=Path)
args = parser.parse_args()
run = args.run.resolve()
config = json.loads((run / "config.json").read_text())
if not config.get("trace_mock") or not (run / "completed").exists():
    raise SystemExit("requires a completed run with OPEN_LOAD_TRACE_MOCK=1")
seen = collections.Counter(
    line.removeprefix("TRACE_REQUEST_ID ")
    for line in (run / "mock.log").read_text().splitlines()
    if line.startswith("TRACE_REQUEST_ID ")
)
if "" in seen:
    raise SystemExit("mock trace contains an empty request ID")
results = []
for path in sorted(run.glob("trace-*.csv")):
    with path.open(newline="") as f:
        reader = csv.DictReader(f)
        if reader.fieldnames != ["request_id", "status", "valid"]:
            raise SystemExit(f"unexpected CSV format: {path.name}")
        rows = list(reader)
    counts = collections.Counter((r["status"], r["valid"], seen[r["request_id"]]) for r in rows)
    audit_path = run / ("trace-audit-" + path.stem.removeprefix("trace-") + ".json")
    audit = json.loads(audit_path.read_text())
    if audit["requests"] != len(rows) or audit.get("anomalies_truncated", 0):
        raise SystemExit(f"incomplete trace audit: {path.name}")
    anomalies = [dict(item, mock_receptions=seen[item["request_id"]]) for item in audit.get("anomalies", [])]
    results.append({"trace": path.name, "requests": len(rows), "by_http_valid_mock_receptions": [
        {"status": status, "valid": valid, "mock_receptions": receptions, "count": count}
        for (status, valid, receptions), count in sorted(counts.items())
    ], "anomalies": anomalies})
if not results:
    raise SystemExit("no request traces")
print(json.dumps({"run": str(run), "mock_log_entries": sum(seen.values()),
                  "mock_unique_ids": len(seen), "duplicate_mock_receptions": sum(max(0, n-1) for n in seen.values()),
                  "traces": results}, ensure_ascii=False, indent=2))
