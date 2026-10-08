import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name('analyze.py')


def prediction(i, ms=100, failed=False, fast=False, calls=2):
    return {'Sample': {'id': str(i), 'text': 'balance', 'intent': 'get_balance',
                       'risk': 'low', 'needs_confirm': False},
            'HTTPCode': 500 if failed else 200, 'Err': 'upstream' if failed else '',
            'Predicted': 'get_balance', 'AllTools': ['get_balance'], 'JevBlocked': False,
            'Pending': False, 'PendingTool': '', 'Latency': ms * 1000000,
            'Telemetry': {'llm_calls': calls, 'jev_calls': 1, 'jev_errors': 0,
                          'jev_fast_path': fast, 'jev_fast_path_eligible': True,
                          'jev_fast_path_reason': 'accepted' if fast else 'low_confidence',
                          'jev_intent_confidence': .95, 'jev_fast_path_tool': 'get_balance',
                          'llm_usage_responses': calls, 'llm_usage_missing': 0,
                          'llm_usage_protocol': 'openai', 'llm_input_tokens': calls * 10,
                          'llm_output_tokens': calls * 2, 'llm_cache_hit_tokens': calls * 4,
                          'llm_cache_miss_tokens': calls * 6}}


def report(preds, mode='on'):
    return {'SchemaVersion': 2, 'JevMode': mode, 'Model': 'model',
            'DatasetSHA256': 'hash', 'Workers': 4, 'Preds': preds, 'AdvPreds': [], 'Metrics': {}}


