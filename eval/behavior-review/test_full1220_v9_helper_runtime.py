import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent


def module():
    path = HERE / 'full1220-v9-helper-runtime.py'
    if not path.exists():
        return None
    spec = importlib.util.spec_from_file_location('helper_primitive', path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = mod
    spec.loader.exec_module(mod)
    return mod


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.m = module()
        self.assertIsNotNone(self.m, 'owned helper primitive is missing')
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        self.runtime = self.root / 'runtime'
        self.runtime.mkdir(mode=0o700)
        self.input = self.root / 'input.json'
        self.input.write_bytes(b'{"window":1}')
        self.input.chmod(0o400)
        self.python = Path(sys.executable).resolve()
        self.launcher = self.runtime / 'fixture-helper'
        self.launcher.write_text('#!/bin/sh\nexec '+str(self.python)+' "$@"\n')
        self.launcher.chmod(0o500)

    def invoke(self, code, **extra):
        # Trusted factory supplies argv/env/cwd; jobs do not select them.
        selected_pin = extra.pop('executable_pin', None) or self.m.pin_file(self.launcher)
        args = dict(state_root=self.root / 'state', attempt_name='one',
                    argv=[str(self.launcher), '-I', '-c', code], cwd=self.runtime,
                    env={'PATH': '/usr/bin:/bin'}, executable_pin=selected_pin,
                    inputs=[self.m.pin_file(self.input)], timeout=2)
        args.update(extra)
        return self.m.run_owned(**args)

    def test_two_fresh_processes_and_original_input_binding(self):
        first = self.invoke('import os;print(os.getpid())')
        self.assertTrue(first['accepted'])
        self.assertTrue(first['cleanup']['group_empty'])
        self.assertTrue(first['cleanup']['leader_reaped'])
        self.input.chmod(0o600)
        self.input.write_bytes(b'{"window":2}')
        self.input.chmod(0o400)
        second = self.invoke('import os;print(os.getpid())', attempt_name='two')
        self.assertTrue(second['accepted'])
        self.assertNotEqual(first['owned_pgid'], second['owned_pgid'])
        self.assertNotEqual(first['inputs'], second['inputs'])
        self.assertFalse((self.root / 'state' / 'active.json').exists())

    def test_timeout_preserves_intent_and_blocks_new_directory(self):
        receipt = self.invoke('import time;time.sleep(30)', timeout=.12)
        self.assertFalse(receipt['accepted'])
        self.assertEqual(receipt['failure_type'], 'TimeoutError')
        self.assertTrue(receipt['cleanup']['group_empty'])
        with self.assertRaises(self.m.Blocked):
            self.invoke('raise SystemExit(0)', attempt_name='other')
        self.assertFalse((self.root / 'state' / 'attempts' / 'other').exists())

    def test_original_changed_before_run_never_reads_stdin_secret(self):
        old = self.m.pin_file(self.input)
        self.input.chmod(0o600)
        self.input.write_bytes(b'changed')
        self.input.chmod(0o400)
        calls = []
        receipt = self.invoke('raise SystemExit(0)', inputs=[old],
                              stdin_factory=lambda: calls.append(True) or b'synthetic-secret')
        self.assertFalse(receipt['accepted'])
        self.assertEqual(calls, [])
        self.assertIsNone(receipt['owned_pgid'])

    def test_capture_stdin_only_and_correct_cwd(self):
        digest = hashlib.sha256(b'synthetic-secret').hexdigest()
        receipt = self.invoke('import sys,os,hashlib; assert hashlib.sha256(sys.stdin.buffer.read()).hexdigest()=='+repr(digest)+';print(os.getcwd())',
                              stdin_factory=lambda: b'synthetic-secret')
        self.assertTrue(receipt['accepted'])
        raw = b''.join(p.read_bytes() for p in (self.root / 'state').rglob('*') if p.is_file())
        self.assertNotIn(b'synthetic-secret', raw)
        self.assertEqual((self.root/'state/attempts/one/stdout').read_text().strip(), str(self.runtime))

    def test_input_mutated_during_run_fails_after_cleanup(self):
        code = 'from pathlib import Path;p=Path('+repr(str(self.input))+');p.chmod(0o600);p.write_bytes(b"bad");p.chmod(0o400)'
        receipt = self.invoke(code)
        self.assertFalse(receipt['accepted'])
        self.assertTrue(receipt['cleanup']['group_empty'])

    def test_nonzero_and_fake_success_never_accept(self):
        receipt = self.invoke('print("success");raise SystemExit(4)')
        self.assertFalse(receipt['accepted'])
        self.assertEqual(receipt['exit_code'], 4)

    def test_original_output_validator_failure_keeps_barrier(self):
        def reject(outdir):
            self.assertTrue((outdir/'stdout').exists())
            raise ValueError('missing original helper marker')
        receipt = self.invoke('print("fake-success")', validate_output=reject)
        self.assertFalse(receipt['accepted'])
        self.assertEqual(receipt['failure_stage'], 'output-validation')
        self.assertTrue(receipt['cleanup']['confirmed'])
        with self.assertRaises(self.m.Blocked):
            self.invoke('raise SystemExit(0)', attempt_name='next')

    def test_stdin_write_deadline_is_bounded(self):
        began = time.monotonic()
        receipt = self.invoke('import time;time.sleep(30)', timeout=.12,
                              stdin_factory=lambda: b'x' * 2_000_000)
        self.assertFalse(receipt['accepted'])
        self.assertLess(time.monotonic() - began, 3)
        self.assertTrue(receipt['cleanup']['group_empty'])

    def test_tree_exact_set_no_alias_and_after_run(self):
        tree = self.m.pin_tree(self.runtime, {'fixture-helper': hashlib.sha256(self.launcher.read_bytes()).hexdigest()})
        receipt = self.invoke('from pathlib import Path;Path("unexpected").mkdir()', trees=[tree])
        self.assertFalse(receipt['accepted'])
        self.assertTrue(receipt['cleanup']['group_empty'])
        alias = self.root/'alias'
        alias.symlink_to(self.input)
        with self.assertRaises(ValueError):
            self.m.pin_file(alias)
        alias.unlink()
        os.link(self.input, alias)
        with self.assertRaises(ValueError):
            self.m.pin_file(self.input)

    def test_unknown_cleanup_remains_blocked(self):
        with mock.patch.object(self.m, '_cleanup', return_value={'confirmed': False, 'group_empty': False,
                'leader_reaped': True, 'error_type': 'PermissionError', 'actions': []}):
            receipt = self.invoke('raise SystemExit(0)')
        self.assertFalse(receipt['accepted'])
        with self.assertRaises(self.m.Blocked):
            self.invoke('raise SystemExit(0)', attempt_name='new')

    def test_receipt_persistence_failure_keeps_shared_barrier(self):
        original = self.m._write
        def write(path, raw):
            if path.name == 'receipt.json':
                raise OSError('controlled receipt disk failure')
            return original(path, raw)
        with mock.patch.object(self.m, '_write', side_effect=write):
            with self.assertRaises(OSError):
                self.invoke('raise SystemExit(0)')
        with self.assertRaises(self.m.Blocked):
            self.invoke('raise SystemExit(0)', attempt_name='new')

    def test_late_signals_during_receipt_and_active_commit_block_next(self):
        for signum in (signal.SIGINT, signal.SIGTERM):
            for point in ('receipt-written', 'active-unlinked-fsync'):
                with self.subTest(signal=signum, point=point):
                    state = self.root/('late-'+str(signum)+'-'+point)
                    sent = []
                    original_write, original_sync = self.m._write, self.m._fsync_dir
                    def write(path, data):
                        original_write(path, data)
                        if point == 'receipt-written' and path.name == 'receipt.json' and not sent:
                            sent.append(True)
                            os.kill(os.getpid(), signum)
                    def sync(path):
                        original_sync(path)
                        if point == 'active-unlinked-fsync' and path == state and not (state/'active.json').exists() and not sent:
                            sent.append(True)
                            os.kill(os.getpid(), signum)
                    with mock.patch.object(self.m, '_write', side_effect=write), mock.patch.object(self.m, '_fsync_dir', side_effect=sync):
                        receipt = self.invoke('raise SystemExit(0)', state_root=state)
                    self.assertEqual(sent, [True])
                    self.assertFalse(receipt['accepted'], receipt)
                    self.assertTrue(receipt['cleanup']['confirmed'])
                    self.assertTrue((state/'active.json').exists())
                    final = json.loads((state/'attempts/one'/receipt['receipt_file']).read_bytes())
                    self.assertEqual(final, receipt)
                    self.assertEqual(final['failure_type'], 'Interrupted')
                    self.assertIn(signum, final['signals'])
                    # Original successful candidate bytes are retained as written.
                    candidate = json.loads((state/'attempts/one/receipt.json').read_bytes())
                    self.assertTrue(candidate['accepted'])
                    with self.assertRaises(self.m.Blocked):
                        self.invoke('raise SystemExit(0)', state_root=state, attempt_name='new-directory')

    def test_stdin_close_exception_cannot_skip_group_cleanup(self):
        original = self.m.subprocess.Popen
        children = []
        class CloseFailure:
            def __init__(self, stream): self.stream = stream
            @property
            def closed(self): return self.stream.closed
            def fileno(self): return self.stream.fileno()
            def close(self):
                self.stream.close()
                raise OSError('controlled close failure')
        def spawn(*args, **kwargs):
            child = original(*args, **kwargs)
            child.stdin = CloseFailure(child.stdin)
            children.append(child)
            return child
        try:
            with mock.patch.object(self.m.subprocess, 'Popen', side_effect=spawn):
                receipt = self.invoke('import time;time.sleep(30)', timeout=.1,
                                      stdin_factory=lambda: b'x'*2_000_000)
            self.assertFalse(receipt['accepted'])
            self.assertTrue(receipt['cleanup']['confirmed'])
        finally:
            for c in children:
                if c.poll() is None:
                    try: os.killpg(c.pid, signal.SIGKILL)
                    except ProcessLookupError: pass
                    c.wait()

    def test_timeout_cleans_descendant_and_reaps_leader(self):
        code = '''import signal,subprocess,sys,time
c=subprocess.Popen([sys.executable,'-I','-c','import time;time.sleep(30)'])
def done(*a):
 c.terminate()
 c.wait(timeout=2)
 raise SystemExit(0)
signal.signal(signal.SIGTERM,done)
print(c.pid,flush=True)
time.sleep(30)
'''
        receipt = self.invoke(code, timeout=.25)
        self.assertFalse(receipt['accepted'])
        self.assertTrue(receipt['cleanup']['group_empty'], receipt)
        self.assertTrue(receipt['cleanup']['leader_reaped'])
        self.assertIn('SIGTERM', receipt['cleanup']['actions'])

    def test_term_ignored_requires_real_kill_escalation(self):
        receipt = self.invoke('import signal,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);print("term-ignored",flush=True);time.sleep(30)', timeout=1.5)
        self.assertFalse(receipt['accepted'])
        self.assertEqual((self.root/'state/attempts/one/stdout').read_bytes(), b'term-ignored\n')
        self.assertTrue(receipt['cleanup']['confirmed'])
        self.assertEqual(receipt['exit_code'], -signal.SIGKILL)
        self.assertEqual(receipt['cleanup']['actions'], ['SIGTERM', 'SIGKILL'])

    def test_mutable_executable_is_rejected_before_stdin_read(self):
        self.launcher.chmod(0o700)
        calls = []
        receipt = self.invoke('raise SystemExit(0)', executable_pin=self.m.pin_file(self.launcher, immutable=False),
                              stdin_factory=lambda: calls.append(True) or b'secret')
        self.assertFalse(receipt['accepted'])
        self.assertEqual(calls, [])

    def test_real_parent_signals_and_spawn_latch(self):
        for signum, spawn in [(signal.SIGINT, False), (signal.SIGTERM, False),
                              (signal.SIGINT, True), (signal.SIGTERM, True)]:
            with self.subTest(signum=signum, during_spawn=spawn):
                state = self.root/('state-'+str(signum)+'-'+str(spawn))
                ready = self.root/('ready-'+str(signum)+'-'+str(spawn))
                child_code = 'from pathlib import Path;import time;Path('+repr(str(ready))+').write_text("ready");time.sleep(30)'
                script = '''import importlib.util,sys,os,signal
from pathlib import Path
p=Path(sys.argv[1]);s=importlib.util.spec_from_file_location('helper_primitive',p);m=importlib.util.module_from_spec(s);sys.modules[s.name]=m;s.loader.exec_module(m)
if sys.argv[8]=='True':
 original=m.subprocess.Popen
 def spawn(*a,**k):
  child=original(*a,**k)
  os.kill(os.getpid(),int(sys.argv[7]))
  return child
 m.subprocess.Popen=spawn
r=m.run_owned(state_root=Path(sys.argv[2]),attempt_name='signal',argv=[sys.argv[3],'-I','-c',sys.argv[5]],cwd=Path(sys.argv[4]),env={'PATH':'/usr/bin:/bin'},executable_pin=m.pin_file(Path(sys.argv[3])),inputs=[m.pin_file(Path(sys.argv[6]))],timeout=4,stdin_factory=(lambda:b'x'*2000000) if sys.argv[8]=='False' else None)
if not r['cleanup']['confirmed']: print(r)
raise SystemExit(0 if not r['accepted'] and r['cleanup']['confirmed'] else 3)
'''
                supervisor = subprocess.Popen([str(self.python), '-I', '-c', script,
                    str(HERE/'full1220-v9-helper-runtime.py'), str(state), str(self.launcher), str(self.runtime),
                    child_code, str(self.input), str(signum), str(spawn)], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                try:
                    if not spawn:
                        end = time.monotonic()+4
                        while not ready.exists() and supervisor.poll() is None and time.monotonic()<end:
                            time.sleep(.01)
                        self.assertTrue(ready.exists(), supervisor.communicate(timeout=1) if supervisor.poll() is not None else 'child never ready')
                        supervisor.send_signal(signum)
                    out, err = supervisor.communicate(timeout=6)
                    self.assertEqual(supervisor.returncode, 0, (out, err))
                    receipt = json.loads((state/'attempts/signal/receipt.json').read_bytes())
                    self.assertEqual(receipt['signals'], [signum])
                    self.assertTrue(receipt['cleanup']['group_empty'])
                    self.assertTrue((state/'active.json').exists())
                finally:
                    if supervisor.poll() is None:
                        supervisor.kill()
                        supervisor.wait()


class CatalogTests(unittest.TestCase):
    def test_system_tools_actual_sdk_clang_and_file_policy_stays_private(self):
        m=module();sdk=Path('/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk/SDKSettings.json')
        clang=Path('/usr/bin/clang')
        for path in (sdk,clang):
            tool=m.pin_tool(path)
            self.assertIs(type(tool),m.SystemToolPin)
            self.assertEqual(tool.uid,0)
            self.assertEqual(tool.nlink,path.stat().st_nlink)
            self.assertGreaterEqual(tool.nlink,1)
            m.check_tool(tool);m.check_file(tool)
            with self.assertRaises(ValueError):m.pin_file(path,immutable=False)

    def test_tool_mutation_alias_mode_and_hardlink_count(self):
        m=module()
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve();path=root/'tool';path.write_bytes(b'controlled tool');path.chmod(0o500)
            alias=root/'tool-hardlink';os.link(path,alias)
            pin=m.pin_tool(path);self.assertEqual(pin.nlink,2);m.check_tool(pin)
            with self.assertRaises(ValueError):m.pin_file(path,immutable=False)
            alias.unlink()
            with self.assertRaises(ValueError):m.check_tool(pin)
            pin=m.pin_tool(path);path.chmod(0o700);path.write_bytes(b'changed tool');path.chmod(0o500)
            with self.assertRaises(ValueError):m.check_tool(pin)
            link=root/'alias';link.symlink_to(path)
            with self.assertRaises(ValueError):m.pin_tool(link)
            path.chmod(0o522)
            with self.assertRaises(ValueError):m.pin_tool(path)

    def fixture(self):
        identity = {'commit': 'a'*40, 'tree': 'b'*40, 'source_map_sha256': 'c'*64,
                    'runtime_map_sha256': 'd'*64, 'go_sha256': 'e'*64,
                    'sdk_sha256': 'f'*64, 'platform': 'darwin/arm64',
                    'build_env': {'GOPROXY': 'off'}, 'tools': {'go': {'path': '/pinned/go', 'sha256': 'e'*64}}}
        entries = []
        for kind, cwd, test in [('source', 'internal/taskbench', 'TestSourceV9'),
                                ('capture', 'internal/observability', 'TestCaptureV9')]:
            entries.append({'kind': kind, 'path': 'artifacts/'+kind+'.test', 'sha256': '1'*64, 'size': 123,
                            'cwd': cwd, 'test': test, 'input_schema': kind+'.input.v9',
                            'output_schema': kind+'.output.v9', 'helper_path': cwd+'/helper_v9_test.go',
                            'helper_sha256': '2'*64})
        catalog = {'schema': 'full-window-helper-build-catalog.v1', 'identity': identity,
                   'artifacts': entries, 'build_evidence_sha256': '3'*64, 'signature': '4'*128}
        return catalog

    def test_catalog_pin_authority_and_strict_schema(self):
        m = module()
        self.assertIsNotNone(m, 'signed helper catalog primitive is missing')
        catalog = self.fixture()
        identity = catalog['identity']
        raw = json.dumps(catalog).encode()
        calls = []
        def verified(key, message, signature):
            calls.append((key, message, signature))
            return True
        args = dict(expected_sha256=hashlib.sha256(raw).hexdigest(), trusted_public_key='5'*64,
                    expected_identity=identity, verify_signature=verified)
        loaded = m.load_catalog(raw, **args)
        self.assertEqual(loaded['artifacts'][0]['kind'], 'source')
        self.assertEqual(calls[0][0], '5'*64)
        self.assertNotIn(b'"signature"', calls[0][1])
        with self.assertRaises(ValueError):
            m.load_catalog(raw, **dict(args, verify_signature=lambda *a: False))
        with self.assertRaises(ValueError):
            m.load_catalog(raw+b' ', **args)
        catalog['public_key'] = '6'*64
        altered = json.dumps(catalog).encode()
        with self.assertRaises(ValueError):
            m.load_catalog(altered, **dict(args, expected_sha256=hashlib.sha256(altered).hexdigest()))

    def test_catalog_selection_requires_actual_immutable_artifact_and_runtime(self):
        m = module()
        self.assertIsNotNone(m)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            runtime = root/'runtime'
            package = runtime/'internal/observability'
            package.mkdir(parents=True)
            helper = package/'helper_v9_test.go'
            helper.write_bytes(b'controlled helper bytes')
            helper.chmod(0o400)
            binary = root/'artifacts/capture.test'
            binary.parent.mkdir()
            binary.write_bytes(b'#!/bin/sh\nexit 0\n')
            binary.chmod(0o500)
            source_map = {'internal/observability/helper_v9_test.go': hashlib.sha256(helper.read_bytes()).hexdigest()}
            tree = m.pin_tree(runtime, source_map)
            entry = {'kind': 'capture', 'path': 'artifacts/capture.test', 'sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                     'size': binary.stat().st_size, 'cwd': 'internal/observability', 'test': 'TestCaptureV9',
                     'input_schema': 'capture.in.v9', 'output_schema': 'capture.out.v9',
                     'helper_path': 'internal/observability/helper_v9_test.go', 'helper_sha256': source_map['internal/observability/helper_v9_test.go']}
            catalog = {'identity': {'runtime_map_sha256': hashlib.sha256(json.dumps(source_map, sort_keys=True, separators=(',', ':')).encode()).hexdigest(), 'tools': {}}, 'artifacts': [entry]}
            selection = m.select_helper(catalog, 'capture', artifact_root=root, runtime_tree=tree, tool_pins={})
            self.assertEqual(selection.cwd, str(package))
            self.assertEqual(selection.argv[1], '-test.run=^TestCaptureV9$')
            self.assertEqual(selection.binary.sha256, entry['sha256'])
            binary.chmod(0o700)
            binary.write_bytes(b'changed')
            binary.chmod(0o500)
            with self.assertRaises(ValueError):
                m.select_helper(catalog, 'capture', artifact_root=root, runtime_tree=tree, tool_pins={})

    def test_real_synthetic_ed25519_signature_rejects_changed_message_and_key(self):
        m = module()
        node = '/Users/a1/开发工具/node/node-v24.22.0-darwin-arm64/bin/node'
        catalog = self.fixture()
        # Synthetic test key exists only in Node memory, never an actual build key.
        signed = subprocess.run([node, '-e', '''const c=require('node:crypto');let s='';process.stdin.on('data',b=>s+=b);process.stdin.on('end',()=>{const k=c.generateKeyPairSync('ed25519');process.stdout.write(JSON.stringify({key:k.publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('hex'),signature:c.sign(null,Buffer.from(s,'hex'),k.privateKey).toString('hex')}));});'''],
            input=m.catalog_signing_bytes(catalog).hex().encode(), capture_output=True, check=True)
        data = json.loads(signed.stdout)
        program = '''const c=require('node:crypto');let s='';process.stdin.on('data',b=>s+=b);process.stdin.on('end',()=>{try{const x=JSON.parse(s);const k=c.createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),Buffer.from(x.key,'hex')]),format:'der',type:'spki'});process.stdout.write(c.verify(null,Buffer.from(x.message,'hex'),k,Buffer.from(x.signature,'hex'))?'true':'false');}catch(e){process.stdout.write('false');}});'''
        def verify(key, message, sig):
            result = subprocess.run([node, '-e', program], input=json.dumps({'key': key, 'message': message.hex(), 'signature': sig}).encode(), capture_output=True, check=True)
            return result.stdout == b'true'
        catalog['signature'] = data['signature']
        raw = json.dumps(catalog).encode()
        args = dict(expected_sha256=hashlib.sha256(raw).hexdigest(), trusted_public_key=data['key'],
                    expected_identity=catalog['identity'], verify_signature=verify)
        self.assertEqual(m.load_catalog(raw, **args)['signature'], data['signature'])
        with self.assertRaises(ValueError):
            m.load_catalog(raw, **dict(args, trusted_public_key='0'*64))
        catalog['build_evidence_sha256'] = '9'*64
        altered = json.dumps(catalog).encode()
        with self.assertRaises(ValueError):
            m.load_catalog(altered, **dict(args, expected_sha256=hashlib.sha256(altered).hexdigest()))


if __name__ == '__main__':
    unittest.main()
