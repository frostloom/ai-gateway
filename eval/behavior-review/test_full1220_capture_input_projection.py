"""Real, offline Go content/projection regression; all identities are synthetic.

No observer constructor, provider/database/API client, settings, or actual key is
used. Only frozen observability Go sources and go.mod/go.sum are copied. Go runs
with GOPROXY/GOSUMDB off. Generated fixtures, binaries and raw process logs stay
under the plan's task-1-build directory for review.

The literal expectations catch using post-preview resolved_args, injecting local
business defaults, accepting unreconstructible evidence, or relaxing old guards.
Set FULL1220_CAPTURE_TEST_TEMPLATE to the original template to reproduce RED.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[2]
FROZEN = REPO / '.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031/regression/source'
BUILD = REPO / '.superpowers/sdd/2026-10-31-capture-input-correction/task-1-build'
GO = Path('/Users/a1/开发工具/go/bin/go')
SDK = '/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
CONTENT_SHA = '1c2884a41724bcc116764eac61f807d7c7845bae92285009406580575ace5a73'
HELPER_NAME = 'full1220_content_projection_v8_test.go'
SYNTHETIC_KEY = 'synthetic-offline-capture-input-fixture-key-only'
MISSING = object()


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, separators=(',', ':')) + '\n').encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def trace(phase='preview', args='{"plan_name":"Professional"}',
          resolved='{"plan_name":"Professional","plan_id":2}', tool='change_plan',
          outcome='pending'):
    value = {'tool': tool, 'phase': phase, 'resolved_args': resolved,
             'preview': 'Confirm plan change', 'result': 'Completed', 'outcome': outcome}
    if args is not MISSING:
        value['args'] = args
    return value


class CaptureInputProjection(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        BUILD.mkdir(parents=True, exist_ok=True)
        cls.work = Path(tempfile.mkdtemp(prefix='real-go-', dir=BUILD))
        cls.runtime = cls.work / 'source'
        cls.package = cls.runtime / 'internal/observability'
        cls.package.mkdir(parents=True)
        cls.source_hashes = {}
        sources = [FROZEN / 'go.mod', FROZEN / 'go.sum']
        sources += sorted(p for p in (FROZEN / 'internal/observability').glob('*.go')
                          if not p.name.endswith('_test.go'))
        for source in sources:
            raw = source.read_bytes()
            cls.source_hashes[str(source)] = sha(raw)
            target = cls.runtime / source.relative_to(FROZEN)
            target.write_bytes(raw)
        if sha((cls.package / 'content.go').read_bytes()) != CONTENT_SHA:
            raise AssertionError('Frozen content.go pin changed')
        template = Path(os.environ.get('FULL1220_CAPTURE_TEST_TEMPLATE',
            str(REPO / 'eval/behavior-review/helpers/full1220_content_projection_input_v1_test.go.txt')))
        cls.helper_raw = template.read_bytes()
        (cls.package / HELPER_NAME).write_bytes(cls.helper_raw)
        cls.env = {'PATH': '/usr/bin:/bin', 'HOME': str(Path.home()),
                   'GOROOT': '/Users/a1/开发工具/go', 'GOPATH': '/Users/a1/go',
                   'GOMODCACHE': '/Users/a1/go/pkg/mod',
                   'GOCACHE': '/Users/a1/Library/Caches/go-build',
                   'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off',
                   'GOWORK': 'off', 'SDKROOT': SDK, 'CGO_ENABLED': '1'}
        cls.binary = cls.work / 'projection.test'
        command = [str(GO), 'test', '-c', '-o', str(cls.binary), '-mod=readonly',
                   '-buildvcs=false', './internal/observability']
        (cls.work / 'build-command.json').write_bytes(encoded({'argv': command, 'env': cls.env}))
        result = subprocess.run(command, cwd=cls.runtime, env=cls.env,
                                stdin=subprocess.DEVNULL, capture_output=True, timeout=150)
        (cls.work / 'build.stdout').write_bytes(result.stdout)
        (cls.work / 'build.stderr').write_bytes(result.stderr)
        if result.returncode:
            raise AssertionError('Real Go build failed: ' + result.stderr.decode(errors='replace'))
        print('REAL_GO_ARTIFACTS=' + str(cls.work), flush=True)

    @classmethod
    def tearDownClass(cls):
        for name, expected in cls.source_hashes.items():
            if sha(Path(name).read_bytes()) != expected:
                raise AssertionError('Frozen source changed: ' + name)

    def project(self, traces, *, turns=None, job_change=None, record_change=None):
        case = Path(tempfile.mkdtemp(prefix=self._testMethodName + '-', dir=self.work))
        (case / 'records').mkdir()
        observations = turns if turns is not None else [
            {'turn': {'message': 'Change my plan', 'response': {
                'reply': 'Please confirm', 'trace': traces}}}]
        record = {'result': {'observations': observations}}
        if record_change:
            record_change(record)
        raw = encoded(record)
        (case / 'records/record.json').write_bytes(raw)
        job = {'schema': 'full1220-content-projection-input.v1',
               'manifest_sha256': '1'*64, 'receipt_sha256': '2'*64,
               'scope_sha256': '3'*64, 'execution_sha256': '4'*64,
               'archive_commit': '5'*40, 'archive_tree': '6'*40,
               'content_source_sha256': CONTENT_SHA, 'helper_sha256': sha(self.helper_raw),
               'window_id': 'synthetic-window', 'private_settings_sha256': '7'*64,
               'rows': [{'trace_id': '8'*32, 'recording_file': 'record.json',
                         'recording_sha256': sha(raw), 'observed_turns': len(observations), 'metadata': {}}]}
        if job_change:
            job_change(job)
        input_raw = encoded(job)
        (case / 'input.json').write_bytes(input_raw)
        env = dict(self.env, FULL1220_PROJECTION_INPUT=str(case / 'input.json'),
                   FULL1220_PROJECTION_OUTPUT=str(case / 'projection.json'),
                   FULL1220_PROJECTION_INPUT_SHA256=sha(input_raw))
        command = [str(self.binary), '-test.run', '^TestFull1220FrozenContentProjection$',
                   '-test.v', '-test.count=1', '-test.timeout=2m']
        result = subprocess.run(command, cwd=self.package, env=env,
                                input=encoded({'hash_key': SYNTHETIC_KEY}),
                                capture_output=True, timeout=125)
        (case / 'helper.stdout').write_bytes(result.stdout)
        (case / 'helper.stderr').write_bytes(result.stderr)
        (case / 'receipt.json').write_bytes(encoded({'exit_code': result.returncode,
            'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)}))
        output = case / 'projection.json'
        if result.returncode:
            self.assertFalse(output.exists(), 'Rejected evidence must not create output')
            return result, None
        projected_raw = output.read_bytes()
        receipts = re.findall(rb'(?m)^\s+full1220_content_projection_v8_test\.go:[0-9]+: FULL1220_PROJECTION_OUTPUT_SHA256=([0-9a-f]{64})$', result.stdout)
        self.assertEqual(receipts, [sha(projected_raw).encode()])
        value = json.loads(projected_raw)
        self.assertEqual(value['helper_sha256'], sha(self.helper_raw))
        self.assertEqual(value['input_sha256'], sha(input_raw))
        self.assertEqual(value['new_external_requests'], 0)
        return result, value

    def tools(self, traces):
        result, value = self.project(traces)
        self.assertEqual(result.returncode, 0, result.stdout.decode(errors='replace'))
        return value['rows'][0]['turns'][0]['tools']

    def rejects(self, item):
        result, _ = self.project([item])
        self.assertNotEqual(result.returncode, 0, 'Unreconstructible evidence was accepted')

    def test_name_only_preview_and_resolved_execute_literal(self):
        # Literal original H40 turn-2 evidence from diagnosis 90d7bd02, copied
        # into a synthetic recording (not reloaded from a live operation).
        preview = {
            'tool': 'change_plan', 'args': '{"plan_name":"月度·旗舰"}',
            'resolved_args': '{"plan_id":2,"plan_name":"月度·旗舰"}',
            'phase': 'preview', 'outcome': 'pending', 'confirmed': False,
            'preview': '将把当前订阅改为定期套餐「月度·旗舰」：下期续费生效，当期已订购额度与到期日不变。确认后生效。',
            'result': '等待确认', 'duration_ms': 20.243417}
        execute = trace('execute', args='{"plan_id":2,"plan_name":"月度·旗舰"}',
                        resolved='{"plan_id":2,"plan_name":"月度·旗舰"}', outcome='success')
        tools = self.tools([preview, execute])
        self.assertEqual(tools, [
            {'name': 'tool.preview.change_plan', 'phase': 'preview',
             'input': {'plan_name': '月度·旗舰'},
             'output': '将把当前订阅改为定期套餐「月度·旗舰」：下期续费生效，当期已订购额度与到期日不变。确认后生效。'},
            {'name': 'tool.change_plan', 'phase': 'execute',
             'input': {'plan_name': '月度·旗舰', 'plan_id': 2}, 'output': 'Completed'}])

    def test_explicit_id_and_uncanonicalized_name_preview(self):
        tools = self.tools([
            trace(args='{"plan_id":2}', resolved='{"plan_id":2,"plan_name":"Professional"}'),
            trace(args='{"plan_name":" professional "}',
                  resolved='{"plan_id":2,"plan_name":"Professional"}')])
        self.assertEqual([t['input'] for t in tools],
                         [{'plan_id': 2}, {'plan_name': ' professional '}])

    def test_cancel_empty_and_local_amount_are_not_augmented(self):
        # tools.go argInt truncates 12.34 to local int64(12); neither the
        # input capture nor persisted map becomes that local business value.
        tools = self.tools([
            trace(args='{}', resolved='{}', tool='cancel_subscription'),
            trace(args='{"amount_money":12.34}',
                  resolved='{"amount_money":12.34}', tool='recharge')])
        self.assertEqual([t['input'] for t in tools], [{}, {'amount_money': 12.34}])

    def test_read_and_execute_preserve_resolved_source(self):
        # tools.go uses local default 3 and clamps to 1..10, without changing
        # args. The synthetic unused/malformed call strings also establish that
        # only preview requires the newly selected call-time evidence.
        tools = self.tools([
            trace('read', args=MISSING, resolved='{}', tool='get_recent_bills', outcome='success'),
            trace('read', args='{', resolved='{"limit":0}', tool='get_recent_bills', outcome='success'),
            trace('read', args=None, resolved='{"limit":999}', tool='get_recent_bills', outcome='success'),
            trace('read', args='{}', resolved='{"limit":3}', tool='get_recent_bills', outcome='success'),
            trace('execute', args=MISSING, resolved='{"amount_money":12.34}', tool='recharge', outcome='success')])
        self.assertEqual([t['input'] for t in tools],
                         [{}, {'limit': 0}, {'limit': 999}, {'limit': 3}, {'amount_money': 12.34}])

    def test_go_object_parser_boundary_and_null(self):
        tools = self.tools([
            trace(args=' {"plan_id":1,"plan_id":2} \n', resolved='{"plan_id":2}'),
            trace(args='null', resolved='null', tool='cancel_subscription'),
            trace(args='{"value":9007199254740993}', resolved='{}')])
        self.assertEqual([t['input'] for t in tools], [{'plan_id': 2}, None, {'value': 9007199254740992}])

    def test_preview_missing_nonstring_and_malformed_evidence_rejected(self):
        for args in [MISSING, None, {}, 2, '', '{', '{} trailing', '[]', 'true', '"{}"', '{"n":1e999}']:
            with self.subTest(args=args):
                self.rejects(trace(args=args))

    def test_resolved_args_guard_preserved_for_all_phases(self):
        for phase in ['preview', 'read', 'execute']:
            for resolved in ['', '{', '[]', '{"n":1e999}']:
                with self.subTest(phase=phase, resolved=resolved):
                    self.rejects(trace(phase, resolved=resolved))

    def test_failure_and_unknown_phase_remain_rejected(self):
        self.rejects(trace(outcome='failure'))
        self.rejects(trace(phase=''))
        self.rejects(trace(phase='invented'))

    def test_original_and_selected_map_width_guard(self):
        wide = json.dumps({str(i): i for i in range(33)})
        for args, resolved in [(wide, '{}'), ('{}', wide),
                               ('{"nested":[' + wide + ']}', '{}')]:
            with self.subTest(args=args, resolved=resolved):
                self.rejects(trace(args=args, resolved=resolved))
        narrow = json.dumps({str(i): i for i in range(32)})
        self.assertEqual(len(self.tools([trace(args=narrow, resolved=narrow)])[0]['input']), 32)

    def test_content_redaction_depth_and_array_limits_unchanged(self):
        tools = self.tools([trace(args='{"password":"raw","note":"person@example.com"}', resolved='{}'),
                            trace(args=json.dumps({'values': list(range(33))}), resolved='{}'),
                            trace(args='{"a":{"b":{"c":{"d":{"e":{"f":{"g":{"h":{"i":"leaf"}}}}}}}}}', resolved='{}')])
        self.assertEqual(tools[0]['input'], {'password': '[REDACTED]', 'note': '[REDACTED]'})
        self.assertEqual(tools[1]['input'], {'values': list(range(32)) + ['[TRUNCATED]']})
        self.assertEqual(tools[2]['input'],
                         {'a': {'b': {'c': {'d': {'e': {'f': {'g': {'h': {'i': '[DEPTH LIMIT]'}}}}}}}}})

    def test_original_identity_and_turn_guards(self):
        changes = [lambda j: j.update(helper_sha256='0'*64),
                   lambda j: j.update(content_source_sha256='0'*64),
                   lambda j: j['rows'][0].update(recording_sha256='0'*64),
                   lambda j: j['rows'][0].update(observed_turns=2),
                   lambda j: j['rows'].append(j['rows'][0].copy())]
        for change in changes:
            with self.subTest(change=change):
                result, _ = self.project([trace()], job_change=change)
                self.assertNotEqual(result.returncode, 0)


if __name__ == '__main__':
    unittest.main(verbosity=2)
