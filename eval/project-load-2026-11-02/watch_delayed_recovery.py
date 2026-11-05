#!/usr/bin/env python3
"""Observe only the exact disposable MySQL container named in the arguments."""
import json, pathlib, subprocess, sys, time
name=sys.argv[1];out=pathlib.Path(sys.argv[2]).resolve()
if not name.startswith('project-delayed-reserve-') or not name.endswith('-mysql') or 'delayed-reserve-recovery-runs' not in out.parts:
 raise SystemExit('refusing unexpected container/output')
deadline=time.monotonic()+690
while time.monotonic()<deadline:
 query="SELECT COUNT(*) FROM bills WHERE phase='reserve' AND status='pending'"
 p=subprocess.run(['docker','exec',name,'mysql','-uroot','-pisolated','-N','project_load','-e',query],capture_output=True,text=True)
 if p.returncode!=0:
  (out/'watch-error.txt').write_text(p.stderr);raise SystemExit('container query failed')
 if p.stdout.strip()=='0':
  sql="SELECT request_id,phase,status FROM bills ORDER BY request_id,phase; SELECT event_key FROM outbox_events ORDER BY event_key;"
  p=subprocess.run(['docker','exec',name,'mysql','-uroot','-pisolated','-N','project_load','-e',sql],capture_output=True,text=True)
  if p.returncode: (out/'watch-error.txt').write_text(p.stderr);raise SystemExit('snapshot failed')
  (out/'watch-final-rows.txt').write_text(p.stdout);(out/'watch-completed').write_text('completed\n');print('per-request snapshot saved',flush=True);break
 time.sleep(1)
else: raise SystemExit('pending never reached zero before deadline')
