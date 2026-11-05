#!/usr/bin/env python3
"""Correlate a completed isolated mock-trace run with reserve error diagnostics."""
import argparse
import collections
import json
import pathlib
import re
import subprocess
import sys

parser = argparse.ArgumentParser()
parser.add_argument('run', type=pathlib.Path)
run = parser.parse_args().run.resolve()
base = pathlib.Path(__file__).resolve().parent
config = json.loads((run / 'config.json').read_text())
if not config.get('trace_mock') or not (run / 'completed').exists():
    raise SystemExit('requires completed isolated run with trace_mock')
trace = json.loads(subprocess.run([sys.executable, str(base/'analyze_mock_trace.py'), str(run)], check=True, capture_output=True, text=True).stdout)
errors = {}
for line in (run/'gateway.log').read_text().splitlines():
    if 'msg="DIAG reserve"' not in line:
        continue
    match = re.search(r'request_id=(\S+) grpc_code=(\S+) elapsed_ms=(\d+)', line)
    if not match:
        raise SystemExit('reserve diagnostic lacks request identity or status')
    errors.setdefault(match.group(1), []).append({'code': match.group(2), 'elapsed_ms': int(match.group(3))})
rows = []
for item in trace['traces']:
    for anomaly in item['anomalies']:
        if anomaly['reserve'] == 'pending' and anomaly['usage'] == 'none':
            rows.append({'request_id': anomaly['request_id'], 'http': anomaly['http'],
                         'mock_receptions': anomaly['mock_receptions'],
                         'reserve_errors': errors.get(anomaly['request_id'], [])})
if len({row['request_id'] for row in rows}) != len(rows):
    raise SystemExit('duplicate pending IDs in trace audits')
summary = {'pending_without_usage_count':len(rows),
           'with_reserve_error':sum(bool(row['reserve_errors']) for row in rows),
           'mock_received':sum(row['mock_receptions'] > 0 for row in rows),
           'error_codes':dict(collections.Counter(e['code'] for row in rows for e in row['reserve_errors'])),
           'reserve_error_ids':len(errors),'mock_log_entries':trace['mock_log_entries'],
           'duplicate_mock_receptions':trace['duplicate_mock_receptions'], 'rows':rows}
print(json.dumps(summary, ensure_ascii=False, indent=2))
