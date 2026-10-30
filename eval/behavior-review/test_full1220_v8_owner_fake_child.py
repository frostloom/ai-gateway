"""UNIT MOCK process supervision only; no actual manifest, authority or proof.

The trusted launch verifier is explicitly replaced. Real local Python children
exercise the owner's protocol and PGID cleanup; no Go/provider/DB is accessed.
"""
import sys
sys.dont_write_bytecode=True
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import unittest

OWNER=Path(__file__).with_name('full1220-v8-owner-template.py').resolve()
HARNESS=r'''
import sys
sys.dont_write_bytecode=True
import os,json,types,hashlib,subprocess
from pathlib import Path
root=Path(sys.argv[1]);mode=sys.argv[2];path=root/'owner-preparation/run-owner.py'
m=types.ModuleType('unit_mock_owner');m.__file__=str(path)
exec(compile(path.read_bytes(),str(path),'exec'),m.__dict__)
m.ROOT=str(root)
def exclusive(path,raw):
    with Path(path).open('xb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
class Reader:
    def read(self,name,expected=None):
        raw=(root/name).read_bytes()
        if expected is not None and hashlib.sha256(raw).hexdigest()!=expected:raise ValueError('unit mock input hash')
        return raw
settings=(root/'final-private-settings.json').read_bytes()
manifest={'private_settings_sha256':hashlib.sha256(settings).hexdigest(),'scope_sha256':'b'*64,'execution_sha256':'c'*64}
context=types.SimpleNamespace(reader=Reader(),manifest=manifest,binding={'owner_sha256':hashlib.sha256(path.read_bytes()).hexdigest()},authority_raw=b'UNIT MOCK, NOT AUTHORITY')
def verify(*args,**kwargs):
    exclusive(root/'unit-mock-verifier-called.json',json.dumps({'unit_mock':True,'not_actual_authority':True}).encode())
    if mode=='verify_failure':raise ValueError('unit mock verifier rejects')
    return {'unit_mock':True,'not_actual_authority':True}
v=types.SimpleNamespace(load_freeze_binding=lambda *args:context,_exclusive=exclusive,_json=lambda raw:json.loads(raw),
    FROZEN_MANIFEST_SHA256='a'*64,FROZEN_BINARY_SHA256='d'*64,sha=lambda raw:hashlib.sha256(raw).hexdigest(),verify_actual=verify)
m._load=lambda root:v
real_popen=subprocess.Popen
class Awake:
    def __init__(self):self.code=None
    def poll(self):return self.code
    def terminate(self):self.code=0
    def kill(self):self.code=-9
    def wait(self,timeout=None):return self.code
def popen(argv,**kwargs):
    if argv[0]=='/usr/bin/caffeinate':return Awake()
    if argv[0]!=str(root/'full-window-actual-v8'):raise AssertionError('unexpected process, blocked')
    return real_popen([sys.executable,'-B',str(root/'unit-worker.py'),str(root),mode],**kwargs)
m.subprocess.Popen=popen
if mode=='timeout':
    class TimeoutSelector:
        def register(self,*args):pass
        def select(self,*args):return []
    m.selectors.DefaultSelector=TimeoutSelector
raise SystemExit(m.run('e'*64))
'''
WORKER=r'''
import sys,json,time
from pathlib import Path
root=Path(sys.argv[1]);mode=sys.argv[2]
if mode in ('startup_hang','timeout'):time.sleep(30)
if mode=='partial_exit':
    sys.stdout.write('{"type":');sys.stdout.flush();raise SystemExit(0)
startup={'unit_mock':True,'type':'invalid' if mode=='invalid_startup' else 'startup',
 'status':{'next_window_index':1,'closed':False},'readiness':{'manifest_sha256':'a'*64,'scope_sha256':'b'*64,'execution_sha256':'c'*64,'pairs':3660,'arms':7320,'defined_turns':11772,'boundaries_excluded':1851}}
raw=json.dumps(startup)+'\n'
if mode=='partial_valid':
    sys.stdout.write(raw[:12]);sys.stdout.flush();time.sleep(.02);sys.stdout.write(raw[12:]);sys.stdout.flush()
else:print(raw,end='',flush=True)
with (root/'unit-worker-seen.jsonl').open('x') as seen:
    for line in sys.stdin:
        value=json.loads(line);seen.write(line);seen.flush()
        if mode=='busy_run' and value['operation']=='run_window':time.sleep(30)
        if value['operation']=='close':print(json.dumps({'type':'closed','unit_mock':True}),flush=True);break
        if value['operation']=='status':print(json.dumps({'type':'status','unit_mock':True}),flush=True)
        else:print(json.dumps({'type':'window','stopped':mode=='stopped_window','receipt_sha256':'1'*64,'unit_mock':True}),flush=True)
'''


