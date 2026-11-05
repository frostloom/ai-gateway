#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='execution-state-overlay-') as d:
 args=['go','test','-race']
 if '--candidate' in sys.argv:
  overlay=pathlib.Path(d)/'overlay.json'
  billing='billing_manual_guard_candidate.go.txt' if os.environ.get('TEST_STORE_MANUAL_GUARD')=='1' else 'billing_execution_state_v4_candidate.go.txt'
  overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/mysql.go'):str(root/'eval/project-load-2026-11-02'/billing),str(root/'internal/gateway/handler/chat.go'):str(root/'eval/project-load-2026-11-02/gateway_truncated_candidate.go.txt')}}))
  args+=['-overlay',str(overlay)]
 args+=['./eval/billing-comparison-2026-11-02','-run','^TestHandlerJournalSurvivesProcessKill$','-count=3','-v']
 raise SystemExit(subprocess.run(args,cwd=root,env=dict(os.environ,TEST_EXECUTION_STATE='1')).returncode)