class ComparisonTest(unittest.TestCase):
    def run_compare(self, baseline, hint=None, fast=None, pricing=None, existing=False):
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent) as tmp:
            root = Path(tmp)
            paths = []
            for name, data in [('baseline', baseline), ('hint', hint or copy.deepcopy(baseline)),
                               ('fast', fast or copy.deepcopy(baseline))]:
                data = copy.deepcopy(data)
                data['JevMode'] = 'off' if name == 'baseline' else 'on'
                path = root / (name + '.json')
                path.write_text(json.dumps(data), encoding='utf-8')
                paths.extend(['--' + name, str(path)])
            out = root / 'new'
            if existing:
                out.mkdir()
                (out / 'sentinel').write_text('untouched')
            args = [sys.executable, str(SCRIPT), 'compare', *paths, '--out', str(out)]
            if pricing is not None:
                price = root / 'prices.json'
                price.write_text(json.dumps(pricing))
                args.extend(['--prices', str(price)])
            result = subprocess.run(args, capture_output=True, text=True)
            output = json.loads((out / 'comparison.json').read_text(encoding='utf-8')) if (out / 'comparison.json').exists() else None
            if existing:
                self.assertEqual((out / 'sentinel').read_text(), 'untouched')
            return result, output

    def test_failures_are_in_latency_and_coverage_denominators(self):
        data = report([prediction(1, 100, fast=True), prediction(2, 900, failed=True, fast=True)])
        result, output = self.run_compare(data)
        self.assertEqual(result.returncode, 0, result.stderr)
        mode = output['runs']['fast']['normal']
        self.assertEqual(mode['latency_all_ms']['average'], 500)
        self.assertEqual(mode['latency_success_ms']['average'], 100)
        self.assertEqual(mode['successful_fast_path_coverage'], .5)
        self.assertEqual(mode['failures'], 1)
        self.assertEqual(mode['accepted_path_accuracy_all'], .5)

    def test_no_success_returns_unavailable(self):
        result, output = self.run_compare(report([prediction(1, failed=True)]))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(output['runs']['fast']['normal']['latency_success_ms']['average'])
        self.assertIsNone(output['paired_latency']['all_three_success']['runs']['fast']['average'])

    def test_unequal_ids_content_model_hash_workers_rejected(self):
        data = report([prediction(1)])
        for mutation in ['id', 'text', 'Model', 'DatasetSHA256', 'Workers']:
            other = copy.deepcopy(data)
            if mutation in ['id', 'text']:
                other['Preds'][0]['Sample'][mutation] = 'different'
            else:
                other[mutation] = 'different'
            result, output = self.run_compare(data, fast=other)
            self.assertNotEqual(result.returncode, 0, mutation)
            self.assertIsNone(output)

    def test_missing_telemetry_is_not_zero_usage_or_savings(self):
        p = prediction(1)
        p['Telemetry'] = None
        result, output = self.run_compare(report([p]))
        self.assertEqual(result.returncode, 0, result.stderr)
        usage = output['runs']['fast']['all']['usage']
        self.assertFalse(usage['complete'])
        self.assertEqual(usage['missing_sample_ids'], ['1'])
        self.assertIsNone(output['llm_attempts_avoided']['baseline_minus_fast'])

    def test_absent_price_never_estimates_fee(self):
        result, output = self.run_compare(report([prediction(1)]))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(output['runs']['fast']['all']['estimated_cost']['total'])

    def test_pairing_uses_same_success_ids_not_independent_means(self):
        base = report([prediction(1, 100), prediction(2, 900)])
        hint = report([prediction(2, 200), prediction(1, 50, failed=True)])
        fast = report([prediction(1, 30), prediction(2, 300)])
        result, output = self.run_compare(base, hint, fast)
        self.assertEqual(result.returncode, 0, result.stderr)
        paired = output['paired_latency']['all_three_success']
        self.assertEqual(paired['ids'], ['2'])
        self.assertEqual(paired['runs']['baseline']['average'], 900)
        self.assertEqual(paired['runs']['fast']['average'], 300)

    def test_existing_output_rejected(self):
        result, output = self.run_compare(report([prediction(1)]), existing=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(output)

    def test_selected_failed_path_kept_in_attempt_savings_and_usage(self):
        base = report([prediction(1, calls=3), prediction(2, calls=4)])
        fast = report([prediction(1, failed=True, fast=True, calls=0), prediction(2, calls=4)])
        result, output = self.run_compare(base, fast=fast)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['llm_attempts_avoided']['ids'], ['1'])
        self.assertEqual(output['llm_attempts_avoided']['baseline_minus_fast'], 3)
        self.assertEqual(output['runs']['fast']['normal']['accepted_failed_ids'], ['1'])

    def test_incomplete_usage_and_anthropic_cost_unavailable(self):
        p = prediction(1)
        p['Telemetry']['llm_usage_missing'] = 1
        p['Telemetry']['llm_usage_responses'] = 1
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3, 'jev_per_request': .01}
        result, output = self.run_compare(report([p]), pricing=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(output['runs']['fast']['all']['estimated_cost']['total'])
        p['Telemetry']['llm_usage_missing'] = 0
        p['Telemetry']['llm_usage_responses'] = 2
        p['Telemetry']['llm_usage_protocol'] = 'anthropic'
        result, output = self.run_compare(report([p]), pricing=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(output['runs']['fast']['all']['estimated_cost']['total'])

    def test_complete_explicit_prices_compute_estimate_units(self):
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3, 'jev_per_request': .01}
        result, output = self.run_compare(report([prediction(1)]), pricing=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertAlmostEqual(output['runs']['fast']['all']['estimated_cost']['total'], .010044)

    def test_matched_quality_does_not_hide_new_errors_or_missing_usage(self):
        base = report([prediction(1), prediction(2)])
        fast = report([prediction(1, failed=True, fast=True), prediction(2, fast=True)])
        fast['Preds'][1]['Telemetry']['llm_usage_missing'] = 1
        fast['Preds'][1]['Telemetry']['llm_usage_responses'] = 1
        result, output = self.run_compare(base, fast=fast)
        self.assertEqual(result.returncode, 0, result.stderr)
        quality = output['matched_quality']['baseline_to_fast']
        self.assertEqual(quality['new_failure_ids'], ['1'])
        self.assertEqual(quality['path_regression_ids'], ['1'])
        self.assertEqual(output['runs']['fast']['normal']['accepted_usage']['missing_sample_ids'], ['2'])
        self.assertEqual(output['matched_complete_usage']['fast_selected_complete_ids'], ['1'])

    def test_latency_regressions_remain_negative_and_adversarial_failures_visible(self):
        base = report([prediction(1, 100)])
        base['AdvPreds'] = [prediction('adv', 10, failed=True)]
        fast = copy.deepcopy(base)
        fast['Preds'][0]['Latency'] = 300000000
        result, output = self.run_compare(base, fast=fast)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['paired_latency']['all_three_success']['baseline_minus_fast_ms']['average'], -200)
        self.assertEqual(output['runs']['fast']['all']['attempts'], 2)
        self.assertEqual(output['runs']['fast']['adversarial']['failure_details'][0]['id'], 'adv')

    def test_jev_token_tariff_uses_actual_complete_usage(self):
        p = prediction(1)
        p['Telemetry'].update(jev_input_tokens=30, jev_output_tokens=5,
                              jev_usage_responses=1, jev_usage_missing=0)
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3,
                  'jev_input_per_million': 10, 'jev_output_per_million': 0}
        result, output = self.run_compare(report([p]), pricing=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        cost = output['runs']['fast']['all']['estimated_cost']
        self.assertAlmostEqual(cost['total'], .000344)
        self.assertEqual(cost['jev_observed_units']['jev_input_tokens'], 30)
        self.assertEqual(cost['jev_pricing_basis'], 'tokens')

    def test_jev_token_tariff_missing_usage_never_assumed_zero(self):
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3,
                  'jev_input_per_million': 10, 'jev_output_per_million': 0}
        p = prediction(1)
        for partial in ({}, {'jev_input_tokens': 30, 'jev_output_tokens': 5,
                             'jev_usage_responses': 0, 'jev_usage_missing': 1}):
            p['Telemetry'].update(partial)
            result, output = self.run_compare(report([p]), pricing=prices)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIsNone(output['runs']['fast']['all']['estimated_cost']['total'])
            self.assertFalse(output['runs']['fast']['all']['jev_usage']['complete'])

    def test_jev_request_and_token_tariffs_are_mutually_exclusive(self):
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3,
                  'jev_per_request': .01, 'jev_input_per_million': 10,
                  'jev_output_per_million': 0}
        result, output = self.run_compare(report([prediction(1)]), pricing=prices)
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(output)

    def test_jev_zero_calls_can_have_complete_zero_token_cost(self):
        p = prediction(1)
        p['Telemetry']['jev_calls'] = 0
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3,
                  'jev_input_per_million': 10, 'jev_output_per_million': 0}
        result, output = self.run_compare(report([p]), pricing=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output['runs']['fast']['all']['estimated_cost']['jev_estimate'], 0)

    def test_selected_matched_cost_keeps_failed_selected_ids(self):
        base = report([prediction(1), prediction(2)])
        fast = report([prediction(1, failed=True, fast=True, calls=0), prediction(2)])
        prices = {'currency': 'CNY', 'cache_hit_input_per_million': 1,
                  'cache_miss_input_per_million': 2, 'output_per_million': 3, 'jev_per_request': .01}
        result, output = self.run_compare(base, fast=fast, pricing=prices)
        self.assertEqual(result.returncode, 0, result.stderr)
        matched = output['matched_fast_selected_cost']
        self.assertEqual(matched['ids'], ['1'])
        self.assertAlmostEqual(matched['baseline_minus_fast_estimated_cost'], .000044)
        self.assertEqual(matched['runs']['fast']['usage']['observed_llm_attempts'], 0)


if __name__ == '__main__':
    unittest.main()
