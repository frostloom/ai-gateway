#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='execution-state-overlay-') as d:
 args=['go','test','-race']
 if '--candidate' in sys.argv:
  overlay=pathlib.Path(d)/'overlay.json'
  overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/mysql.go'):str(root/'eval/project-load-2026-11-02/billing_execution_state_current_candidate.go.txt'),str(root/'internal/gateway/handler/chat.go'):str(root/'eval/project-load-2026-11-02/gateway_attempt_current_candidate.go.txt')}}))
  args+=['-overlay',str(overlay)]
 args+=['./eval/billing-comparison-2026-11-02','-run','^Test(AttemptOutboxFailureAfterUpstreamResponse|RealGatewayWithExecutionBridge)$','-count=3','-v']
 raise SystemExit(subprocess.run(args,cwd=root,env=dict(os.environ,TEST_EXECUTION_STATE='1',TEST_ATTEMPT_GUARD='1',TEST_ATTEMPT_FINISH='1',TEST_FINISH_NONBLOCKING='1',TEST_COMPLETION_EVIDENCE='1',TEST_COORDINATED_FINISH='1')).returncode)
