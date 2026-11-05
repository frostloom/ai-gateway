#!/usr/bin/env python3
import json, os, pathlib, subprocess, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='audit-conflict-') as d:
 args=['go','test','-race']
 if os.environ.get('TEST_AUDIT_CANDIDATE')=='1':
  overlay=pathlib.Path(d)/'overlay.json'
  overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/audit.go'):str(root/'eval/billing-comparison-2026-11-02/audit_conflict_candidate.go.txt')}}))
  args+=['-overlay',str(overlay)]
 pattern='^TestAudit(Quarantine(Transaction|RaceAndCommitAck)|GuardCaseEquivalentRace)$' if os.environ.get('TEST_AUDIT_QUARANTINE')=='1' else '^TestAuditRejectsConflictingEventID$'
 if os.environ.get('TEST_AUDIT_LOAD')=='1': pattern='^TestAuditQuarantineLoad$'
 args+=['./eval/billing-comparison-2026-11-02','-run',pattern,'-count=' + ('1' if os.environ.get('TEST_AUDIT_LOAD')=='1' else '3'),'-v']
 raise SystemExit(subprocess.run(args,cwd=root,env=dict(os.environ,TEST_AUDIT_CONFLICT='1')).returncode)
