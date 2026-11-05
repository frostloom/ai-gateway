#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
args=['go','test','-race']
with tempfile.TemporaryDirectory(prefix='unknown-billing-overlay-') as d:
 if '--candidate' in sys.argv:
  overlay=pathlib.Path(d)/'overlay.json'
  overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/mysql.go'):str(root/'eval/project-load-2026-11-02/billing_unknown_candidate.go.txt')}}))
  args+=['-overlay',str(overlay)]
 args+=['./eval/billing-comparison-2026-11-02','-run','^TestUnknownBillingCandidateRefundAndSettlement$','-count=3','-v']
 env=dict(os.environ)
 if '--candidate' in sys.argv: env['UNKNOWN_BILLING_CANDIDATE']='1'
 raise SystemExit(subprocess.run(args,cwd=root,env=env).returncode)
