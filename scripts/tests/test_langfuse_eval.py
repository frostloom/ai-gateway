import importlib.util
import json
import unittest
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]


def load():
    spec = importlib.util.spec_from_file_location('langfuse_eval', ROOT / 'scripts/langfuse-eval.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def report(status='needs_review', checks=None):
    return {'schema': 'small-paired-report.v1', 'journal_sha256': 'a' * 64,
            'evidence_kind': 'local_stub', 'planned_arms': 1, 'finished_arms': 1, 'unrun_arms': 0,
            'rows': [{'key': 'balance-020/explicit_scope_followup', 'arm': 'model',
                      'langfuse_trace_id': 'b' * 32, 'status': status, 'structural_pass': True,
                      'observed_turns': 2,
                      'fact_checks': checks or [], 'fact_errors': 0, 'http_failures': 0,
                      'necessary_clarifications': [{'turn': 1, 'recorded_action': 'unreviewed',
                                                   'semantic_review': 'pending_independent_review'}]}]}


class LangfuseEvaluationTests(unittest.TestCase):
    def test_pending_review_is_not_a_zero_success_score(self):
        p = load().build_plan([report()])
        scores = {x['name']: x for x in p['scores']}
        self.assertEqual(scores['task_success']['value'], 'unknown')
        self.assertEqual(scores['evaluation_status']['value'], 'needs_review')
        self.assertEqual(scores['fact_check_status']['value'], 'not_evaluated')
        self.assertEqual(scores['necessary_clarification_status']['value'], 'pending_independent_review')
        self.assertEqual(p['model_calls'], 0)

    def test_partial_facts_do_not_claim_whole_answer_correctness(self):
        p = load().build_plan([report(checks=[{'status': 'match'}, {'status': 'not_evaluated'}])])
        score = next(x for x in p['scores'] if x['name'] == 'fact_check_status')
        self.assertEqual(score['value'], 'partial')
        self.assertNotIn('answer_correctness', [x['name'] for x in p['scores']])

    def test_ids_are_stable_but_different_metrics_do_not_collide(self):
        m = load()
        a, b = m.build_plan([report()]), m.build_plan([report('failed')])
        self.assertEqual([x['id'] for x in a['scores']], [x['id'] for x in b['scores']])
        self.assertEqual(len({x['id'] for x in a['scores']}), len(a['scores']))

    def test_missing_trace_is_skipped_without_fabricating_one(self):
        r = report()
        del r['rows'][0]['langfuse_trace_id']
        p = load().build_plan([r])
        self.assertEqual(p['scores'], [])
        self.assertEqual(len(p['unlinked_rows']), 1)

    def test_duplicate_trace_and_invalid_state_rejected(self):
        m = load()
        with self.assertRaises(ValueError):
            m.build_plan([report(), report()])
        with self.assertRaises(ValueError):
            m.build_plan([report('made_up_success')])

    def test_preflight_never_means_paid_authorization(self):
        c = {'AGENT_LANGFUSE_ENABLED': 'true', 'AGENT_LANGFUSE_CAPTURE_CONTENT': 'true',
             'AGENT_LANGFUSE_ENVIRONMENT': 'evaluation', 'AGENT_LANGFUSE_SAMPLE_RATE': '1',
             'AGENT_LLM_BASE_URL': 'https://maas.qianwenaiapi.com/apps/anthropic',
             'AGENT_LLM_MODEL': 'deepseek-v4.1-flash', 'AGENT_LLM_PROTOCOL': 'anthropic',
             'AGENT_LLM_API_KEY': 'private-value', 'JEV_API_KEY': 'private-jev', 'JEV_MODEL': 'jev-1.13.0'}
        c['AGENT_LANGFUSE_HASH_KEY'] = 'h' * 32
        p = load().check_settings(c)
        self.assertTrue(p['settings_valid'])
        self.assertFalse(p['ready_for_paid_run'])
        self.assertNotIn('private-value', json.dumps(p))
        del c['AGENT_LANGFUSE_HASH_KEY']
        self.assertFalse(load().check_settings(c)['settings_valid'])
        c['AGENT_LLM_BASE_URL'] = 'https://api.deepseek.com'
        self.assertFalse(load().check_settings(c)['settings_valid'])

    def test_publishing_requires_explicit_execution_before_network_or_file(self):
        m = load()
        with patch.object(m, 'Client', side_effect=AssertionError('network constructed')):
            self.assertEqual(m.main(['publish', '--report', 'missing.json', '--out', 'missing-receipt.json']), 2)

    def test_local_score_write_readback_and_stable_ids(self):
        m = load()
        saved = {}
        calls = []
        test = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_GET(self):
                test.assertTrue(self.headers['Authorization'].startswith('Basic '))
                if '/observations?' in self.path:
                    data = [{'name': 'evaluation.task', 'metadata': {'case_key': 'balance-020/explicit_scope_followup', 'arm': 'model', 'evidence_kind': 'local_stub'}}]
                    data += [{'name': 'agent.turn', 'metadata': {'turn_index': i}, 'input': '"question"', 'output': '"answer"'} for i in (1, 2)]
                else:
                    data = [{**x, 'subject': {'kind': 'trace', 'id': x['traceId']}} for x in saved.values()]
                raw = json.dumps({'data': data}).encode()
                self.send_response(200); self.end_headers(); self.wfile.write(raw)
            def do_POST(self):
                item = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                calls.append(self.path)
                saved[item['id']] = item
                self.send_response(200); self.end_headers(); self.wfile.write(b'{}')
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            c = m.Client({'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT': f'http://127.0.0.1:{server.server_port}/api/public/otel/v1/traces',
                          'AGENT_LANGFUSE_ENVIRONMENT': 'evaluation', 'LANGFUSE_PUBLIC_KEY': 'pk-local', 'LANGFUSE_SECRET_KEY': 'sk-local'})
            p = m.build_plan([report()])
            for _ in range(2):
                with tempfile.TemporaryFile(mode='w+', encoding='utf-8') as receipt:
                    m.publish(p, c, receipt)
                    receipt.seek(0)
                    self.assertEqual(json.loads(receipt.readlines()[-1])['kind'], 'verified')
            self.assertEqual(len(saved), len(p['scores']))
            self.assertEqual(calls, ['/api/public/scores'] * (len(p['scores']) * 2))
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_no_retry_after_post_failure_and_no_post_for_wrong_trace(self):
        m = load()
        p = m.build_plan([report()])
        client = unittest.mock.Mock()
        client.validate_traces.side_effect = ValueError('wrong association')
        with tempfile.TemporaryFile(mode='w+', encoding='utf-8') as receipt:
            with self.assertRaises(ValueError):
                m.publish(p, client, receipt)
        client.request.assert_not_called()
        client.validate_traces.side_effect = None
        client.validate_traces.return_value = []
        client.request.side_effect = ValueError('401 secret body must not escape')
        with tempfile.TemporaryFile(mode='w+', encoding='utf-8') as receipt:
            with self.assertRaises(ValueError):
                m.publish(p, client, receipt)
            receipt.seek(0)
            self.assertEqual([json.loads(x)['kind'] for x in receipt], ['plan', 'start'])
        self.assertEqual(client.request.call_count, 1)

    def test_empty_trace_or_blank_answer_cannot_pass_content_gate(self):
        m = load()
        client = m.Client.__new__(m.Client)
        root = {'name': 'evaluation.task', 'metadata': {'case_key': 'balance-020/explicit_scope_followup', 'arm': 'model', 'evidence_kind': 'local_stub'}}
        client.observations = lambda trace: [root]
        with self.assertRaises(ValueError):
            client.validate_traces(m.build_plan([report()]))
        client.observations = lambda trace: [root] + [{'name': 'agent.turn', 'metadata': {'turn_index': i}, 'input': '"question"', 'output': '""'} for i in (1, 2)]
        with self.assertRaises(ValueError):
            client.validate_traces(m.build_plan([report()]))

    def test_exported_string_turn_indices_and_invalid_coverage(self):
        m = load()
        client = m.Client.__new__(m.Client)
        root = {'name': 'evaluation.task', 'metadata': {'case_key': 'balance-020/explicit_scope_followup', 'arm': 'model', 'evidence_kind': 'local_stub'}}
        for indices in [('1', '2'), (1, '2')]:
            client.observations = lambda trace: [root] + [{'name': 'agent.turn', 'metadata': {'turn_index': i}, 'input': 'question', 'output': 'answer'} for i in indices]
            self.assertEqual(client.validate_traces(m.build_plan([report()]))[0]['content_turns'], 2)
        for indices in [('1', '1'), ('one', '2'), (True, '2'), ('01', '2')]:
            client.observations = lambda trace: [root] + [{'name': 'agent.turn', 'metadata': {'turn_index': i}, 'input': 'question', 'output': 'answer'} for i in indices]
            with self.assertRaises(ValueError):
                client.validate_traces(m.build_plan([report()]))

    def test_startup_revalidates_frozen_profile_bytes(self):
        m = load()
        spec = importlib.util.spec_from_file_location('observed_test', ROOT / 'scripts/prepare-observed-customer-pilot.py')
        generator = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(generator)
        raw = m.encoded(generator.prepare(ROOT))
        with tempfile.TemporaryDirectory() as directory:
            p = Path(directory)
            (p / 'proposal.json').write_bytes(raw)
            (p / 'manifest.json').write_bytes(m.encoded({'schema': 'observed-preparation-manifest.v2',
                'generator_sha256': m.sha((ROOT / 'scripts/prepare-observed-customer-pilot.py').read_bytes()),
                'files': {'proposal.json': m.sha(raw)}}))
            self.assertEqual(m.verify_bindings(p), m.sha(raw))
            original = Path.read_bytes
            profile = ROOT / 'eval/runtime-profiles/2026-10-13-six-profiles-v1/profiles.json'
            with patch.object(Path, 'read_bytes', lambda path: b'corrupt profile' if path == profile else original(path)):
                with self.assertRaises(ValueError):
                    m.verify_bindings(p)
    def test_reject_remote_or_redirect_prone_credential_endpoints(self):
        m = load()
        for endpoint in ('https://other.example/api/public/otel/v1/traces',
                         'http://localhost:13000/api/public/otel/v1/traces?key=bad',
                         'http://user:password@localhost:13000/api/public/otel/v1/traces'):
            with self.assertRaises(ValueError):
                m.Client({'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT': endpoint})


if __name__ == '__main__':
    unittest.main()
