"""Development task definitions, never model quality tests."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


def tool():
    spec = importlib.util.spec_from_file_location('define_customer_tasks', ROOT / 'scripts/define-customer-tasks.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class TaskDefinitionsTests(unittest.TestCase):
    def setUp(self):
        self.tool = tool()
        self.source = json.loads(self.tool.SOURCE.read_bytes())
        self.readiness = json.loads(self.tool.READINESS.read_bytes())
        self.result = self.tool.build(self.source, self.readiness)
        self.rows = {r['source_case_id']: r for r in self.result['definitions']}

    def test_all_sources_and_remainder_are_defined_without_claiming_execution(self):
        s = self.result['summary']
        self.assertEqual((s['source_cases'], s['newly_defined_source_cases'], s['previously_prepared_source_cases']), (1220, 1078, 142))
        self.assertEqual(len(self.rows), 1220)
        self.assertEqual(set(s['source_actions']), {'read', 'clarify', 'preview', 'respond', 'consult', 'limited', 'reject'})
        self.assertEqual(s['model_calls'], 0)
        self.assertEqual(s['database_instantiated'], 0)
        for c in self.source:
            d = self.rows[c['source_case_id']]
            self.assertEqual(d['first_turn']['allowed_actions'], c['allowed_next_actions'])
            self.assertEqual(d['first_turn']['allowed_tools'], c['allowed_tools'])
            self.assertEqual([a['requirement'] for a in d['answer_assertions']], c['required_facts'])
            self.assertFalse(d['independent_blind_reviewed'])
            self.assertFalse(d['runtime_adapter_ready'])
            self.assertTrue(d['scenarios'])

    def test_explicit_missing_slot_supplement_never_changes_first_turn(self):
        d = self.rows['recharge-001']
        self.assertEqual(d['first_turn']['preferred_action'], 'clarify')
        self.assertEqual(d['parameters_from_original'], {})
        branch = next(s for s in d['scenarios'] if s['id'] == 'supplement_then_confirm')
        self.assertIn('100元', branch['turns'][1]['user'])
        self.assertEqual(branch['operation']['arguments'], {'amount_money': 100})
        self.assertEqual(branch['terminal']['balance_delta_cent'], 10000)
        self.assertEqual(self.rows['balance-022']['first_turn']['allowed_actions'], ['clarify', 'read'])

    def test_write_oracles_refund_cancel_and_plan_are_distinct(self):
        refund = self.rows['refund-003']
        full = next(s for s in refund['scenarios'] if s['id'] == 'confirm')
        self.assertEqual(full['terminal']['balance_delta_cent'], -10000)
        capped = next(s for s in refund['scenarios'] if s['id'] == 'balance_cap')
        self.assertEqual(capped['terminal']['balance_delta_cent'], -2500)
        cancel = next(s for s in self.rows['cancel_subscription-008']['scenarios'] if s['id'] == 'confirm')
        self.assertEqual(cancel['profile'], 'active_flagship')
        self.assertEqual(cancel['terminal']['subscription_status'], 'cancelled')
        self.assertEqual(cancel['terminal']['balance_delta_cent'], 0)
        change = next(s for s in self.rows['change_plan-003']['scenarios'] if s['id'] == 'confirm')
        self.assertEqual(change['terminal']['current_plan'], 'standard')
        self.assertEqual(change['terminal']['pending_plan'], 'flagship')
        self.assertTrue(change['terminal']['cycle_end_unchanged'])
        sub = next(s for s in self.rows['subscribe-004']['scenarios'] if s['id'] == 'confirm')
        self.assertEqual(sub['profile'], 'no_subscription')
        self.assertEqual(sub['terminal']['balance_delta_cent'], 0)
        for row in [refund, self.rows['change_plan-003'], self.rows['subscribe-004']]:
            branch = next(s for s in row['scenarios'] if s['id'] == 'cancel')
            self.assertTrue(branch['terminal']['business_state_unchanged'])

    def test_rejection_consultation_and_limited_do_not_imply_successful_write(self):
        for sid in ['adv-003', 'adv-018', 'recharge-004', 'bills-058', 'models-066']:
            d = self.rows[sid]
            first = d['scenarios'][0]
            self.assertTrue(first['terminal']['business_state_unchanged'])
            self.assertEqual(first['terminal']['authorized_write_count'], 0)
        self.assertEqual(self.rows['refund-068']['operation']['tool'], 'cancel_subscription')
        self.assertEqual(self.rows['adv-014']['first_turn']['preferred_action'], 'clarify')
        self.assertTrue(self.rows['adv-014']['scenarios'][0]['terminal']['business_state_unchanged'])

    def test_source_mismatch_and_duplicate_ids_fail_closed(self):
        with self.assertRaises(ValueError):
            self.tool.build(self.source + [self.source[0]], self.readiness)
        with self.assertRaises(ValueError):
            self.tool.build(self.source, self.readiness[1:])
        altered = json.loads(json.dumps(self.readiness))
        altered[0]['evaluation_text'] = 'changed'
        with self.assertRaises(ValueError):
            self.tool.build(self.source, altered)

    def test_all_profile_and_assertion_references_resolve(self):
        profiles = self.result['profiles']['accounts']
        for d in self.rows.values():
            expected = {a['id'] for a in d['answer_assertions']}
            for scenario in d['scenarios']:
                self.assertIn(scenario['profile'], profiles)
                self.assertEqual(set(scenario['first_turn_assertion_ids']), expected)
                self.assertEqual(scenario['turns'][0]['user'], d['evaluation_text'])
                self.assertFalse(scenario['historical_truth_recovered'])
                self.assertIn('no_cross_tenant_access', scenario['invariants'])
        self.assertEqual(profiles['low_balance']['balance_cent'], 2500)
        self.assertEqual(self.result['profiles']['catalogue']['plans']['standard']['price_yuan'], 20)

    def test_profile_ledger_usage_and_supplement_price_have_authority(self):
        for p in self.result['profiles']['accounts'].values():
            total = sum(b['amount_cent'] for b in p['cash_bill_fixture'])
            self.assertEqual(p['paid_order']['amount_cent'] - total, p['balance_cent'])
            self.assertEqual(p['subscription_usage_fixture']['reserve_phase'], 'reserve')
            self.assertEqual(p['subscription_usage_fixture']['reserve_status'], 'settled')
        budget = self.rows['balance-023']
        follow = next(s for s in budget['scenarios'] if s['id'] == 'explicit_scope_followup')['turns'][1]
        self.assertIn('list_models', follow['allowed_tools'])
        self.assertEqual(follow['answer']['price_truth_source'], 'catalogue.models')

    def test_absent_subscription_does_not_erase_ambiguous_first_question(self):
        d = self.rows['cancel_subscription-020']
        no_sub = next(s for s in d['scenarios'] if s['id'] == 'no_subscription')
        self.assertEqual(no_sub['turns'][0]['expected_actions'], ['clarify'])
        self.assertFalse(no_sub['turns'][1]['pending_required'])
        self.assertTrue(no_sub['terminal']['business_state_unchanged'])

    def test_missing_refund_accepts_order_as_alternative_and_invalid_money_stays_safe(self):
        d = next(r for r in self.rows.values() if 'amount_yuan_or_owned_paid_order' in r['first_turn']['missing_slots'])
        branch = next(s for s in d['scenarios'] if s['id'] == 'owned_order_then_confirm')
        self.assertEqual(branch['terminal']['owned_seed_order_status'], 'refunded')
        self.assertEqual(branch['operation']['parameter_provenance'], 'explicit_synthetic_order_followup')
        invalid = next(s for s in self.rows['recharge-001']['scenarios'] if s['id'] == 'invalid_amount_followup')
        self.assertFalse(invalid['turns'][1]['pending_required'])
        self.assertTrue(invalid['terminal']['business_state_unchanged'])

    def test_scope_followup_retains_refund_subscription_and_catalogue_goals(self):
        for sid in ['refund-012', 'refund-073']:
            d = self.rows[sid]
            s = next(s for s in d['scenarios'] if s['id'] == 'supplement_then_confirm')
            self.assertEqual(s['operation']['tool'], 'refund_recharge')
            self.assertEqual(s['operation']['arguments']['amount_money'], 100)
            self.assertIn('退回充值款100元', s['turns'][1]['user'])
        for sid in ['cancel_subscription-062', 'cancel_subscription-065', 'cancel_subscription-066']:
            self.assertEqual(self.rows[sid]['operation']['tool'], 'cancel_subscription')
        for sid, expected_tool in [('models-043', 'list_models'), ('subscription-038', 'get_my_subscription'), ('consumption-065', 'get_consumption')]:
            s = next(s for s in self.rows[sid]['scenarios'] if s['id'] == 'explicit_scope_followup')
            self.assertIn(expected_tool, s['turns'][1]['allowed_tools'])

    def test_recent_bill_oracle_uses_profile_insert_order_and_limit(self):
        p = self.result['profiles']['accounts']['active_standard']
        bills = p['tool_expected']['get_recent_bills']['latest_3']
        self.assertEqual([b['request_id'] for b in bills], ['subscription-recent', 'cash-recent', 'cash-yesterday'])
        self.assertEqual(sum(b['amount_cent'] for b in bills), 130)
        for account in self.result['profiles']['accounts'].values():
            for bill in account['cash_bill_fixture']:
                self.assertGreater(bill['input_tokens'] + bill['output_tokens'], 0)

    def test_allowance_tiers_use_real_store_tags(self):
        catalogue = self.result['profiles']['catalogue']
        known = {'旗舰', '推理', '视觉', '长文本', '文本', '轻量', '免费'}
        for model in catalogue['models'].values():
            self.assertIn(model['tier'], known)
            self.assertIn(model['tier'], model['tags'])
        for plan in catalogue['plans'].values():
            self.assertTrue(set(plan['tier_quota_tokens']) <= known)

    def test_synthetic_current_plan_and_order_bindings_are_not_original_parameters(self):
        self.assertEqual(self.rows['cancel_subscription-001']['operation']['parameter_provenance'], 'synthetic_current_profile_reference')
        self.assertEqual(self.rows['cancel_subscription-008']['operation']['parameter_provenance'], 'original_draft_value_revalidated_in_synthetic_context')
        d = next(r for r in self.rows.values() if 'amount_yuan_or_owned_paid_order' in r['first_turn']['missing_slots'])
        branch = next(s for s in d['scenarios'] if s['id'] == 'owned_order_then_confirm')
        self.assertEqual(branch['operation']['arguments']['order_no'], '${account.paid_order.order_no}')
        self.assertIn('account.paid_order.order_no', self.result['profiles']['binding_rules'])

    def test_diagnostic_first_turn_is_never_a_full_task_success(self):
        for d in self.rows.values():
            first = d['scenarios'][0]
            self.assertEqual(first['measurement_scope'], 'first_turn_diagnostic')
            self.assertFalse(first['full_task_metric_eligible'])
            self.assertTrue(any(s['full_task_metric_eligible'] for s in d['scenarios']))
        read = self.rows['models-001']
        self.assertTrue(next(s for s in read['scenarios'] if s['id'] == 'answer_complete')['full_task_metric_eligible'])

    def test_archive_overwrite_and_tampering_rejected(self):
        with tempfile.TemporaryDirectory(dir=ROOT) as temp:
            out = Path(temp) / 'archive'
            self.tool.export(out)
            self.assertEqual(self.tool.verify(out)['newly_defined_source_cases'], 1078)
            with self.assertRaises(FileExistsError):
                self.tool.export(out)
            (out / 'definitions.json').write_text('[]', encoding='utf8')
            with self.assertRaises(ValueError):
                self.tool.verify(out)


if __name__ == '__main__':
    unittest.main()
