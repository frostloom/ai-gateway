"""Supplemental capture trust boundary tests; no actual credentials or API IO."""
import hashlib
import ast
import importlib.util
import json
from pathlib import Path
import sys
import subprocess
import tempfile
import types
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
BASE = REPO / '.tmp/v9-live-score-horizon-20261031/historical-scores.py'
NEW = HERE / 'full1220-capture-input-v1.py'


def load():
    if not NEW.exists():
        return None
    spec = importlib.util.spec_from_file_location('capture_input_v1', NEW)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class DerivationTests(unittest.TestCase):
    def setUp(self):
        self.m = load()
        self.assertIsNotNone(self.m, 'authenticated supplemental capture component missing')
        self.base = BASE.read_bytes()

    def fresh(self):
        raw = self.m.derive_consumer_source(self.base, self.m.CAPTURE_SPEC)
        module = types.ModuleType('isolated_corrected_consumer')
        module.__file__ = str(NEW)
        exec(compile(raw, str(NEW), 'exec'), module.__dict__)
        return module

    def test_derivation_rejects_modified_base(self):
        with self.assertRaisesRegex(ValueError, 'base consumer'):
            self.m.derive_consumer_source(self.base + b'\n', self.m.CAPTURE_SPEC)

    def test_derivation_rejects_caller_key_registry_and_artifact_roots(self):
        for field in ('public_key', 'registry_root', 'artifact_root'):
            with self.subTest(field=field):
                correction = dict(self.m.CAPTURE_SPEC, **{field: '/untrusted'})
                with self.assertRaises(ValueError):
                    self.m.derive_consumer_source(self.base, correction)

    def test_fresh_module_seals_reject_serialized_projection_and_actual_proofs(self):
        first, second = self.fresh(), self.fresh()
        self.assertIsNot(first._PROJECTION_SEAL, second._PROJECTION_SEAL)
        self.assertIsNot(first._SCORE_SEAL, second._SCORE_SEAL)
        with self.assertRaisesRegex(ValueError, 'Opaque authenticated'):
            first._check_actual({'signature_verified': True})
        with self.assertRaises(ValueError):
            first._validate_projection_plan_binding({}, {})

    def test_capture_defaults_reject_before_settings_callback(self):
        module = self.fresh()
        calls = []
        module._projection_private_settings = lambda *a: calls.append(a)
        with self.assertRaisesRegex(ValueError, 'Explicit prebuilt'):
            module.project_window_content(b'{}', b'{}', b'{}', {}, b'{}', {},
                                          source_root='/untrusted', output_dir='/untrusted')
        self.assertEqual(calls, [])

    def test_forged_actual_proof_rejects_before_settings_callback(self):
        module = self.fresh()
        calls = []
        module._projection_private_settings = lambda *a: calls.append(a)
        with self.assertRaisesRegex(ValueError, 'Opaque authenticated'):
            module.project_window_content(b'{}', b'{}', b'{}', {}, b'{}', {}, execute=True,
                actual_proof={'accepted': True}, source_root='/untrusted', output_dir='/untrusted')
        self.assertEqual(calls, [])

    def test_exact_content_comparator_remains_type_and_field_sensitive(self):
        module = self.fresh()
        for got, want in [({'plan_name': 'Professional', 'plan_id': 2}, {'plan_name': 'Professional'}),
                          ({'x': True}, {'x': 1})]:
            with self.assertRaises(ValueError):
                module._equal(got, want, 'strict')

    def test_derivation_preserves_all_other_factory_comparator_and_once_code(self):
        original = ast.parse(self.base)
        corrected = ast.parse(self.m.derive_consumer_source(self.base, self.m.CAPTURE_SPEC))
        functions = lambda tree: {node.name: ast.dump(node, include_attributes=False)
            for node in tree.body if isinstance(node, (ast.FunctionDef, ast.ClassDef))}
        before, after = functions(original), functions(corrected)
        self.assertEqual(set(before), set(after))
        for name in before.keys() - {'project_window_content'}:
            self.assertEqual(before[name], after[name], name)

    def test_loader_rejects_unfrozen_sha_and_alternate_binding_without_io(self):
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'):
            self.m.load_corrected_consumer(str(self.m.BINDING_PATH), 'UNFROZEN')
        with self.assertRaisesRegex(ValueError, 'fixed correction binding'):
            self.m.load_corrected_consumer('/untrusted/binding.json', '1' * 64)

    def test_loader_rejects_missing_binding(self):
        self.assertFalse(self.m.BINDING_PATH.exists(), 'Task2 must remain inactive')
        with self.assertRaises((ValueError, FileNotFoundError)):
            self.m.load_corrected_consumer(str(self.m.BINDING_PATH), '1' * 64)

    def test_build_default_has_no_reads_or_writes(self):
        result = self.m.build_capture_components('/missing', 'UNFROZEN')
        self.assertEqual(result['state'], 'UNFROZEN')
        self.assertFalse(result['executed'])
        self.assertEqual(result['proofs_issued'], 0)

    def test_bound_file_change_is_rejected_when_freshly_pinned(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary).resolve() / 'catalog.json'
            path.write_bytes(b'changed after catalog validation')
            runner = self.m._prebuilder().module(
                HERE / 'full1220-v9-helper-runtime.py', self.m.RUNTIME_SHA, 'pin_race_runner')
            self.assertTrue(callable(getattr(self.m, '_checked_pin', None)), 'bound fresh pin check missing')
            with self.assertRaisesRegex(ValueError, 'bound fresh file'):
                self.m._checked_pin(runner, path, '1' * 64)

    def test_context_runner_recreates_pins_from_independent_same_sha_module(self):
        b = self.m._prebuilder()
        path = HERE/'full1220-v9-helper-runtime.py'
        a = b.module(path, self.m.RUNTIME_SHA, 'capture_runner_a')
        other = b.module(path, self.m.RUNTIME_SHA, 'capture_runner_b')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            (root/'source').write_bytes(b'fixture source')
            (root/'source').chmod(0o400)
            original = a.pin_tree(root, {'source': self.m._sha(b'fixture source')})
            tools = {'go': a.pin_tool(Path(b.GO))}
            with self.assertRaisesRegex(ValueError, 'catalog tool identity'):
                other.check_tool(tools['go'])
            self.assertTrue(callable(getattr(self.m, '_context_runner_pins', None)), 'context runner pin rebinding missing')
            p, tree, selected_tools = self.m._context_runner_pins(types.SimpleNamespace(helper={'runner': other}), a, original, tools)
            self.assertIs(p, other)
            other.check_tree(tree)
            other.check_tool(selected_tools['go'])
            self.assertIs(type(tree), other.TreePin)
            self.assertIs(type(selected_tools['go']), other.SystemToolPin)


