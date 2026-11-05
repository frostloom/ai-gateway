#!/usr/bin/env python3
"""Check synthetic trace IDs against a local, pre-cleanup SQL snapshot."""
import csv,json,pathlib,sys
p=pathlib.Path(sys.argv[1]).resolve()
if 'delayed-reserve-recovery-runs' not in p.parts: raise SystemExit('unexpected output directory')
if not (p/'completed').exists() or not (p/'watch-completed').exists(): raise SystemExit('run or SQL snapshot incomplete')
ids=[]
for rep in (1,2):
 with (p/f'trace-64-32-{rep}.csv').open(newline='') as f:
  for x in csv.DictReader(f):
   if x['status']!='503' or x['valid']!='false':raise SystemExit(f'unexpected HTTP outcome {x}')
   ids.append(x['request_id'])
if len(ids)!=128 or len(set(ids))!=128:raise SystemExit(f'expected 128 unique synthetic IDs, got {len(ids)}')
bills={};events=set()
for line in (p/'watch-final-rows.txt').read_text().splitlines():
 a=line.split('\t')
 if len(a)==3:bills.setdefault(a[0],[]).append((a[1],a[2]))
 elif len(a)==1:events.add(a[0])
 else:raise SystemExit('unexpected SQL row shape')
for id in ids:
 if bills.get(id)!=[('reserve','reversed')]:raise SystemExit(f'unexpected bill for {id}: {bills.get(id)}')
 for phase in ('reserve','reverse'):
  if id+':'+phase not in events:raise SystemExit(f'missing {phase} event for {id}')
 for phase in ('usage','settle'):
  if id+':'+phase in events:raise SystemExit(f'unexpected {phase} event for {id}')
a=json.loads((p/'audit-recovered.json').read_text())
if a['pending']!=0 or a['reversed']!=128 or a['settled']!=0 or a['balance_mismatches']:raise SystemExit(f'unexpected audit {a}')
print(json.dumps({'requests':len(ids),'unique':len(set(ids)),'reversed':a['reversed'],'pending':a['pending'],'balance_mismatches':a['balance_mismatches'],'outbox_unsent':a['outbox_unsent']}))