def alive(pgid):
    try:os.killpg(pgid,0);return True
    except ProcessLookupError:return False


class FakeChild(unittest.TestCase):
    def setUp(self):
        output=os.environ.get('TASKBENCH_V8_OWNER_FAKE_OUTPUT')
        if output:Path(output).mkdir(parents=True,exist_ok=True)
        self.root=Path(tempfile.mkdtemp(prefix='unit-v8-owner-',dir=output)).resolve()
        (self.root/'owner-preparation').mkdir();(self.root/'verifier-preparation').mkdir()
        (self.root/'owner-preparation/run-owner.py').write_bytes(OWNER.read_bytes())
        (self.root/'final-private-settings.json').write_bytes(b'{"unit_mock":true,"not_actual_settings":true}')
        (self.root/'final-private-settings.json').chmod(0o600)
        (self.root/'unit-harness.py').write_text(HARNESS);(self.root/'unit-worker.py').write_text(WORKER)
        self.proc=None
    def tearDown(self):
        # Test-owned cleanup is unconditional, including expected RED signal case.
        process=self.root/'actual-owner-process.json'
        if process.exists():
            pgid=json.loads(process.read_bytes())['owned_pgid']
            if alive(pgid):
                os.killpg(pgid,signal.SIGKILL)
                (self.root/'unit-test-emergency-cleanup.json').write_text(json.dumps({'unit_mock':True,'owned_pgid':pgid,'signal':'SIGKILL'}))
        if self.proc is not None and self.proc.poll() is None:self.proc.kill();self.proc.wait()
    def start(self,mode):
        self.proc=subprocess.Popen([sys.executable,'-B',str(self.root/'unit-harness.py'),str(self.root),mode],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        return self.proc
    def run_case(self,mode,commands=b''):
        p=self.start(mode);out,err=p.communicate(commands,timeout=12)
        (self.root/'unit-supervisor.stdout').write_bytes(out);(self.root/'unit-supervisor.stderr').write_bytes(err)
        (self.root/'unit-result.json').write_text(json.dumps({'unit_mock':True,'mode':mode,'returncode':p.returncode}))
        return p.returncode
    def assert_cleanup(self):
        cleanup=json.loads((self.root/'actual-owner-group-cleanup.json').read_bytes())
        self.assertTrue(cleanup['leader_reaped']);self.assertTrue(cleanup['group_empty']);self.assertFalse(alive(cleanup['owned_pgid']))
    def test_eof_closes_and_reaps_fake_child(self):
        self.assertEqual(self.run_case('normal'),0);self.assert_cleanup()
        seen=[json.loads(x) for x in (self.root/'unit-worker-seen.jsonl').read_text().splitlines()]
        self.assertEqual(seen,[{'operation':'close'}])
    def test_partial_startup_then_one_explicit_window(self):
        raw=b'{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}\n'
        self.assertEqual(self.run_case('partial_valid',raw),0);self.assert_cleanup()
        self.assertTrue((self.root/'unit-mock-verifier-called.json').exists())
        report=json.loads((self.root/'verifier-preparation/window-000001-verified.json').read_bytes())
        self.assertEqual(report,{'unit_mock':True,'not_actual_authority':True})
    def test_verifier_failure_never_forwards_next_window(self):
        raw=b'{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}\n'+json.dumps({'operation':'run_window','expected_index':2,'prior_receipt_sha256':'1'*64}).encode()+b'\n'
        self.assertNotEqual(self.run_case('verify_failure',raw),0);self.assert_cleanup()
        seen=[json.loads(x) for x in (self.root/'unit-worker-seen.jsonl').read_text().splitlines()]
        self.assertEqual(len(seen),1);self.assertEqual(seen[0]['expected_index'],1)
    def test_invalid_startup_stops_group(self):
        self.assertNotEqual(self.run_case('invalid_startup'),0);self.assert_cleanup()
    def test_partial_json_eof_stops_group(self):
        self.assertNotEqual(self.run_case('partial_exit'),0);self.assert_cleanup()
    def test_stopped_window_never_calls_verifier(self):
        raw=b'{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}\n'
        self.assertNotEqual(self.run_case('stopped_window',raw),0);self.assert_cleanup()
        self.assertFalse((self.root/'unit-mock-verifier-called.json').exists())
    def test_mock_timeout_stops_group(self):
        self.assertNotEqual(self.run_case('timeout'),0);self.assert_cleanup()
    def test_existing_launch_never_reused(self):
        self.assertEqual(self.run_case('normal'),0);self.assert_cleanup()
        before=(self.root/'actual-owner-process.json').read_bytes()
        p=self.start('normal');out,err=p.communicate(b'',timeout=5)
        (self.root/'unit-relaunch.stdout').write_bytes(out);(self.root/'unit-relaunch.stderr').write_bytes(err)
        self.assertNotEqual(p.returncode,0)
        self.assertEqual((self.root/'actual-owner-process.json').read_bytes(),before)

    def signal_case(self,mode):
        p=self.start(mode);path=self.root/'actual-owner-process.json';until=time.monotonic()+5
        value=None
        while time.monotonic()<until:
            try:value=json.loads(path.read_bytes());break
            except (FileNotFoundError,json.JSONDecodeError):time.sleep(.01)
        self.assertIsNotNone(value);pgid=value['owned_pgid']
        if mode=='busy_run':
            p.stdin.write(b'{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}\n'+json.dumps({'operation':'run_window','expected_index':2,'prior_receipt_sha256':'1'*64}).encode()+b'\n');p.stdin.flush()
            seen=self.root/'unit-worker-seen.jsonl';until=time.monotonic()+5
            while (not seen.exists() or not seen.read_bytes()) and time.monotonic()<until:time.sleep(.01)
            self.assertTrue(seen.exists() and seen.read_bytes())
        p.terminate();out,err=p.communicate(timeout=8)
        (self.root/'unit-supervisor.stdout').write_bytes(out);(self.root/'unit-supervisor.stderr').write_bytes(err)
        until=time.monotonic()+1
        while alive(pgid) and time.monotonic()<until:time.sleep(.02)
        (self.root/'unit-signal-result.json').write_text(json.dumps({'unit_mock':True,'supervisor_returncode':p.returncode,'owned_pgid':pgid,'group_alive_after_supervisor_exit':alive(pgid)}))
        self.assertFalse(alive(pgid),'SIGTERM must not orphan start_new_session child')
        self.assert_cleanup()
        exit_record=json.loads((self.root/'actual-owner-exit.json').read_bytes())
        self.assertEqual(exit_record['supervisor_exit_code'],1)
        self.assertEqual(exit_record['supervision_signal'],signal.SIGTERM)
        self.assertFalse(exit_record['relaunch_allowed'])
        if mode=='busy_run':self.assertEqual(len((self.root/'unit-worker-seen.jsonl').read_text().splitlines()),1)
    def test_external_sigterm_cleans_busy_owned_group(self):self.signal_case('busy_run')
    def test_external_sigterm_cleans_startup_owned_group(self):self.signal_case('startup_hang')

if __name__=='__main__':unittest.main()
