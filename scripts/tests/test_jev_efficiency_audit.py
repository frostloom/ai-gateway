"""Read-only audit of recorded evidence; no provider calls."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('jev_efficiency_audit', ROOT / 'scripts/audit-jev-efficiency.py')
audit = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(audit)
PILOT = ROOT / 'eval/paired-pilots/2026-10-17-jev-acceptance-v1'


class EfficiencyAuditTests(unittest.TestCase):
    def documents(self):
        read = lambda name: json.loads((PILOT / name).read_text(encoding='utf-8'))
        lines = lambda name: [json.loads(x) for x in (PILOT / name).read_text(encoding='utf-8').splitlines()]
        return [read('attempt-01/report.json'), lines('attempt-01/run.jsonl'), lines('attempt-01/budget.jsonl'), read('attempt-01/execution-receipt.json'), read('manifest.json')]

    def test_real_evidence_separates_cached_input_and_estimates(self):
        result = audit.audit_documents(*self.documents())
        self.assertIn('budget_model_input_tokens', result)
        self.assertEqual(result['budget_model_input_tokens'], 21528)
        self.assertEqual(result['budget_model_output_tokens'], 442)
        self.assertEqual(result['estimated_model_micro_cny'], 46592)
        self.assertEqual(result['arms']['model']['llm_calls'], 7)
        self.assertEqual(result['arms']['jev_selective']['llm_calls'], 5)
        self.assertEqual(result['arms']['jev_selective']['jev_calls'], 5)
        self.assertFalse(result['effect_claims_allowed'])
        self.assertIsNone(result['total_system_cost_micro_cny'])
        self.assertIn('manifest_priced_usage_mismatch', result['findings'])
        self.assertEqual(result['rows'][0]['llm_noncache_input_tokens'], 874)
        self.assertEqual(result['rows'][0]['llm_cache_read_tokens'], 2688)
        self.assertEqual(result['rows'][0]['llm_billable_input_tokens'], 3562)

    def test_duplicate_arm_is_rejected(self):
        docs = self.documents()
        docs[0]['rows'].append(copy.deepcopy(docs[0]['rows'][0]))
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            audit.audit_documents(*docs)

    def test_missing_usage_is_not_zero_cost(self):
        docs = self.documents()
        finish = next(x for x in docs[1] if x['kind'] == 'finish')
        finish['result']['observations'][0]['turn']['telemetry_raw']['llm_usage_missing'] = 1
        with self.assertRaisesRegex(ValueError, 'usage'):
            audit.audit_documents(*docs)

    def test_latency_tampering_is_rejected(self):
        docs = self.documents()
        docs[0]['rows'][0]['telemetry']['response_latency_ms'] += 100
        with self.assertRaisesRegex(ValueError, 'latency'):
            audit.audit_documents(*docs)

    def test_invalid_or_missing_latency_is_rejected(self):
        for value in [None, -1, float('nan'), float('inf'), True]:
            docs = self.documents()
            finish = next(x for x in docs[1] if x['kind'] == 'finish')
            finish['result']['observations'][0]['turn']['telemetry_raw']['total_latency_ms'] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                audit.audit_documents(*docs)

    def test_budget_mismatch_is_rejected(self):
        docs = self.documents()
        docs[2][-1]['Input'] += 1
        with self.assertRaisesRegex(ValueError, 'budget'):
            audit.audit_documents(*docs)

    def test_comparison_aggregate_tampering_is_rejected(self):
        docs = self.documents()
        docs[0]['comparison']['selective']['llm_calls'] += 1
        with self.assertRaisesRegex(ValueError, 'comparison'):
            audit.audit_documents(*docs)

    def test_mismatched_reserved_cost_is_rejected(self):
        docs = self.documents()
        docs[0]['rows'][0]['reserved_model_micro_cny'] += 1
        with self.assertRaisesRegex(ValueError, 'reserved'):
            audit.audit_documents(*docs)

    def test_manifest_hash_failure_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'digest'):
            audit.verify_digest(b'changed', '0' * 64)

    def test_manifest_cannot_omit_audited_files(self):
        with tempfile.TemporaryDirectory() as folder:
            (Path(folder) / 'manifest.json').write_text('{"files":{}}', encoding='utf-8')
            with self.assertRaisesRegex(ValueError, 'required sources'):
                audit.audit_pilot(folder)

    def test_unknown_cache_protocol_is_not_fabricated_as_zero(self):
        docs = self.documents()
        finish = next(x for x in docs[1] if x['kind'] == 'finish')
        finish['result']['observations'][0]['turn']['telemetry_raw']['llm_usage_protocol'] = 'openai'
        with self.assertRaisesRegex(ValueError, 'Anthropic'):
            audit.audit_documents(*docs)

    def test_receipt_completion_and_all_budget_counters_are_checked(self):
        for field in ['runner_completed', 'model_http_statuses', 'attempts', 'reserved_micro_cny', 'input_tokens', 'output_tokens']:
            docs = self.documents()
            if field == 'runner_completed':
                docs[3][field] = False
            elif field == 'model_http_statuses':
                docs[3][field] = []
            else:
                docs[3]['model_budget'][field] = 0
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, 'receipt'):
                audit.audit_documents(*docs)

    def test_opposing_settlement_changes_cannot_cancel_out(self):
        docs = self.documents()
        settles = [x for x in docs[2] if x['kind'] == 'settle']
        settles[0]['Input'] += 1
        settles[1]['Input'] -= 1
        with self.assertRaisesRegex(ValueError, 'per-call'):
            audit.audit_documents(*docs)

    def test_shifted_snapshots_are_rejected(self):
        docs = self.documents()
        for event in docs[1]:
            if event['kind'] == 'finish':
                for boundary in ['budget_before', 'budget_after']:
                    for field in ['attempts', 'reserved_micro_cny', 'input_tokens', 'output_tokens', 'priced_usage_micro_cny']:
                        event[boundary][field] += 1000000
        with self.assertRaisesRegex(ValueError, 'continuity'):
            audit.audit_documents(*docs)


if __name__ == '__main__':
    unittest.main()
