#!/usr/bin/env python3
import hashlib, json, os, pathlib, subprocess, tempfile
root = pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='mixed-billing-') as d:
    d = pathlib.Path(d)
    overlay = d/'overlay.json'
    overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/mysql.go'):str(root/'eval/project-load-2026-11-02/billing_execution_state_current_candidate.go.txt')}}))
    package = './eval/billing-comparison-2026-11-02'
    for name, extra in [('legacy', []), ('candidate', ['-overlay', str(overlay)])]:
        path = d/name
        subprocess.run(['go','test','-race','-c','-o',str(path),*extra,package],cwd=root,check=True)
        print(name+'_binary_sha256='+hashlib.sha256(path.read_bytes()).hexdigest(),flush=True)
    env = dict(os.environ, TEST_EXECUTION_STATE='1', MIXED_TEST_LEGACY_BINARY=str(d/'legacy'))
    raise SystemExit(subprocess.run([str(d/'candidate'),'-test.run=^TestMixedVersionLegacyCanReverseForwardingBill$', '-test.count=3','-test.v'],cwd=root,env=env).returncode)
