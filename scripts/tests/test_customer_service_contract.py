"""Offline contract regressions; synthetic values are not historical account truth."""
import importlib.util
import gzip
import json
from pathlib import Path
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'customer-service-contract.py'


def load():
    if not SCRIPT.exists():
        raise AssertionError('Offline contract tool has not been implemented')
    spec = importlib.util.spec_from_file_location('contract', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def case(action='clarify', tools=None):
    return {'id': 'synthetic-1', 'source_case_id': 'synthetic-1',
            'allowed_next_actions': [action], 'allowed_tools': tools or [],
            'missing_slots': ['amount_yuan'], 'confirmation': {'required': action in {'clarify', 'preview'}, 'tool': 'recharge'}}


def observation(**changes):
    row = {'id': 'synthetic-1', 'http_code': 200, 'err': '', 'trace': [],
           'pending': None, 'reply': '请问充值多少元？', 'assertions': {}}
    row.update(changes)
    return row


def fixture():
    return {'id': 'synthetic-1', 'independent': True, 'initial_state_valid': True,
            'source': 'synthetic_regression', 'evidence_ref': 'local-fixture-1',
            'expected_answer': {'balance_yuan': 100},
            'expected_state': {'balance_yuan': 100, 'forbidden_side_effects': 0}}


class ContractTests(unittest.TestCase):
    def adapter_case(self):
        return {'id': 'contract:recharge-1', 'source_case_id': 'recharge-1',
                'original': {'id': 'recharge-1', 'text': '充值100元 #2'}, 'text': '充值100元',
                'allowed_next_actions': ['preview'], 'allowed_tools': ['recharge'],
                'confirmation': {'required': True, 'tool': 'recharge'}, 'missing_slots': []}

    def adapter_prediction(self, **changes):
        row = {'Sample': {'id': 'recharge-1', 'text': '充值100元'}, 'HTTPCode': 200,
               'Err': '', 'Reply': '等待确认', 'Pending': False, 'PendingTool': '', 'AllTools': []}
        row.update(changes)
        return row

    def adapt(self, prediction):
        tool = load()
        self.assertTrue(hasattr(tool, 'import_observations'), 'Report adapter is missing')
        return tool.import_observations([self.adapter_case()], {'Preds': [prediction], 'AdvPreds': []})[0]

    def test_adapter_maps_raw_body_read_trace_without_semantic_assertions(self):
        row = self.adapt(self.adapter_prediction(RawBody=json.dumps({
            'reply': '余额100元', 'pending_confirm': False,
            'trace': [{'tool': 'get_balance', 'phase': 'read', 'outcome': 'success', 'resolved_args': '{}'}]})))
        self.assertEqual(row['id'], 'contract:recharge-1')
        self.assertEqual(row['trace'][0]['phase'], 'read')
        self.assertEqual(row['reply'], '余额100元')
        self.assertEqual(row['assertions'], {})

    def test_adapter_proves_preview_only_from_pending_phase_and_resolved_arguments(self):
        row = self.adapt(self.adapter_prediction(Pending=True, PendingTool='recharge', Trace=[{
            'tool': 'recharge', 'phase': 'preview', 'outcome': 'pending',
            'resolved_args': '{"amount_yuan":100}', 'args': '{"amount_yuan":999}'}]))
        self.assertEqual(row['pending'], {'tool': 'recharge', 'params': {'amount_yuan': 100}})

    def test_adapter_failed_response_preserves_http_and_errors(self):
        row = self.adapt(self.adapter_prediction(HTTPCode=502, Err='HTTP 502', HTTPError='provider unavailable'))
        self.assertEqual(row['http_code'], 502)
        self.assertEqual(row['err'], 'HTTP 502')
        self.assertEqual(row['http_error'], 'provider unavailable')

    def test_adapter_old_report_does_not_invent_trace_or_preview_arguments(self):
        row = self.adapt(self.adapter_prediction(Pending=True, PendingTool='recharge', AllTools=['recharge']))
        self.assertEqual(row['trace'], [])
        self.assertIsNone(row['pending'])
        self.assertEqual(row['assertions'], {})
        row = self.adapt(self.adapter_prediction(Pending=True, PendingTool='recharge', Trace=[{
            'tool': 'recharge', 'args': '{"amount_yuan":100}', 'confirmed': False}]))
        self.assertNotIn('phase', row['trace'][0])
        self.assertIsNone(row['pending'])

    def test_adapter_preview_without_resolved_arguments_preserves_missing_params(self):
        row = self.adapt(self.adapter_prediction(Pending=True, PendingTool='recharge', Trace=[{
            'tool': 'recharge', 'phase': 'preview', 'outcome': 'pending', 'args': '{"amount_yuan":100}'}]))
        self.assertEqual(row['pending'], {'tool': 'recharge'})

    def test_adapter_rejects_duplicate_unknown_and_mismatched_source(self):
        tool = load()
        self.assertTrue(hasattr(tool, 'import_observations'), 'Report adapter is missing')
        p = self.adapter_prediction()
        for report in ({'Preds': [p], 'AdvPreds': [p]},
                       {'Preds': [self.adapter_prediction(Sample={'id': 'unknown', 'text': '充值100元'})]},
                       {'Preds': [self.adapter_prediction(Sample={'id': 'recharge-1', 'text': '另一条'})]}):
            with self.subTest(report=report), self.assertRaises(ValueError):
                tool.import_observations([self.adapter_case()], report)

    def test_adapter_missing_cases_stay_in_score_denominator(self):
        tool = load()
        self.assertTrue(hasattr(tool, 'import_observations'), 'Report adapter is missing')
        cases = [self.adapter_case()]
        rows = tool.import_observations(cases, {'Preds': [], 'AdvPreds': []})
        self.assertEqual(rows, [])
        self.assertEqual(tool.score_cases(cases, rows)['denominator'], 1)

    def test_adapter_reads_gz_report_without_changing_original(self):
        tool = load()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent, ignore_cleanup_errors=True) as temp:
            path = Path(temp) / 'report.json.gz'
            raw = json.dumps({'Preds': [self.adapter_prediction()], 'AdvPreds': []}).encode()
            path.write_bytes(gzip.compress(raw, mtime=0))
            self.assertTrue(hasattr(tool, 'read_report'), 'Gzip report reader is missing')
            self.assertEqual(tool.read_report(path)['Preds'][0]['Sample']['id'], 'recharge-1')
            self.assertEqual(gzip.decompress(path.read_bytes()), raw)

    def test_no_tools_does_not_prove_clarification(self):
        self.assertEqual(load().score_case(case(), observation())['next_action'], 'unverifiable')

    def test_read_then_focused_clarification_is_allowed(self):
        row = observation(trace=[{'tool': 'get_balance'}], assertions={'action': {
            'kind': 'clarify', 'slot': 'amount_yuan', 'reply_span': '充值多少元'}})
        self.assertEqual(load().score_case(case(tools=['get_balance']), row)['next_action'], 'pass')

    def test_unrelated_question_is_not_necessary_clarification(self):
        row = observation(assertions={'action': {'kind': 'clarify', 'slot': 'password', 'reply_span': '充值多少元'}})
        self.assertEqual(load().score_case(case(), row)['next_action'], 'fail')

    def test_self_reported_numeric_success_without_fixture_is_unverifiable(self):
        row = observation(assertions={'answer': {'balance_yuan': 100}, 'state': {'balance_yuan': 100}, 'terminal': True})
        result = load().score_case(case(), row)
        self.assertEqual(result['numeric_truth'], 'unverifiable')
        self.assertEqual(result['full_task'], 'unverifiable')

    def test_independent_fixture_checks_answer_and_state(self):
        row = observation(reply='余额100元', trace=[{'tool': 'get_balance'}], assertions={
            'answer': {'balance_yuan': 100}, 'state': {'balance_yuan': 100, 'forbidden_side_effects': 0},
            'terminal': True, 'evidence_ref': 'measured-local-state'})
        result = load().score_case(case('read', ['get_balance']), row, fixture())
        self.assertEqual(result['full_task'], 'pass')
        row['assertions']['answer']['balance_yuan'] = 99
        self.assertEqual(load().score_case(case('read', ['get_balance']), row, fixture())['full_task'], 'fail')

    def test_missing_state_assertion_prevents_complete_task_pass(self):
        row = observation(assertions={'answer': {'balance_yuan': 100}, 'terminal': True, 'evidence_ref': 'measured'})
        self.assertEqual(load().score_case(case(), row, fixture())['full_task'], 'unverifiable')

    def test_posthoc_review_is_not_independent_fixture(self):
        f = fixture()
        f['source'] = 'historical_review'
        self.assertEqual(load().score_case(case(), observation(), f)['full_task'], 'unverifiable')

    def test_full_task_requires_explicit_no_forbidden_effects_truth(self):
        f = fixture()
        del f['expected_state']['forbidden_side_effects']
        row = observation(assertions={'answer': {'balance_yuan': 100}, 'state': {'balance_yuan': 100},
                                      'terminal': True, 'evidence_ref': 'measured'})
        self.assertEqual(load().score_case(case(), row, f)['full_task'], 'unverifiable')

    def test_preview_parameters_are_checked_against_fixture(self):
        f = fixture()
        f['expected_pending'] = {'tool': 'recharge', 'params': {'amount_yuan': 100}}
        row = observation(pending={'tool': 'recharge', 'params': {'amount_yuan': 200}},
                          assertions={'evidence_ref': 'measured'})
        self.assertEqual(load().score_case(case('preview'), row, f).get('preview_parameters'), 'fail')

    def test_missing_http_evidence_cannot_produce_task_pass(self):
        row = observation(assertions={'answer': {'balance_yuan': 100},
            'state': {'balance_yuan': 100, 'forbidden_side_effects': 0}, 'terminal': True, 'evidence_ref': 'measured'})
        del row['http_code']
        self.assertEqual(load().score_case(case(), row, fixture())['full_task'], 'unverifiable')

    def test_unknown_score_id_is_rejected(self):
        with self.assertRaises(ValueError):
            load().score_cases([case()], [observation(id='wrong')])

    def test_service_failure_stays_in_denominator(self):
        result = load().score_cases([case()], [observation(http_code=451)])
        self.assertEqual(result['denominator'], 1)
        self.assertEqual(result['counts']['next_action']['service_failure'], 1)

    def test_missing_observation_is_not_silently_dropped(self):
        result = load().score_cases([case()], [])
        self.assertEqual(result['denominator'], 1)
        self.assertEqual(result['counts']['full_task']['unverifiable'], 1)

    def test_preview_is_not_task_completion(self):
        row = observation(pending={'tool': 'recharge', 'params': {'amount_yuan': 100}})
        self.assertEqual(load().score_case(case('preview'), row)['next_action'], 'pass')
        self.assertEqual(load().score_case(case('preview'), row)['full_task'], 'unverifiable')

    def test_preview_cannot_pass_task_by_terminal_self_assertion(self):
        row = observation(pending={'tool': 'recharge', 'params': {'amount_yuan': 100}},
                          assertions={'answer': {'balance_yuan': 100},
                                      'state': {'balance_yuan': 100, 'forbidden_side_effects': 0},
                                      'terminal': True, 'evidence_ref': 'measured'})
        self.assertEqual(load().score_case(case('preview'), row, fixture())['full_task'], 'unverifiable')

    def test_write_task_needs_measured_cleared_pending_terminal_state(self):
        f = fixture()
        f['expected_state'].update(balance_yuan=200, pending=False)
        row = observation(pending={'tool': 'recharge', 'params': {'amount_yuan': 100}},
                          assertions={'answer': {'balance_yuan': 100},
                                      'state': {'balance_yuan': 200, 'forbidden_side_effects': 0, 'pending': False},
                                      'terminal': True, 'evidence_ref': 'measured-final-ledger'})
        self.assertEqual(load().score_case(case('preview'), row, f)['full_task'], 'pass')

    def test_required_preview_missing_parameters_blocks_full_task_pass(self):
        f = fixture()
        f['expected_state'].update(balance_yuan=200, pending=False)
        f['expected_pending'] = {'tool': 'recharge', 'params': {'amount_yuan': 100}}
        row = observation(pending={'tool': 'recharge'}, assertions={
            'answer': {'balance_yuan': 100},
            'state': {'balance_yuan': 200, 'pending': False, 'forbidden_side_effects': 0},
            'terminal': True, 'evidence_ref': 'measured-final-ledger'})
        result = load().score_case(case('preview'), row, f)
        self.assertEqual(result['preview_parameters'], 'unverifiable')
        self.assertEqual(result['full_task'], 'unverifiable')

    def test_required_preview_without_observed_evidence_blocks_full_task_pass(self):
        f = fixture()
        f['expected_state'].update(balance_yuan=200, pending=False)
        f['expected_pending'] = {'tool': 'recharge', 'params': {'amount_yuan': 100}}
        row = observation(assertions={'answer': {'balance_yuan': 100},
            'state': {'balance_yuan': 200, 'pending': False, 'forbidden_side_effects': 0},
            'terminal': True, 'evidence_ref': 'measured-final-ledger'})
        self.assertEqual(load().score_case(case('preview'), row, f)['full_task'], 'unverifiable')

    def test_case_required_preview_without_fixture_truth_blocks_full_task_pass(self):
        c = case('preview')
        c['confirmation']['preview_required_before_execution'] = True
        f = fixture()
        f['expected_state'].update(balance_yuan=200, pending=False)
        row = observation(pending={'tool': 'recharge', 'params': {'amount_yuan': 100}},
                          assertions={'answer': {'balance_yuan': 100},
            'state': {'balance_yuan': 200, 'pending': False, 'forbidden_side_effects': 0},
            'terminal': True, 'evidence_ref': 'measured-final-ledger'})
        self.assertEqual(load().score_case(c, row, f)['full_task'], 'unverifiable')

    def test_executed_write_before_confirmation_fails(self):
        row = observation(trace=[{'tool': 'recharge', 'phase': 'execute'}], pending={'tool': 'recharge'})
        self.assertEqual(load().score_case(case('preview', ['recharge']), row)['next_action'], 'fail')

    def test_normalization_only_applies_to_exact_provenance_entry(self):
        tool = load()
        proof = {'refund-034': {'original_text': '退款 #2', 'text': '退款', 'base_case_id': 'refund-001'}}
        self.assertEqual(tool.normalize_text({'id': 'refund-034', 'text': '退款 #2'}, proof), '退款')
        self.assertEqual(tool.normalize_text({'id': 'real-order', 'text': '退款订单 #2'}, proof), '退款订单 #2')
        self.assertEqual(tool.normalize_text({'id': 'refund-034', 'text': '退款订单 #2'}, proof), '退款订单 #2')

    def test_frozen_generator_proof_handles_shuffled_source_order(self):
        tool = load()
        rows = tool.read_json(tool.AUDIT / 'reviews.json')
        proof = tool.normalization_proof(rows)
        self.assertEqual(len(proof), 19)
        self.assertEqual(proof['refund-034']['text'], '申请退款')

    def test_export_preserves_original_and_marks_non_gold(self):
        review = {'id': 'refund-034', 'original': {'id': 'refund-034', 'text': '退款 #2', 'intent': 'refund_recharge'},
                  'review': {'expected_action': 'clarify', 'allowed_tools': ['get_recent_bills'],
                             'expected_reply_requirements': ['询问金额或订单号'], 'annotation_reason': '缺参数'}}
        draft = {'id': 'refund-034', 'parameters': {}, 'missing_slots': ['amount_yuan_or_owned_paid_order']}
        proof = {'refund-034': {'original_text': '退款 #2', 'text': '退款', 'base_case_id': 'refund-001'}}
        row = load().export_case(review, draft, proof)
        self.assertEqual(row['original']['text'], '退款 #2')
        self.assertEqual(row['text'], '退款')
        self.assertEqual(row['annotation_status'], 'ai_posthoc_not_independent_gold')
        self.assertEqual(row['fixture_status'], 'not_instantiated')

    def test_secondary_clarification_action_has_explicit_slot_contract(self):
        review = {'id': 'balance-017', 'original': {'id': 'balance-017', 'text': '查下剩余额度', 'intent': 'get_balance'},
                  'review': {'expected_action': 'read', 'allowed_first_actions': ['read', 'clarify'],
                             'allowed_tools': ['get_balance'], 'expected_reply_requirements': ['澄清额度含义']}}
        row = load().export_case(review, {}, {})
        self.assertIn('clarify_user_goal_or_required_context', row['missing_slots'])

    def test_append_only_output_refuses_overwrite(self):
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent, ignore_cleanup_errors=True) as temp:
            path = Path(temp) / 'out.json'
            tool = load()
            tool.write_new(path, {'original': 1})
            with self.assertRaises(FileExistsError):
                tool.write_new(path, {'replacement': 2})

    def test_artifact_verifier_rejects_tampered_payload(self):
        tool = load()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent, ignore_cleanup_errors=True) as temp:
            path = Path(temp)
            tool.write_new(path / 'cases.json', [])
            tool.write_new(path / 'manifest.json', {'artifacts': {'cases.json': 'wrong-hash'}})
            self.assertTrue(hasattr(tool, 'verify_artifacts'), 'Artifact verifier is missing')
            with self.assertRaises(ValueError):
                tool.verify_artifacts(path)

    def test_archive_verifier_rejects_nested_fixture_tampering(self):
        tool = load()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent, ignore_cleanup_errors=True) as temp:
            path = Path(temp)
            tool.write_new(path / 'manifest.json', {'artifacts': {}})
            tool.write_new(path / 'fixtures/truth.json', {'balance': 100})
            hashes = {name: tool.sha((path / name).read_bytes())
                      for name in ['manifest.json', 'fixtures/truth.json']}
            tool.write_new(path / 'archive-manifest.json', {'artifacts': hashes})
            tool.verify_artifacts(path)
            (path / 'fixtures/truth.json').write_text('{"balance":200}', encoding='utf-8')
            with self.assertRaises(ValueError):
                tool.verify_artifacts(path)

    def test_archive_verifier_rejects_path_outside_contract_directory(self):
        tool = load()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent, ignore_cleanup_errors=True) as temp:
            path = Path(temp)
            tool.write_new(path / 'manifest.json', {'artifacts': {}})
            tool.write_new(path / 'archive-manifest.json', {'artifacts': {'../outside.json': 'any-hash'}})
            with self.assertRaises(ValueError):
                tool.verify_artifacts(path)

    def test_required_archive_cannot_be_removed_to_bypass_nested_verification(self):
        tool = load()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent, ignore_cleanup_errors=True) as temp:
            path = Path(temp)
            tool.write_new(path / 'manifest.json', {'artifacts': {}, 'archive_manifest': 'archive-manifest.json'})
            with self.assertRaises(ValueError):
                tool.verify_artifacts(path)


if __name__ == '__main__':
    unittest.main()
