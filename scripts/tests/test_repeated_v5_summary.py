"""Synthetic evidence only; never a customer/model quality judgment."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/repeated-v5-summary.py'


def load():
    spec = importlib.util.spec_from_file_location('repeated_summary', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class SummaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.plan = json.loads((ROOT / 'eval/paired-pilots/2026-10-24-v5-repeated-entry/plan.json').read_text())
        self.manifest = {'schema': 'repeated-v5-summary-input.v1', 'mode': 'repeated',
                         'evidence_origin': 'synthetic_test', 'execution_revision': 'a' * 40,
                         'customer_source_revision': '50e3c8dab14b4b6cc25322fd340a274e6970200b',
                         'runtime_contract_sha256': 'a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f',
                         'plan': self.put('plan.json', self.plan), 'pairs': []}
        self.receipt = {'schema': 'authorized-v5-repeated-cohort.v1', 'status': 'checkpoint_pending',
                        'execution_revision': 'a' * 40, 'customer_source_revision': self.manifest['customer_source_revision'],
                        'runtime_contract_sha256': self.manifest['runtime_contract_sha256'],
                        'plan_sha256': self.manifest['plan']['sha256'], 'batches': []}
        for i, batch in enumerate(self.plan['batches']):
            order = batch['arm_order']
            config = {'pair_id': batch['pair_id'], 'block_id': batch['block_id'], 'arm_order': order,
                      'model': {'execution_revision': 'a' * 40}, 'selective': {'execution_revision': 'a' * 40}}
            events = [{'kind': 'config', 'config': config}]
            rows, reviewed = [], []
            for arm in order:
                trace = f'{i*2 + (arm == "jev_selective") + 1:032x}'
                selective = arm == 'jev_selective'
                telemetry = {'total_latency_ms': 12 if selective else 10, 'runtime': {'contract_sha256': self.manifest['runtime_contract_sha256']}}
                observation = {'turn': {'message': 'synthetic', 'latency_ms': 22 if selective else 20,
                                       'response': {'reply': 'synthetic', 'telemetry': telemetry}}}
                result = {'status':'needs_review','structural_pass':True,'observations': [observation], 'duration_ms': 32 if selective else 30}
                events += [{'kind': 'start', 'key': batch['key'], 'arm': arm},
                           {'kind': 'finish', 'key': batch['key'], 'arm': arm, 'langfuse_trace_id': trace,
                            'result': result, 'runner_elapsed_ms': 42 if selective else 40}]
                requirement = {'id': 'synthetic-requirement', 'turn_index': 0, 'requirement': 'synthetic only',
                               'requirement_sha256': 'b'*64, 'reply_sha256': hashlib.sha256(b'synthetic').hexdigest()}
                rows.append({'key': batch['key'], 'arm': arm, 'langfuse_trace_id': trace,
                             'observed_turns': 1, 'status': 'needs_review', 'structural_pass': True,
                             'fact_errors': 0, 'http_failures': 0, 'review_requirements': [requirement],
                             'runner_elapsed_ms': result['duration_ms'] + 10,
                             'guarded_upstream_model_attempts': 1 if selective else 2,
                             'priced_model_usage_micro_cny': 3 if selective else 5,
                             'telemetry': {'response_latency_ms': telemetry['total_latency_ms'],
                                           'tokens': {'jev_input': 101 if selective else 0, 'jev_output': 0}}})
                reviewed.append({'case_key': batch['key'], 'arm': arm, 'verdict': 'passed', 'rationale': 'synthetic test',
                                 'requirements': [{**requirement, 'verdict': 'passed'}]})
            rows.sort(key=lambda r: ['model','jev_selective'].index(r['arm']))
            journal = self.put(f'{i}/run.jsonl', b''.join(json.dumps(e).encode()+b'\n' for e in events))
            report = {'schema': 'small-paired-report.v1', 'evidence_kind': 'recorded_run',
                      'journal_sha256': journal['sha256'], 'pair_id': batch['pair_id'], 'block_id': batch['block_id'],
                      'arm_order': order, 'planned_cases': 1, 'planned_arms': 2, 'finished_arms': 2, 'unrun_arms': 0, 'rows': rows}
            report_ref = self.put(f'{i}/report.json', report)
            review = {'schema': 'pi-first-live-independent-review.v1', 'method': 'independent_agent_evidence_review',
                      'reviewer': 'synthetic-test-only', 'execution_revision': 'a'*40, 'journal_sha256': journal['sha256'],
                      'report_sha256': report_ref['sha256'], 'blocking_findings': [], 'rows': reviewed}
            self.manifest['pairs'].append({'batch_id': f'batch-{i+1:03d}', 'journal': journal, 'report': report_ref,
                                           'review': self.put(f'{i}/review.json', review)})
            self.receipt['batches'].append({**{k: batch[k] for k in ('pair_id','block_id','arm_order','key')},
                                            'id': f'batch-{i+1:03d}', 'status': 'completed_pending_review', 'report': report})

    def put(self, name, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value, indent=2)+'\n').encode()
        path = self.root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)
        return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest()}

    def run_summary(self):
        self.manifest['receipt'] = self.put('receipt.json', self.receipt)
        return load().summarize(self.manifest, self.root)

    def test_all_24_pairs_both_orders_exact_currencies_and_clocks(self):
        result = self.run_summary()
        self.assertEqual(result['eligible_pairs'], 24)
        self.assertEqual(len(result['strata']), 12)
        delta = result['pairs'][0]['delta_selective_minus_model']
        self.assertEqual(delta, {'model_attempts': '-1', 'model_micro_cny': '-2', 'jev_micro_usd': '4.242',
                                 'server_ms': '2', 'http_ms': '2', 'conversation_ms': '2', 'runner_ms': '2'})
        self.assertFalse(result['population_effect_claims_allowed'])

    def test_duplicate_pair_or_trace_rejected(self):
        self.manifest['pairs'][1] = copy.deepcopy(self.manifest['pairs'][0])
        with self.assertRaisesRegex(ValueError, 'batch'): self.run_summary()

    def test_genuinely_failed_review_is_retained_not_called_pass(self):
        p=self.manifest['pairs'][0]
        r=json.loads(Path(p['review']['path']).read_text())
        r['rows'][0]['verdict']='failed'
        r['rows'][0]['requirements'][0]['verdict']='failed'
        p['review']=self.put('failed-review.json',r)
        result=self.run_summary()
        self.assertEqual(result['eligible_pairs'],23)
        self.assertEqual(result['pairs'][0]['independent_review'],'failed')
        self.assertIn('independent_review_failed',result['pairs'][0]['exclusion_reasons'])

    def test_original_journal_duplicate_trace_rejected_even_with_rebound_hashes(self):
        p=self.manifest['pairs'][0]
        events=[json.loads(line) for line in Path(p['journal']['path']).read_text().splitlines()]
        events[4]['langfuse_trace_id']=events[2]['langfuse_trace_id']
        p['journal']=self.put('duplicate.jsonl',b''.join(json.dumps(e).encode()+b'\n' for e in events))
        r=copy.deepcopy(self.receipt['batches'][0]['report'])
        r['journal_sha256']=p['journal']['sha256']
        r['rows'][1]['langfuse_trace_id']=r['rows'][0]['langfuse_trace_id']
        p['report']=self.put('duplicate-report.json',r);p['review']=None
        self.receipt['batches'][0]['report']=r
        with self.assertRaisesRegex(ValueError,'trace'):self.run_summary()

    def test_receipt_failed_pair_cannot_enter_deltas(self):
        self.receipt['batches'][0]['status']='failed'
        self.receipt['batches'][0]['failed_stage']='cleanup'
        result=self.run_summary()
        self.assertEqual(result['eligible_pairs'],23)
        self.assertIn('batch_failed',result['pairs'][0]['exclusion_reasons'])

    def test_original_finish_failure_cannot_be_hidden_by_report_or_review(self):
        p=self.manifest['pairs'][0]
        events=[json.loads(line) for line in Path(p['journal']['path']).read_text().splitlines()]
        events[2]['execution_failures']=['cleanup']
        events[2]['result'].update(status='failed',structural_pass=False,errors=['failed'])
        p['journal']=self.put('failed-original.jsonl',b''.join(json.dumps(e).encode()+b'\n' for e in events))
        report=copy.deepcopy(self.receipt['batches'][0]['report']);report['journal_sha256']=p['journal']['sha256']
        p['report']=self.put('failed-original-report.json',report);self.receipt['batches'][0]['report']=report
        review=json.loads(Path(p['review']['path']).read_text());review.update(journal_sha256=p['journal']['sha256'],report_sha256=p['report']['sha256'])
        p['review']=self.put('failed-original-review.json',review)
        result=self.run_summary()
        self.assertEqual(result['eligible_pairs'],23)
        self.assertIn('original_execution_failed',result['pairs'][0]['exclusion_reasons'])

    def test_missing_pair_rejected(self):
        self.manifest['pairs'].pop()
        with self.assertRaisesRegex(ValueError, '24'): self.run_summary()

    def test_sha_revision_review_and_clock_tampering_rejected(self):
        for kind in ('sha', 'revision', 'review', 'clock', 'trace'):
            with self.subTest(kind=kind):
                saved = copy.deepcopy((self.manifest, self.receipt))
                p = self.manifest['pairs'][0]
                if kind == 'sha': p['journal']['sha256'] = '0'*64
                elif kind == 'revision': self.manifest['execution_revision'] = 'c'*40
                elif kind == 'review':
                    r=json.loads(Path(p['review']['path']).read_text());r['rows'][0]['requirements'][0]['reply_sha256']='0'*64
                    p['review']=self.put('bad-review.json',r)
                else:
                    r=copy.deepcopy(self.receipt['batches'][0]['report'])
                    if kind=='clock': r['rows'][0]['telemetry']['response_latency_ms']=999
                    else: r['rows'][1]['langfuse_trace_id']=r['rows'][0]['langfuse_trace_id']
                    p['report']=self.put('bad-report.json',r);self.receipt['batches'][0]['report']=r
                with self.assertRaises(ValueError): self.run_summary()
                self.manifest,self.receipt=saved

    def test_missing_usage_and_review_stay_visible_excluded(self):
        p = self.manifest['pairs'][0]
        r = copy.deepcopy(self.receipt['batches'][0]['report'])
        r['rows'][0]['priced_model_usage_micro_cny'] = None
        p['report'] = self.put('unknown-report.json',r);p['review']=None
        self.receipt['batches'][0]['report']=r
        result=self.run_summary()
        self.assertEqual(result['eligible_pairs'],23)
        pair=result['pairs'][0]
        self.assertIsNone(pair['arms']['model']['metrics']['model_micro_cny'])
        self.assertIsNone(pair['delta_selective_minus_model'])
        self.assertIn('independent_review_missing',pair['exclusion_reasons'])

    def test_failed_report_null_aggregate_retains_observed_clocks(self):
        p=self.manifest['pairs'][0]
        r=copy.deepcopy(self.receipt['batches'][0]['report'])
        r['rows'][0]['status']='failed'
        r['rows'][0]['telemetry']={'response_latency_ms':None,'tokens':None}
        p['report']=self.put('failed-report.json',r);p['review']=None
        self.receipt['batches'][0]['report']=r
        result=self.run_summary()
        self.assertEqual(result['pairs'][0]['arms']['model']['metrics']['server_ms'],'10')
        self.assertIsNone(result['pairs'][0]['delta_selective_minus_model'])

    def test_unrun_suffix_is_unknown_and_stopped_receipt_never_eligible(self):
        self.receipt['status']='stopped'
        for i in range(1,24):
            self.manifest['pairs'][i]={'batch_id':f'batch-{i+1:03d}', 'journal':None,'report':None,'review':None}
            self.receipt['batches'][i].pop('report')
            self.receipt['batches'][i]['status']='not_started'
        result=self.run_summary()
        self.assertEqual(result['eligible_pairs'],1)
        self.assertEqual(result['pairs'][1]['status'],'not_started')
        self.assertIsNone(result['pairs'][1]['arms']['model']['metrics'])


if __name__=='__main__': unittest.main()