class SupplementalBuildTests(unittest.TestCase):
    """Real copied-source two-build pipeline with a synthetic catalog key.

    Only a fresh test module's fixed artifact directory is substituted. Public
    production entry points do not accept either this root or this test key.
    The original historical module and its actual proof gates are never patched.
    """
    @classmethod
    def setUpClass(cls):
        cls.m = load()
        cls.b = cls.m._prebuilder()
        cls.work = Path(tempfile.mkdtemp(prefix='synthetic-build-', dir=cls.m.ARTIFACT_ROOT))
        cls.m.ARTIFACT_ROOT = cls.work
        cls.build, cls.build_raw = cls.m._build_module(cls.b)
        cls.manifest = json.loads(cls.b.read(cls.m.ORIGINAL_ROOT/'actual-manifest.json', cls.m.MANIFEST_SHA))
        original_job = json.loads(cls.b.read(cls.m.ORIGINAL_ROOT/'helper-prebuild-job-v2.private.json'))
        cls.job = dict(original_job, freeze_root=str(cls.work), build_attestation_public_key='1'*64)
        cls.job_path = cls.work/'prebuild-job.json'
        cls.job_path.write_bytes(cls.m._wire(cls.job))
        cls.job_sha = cls.m._sha(cls.job_path.read_bytes())
        cls.preparation = cls.build.prepare(cls.job_path, cls.job_sha)
        loaded = cls.build.load_job(cls.job_path, cls.job_sha)
        runner_a, original_a, tools_a = loaded[2], loaded[-2], loaded[-1]
        runner_b = cls.b.module(cls.m.ORIGINAL_ROOT/'regression/source'/cls.b.RUNTIME_MODULE,
                               cls.m.RUNTIME_SHA, 'synthetic_actual_context_runner')
        cls.p, _, cls.tools = cls.m._context_runner_pins(
            types.SimpleNamespace(helper={'runner': runner_b}), runner_a, original_a, tools_a)
        cls.catalog = json.loads((cls.work/'helper-build/catalog.unsigned.json').read_bytes())
        node = '/Users/a1/开发工具/node/node-v24.22.0-darwin-arm64/bin/node'
        signed = subprocess.run([node, '-e', "const c=require('node:crypto');let s='';process.stdin.on('data',b=>s+=b);process.stdin.on('end',()=>{const k=c.generateKeyPairSync('ed25519');process.stdout.write(JSON.stringify({key:k.publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('hex'),signature:c.sign(null,Buffer.from(s,'hex'),k.privateKey).toString('hex')}));});"],
            input=cls.p.catalog_signing_bytes(cls.catalog).hex().encode(), capture_output=True, check=True)
        synthetic = json.loads(signed.stdout)
        cls.manifest['build_attestation_public_key'] = synthetic['key']
        cls.catalog['signature'] = synthetic['signature']
        cls.catalog_path = cls.work/'helper-build/catalog.json'
        cls.catalog_path.write_bytes(cls.m._wire(cls.catalog))
        cls.catalog_path.chmod(0o400)
        cls.binding = {'catalog_sha256': cls.m._sha(cls.catalog_path.read_bytes()),
            'catalog_identity': cls.catalog['identity'], 'build_evidence_sha256': cls.catalog['build_evidence_sha256'],
            'artifacts': {e['path']: e['sha256'] for e in cls.catalog['artifacts']}}
        cls.verifier = cls.b.module(cls.m.ORIGINAL_ROOT/'regression/source'/cls.b.VERIFIER_MODULE,
                                   cls.job['verifier_module_sha256'], 'synthetic_capture_catalog_crypto')
        print('SYNTHETIC_BUILD_ARTIFACTS='+str(cls.work), flush=True)

    def crypto(self, key, message, signature):
        self.verifier._signature(key, message, signature)
        return True

    def select(self, binding=None):
        catalog, runtime_map = self.m._validate_catalog(binding or self.binding, self.manifest, self.b, self.p, self.crypto)
        runtime = self.p.pin_tree(self.work/'helper-build/runtime', runtime_map)
        tools = self.tools
        selected = self.p.select_helper(catalog, 'capture', artifact_root=self.work, runtime_tree=runtime, tool_pins=tools)
        return selected, runtime, tools

    def test_two_real_builds_new_helper_source_and_catalog(self):
        selected, runtime, _ = self.select()
        self.assertEqual(selected.cwd, str(self.work/'helper-build/runtime/internal/observability'))
        self.assertEqual(selected.binary.sha256, self.binding['artifacts']['helper-build/capture.test'])
        self.assertNotEqual(self.binding['catalog_sha256'], self.m.OLD_CATALOG_SHA)
        self.assertEqual(self.preparation['proofs_issued'], 0)
        for kind in ('source', 'capture'):
            receipt = json.loads((self.work/'helper-supervision/attempts'/('build-'+kind)/'receipt.json').read_bytes())
            self.assertTrue(receipt['accepted'])
            self.assertTrue(receipt['cleanup']['confirmed'])

    def test_wrong_signature_rejected_before_delayed_key_callback(self):
        bad = json.loads(self.m._wire(self.catalog))
        bad['signature'] = '0'*128
        raw = self.m._wire(bad)
        original = self.b.read
        calls = []
        def read(path, *args, **kwargs):
            return raw if Path(path) == self.catalog_path else original(path, *args, **kwargs)
        with mock.patch.object(self.b, 'read', side_effect=read):
            with self.assertRaises(ValueError):
                self.select(dict(self.binding, catalog_sha256=self.m._sha(raw)))
                calls.append('key callback')
        self.assertEqual(calls, [])

    def test_old_catalog_cannot_select_new_helper(self):
        original = self.b.read
        def read(path, *args, **kwargs):
            if Path(path) == self.catalog_path:
                return original(self.m.ORIGINAL_ROOT/'helper-build/catalog.json', self.m.OLD_CATALOG_SHA)
            return original(path, *args, **kwargs)
        with mock.patch.object(self.b, 'read', side_effect=read):
            with self.assertRaisesRegex(ValueError, 'new supplemental catalog'):
                self.select(dict(self.binding, catalog_sha256=self.m.OLD_CATALOG_SHA))

    def invoke_capture(self, *, mutate=None, cleanup_unknown=False):
        selected, runtime, tools = self.select()
        case = Path(tempfile.mkdtemp(prefix='capture-', dir=self.work))
        records = case/'records'; records.mkdir()
        raw = self.m._wire({'result': {'observations': [{'turn': {'message': 'Change plan', 'response': {
            'reply': 'Confirm', 'trace': [{'tool': 'change_plan', 'phase': 'preview',
            'args': '{"plan_name":"Professional"}', 'resolved_args': '{"plan_id":2,"plan_name":"Professional"}',
            'preview': 'Confirm', 'result': 'Waiting', 'outcome': 'pending'}]}}}]}})
        (records/'record.json').write_bytes(raw)
        job = {'schema': 'full1220-content-projection-input.v1', 'manifest_sha256': '1'*64,
            'receipt_sha256': '2'*64, 'scope_sha256': '3'*64, 'execution_sha256': '4'*64,
            'archive_commit': '5'*40, 'archive_tree': '6'*40,
            'content_source_sha256': '1c2884a41724bcc116764eac61f807d7c7845bae92285009406580575ace5a73',
            'helper_sha256': self.m.TEMPLATE_SHA, 'window_id': 'synthetic', 'private_settings_sha256': '7'*64,
            'rows': [{'trace_id': '8'*32, 'recording_file': 'record.json', 'recording_sha256': self.m._sha(raw),
                      'observed_turns': 1, 'metadata': {}}]}
        (case/'input.json').write_bytes(self.m._wire(job))
        inputs = [self.p.pin_file(case/'input.json', immutable=False), self.p.pin_file(records/'record.json', immutable=False)]
        calls, accepted = [], []
        def stdin():
            calls.append(True)
            return b'{"hash_key":"synthetic-memory-only-capture-test-key"}'
        def validate(_):
            value = json.loads((case/'projection.json').read_bytes())
            self.assertEqual(value['rows'][0]['turns'][0]['tools'][0]['input'], {'plan_name': 'Professional'})
            accepted.append(value)
        if mutate:
            mutate(case)
        env = {'PATH': '/usr/bin:/bin', 'SDKROOT': self.b.SDK, 'FULL1220_PROJECTION_INPUT': str(case/'input.json'),
            'FULL1220_PROJECTION_OUTPUT': str(case/'projection.json'), 'FULL1220_PROJECTION_INPUT_SHA256': self.m._sha(self.m._wire(job))}
        original_cleanup = self.p._cleanup
        def unknown(child):
            result = original_cleanup(child)
            result['confirmed'] = False
            return result
        with mock.patch.object(self.p, '_cleanup', side_effect=unknown if cleanup_unknown else original_cleanup):
            receipt = self.p.run_owned(state_root=case/'supervision', attempt_name='capture', argv=list(selected.argv),
                cwd=Path(selected.cwd), env=env, executable_pin=selected.binary, inputs=inputs+list(tools.values()),
                trees=[runtime], timeout=125, phase='capture', stdin_factory=stdin, validate_output=validate)
        return case, receipt, calls, accepted

    def test_real_capture_uses_new_input_semantics_after_owned_build(self):
        _, receipt, calls, validated = self.invoke_capture()
        self.assertTrue(receipt['accepted'])
        self.assertTrue(receipt['cleanup']['confirmed'])
        self.assertEqual(calls, [True])
        self.assertEqual(len(validated), 1)
        fresh = types.ModuleType('synthetic_capture_diagnostic_consumer')
        exec(compile(self.m.derive_consumer_source(BASE.read_bytes(), self.m.CAPTURE_SPEC), str(NEW), 'exec'), fresh.__dict__)
        with self.assertRaisesRegex(ValueError, 'Opaque|projection|execution'):
            fresh._validate_projection_plan_binding({}, validated[0])

    def test_changed_source_input_rejected_before_key(self):
        _, receipt, calls, validated = self.invoke_capture(mutate=lambda case: (case/'records/record.json').write_bytes(b'{}'))
        self.assertFalse(receipt['accepted'])
        self.assertIsNone(receipt['owned_pgid'])
        self.assertEqual(calls, [])
        self.assertEqual(validated, [])

    def test_changed_runtime_rejected_before_key(self):
        path = self.work/'helper-build/runtime/internal/observability/content.go'
        raw = path.read_bytes()
        def mutate(_):
            path.chmod(0o600)
            path.write_bytes(raw+b'\n')
            path.chmod(0o400)
        try:
            _, receipt, calls, validated = self.invoke_capture(mutate=mutate)
            self.assertFalse(receipt['accepted'])
            self.assertIsNone(receipt['owned_pgid'])
            self.assertEqual(calls, [])
            self.assertEqual(validated, [])
        finally:
            path.chmod(0o600)
            path.write_bytes(raw)
            path.chmod(0o400)

    def test_unknown_cleanup_after_start_never_accepts_or_retries(self):
        case, receipt, calls, validated = self.invoke_capture(cleanup_unknown=True)
        self.assertIsNotNone(receipt['owned_pgid'])
        self.assertEqual(calls, [True])  # stdin was delivered; no contrary claim.
        self.assertFalse(receipt['accepted'])
        self.assertEqual(validated, [])
        self.assertTrue((case/'supervision/active.json').exists())
        selected, _, _ = self.select()
        with self.assertRaises(self.p.Blocked):
            self.p.run_owned(state_root=case/'supervision', attempt_name='different-output', argv=list(selected.argv),
                cwd=Path(selected.cwd), env={'PATH': '/usr/bin:/bin'}, executable_pin=selected.binary, inputs=[], timeout=1)


if __name__ == '__main__':
    unittest.main()
