import copy
import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]


def load():
    spec = importlib.util.spec_from_file_location('langfuse_review_test', ROOT / 'scripts/langfuse-eval.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


def fixture():
    revision = 'c' * 40
    events = [{'kind': 'config', 'config': {name: {'execution_revision': revision}
                                            for name in ('model', 'selective')}}]
    rows, reviews = [], []
    for arm, trace in [('model', 'a' * 32), ('jev_selective', 'b' * 32)]:
        req = {'id': 'case/turn-1/action-reply', 'turn_index': 0, 'requirement': 'Read own cash balance',
               'requirement_sha256': 'e' * 64, 'reply_sha256': digest('余额 ¥5000.00')}
        rows.append({'key': 'case', 'arm': arm, 'langfuse_trace_id': trace, 'observed_turns': 1,
                     'status': 'needs_review', 'structural_pass': True, 'fact_errors': 0,
                     'http_failures': 0, 'fact_checks': [{'status': 'match'}],
                     'review_requirements': [req], 'necessary_clarifications': []})
        reviews.append({'case_key': 'case', 'arm': arm, 'verdict': 'passed',
                        'requirements': [{**req, 'verdict': 'passed'}], 'rationale': 'Matches own account oracle.'})
        events += [{'kind': 'start', 'key': 'case', 'arm': arm},
                   {'kind': 'finish', 'key': 'case', 'arm': arm, 'langfuse_trace_id': trace,
                    'result': {'observations': [{'turn': {'message': '查询余额',
                                                         'response': {'reply': '余额 ¥5000.00'}}}]}}]
    raw = ('\n'.join(json.dumps(e, ensure_ascii=False) for e in events) + '\n').encode()
    report = {'schema': 'small-paired-report.v1', 'evidence_kind': 'recorded_run',
              'journal_sha256': hashlib.sha256(raw).hexdigest(), 'rows': rows,
              'planned_arms': 2, 'finished_arms': 2, 'unrun_arms': 0}
    review = {'schema': 'pi-first-live-independent-review.v1', 'execution_revision': revision,
              'journal_sha256': report['journal_sha256'], 'reviewer': 'independent-auditor',
              'method': 'independent_agent_evidence_review', 'blocking_findings': [], 'rows': reviews}
    return report, review, raw


class IndependentReviewTests(unittest.TestCase):
    def plan(self, report=None, review=None, raw=None):
        a, b, c = fixture()
        return load().build_review_plan(report or a, review or b, raw or c, 'f' * 64)

    def test_two_arms_have_bound_review_scores_without_mutating_original(self):
        report, review, raw = fixture()
        original = copy.deepcopy(report)
        plan = self.plan(report, review, raw)
        self.assertEqual(report, original)
        self.assertEqual(len(plan['scores']), 4)
        self.assertEqual({s['name'] for s in plan['scores']}, {'independent_review_status', 'reviewed_task_success'})
        self.assertEqual({s['value'] for s in plan['scores']}, {'reviewed_pass', 'passed'})
        self.assertEqual(plan['model_calls'], 0)
        self.assertEqual(plan['jev_calls'], 0)
        self.assertTrue(all(s['metadata']['review_sha256'] == 'f' * 64 for s in plan['scores']))
        old_ids = {s['id'] for s in load().build_plan([report])['scores']}
        self.assertFalse(old_ids & {s['id'] for s in plan['scores']})
        self.assertEqual(plan, self.plan(report, review, raw))

    def test_journal_revision_trace_and_raw_reply_must_match(self):
        report, review, raw = fixture()
        for changed in [raw + b' ', raw.replace(b'"' + b'c' * 40 + b'"', b'"' + b'd' * 40 + b'"'),
                        raw.replace(b'a' * 32, b'9' * 32), raw.replace('余额 ¥5000.00'.encode(), '余额 ¥9000.00'.encode())]:
            r, v = copy.deepcopy(report), copy.deepcopy(review)
            # Rebind the outer journal digest to exercise deeper checks too.
            r['journal_sha256'] = v['journal_sha256'] = hashlib.sha256(changed).hexdigest()
            with self.subTest(raw=changed[-30:]), self.assertRaises(ValueError):
                self.plan(r, v, changed)

    def test_requirement_coverage_and_verdict_fail_closed(self):
        mutations = [lambda r, v: v['rows'].pop(),
                     lambda r, v: v['rows'].append(copy.deepcopy(v['rows'][0])),
                     lambda r, v: v['rows'][0]['requirements'].clear(),
                     lambda r, v: v['rows'][0]['requirements'].append(copy.deepcopy(v['rows'][0]['requirements'][0])),
                     lambda r, v: v['rows'][0]['requirements'][0].update(reply_sha256='0' * 64),
                     lambda r, v: v['rows'][0]['requirements'][0].update(requirement_sha256='0' * 64),
                     lambda r, v: v['rows'][0]['requirements'][0].update(verdict='failed'),
                     lambda r, v: v['rows'][0].update(verdict='failed'),
                     lambda r, v: v.update(blocking_findings=['issue']),
                     lambda r, v: v.update(reviewer=' '),
                     lambda r, v: v['rows'][0].update(rationale=''),
                     lambda r, v: r['rows'][0].update(status='failed'),
                     lambda r, v: r['rows'][0].update(structural_pass=False),
                     lambda r, v: r['rows'][0].update(fact_errors=1),
                     lambda r, v: r['rows'][0].update(http_failures=1),
                     lambda r, v: r.update(unrun_arms=1)]
        for mutate in mutations:
            r, v, raw = fixture(); mutate(r, v)
            with self.subTest(mutation=mutate), self.assertRaises(ValueError):
                self.plan(r, v, raw)

    def test_exported_reply_and_input_must_match_before_any_write(self):
        m = load(); report, review, raw = fixture()
        plan = m.build_review_plan(report, review, raw, 'f' * 64)
        client = m.Client.__new__(m.Client)
        def observations(trace):
            arm = 'model' if trace == 'a' * 32 else 'jev_selective'
            return [{'name': 'evaluation.task', 'metadata': {'case_key': 'case', 'arm': arm, 'evidence_kind': 'recorded_run'}},
                    {'name': 'agent.turn', 'metadata': {'turn_index': 1},
                     'input': json.dumps('查询余额'), 'output': json.dumps('余额 ¥5000.00')}]
        client.observations = observations
        self.assertEqual(len(client.validate_traces(plan)), 2)
        for field in ['input', 'output']:
            def changed(trace):
                data = observations(trace)
                if trace == 'b' * 32:
                    data[1][field] = json.dumps('wrong')
                return data
            client.observations = changed
            client.request = unittest.mock.Mock()
            with tempfile.TemporaryFile(mode='w+') as receipt, self.assertRaises(ValueError):
                m.publish(plan, client, receipt)
            client.request.assert_not_called()

    def test_readback_must_include_review_provenance(self):
        m = load(); plan = self.plan(); client = m.Client.__new__(m.Client)
        rows = [{**s, 'subject': {'id': s['traceId']}} for s in plan['scores']]
        client.request = lambda path: {'data': rows}
        client.verify_scores(plan['scores'])
        rows[0]['metadata'] = {**rows[0]['metadata'], 'review_sha256': '0' * 64}
        with self.assertRaises(ValueError):
            client.verify_scores(plan['scores'])

    def test_invalid_review_flags_never_construct_network_client(self):
        m = load()
        for args in [('check', '--review', 'review'), ('publish', '--review', 'review'),
                     ('plan', '--journal', 'journal'), ('plan', '--review', 'review', '--journal', 'journal')]:
            with patch.object(m, 'Client', side_effect=AssertionError('network constructed')):
                self.assertEqual(m.main([*args, '--out', '/unused-review-output']), 2)

    def test_cli_plan_publish_and_readback_preserve_inputs_and_deterministic_scores(self):
        m = load(); report, review, raw = fixture()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, value in [('report.json', report), ('review.json', review)]:
                (root / name).write_text(json.dumps(value))
            (root / 'run.jsonl').write_bytes(raw)
            args = ['--report', str(root / 'report.json'), '--review', str(root / 'review.json'),
                    '--journal', str(root / 'run.jsonl')]
            original = {p.name: p.read_bytes() for p in root.iterdir()}
            client = unittest.mock.Mock()
            client.validate_traces.return_value = []
            with patch.object(m, 'Client', return_value=client):
                self.assertEqual(m.main(['plan', *args, '--out', str(root / 'plan.json')]), 0)
                self.assertEqual(m.main(['publish', *args, '--out', str(root / 'sync.jsonl'), '--execute']), 0)
                self.assertEqual(m.main(['verify', *args, '--out', str(root / 'verified.json')]), 0)
            writes = client.request.call_args_list
            self.assertEqual(len(writes), 4)
            self.assertTrue(all(x.args[0] == '/api/public/scores' for x in writes))
            self.assertEqual({x.args[1]['name'] for x in writes}, {'independent_review_status', 'reviewed_task_success'})
            self.assertEqual(json.loads((root / 'verified.json').read_text())['score_count'], 4)
            for name, contents in original.items():
                self.assertEqual((root / name).read_bytes(), contents)

    def test_explicit_empty_review_flags_cannot_fall_back_to_original_scores(self):
        m = load(); report, _, _ = fixture()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); source = root / 'report.json'
            source.write_text(json.dumps(report))
            for mode in ('plan', 'publish', 'verify'):
                out = root / (mode + '.json')
                args = [mode, '--report', str(source), '--review', '', '--journal', '', '--out', str(out)]
                if mode == 'publish':
                    args.append('--execute')
                with patch.object(m, 'Client', side_effect=AssertionError('network constructed')):
                    self.assertEqual(m.main(args), 2)
                self.assertFalse(out.exists())


if __name__ == '__main__':
    unittest.main()
