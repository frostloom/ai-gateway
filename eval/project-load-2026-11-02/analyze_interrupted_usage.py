#!/usr/bin/env python3
"""Correlate a saved per-round trace audit with synthetic gateway diagnostics."""
import argparse
import collections
import json
import pathlib
import re

parser=argparse.ArgumentParser()
parser.add_argument('run',type=pathlib.Path)
parser.add_argument('--trace',required=True,help='Exact saved trace-audit JSON filename')
args=parser.parse_args();run=args.run.resolve();config=json.loads((run/'config.json').read_text())
if not config.get('hold_unknown') or '/' in args.trace or not args.trace.startswith('trace-audit-') or not args.trace.endswith('.json'):
    raise SystemExit('requires synthetic hold-unknown run and an exact trace-audit filename')
audit=json.loads((run/args.trace).read_text())
if audit.get('anomalies_truncated') or audit.get('requests',0)<1:
    raise SystemExit('incomplete audit')
logs=collections.defaultdict(list)
for line in (run/'gateway.log').read_text().splitlines():
    if 'msg="settle failed, reconciliation will catch up"' not in line and 'msg="usage report not acknowledged, bill left pending for reconciliation"' not in line:
        continue
    m=re.search(r'request_id=(\S+)',line)
    if not m: raise SystemExit('billing failure log lacks request ID')
    logs[m.group(1)].append('settle_error' if 'msg="settle failed' in line else 'usage_unacknowledged')
rows=[]
for item in audit.get('anomalies',[]):
    if item.get('http')=='200' and item.get('reserve')=='pending':
        rows.append({'request_id':item['request_id'],'usage':item['usage'],'settle_rows':item['settle_rows'],'diagnostics':logs.get(item['request_id'],[])})
print(json.dumps({'run':str(run),'trace_audit':args.trace,'completed':(run/'completed').exists(),
                  'requests':audit['requests'],'groups':audit['groups'],'http_200_pending':rows},ensure_ascii=False,indent=2))
