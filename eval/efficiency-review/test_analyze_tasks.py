import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name('analyze_tasks.py')


def report(arm):
    rows = []
    for i in range(24):
        t = {'llm_calls': 1, 'jev_calls': 0, 'llm_usage_responses': 1,
             'llm_usage_missing': 0, 'llm_input_tokens': 100, 'llm_output_tokens': 10,
             'llm_cache_hit_tokens': 40, 'llm_cache_miss_tokens': 60,
             'llm_usage_protocol': 'openai', 'jev_fast_path': False}
        rows.append({'case': {'id': str(i), 'category': 'recharge', 'message': 'pay', 'missing_slot': 'amount'},
                     'fixture': {'tenant_id': (arm + 1) * 100 + i}, 'success': True,
                     'setup_error': False, 'errors': [], 'clarification_offered': True,
                     'duration_ms': 100 + i, 'turns': [{'stage': 'request', 'status': 200,
                     'latency_ms': 100, 'raw_body': json.dumps({'telemetry': t}),
                     'response': {'telemetry': {'llm_calls': 999}}, 'error': ''}]})
    return {'schema': 'taskbench.v2', 'dataset_sha256': 'same', 'run_id': str(arm),
            'metadata': {'jev_mode': 'off' if arm == 0 else 'on', 'model': 'model',
                         'source_git': 'source', 'binary_sha256': 'binary',
                         'evaluator_binary_sha256': 'eval'}, 'results': rows,
            'metrics': {'completion_rate': 1, 'clarification_coverage': 1},
            'limitations': ['authoritative original rubric']}


class TasksTest(unittest.TestCase):
    def run_compare(self, mutate=None, prices=None, existing=False):
        reports = [report(i) for i in range(3)]
        if mutate:
            mutate(reports)
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent) as directory:
            root = Path(directory)
            args = [sys.executable, str(SCRIPT), 'compare']
            for name, value in zip(('baseline', 'hint', 'fast'), reports):
                path = root / (name + '.json')
                path.write_text(json.dumps(value), encoding='utf-8')
                args.extend(['--' + name, str(path)])
            out = root / 'out'
            if existing:
                out.mkdir()
            args.extend(['--out', str(out)])
            if prices:
                path = root / 'prices.json'
                path.write_text(json.dumps(prices))
                args.extend(['--prices', str(path)])
            result = subprocess.run(args, capture_output=True, text=True)
            output = json.loads((out / 'comparison.json').read_text(encoding='utf-8')) if (out / 'comparison.json').exists() else None
            return result, output

    def test_raw_telemetry_and_source_rubric_are_preserved(self):
        result, output = self.run_compare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['runs']['fast']['usage']['observed_llm_attempts'], 24)
        self.assertEqual(output['runs']['fast']['source_metrics']['clarification_coverage'], 1)
        self.assertTrue(output['runs']['fast']['tasks']['0']['success'])
        self.assertEqual(output['runs']['fast']['usage']['observed_tokens']['llm_input_tokens'], 2400)

    def test_failed_task_and_missing_raw_body_are_not_hidden(self):
        def mutate(reports):
            task = reports[2]['results'][0]
            task.update(success=False, errors=['failed state verification'], duration_ms=1000)
            task['turns'][0]['raw_body'] = 'not json'
        result, output = self.run_compare(mutate)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['runs']['fast']['failure_details'][0]['id'], '0')
        self.assertFalse(output['runs']['fast']['usage']['complete'])
        self.assertEqual(output['paired_latency']['all_three_success']['count'], 23)
        self.assertEqual(output['runs']['fast']['latency_all_tasks_ms']['count'], 24)

    def test_changed_cases_or_provenance_or_reused_tenants_rejected(self):
        mutations = [lambda r: r[2]['results'][0]['case'].update(message='different'),
                     lambda r: r[2]['metadata'].update(model='different'),
                     lambda r: r[2]['metadata'].update(source_git='different'),
                     lambda r: r[2].update(dataset_sha256='different'),
                     lambda r: r[2]['results'][0]['fixture'].update(tenant_id=100),
                     lambda r: r[2]['results'].pop()]
        for mutate in mutations:
            result, output = self.run_compare(mutate)
            self.assertNotEqual(result.returncode, 0)
            self.assertIsNone(output)

    def test_nonfast_confirmation_reduction_not_attributed_to_fast_path(self):
        def mutate(reports):
            for task in reports[2]['results']:
                task['turns'][0]['stage'] = 'confirm'
                task['turns'][0]['raw_body'] = json.dumps({'telemetry': {'llm_calls': 0, 'jev_calls': 0, 'jev_fast_path': False}})
        result, output = self.run_compare(mutate)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['fast_selected_task_comparison']['ids'], [])
        self.assertEqual(output['non_fast_selected_task_comparison']['baseline_minus_fast_llm_attempts'], 24)
        self.assertIn('not attributable', output['non_fast_selected_task_comparison']['interpretation'])

    def test_selected_task_failure_included_in_attempts_avoided(self):
        def mutate(reports):
            task = reports[2]['results'][0]
            task.update(success=False, errors=['tool failed'])
            task['turns'][0]['raw_body'] = json.dumps({'telemetry': {'llm_calls': 0, 'jev_calls': 0,
                                                                      'jev_fast_path': True, 'jev_fast_path_reason': 'tool_error'}})
        result, output = self.run_compare(mutate)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['fast_selected_task_comparison']['ids'], ['0'])
        self.assertEqual(output['fast_selected_task_comparison']['baseline_minus_fast_llm_attempts'], 1)
        self.assertEqual(output['paired_latency']['fast_selected_all_three_success']['count'], 0)

    def test_output_immutable_and_missing_prices_unavailable(self):
        result, output = self.run_compare(existing=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(output)
        result, output = self.run_compare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(output['runs']['fast']['estimated_cost']['total'])

    def test_task_cost_includes_every_turn_and_failed_http_attempt(self):
        def mutate(reports):
            task = reports[2]['results'][0]
            second = copy.deepcopy(task['turns'][0])
            second.update(stage='confirm', status=500, error='upstream failed')
            task['turns'].append(second)
            task.update(success=False, errors=['upstream failed'])
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3,
                  'jev_input_per_million': 100, 'jev_output_per_million': 0}
        result, output = self.run_compare(mutate, prices=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        task = output['runs']['fast']['tasks']['0']
        self.assertEqual(task['usage']['observed_llm_attempts'], 2)
        self.assertAlmostEqual(task['estimated_cost']['total'], .00038)
        self.assertEqual(task['request_failure_details'][0]['id'], '0:1')
        self.assertEqual(output['runs']['fast']['turn_attempts'], 25)


if __name__ == '__main__':
    unittest.main()
