import copy
import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class FullCustomerReadinessTests(unittest.TestCase):
    def setUp(self):
        self.tool = load('full_customer_audit', ROOT / 'scripts/audit-full-customer-readiness.py')
        prepare = load('original_full_prepare', ROOT / 'scripts/prepare-full-customer-pilot.py')
        original = prepare.prepare(ROOT)
        self.probe = {
            'schema': 'full-customer-offline-preflight.v1', 'source_cases': 1220,
            'paired_arms': 2440, 'planned_user_turns': 3924, 'boundary_count': 1851,
            'definition_sha256': original['source_bindings']['definition_sha256'],
            'manifest_sha256': prepare.MANIFEST_SHA, 'profile_sha256': prepare.PROFILE_SHA,
            'source_revision': 'a' * 40, 'allow_paid': False, 'ready_for_paid_run': False,
            'model_calls': 0, 'jev_calls': 0,
            'rows': [{'key': c['key'], 'source_case_id': c['source_case_id'], 'profile': c['profile'],
                      'scenario': c['key'].split('/')[1], 'turns': c['turns'],
                      'pair_run_contract': 'b' * 64, 'configuration_prepared': True,
                      'pilot_whitelist_member': c['key'] in self.tool.PILOT_KEYS} for c in original['primary']]
        }
        self.probe['pilot_whitelist_primary_count'] = sum(r['pilot_whitelist_member'] for r in self.probe['rows'])
        self.probe['prepared_runtime'] = {
            'model': 'deepseek-flash', 'provider_protocol': 'openai', 'output_budget': 128,
            'routing_mode': 'model', 'evidence_kind': 'recorded_run', 'thinking_mode': 'disabled',
            'definition_sha256': self.probe['definition_sha256'], 'manifest_sha256': prepare.MANIFEST_SHA,
            'profile_sha256': None, 'execution_revision': self.probe['source_revision'],
            'prompt_sha256': 'c' * 64, 'tool_schema_sha256': 'd' * 64}
        self.reservation = {'schema': 'paired-live-receiver-repeated-claim.v1',
                            'campaign_id': 'v5-repeated-receiver-20261027',
                            'status': 'consumed_before_execution',
                            'cumulative_reserved_model_micro_cny': 29061120,
                            'cumulative_reserved_jev_micro_usd': 223104}

    def test_exact_full_coverage_is_preparation_not_execution(self):
        result = self.tool.audit(ROOT, self.probe, self.reservation)
        self.assertEqual(len(result['cases']), 1220)
        self.assertEqual(result['source_cases'], 1220)
        self.assertEqual(result['paired_arms_per_pass'], 2440)
        self.assertEqual(result['planned_turns_per_pass'], 3924)
        self.assertEqual(result['boundary_scenarios_separate'], 1851)
        self.assertEqual(result['pilot_whitelist_primary_count'], 8)
        self.assertEqual(result['not_in_legacy_pilot_whitelist'], 1212)
        self.assertFalse(result['ready_for_paid_run'])
        self.assertFalse(result['full1220_evaluation_complete'])

    def test_budget_includes_three_passes_and_permanent_reservations(self):
        result = self.tool.audit(ROOT, self.probe, self.reservation)
        budget = result['budget']
        self.assertEqual(budget['one_pass']['model_attempts'], 19620)
        self.assertEqual(budget['one_pass']['jev_attempts'], 1962)
        self.assertEqual(budget['one_pass']['model_micro_cny'], 662999040)
        self.assertEqual(budget['one_pass']['jev_micro_usd'], 5273856)
        self.assertEqual(budget['minimum_three_passes']['model_micro_cny'], 1988997120)
        self.assertEqual(budget['minimum_three_passes']['jev_micro_usd'], 15821568)
        self.assertEqual(budget['remaining_micro_cny'], 70938880)
        self.assertEqual(budget['remaining_micro_usd'], 776896)
        self.assertFalse(budget['one_pass']['fits_existing_remaining_ceiling'])
        self.assertFalse(budget['minimum_three_passes']['fits_existing_remaining_ceiling'])
        self.assertFalse(budget['full_request_envelope_verified'])

    def test_missing_duplicate_or_changed_task_is_rejected(self):
        for mutation in ['missing', 'duplicate', 'profile', 'turns', 'scenario', 'whitelist', 'contract']:
            with self.subTest(mutation=mutation):
                probe = copy.deepcopy(self.probe)
                if mutation == 'missing': probe['rows'].pop()
                elif mutation == 'duplicate': probe['rows'][-1] = copy.deepcopy(probe['rows'][0])
                elif mutation == 'profile': probe['rows'][0]['profile'] = 'empty_balance'
                elif mutation == 'turns': probe['rows'][0]['turns'] = 2
                elif mutation == 'scenario': probe['rows'][0]['scenario'] = 'first_turn'
                elif mutation == 'whitelist': probe['rows'][0]['pilot_whitelist_member'] = not probe['rows'][0]['pilot_whitelist_member']
                elif mutation == 'contract': probe['rows'][0]['pair_run_contract'] = 'invalid'
                with self.assertRaises(ValueError): self.tool.audit(ROOT, probe, self.reservation)

    def test_paid_claims_and_false_denominators_are_rejected(self):
        for key, value in [('ready_for_paid_run', True), ('allow_paid', True), ('model_calls', 1),
                           ('source_cases', 12), ('paired_arms', 24), ('planned_user_turns', 26),
                           ('pilot_whitelist_primary_count', 1220), ('source_revision', 'invalid'),
                           ('definition_sha256', 'a' * 64), ('profile_sha256', 'a' * 64)]:
            with self.subTest(key=key):
                probe = copy.deepcopy(self.probe); probe[key] = value
                with self.assertRaises(ValueError): self.tool.audit(ROOT, probe, self.reservation)
        for key, value in [('cumulative_reserved_model_micro_cny', 0),
                           ('cumulative_reserved_jev_micro_usd', True), ('status', 'available')]:
            with self.subTest(key=key):
                reservation = dict(self.reservation); reservation[key] = value
                with self.assertRaises(ValueError): self.tool.audit(ROOT, self.probe, reservation)

    def test_runtime_budget_and_comparison_contract_must_match(self):
        mutations = {'model': 'wrong-model', 'provider_protocol': 'anthropic', 'output_budget': 65536,
                     'routing_mode': 'jev_selective', 'evidence_kind': 'local_stub',
                     'thinking_mode': 'enabled', 'definition_sha256': 'f' * 64,
                     'manifest_sha256': 'f' * 64, 'execution_revision': 'f' * 40,
                     'profile_sha256': {'active_standard': 'f' * 64},
                     'prompt_sha256': 'invalid', 'tool_schema_sha256': 'invalid'}
        for key, value in mutations.items():
            with self.subTest(key=key):
                probe = copy.deepcopy(self.probe); probe['prepared_runtime'][key] = value
                with self.assertRaises(ValueError): self.tool.audit(ROOT, probe, self.reservation)
        for runtime in [None, {}, {'model': 'wrong-model', 'output_budget': 65536}]:
            probe = copy.deepcopy(self.probe); probe['prepared_runtime'] = runtime
            with self.assertRaises(ValueError): self.tool.audit(ROOT, probe, self.reservation)


if __name__ == '__main__':
    unittest.main()
