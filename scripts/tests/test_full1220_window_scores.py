"""Offline full-window evidence binding tests; no services or credentials."""
import sys
sys.dont_write_bytecode = True
import inspect
import copy
import hashlib
import importlib.util
import json
import unittest
import tempfile
import subprocess
import base64
import io
import threading
from concurrent.futures import ThreadPoolExecutor
from unittest.mock import patch
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / 'full1220-window-scores.py'


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def go_string(value):
    return (json.dumps(value, ensure_ascii=False, separators=(',', ':'))
            .replace('&', '\\u0026').replace('<', '\\u003c').replace('>', '\\u003e')
            .replace('\u2028', '\\u2028').replace('\u2029', '\\u2029'))


def wire(value):
    # Fixture structures intentionally have the frozen Go field order; nested
    # maps have their Go-sorted keys. Float .125 is exactly representable.
    return (json.dumps(value, ensure_ascii=False, separators=(',', ':'))
            .replace('&', '\\u0026').replace('<', '\\u003c').replace('>', '\\u003e')
            .replace('\u2028', '\\u2028').replace('\u2029', '\\u2029')).encode()


ACTION = "Independently verify observed action label read, allowed actions and reply facts against this turn's definition and authoritative state; no fabricated claims."
FINAL = 'Final reply accurately describes the task outcome, amounts, identities, scope and limitations using the verified final state; no false success claim.'


def fixture():
    case = {'source_case_id': 'source-001', 'answer_assertions': [
        {'id': 'source-001:reply-1', 'requirement': '原要求：金额、身份与范围 <完整> & 保真\u2028'}],
        'scenario': {'id': 'followup', 'profile': 'standard', 'turns': [
            {'user': '查余额', 'expected_actions': ['read'], 'allowed_tools': ['get_balance'], 'answer': {'amount': 5, 'unit': '元'}},
            {'user': '再确认', 'expected_actions': ['read'], 'allowed_tools': ['get_balance']}],
            'terminal': {'authorized_write_count': 0}, 'operation': None,
            'measurement_scope': 'complete_task', 'full_task_metric_eligible': True}}
    slots = [{'ordinal': i, 'pass': 1 if i == 1 else 2,
              'case_key': 'source-001/followup', 'arm_order': ['model', 'jev_selective'],
              'window_id': 'window-001', 'user_turns': 2} for i in (1, 2)]
    # Keep the original full1220 denominator while the explicit test window has
    # two slots. The other slots never acquire records, traces or scores.
    slots += [{'ordinal': i, 'pass': (i-1)//1220+1, 'case_key': f'future-{i}/task',
               'arm_order': ['model', 'jev_selective'], 'window_id': 'window-002',
               'user_turns': 2 if i <= 2226 else 1} for i in range(3, 3661)]
    scope = {'schema': 'full-window-scope.v2', 'bindings': {'executor_revision': 'a'*40},
             'slots': slots, 'windows': [{'id': 'window-001', 'first_ordinal': 1, 'last_ordinal': 2},
                                        {'id': 'window-002', 'first_ordinal': 3, 'last_ordinal': 3660}]}
    manifest = {'schema': 'full-window-actual-ready.v2', 'evidence_kind': 'actual',
                'archive_commit': 'a'*40, 'archive_tree': 'b'*40,
                'sources': {'internal/taskbench/conversation_runner.go': 'c'*64},
                'scope': scope, 'scope_sha256': digest(wire(scope)), 'execution_sha256': 'd'*64,
                'knowledge_model': 'off', 'knowledge_jev': 'off'}
    records, rows, reviews = {}, [], []
    for slot in slots[:2]:
        for arm in slot['arm_order']:
            ordinal = slot['ordinal']; trace = f'{ordinal:016x}{1 if arm == "model" else 2:016x}'
            state = {'own': {'balance_cent': 500, 'ledger_balance': 500, 'consistent': True},
                     'controls': {'2': {'balance_cent': 100}}, 'captured_at': '2026-10-28T00:00:00Z'}
            result = {'case': copy.deepcopy(case), 'fixture': {'tenant_id': 1, 'tenant_name': 'fixture'},
                      'initial': copy.deepcopy(state), 'final': copy.deepcopy(state), 'observations': [],
                      'review_requirements': [], 'errors': None, 'status': 'needs_review',
                      'setup_error': False, 'structural_pass': True, 'success': False,
                      'http_failures': 0, 'duration_ms': 1.125}
            for i in range(2):
                reply = f'余额5元 <本人> & 已核对 {ordinal}/{arm}/{i}\u2029'
                response = {'reply': reply, 'trace': [], 'pending_confirm': False,
                            'pending_tool': '', 'pending_preview': '', 'telemetry': {
                                'langfuse_trace_id': trace, 'runtime': {'routing_mode': arm}}}
                result['observations'].append({'action': 'read', 'turn': {
                    'message': case['scenario']['turns'][i]['user'], 'status': 200,
                    'response': response}, 'state': copy.deepcopy(state)})
                assertions = [{'id': f'source-001/followup/turn-{i+1}/action-reply', 'requirement': ACTION}]
                if i == 0:
                    assertions += case['answer_assertions']
                    assertions += [{'id': 'source-001/followup/turn-1/answer',
                                    'requirement': wire(case['scenario']['turns'][0]['answer']).decode()}]
                if i == 1:
                    assertions += [{'id': 'source-001/followup/final-reply', 'requirement': FINAL}]
                for assertion in assertions:
                    # Independent, explicit frozen Go outer struct, not adapter API.
                    context = (b'{"Case":' + wire(case) + b',"Fixture":' + wire(result['fixture'])
                               + b',"Initial":' + wire(state) + b',"TurnIndex":' + str(i).encode()
                               + b',"State":' + wire(state) + b',"Trace":[],"Pending":null,"Message":'
                               + go_string(case['scenario']['turns'][i]['user']).encode()
                               + b',"Requirement":' + go_string(assertion['requirement']).encode() + b'}')
                    result['review_requirements'].append({**assertion, 'turn_index': i,
                        'requirement_sha256': digest(context), 'reply_sha256': digest(reply.encode())})
            record = {'policy_id': 'full-customer-recording-policy.v1', 'policy_sha256': 'e'*64,
                      'result_sha256': digest(wire(result)), 'disposition': 'continue_recorded',
                      'issues': [], 'result': result}
            name = f'arm-{ordinal:06d}-{arm}.json'; records[name] = wire(record)
            row = {'ordinal': ordinal, 'pass': slot['pass'], 'case_key': slot['case_key'], 'arm': arm,
                   'defined_turns': 2, 'observed_turns': 2, 'trace_id': trace,
                   'recording_file': name, 'recording_sha256': digest(records[name]),
                   'status': 'needs_review', 'disposition': 'continue_recorded', 'attempted': True,
                   'finished': True, 'cleanup': True, 'observer': True,
                   'structural_pass': True, 'success': False, 'duration_ms': 1.125}
            rows.append(row)
            reviews.append({k: copy.deepcopy(v) for k, v in row.items()
                            if k in ('ordinal', 'pass', 'case_key', 'arm', 'trace_id', 'recording_file',
                                     'recording_sha256', 'attempted', 'finished', 'defined_turns', 'observed_turns')})
            reviews[-1].update({'source_case': copy.deepcopy(case), 'original_status': 'needs_review',
                'original_structural_pass': True, 'original_success': False, 'original_errors': None,
                'semantic_task_verdict': 'passed', 'task_verdict': 'passed',
                'pass_score_publish_eligible': True, 'requirements': [
                    {**req, 'verdict': 'passed', 'reason': '逐项独立核对原文和权威状态',
                     'requirement_context_sha256_verified': True, 'reply_sha256_verified': True,
                     'evidence_turn_indexes': [req['turn_index']]} for req in result['review_requirements']]})
    receipt = {'schema': 'full-window-acquisition-receipt.v1', 'evidence_kind': 'recorded_run',
               'status': 'collected_window', 'scope_sha256': manifest['scope_sha256'],
               'execution_sha256': manifest['execution_sha256'], 'window_id': 'window-001', 'window_index': 1,
               'planned_campaign_pairs': 3660, 'planned_campaign_arms': 7320, 'planned_campaign_turns': 11772,
               'planned_current_pairs': 2, 'planned_current_arms': 4, 'planned_current_turns': 8,
               'attempted_arms': 4, 'finished_arms': 4, 'unrun_arms': 0, 'observed_turns': 8,
               'rows': rows, 'ledger_sha256': 'f'*64, 'payload_manifest_sha256': '1'*64}
    summary = {'evidence_kind': 'recorded_run', 'planned_pairs': 3660, 'planned_arms': 7320,
               'defined_turns': 11772, 'boundaries_excluded': 1851, 'attempted_arms': 4,
               'finished_arms': 4, 'failed_attempted_arms': 0, 'unattempted_arms': 7316,
               'observed_turns': 8, 'quality_end_unrun_turns': 0, 'global_unrun_turns': 11764,
               'window_index': 1, 'report_status': 'collected_window', 'report_sha256': digest(wire(receipt)),
               'ledger_sha256': receipt['ledger_sha256'], 'payload_manifest_sha256': receipt['payload_manifest_sha256']}
    review = {'schema': 'full1220-window-independent-review.v1',
              'method': 'independent_agent_evidence_review', 'reviewer': 'fixture-independent-reviewer',
              'evidence_kind': 'recorded_run', 'blind': False, 'human_review': False,
              'bindings': {'manifest_sha256': digest(wire(manifest)), 'scope_sha256': manifest['scope_sha256'],
                           'execution_sha256': manifest['execution_sha256'], 'receipt_sha256': digest(wire(receipt)),
                           'archive_commit': manifest['archive_commit'], 'archive_tree': manifest['archive_tree']},
              'rows': reviews}
    return {'manifest': manifest, 'summary': summary, 'receipt': receipt, 'records': records, 'review': review}


def arguments(f):
    raw = [wire(f[key]) for key in ('manifest', 'summary', 'receipt')]
    review_raw = wire(f['review'])
    expected = {f'{key}_sha256': digest(value) for key, value in zip(
        ('manifest', 'summary', 'receipt', 'review'), raw + [review_raw])}
    expected['records'] = {name: digest(value) for name, value in f['records'].items()}
    return (*raw, f['records'], review_raw, expected)


def refresh_record(f, index, mutate):
    row = f['receipt']['rows'][index]; name = row['recording_file']
    d = json.loads(f['records'][name]); mutate(d)
    d['result_sha256'] = digest(wire(d['result'])); f['records'][name] = wire(d)
    row['recording_sha256'] = digest(f['records'][name])
    f['review']['rows'][index]['recording_sha256'] = row['recording_sha256']
    refresh_links(f)


def refresh_links(f):
    f['summary']['report_sha256'] = digest(wire(f['receipt']))
    f['review']['bindings']['receipt_sha256'] = digest(wire(f['receipt']))
    f['review']['bindings']['manifest_sha256'] = digest(wire(f['manifest']))


def recalculate_counts(f):
    rows=f['receipt']['rows']; receipt=f['receipt']; summary=f['summary']
    receipt['attempted_arms']=sum(r['attempted'] for r in rows)
    receipt['finished_arms']=sum(r['finished'] for r in rows)
    receipt['unrun_arms']=sum(not r['attempted'] for r in rows)
    receipt['observed_turns']=sum(r['observed_turns'] for r in rows)
    quality=sum((r.get('quality_end') or {}).get('unrun_turns',0) for r in rows)
    if quality:
        receipt['quality_end_arms']=sum(bool(r.get('quality_end')) for r in rows)
        receipt['quality_end_unrun_turns']=quality
        receipt['global_unrun_turns']=receipt['planned_current_turns']-receipt['observed_turns']-quality
    summary.update(attempted_arms=receipt['attempted_arms'], finished_arms=receipt['finished_arms'],
        failed_attempted_arms=receipt['attempted_arms']-receipt['finished_arms'],
        unattempted_arms=7320-receipt['attempted_arms'], observed_turns=receipt['observed_turns'],
        quality_end_unrun_turns=quality, global_unrun_turns=11772-receipt['observed_turns']-quality,
        report_status=receipt['status'])
    refresh_links(f)


def partial_fixture(quality=False):
    f=fixture(); row=f['receipt']['rows'][0]; review=f['review']['rows'][0]
    def mutate(d):
        result=d['result'];result['observations'].pop(); result['review_requirements']=[r for r in result['review_requirements'] if r['turn_index']==0]
        result.update(status='failed', structural_pass=False, errors=['quality negative' if quality else 'technical failure'])
        d['disposition']='halt'
    refresh_record(f,0,mutate)
    row.update(status='failed',structural_pass=False,observed_turns=1,finished=quality,disposition='halt')
    review.update(original_status='failed',original_structural_pass=False,
                  original_errors=['quality negative' if quality else 'technical failure'],
                  observed_turns=1,finished=quality,semantic_task_verdict='failed' if quality else 'not_assessable',
                  task_verdict='failed',pass_score_publish_eligible=False)
    review['requirements']=[r for r in review['requirements'] if r['turn_index']==0]
    for req in review['requirements']:req['verdict']='failed' if quality else 'not_assessable'
    if quality:
        row['quality_end']={'policy_sha256':'2'*64,'source_sha256':'3'*64,'rule_sha256':'4'*64,
            'candidate_file':'quality-candidate.json','candidate_sha256':'5'*64,
            'cleanup_file':'quality-cleanup.json','cleanup_sha256':'6'*64,
            'envelope_sha256':'7'*64,'observed_turns':1,'unrun_turns':1}
        row['exclude_complete_task_comparison']=True
    else: f['receipt']['status']='stopped'
    recalculate_counts(f);return f


def later_quality_fixture(prior_quality):
    """Two completed prior slots; current window retains its original bytes."""
    f=partial_fixture(quality=True)
    slots=f['manifest']['scope']['slots']
    for slot in slots[4:]:slot['window_id']='window-003'
    for i in range(2):
        slots[i+2]['case_key']=slots[i]['case_key']
        slots[i+2]['pass']=slots[i]['pass']
    f['manifest']['scope']['windows']=[
        {'id':'window-001','first_ordinal':1,'last_ordinal':2},
        {'id':'window-002','first_ordinal':3,'last_ordinal':4},
        {'id':'window-003','first_ordinal':5,'last_ordinal':3660}]
    renamed={}
    for row,review in zip(f['receipt']['rows'],f['review']['rows']):
        old=row['recording_file'];row['ordinal']+=2;review['ordinal']+=2
        name=f"arm-{row['ordinal']:06d}-{row['arm']}.json"
        row['recording_file']=review['recording_file']=name;renamed[name]=f['records'][old]
    f['records']=renamed
    f['manifest']['scope_sha256']=digest(wire(f['manifest']['scope']))
    f['receipt'].update(scope_sha256=f['manifest']['scope_sha256'],window_id='window-002',window_index=2)
    f['review']['bindings']['scope_sha256']=f['manifest']['scope_sha256']
    f['summary'].update(window_index=2,attempted_arms=8,finished_arms=8,failed_attempted_arms=0,
        unattempted_arms=7312,observed_turns=15-prior_quality,
        quality_end_unrun_turns=1+prior_quality,global_unrun_turns=11772-16)
    refresh_links(f);return f


class WindowScorePlanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.adapter = None
        if SCRIPT.exists():
            spec = importlib.util.spec_from_file_location('full_window_scores_test', SCRIPT)
            cls.adapter = importlib.util.module_from_spec(spec); spec.loader.exec_module(cls.adapter)

    def plan(self, f):
        self.assertTrue(self.adapter is not None and callable(getattr(self.adapter, 'build_window_score_plan', None)),
                        'Task1 pure build_window_score_plan is missing')
        return self.adapter.build_window_score_plan(*arguments(f))

    def reject(self, f):
        with self.assertRaises(ValueError):
            self.plan(f)

    def test_complete_dual_arm_plan_preserves_repeated_case_across_pass(self):
        plan = self.plan(fixture())
        self.assertEqual(plan['schema'], 'full1220-window-score-plan.v1')
        self.assertEqual(len(plan['rows']), 4)
        self.assertEqual(len(plan['trace_bindings']), 4)
        self.assertEqual(plan['denominators']['campaign'], {'pairs': 3660, 'arms': 7320, 'defined_turns': 11772, 'boundaries_excluded': 1851})
        self.assertEqual(plan['denominators']['window']['requirements'], 20)
        self.assertEqual([r['pass'] for r in plan['rows']], [1, 1, 2, 2])
        self.assertFalse(plan['publish_ready'])
        self.assertEqual((plan['model_calls'], plan['jev_calls'], plan['claims_created']), (0, 0, 0))
        self.assertTrue(all(s['name'].startswith('full1220.v1.') for s in plan['scores']))
        self.assertEqual(len({s['logical_identity'] for s in plan['scores']}), len(plan['scores']))

    def test_wrong_external_sha_fails_for_each_artifact(self):
        f = fixture(); self.plan(f)
        for key in ('manifest', 'summary', 'receipt', 'review'):
            with self.subTest(key=key):
                args = list(arguments(f)); args[-1][key + '_sha256'] = '0'*64
                with self.assertRaises(ValueError): self.adapter.build_window_score_plan(*args)
        args = list(arguments(f)); args[-1]['records'][next(iter(f['records']))] = '0'*64
        with self.assertRaises(ValueError): self.adapter.build_window_score_plan(*args)

    def test_scope_sha_recomputed_not_trusted_from_manifest(self):
        f = fixture(); f['manifest']['scope']['slots'][0]['case_key'] = 'other/task'; refresh_links(f); self.reject(f)

    def test_scope_execution_source_and_window_bindings_reject(self):
        for path in [('bindings','scope_sha256'), ('bindings','execution_sha256'), ('bindings','archive_commit'), ('bindings','archive_tree')]:
            f = fixture(); f['review'][path[0]][path[1]] = '0'*len(f['review'][path[0]][path[1]])
            with self.subTest(path=path): self.reject(f)
        for field in ('ordinal', 'pass', 'case_key', 'arm', 'trace_id'):
            f = fixture(); row = f['review']['rows'][0]
            row[field] = 999 if field in ('ordinal','pass') else 'other'
            with self.subTest(field=field): self.reject(f)
        f=fixture();f['summary']['window_index']=2;self.reject(f)
        f=fixture();f['receipt']['window_id']='window-002';refresh_links(f);self.reject(f)

    def test_complete_review_and_record_inventory_required(self):
        for mutation in ('missing_review','duplicate_review','missing_requirement','duplicate_requirement','missing_record','extra_record'):
            f=fixture()
            if mutation=='missing_review': f['review']['rows'].pop()
            if mutation=='duplicate_review': f['review']['rows'].append(copy.deepcopy(f['review']['rows'][0]))
            if mutation=='missing_requirement': f['review']['rows'][0]['requirements'].pop()
            if mutation=='duplicate_requirement': f['review']['rows'][0]['requirements'].append(copy.deepcopy(f['review']['rows'][0]['requirements'][0]))
            if mutation=='missing_record': f['records'].pop(next(iter(f['records'])))
            if mutation=='extra_record': f['records']['extra.json']=b'{}'
            with self.subTest(mutation=mutation): self.reject(f)

    def test_verified_flags_do_not_replace_context_reply_and_requirement_recomputation(self):
        mutations = [lambda d: d['result']['observations'][0]['turn']['response'].__setitem__('reply','changed'),
                     lambda d: d['result']['observations'][0]['state']['own'].__setitem__('balance_cent',999),
                     lambda d: d['result']['initial']['own'].__setitem__('ledger_balance',999),
                     lambda d: d['result']['fixture'].__setitem__('tenant_id',2),
                     lambda d: d['result']['observations'][0]['turn'].__setitem__('message','同义改写'),
                     lambda d: d['result']['review_requirements'][0].__setitem__('requirement','同义但不同要求')]
        for mutate in mutations:
            f=fixture();refresh_record(f,0,mutate)
            with self.subTest(mutate=mutations.index(mutate)): self.reject(f)
        f=fixture();f['review']['rows'][0]['requirements'][0]['requirement']='同义改写';self.reject(f)
        f=fixture();f['review']['rows'][0]['source_case']['answer_assertions'][0]['requirement']='同义改写';self.reject(f)

    def test_omitted_record_requirement_rejected_by_original_source_derivation(self):
        f=fixture();refresh_record(f,0,lambda d: d['result']['review_requirements'].pop(1))
        f['review']['rows'][0]['requirements'].pop(1);self.reject(f)

    def test_wrong_trace_in_record_and_duplicate_trace_rejected(self):
        f=fixture();refresh_record(f,0,lambda d: d['result']['observations'][0]['turn']['response']['telemetry'].__setitem__('langfuse_trace_id','9'*32));self.reject(f)
        f=fixture();f['receipt']['rows'][1]['trace_id']=f['receipt']['rows'][0]['trace_id'];f['review']['rows'][1]['trace_id']=f['receipt']['rows'][0]['trace_id'];refresh_links(f);self.reject(f)

    def test_original_negative_semantic_pass_preserved_but_positive_score_omitted(self):
        f=fixture()
        refresh_record(f,0,lambda d:d['result'].update(status='failed',structural_pass=False,errors=['deterministic amount parser error']))
        row=f['receipt']['rows'][0];row.update(status='failed',structural_pass=False)
        review=f['review']['rows'][0];review.update(original_status='failed',original_structural_pass=False,
            original_errors=['deterministic amount parser error'],task_verdict='failed',pass_score_publish_eligible=False,
            deterministic_assessment_disagreement=[{'evidence':'Independent amount proof'}])
        refresh_links(f);plan=self.plan(f);result=plan['rows'][0]
        self.assertEqual(result['semantic_task_verdict'],'passed');self.assertEqual(result['strict_task_verdict'],'failed')
        self.assertTrue(result['deterministic_semantic_disagreement'])
        self.assertTrue(result['positive_score_suppressed'])
        self.assertFalse(any(s['traceId']==row['trace_id'] and s['value'] in ('passed',1) for s in plan['scores']))

    def test_semantic_not_assessable_not_laundered_into_failure(self):
        f=fixture();r=f['review']['rows'][0];r['requirements'][0]['verdict']='not_assessable'
        r.update(semantic_task_verdict='not_assessable',task_verdict='not_assessable',pass_score_publish_eligible=False)
        plan=self.plan(f);self.assertEqual(plan['rows'][0]['semantic_task_verdict'],'not_assessable')

    def test_inconsistent_semantic_or_strict_aggregate_rejected(self):
        for axis in ('semantic_task_verdict','task_verdict'):
            f=fixture();f['review']['rows'][0][axis]='failed';self.reject(f)
        f=fixture();f['review']['rows'][0]['requirements'][0]['verdict']='unknown';self.reject(f)
        f=fixture();f['review']['rows'][0]['requirements'][0]['reason']='';self.reject(f)

    def test_legacy_review_and_journal_are_rejected(self):
        f=fixture();f['review']['schema']='pi-first-live-independent-review.v1';self.reject(f)
        f=fixture();f['manifest']['schema']='full-window-actual-ready.v1';self.reject(f)

    def test_first_window_schema_omission_is_explicit_and_does_not_weaken_other_gates(self):
        f=fixture();del f['review']['evidence_kind']
        plan=self.plan(f)
        self.assertEqual(plan['provenance']['review_evidence_kind_source'],'bound_recorded_run_receipt')
        f['review']['evidence_kind']='local_stub';self.reject(f)

    def test_unattempted_rows_keep_denominator_without_trace_or_scores(self):
        f=fixture();row=f['receipt']['rows'][-1];name=row['recording_file'];del f['records'][name]
        row.update(trace_id='',recording_file='',recording_sha256='',attempted=False,finished=False,
                   cleanup=False,observer=False,structural_pass=False,status='not_started',disposition='',observed_turns=0)
        f['review']['rows'][-1]={**{k:row[k] for k in ('ordinal','pass','case_key','arm','attempted','finished','defined_turns','observed_turns')},
            'trace_id':None,'recording_file':None,'recording_sha256':None,'original_status':'not_started',
            'original_success':False,'semantic_task_verdict':'not_started','task_verdict':'not_started',
            'requirements':[],'pass_score_publish_eligible':False}
        f['receipt']['status']='stopped';recalculate_counts(f);plan=self.plan(f)
        self.assertEqual(len(plan['rows']),4);self.assertEqual(len(plan['trace_bindings']),3)
        self.assertEqual(plan['rows'][-1]['unrun_turns'],2);self.assertEqual(plan['rows'][-1]['scores_planned'],0)
        self.assertEqual(plan['denominators']['window']['unattempted_arms'],1)
        f['review']['rows'][-1]['trace_id']='9'*32;self.reject(f)

    def test_partial_technical_failure_keeps_semantic_unknown_and_unrun_suffix(self):
        f=partial_fixture();plan=self.plan(f);row=plan['rows'][0]
        self.assertEqual(row['semantic_task_verdict'],'not_assessable');self.assertEqual(row['strict_task_verdict'],'failed')
        self.assertEqual(row['unrun_turns'],1);self.assertFalse(row['finished'])
        self.assertEqual(plan['denominators']['window']['global_unrun_turns'],1)
        self.assertEqual(plan['denominators']['window']['quality_end_unrun_turns'],0)

    def test_quality_end_preserves_explicit_suffix_and_pair_exclusion(self):
        f=partial_fixture(quality=True);plan=self.plan(f);row=plan['rows'][0]
        self.assertEqual(row['original_status'],'failed');self.assertEqual(row['semantic_task_verdict'],'failed')
        self.assertTrue(row['exclude_complete_task_comparison']);self.assertEqual(row['quality_end']['unrun_turns'],1)
        self.assertEqual(plan['denominators']['window']['quality_end_unrun_turns'],1)
        self.assertEqual(plan['denominators']['window']['global_unrun_turns'],0)
        f['receipt']['rows'][0]['quality_end']['unrun_turns']=2;refresh_links(f);self.reject(f)

    def test_finished_missing_turn_without_quality_suffix_is_rejected(self):
        f=partial_fixture(quality=True);del f['receipt']['rows'][0]['quality_end'];recalculate_counts(f);self.reject(f)

    def test_optional_review_projection_empty_trace_shape_is_explicitly_normalized(self):
        f=fixture();f['review']['rows'][0]['observed_evidence']=[
            {'turn_index':0,'tool_trace':None},{'turn_index':1,'tool_trace':[]}]
        plan=self.plan(f)
        self.assertEqual(plan['denominators']['window']['requirements'],20)
        f['review']['rows'][0]['observed_evidence'][0]['tool_trace']=[{'tool':'invented'}];self.reject(f)

    def test_review_empty_error_projection_does_not_change_original_null(self):
        f=fixture();f['review']['rows'][0]['original_errors']=[]
        plan=self.plan(f);self.assertIsNone(plan['rows'][0]['original_errors'])
        f['review']['rows'][0]['original_errors']=['invented'];self.reject(f)

    def test_review_identity_booleans_and_float_ordinals_are_not_integer_identities(self):
        for field,value in [('ordinal',True),('ordinal',1.0),('pass',True),('pass',1.0)]:
            f=fixture();f['review']['rows'][0][field]=value
            with self.subTest(field=field,value=value):self.reject(f)

    def test_unattempted_review_cannot_claim_passing_eligibility_or_original_success(self):
        f=fixture();row=f['receipt']['rows'][-1];del f['records'][row['recording_file']]
        row.update(trace_id='',recording_file='',recording_sha256='',attempted=False,finished=False,
                   cleanup=False,observer=False,structural_pass=False,status='not_started',disposition='',observed_turns=0)
        f['review']['rows'][-1]={**{k:row[k] for k in ('ordinal','pass','case_key','arm')},
            'trace_id':None,'recording_file':None,'recording_sha256':None,'original_status':'not_started',
            'original_success':False,'semantic_task_verdict':'not_started','task_verdict':'not_started',
            'requirements':[],'pass_score_publish_eligible':False}
        f['receipt']['status']='stopped';recalculate_counts(f)
        for key in ('original_success','pass_score_publish_eligible'):
            bad=copy.deepcopy(f);bad['review']['rows'][-1][key]=True
            with self.subTest(key=key):self.reject(bad)

    def test_terminal_review_projection_must_match_original_authoritative_state(self):
        f=fixture();f['review']['rows'][0]['terminal_evidence']={'initial_balance_cent':500,
            'final_balance_cent':500,'final_ledger_balance':500,'final_consistent':True,'controls_unchanged':True}
        self.plan(f)
        for key,value in [('final_balance_cent',999),('initial_balance_cent',999),('controls_unchanged',False)]:
            bad=copy.deepcopy(f);bad['review']['rows'][0]['terminal_evidence'][key]=value
            with self.subTest(key=key):self.reject(bad)

    def test_score_logical_identity_does_not_change_with_review_revision_or_sha(self):
        f=fixture();first=self.plan(f);f['review']['revision']=2;f['review']['rows'][0]['requirements'][0]['reason']='另一份独立解释'
        second=self.plan(f)
        self.assertNotEqual(first['provenance']['review_sha256'],second['provenance']['review_sha256'])
        self.assertEqual([s['logical_identity'] for s in first['scores']],[s['logical_identity'] for s in second['scores']])
        self.assertEqual([s['id'] for s in first['scores']],[s['id'] for s in second['scores']])

    def test_duplicate_json_keys_and_nonfinite_numbers_fail_closed(self):
        f=fixture();self.plan(f)
        for raw in (b'{"schema":"a","schema":"b"}',b'{"a":NaN}',b'{"a":1e400}',b'{"a":1,}',b'{"a":[1,]}'):
            args=list(arguments(f));args[1]=raw;args[-1]['summary_sha256']=digest(raw)
            with self.subTest(raw=raw):
                with self.assertRaises(ValueError):self.adapter.build_window_score_plan(*args)

    def test_cumulative_summary_cannot_invent_attempts_or_observed_turns(self):
        for mode in ('attempted','observed','quality'):
            f=fixture();summary=f['summary']
            if mode=='attempted':
                summary['attempted_arms']+=1;summary['finished_arms']+=1;summary['unattempted_arms']-=1
            if mode=='observed':summary['observed_turns']+=1;summary['global_unrun_turns']-=1
            if mode=='quality':summary['quality_end_unrun_turns']+=1;summary['global_unrun_turns']-=1
            with self.subTest(mode=mode):self.reject(f)

    def test_first_window_cumulative_observed_quality_swap_is_rejected(self):
        f=partial_fixture(quality=True)
        f['summary']['observed_turns']+=1
        f['summary']['quality_end_unrun_turns']-=1
        self.reject(f)

    def test_later_cumulative_quality_cannot_be_less_than_current_window(self):
        f=later_quality_fixture(prior_quality=0)
        f['summary']['observed_turns']+=1
        f['summary']['quality_end_unrun_turns']-=1
        self.reject(f)

    def test_first_window_turn_components_are_independently_exact(self):
        plan=self.plan(partial_fixture(quality=True))
        self.assertIn('cumulative_turn_validation',plan.keys())
        validation=plan['cumulative_turn_validation']
        self.assertEqual(validation['status'],'exact_current_window')
        self.assertTrue(validation['prior_partition_verified'])
        self.assertEqual(validation['claimed_prior_observed_turns'],0)
        self.assertEqual(validation['claimed_prior_quality_end_unrun_turns'],0)
        self.assertNotIn('prior_receipt_turn_partition_chain',plan['pending_gates'])

    def test_legal_prior_quality_suffix_boundaries_are_preserved_with_pending_chain_gate(self):
        for prior_quality in (0,4):
            f=later_quality_fixture(prior_quality)
            with self.subTest(prior_quality=prior_quality):
                plan=self.plan(f)
                self.assertIn('cumulative_turn_validation',plan.keys())
                validation=plan['cumulative_turn_validation']
                self.assertEqual(validation['status'],'prior_receipt_chain_pending')
                self.assertFalse(validation['prior_partition_verified'])
                self.assertEqual(validation['claimed_prior_observed_turns'],8-prior_quality)
                self.assertEqual(validation['claimed_prior_quality_end_unrun_turns'],prior_quality)
                self.assertIn('prior_receipt_turn_partition_chain',plan['pending_gates'])
                self.assertEqual(plan['denominators']['acquisition_cumulative']['observed_turns'],15-prior_quality)
                self.assertEqual(plan['denominators']['acquisition_cumulative']['quality_end_unrun_turns'],1+prior_quality)
                self.assertFalse(plan['publish_ready'])

    def test_denominator_corruption_rejected(self):
        for target,field in [('summary','planned_pairs'),('summary','boundaries_excluded'),('receipt','observed_turns'),('receipt','planned_current_arms')]:
            f=fixture();f[target][field]+=1;refresh_links(f)
            with self.subTest(target=target,field=field):self.reject(f)


class ContentProjectionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec=importlib.util.spec_from_file_location('window_content_projection_tests',SCRIPT)
        cls.adapter=importlib.util.module_from_spec(spec);spec.loader.exec_module(cls.adapter)

    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory();self.addCleanup(self.temporary.cleanup)
        self.root=Path(self.temporary.name);self.source=self.root/'source';self.source.mkdir()
        self.f=fixture();self.f['manifest']['source_root']=str(self.source)
        content=(SCRIPT.parents[1]/'internal/observability/content.go').read_bytes()
        sources={'internal/observability/content.go':content,
            'internal/taskbench/conversation_runner.go':b'package taskbench\n',
            'go.mod':b'module github.com/frostloom/ai-gateway\n'}
        for name,body in sources.items():
            target=self.source/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(body)
        self.f['manifest']['sources']={name:digest(body) for name,body in sources.items()}
        self.f['manifest']['observer']={'enabled':True,'capture_content':True,'sample_rate':1,
            'environment':'evaluation','release':self.f['manifest']['archive_commit']}
        refresh_links(self.f)
        self.go=self.root/'go';self.go.write_bytes(b'pinned fake compiler for mocked boundary only');self.go.chmod(0o700)
        self.cache=self.root/'modules';self.cache.mkdir()
        self.sdk=self.root/'MacOSX26.5.sdk';self.sdk.mkdir()
        self.sdk_patch=patch.object(self.adapter,'_PROJECTION_SDKROOT',str(self.sdk),create=True)
        self.sdk_patch.start();self.addCleanup(self.sdk_patch.stop)
        self.overrides={};self.commands=[];self.run_mutation=None;self.after_output=None;self.stdout_override=None

    def api(self):
        for name in ('project_window_content','validate_window_traces'):
            self.assertTrue(callable(getattr(self.adapter,name,None)),name+' is missing')

    def fake_go(self,command,**kwargs):
        self.commands.append((command,kwargs))
        job=json.loads(Path(kwargs['env']['FULL1220_PROJECTION_INPUT']).read_bytes())
        result={key:job[key] for key in ('manifest_sha256','receipt_sha256','scope_sha256',
            'execution_sha256','archive_commit','archive_tree','content_source_sha256','helper_sha256','window_id')}
        result.update(schema='full1220-content-projection.v1',input_sha256=digest(
            Path(kwargs['env']['FULL1220_PROJECTION_INPUT']).read_bytes()),new_external_requests=0,rows=[])
        for item in job['rows']:
            record=json.loads((Path(kwargs['env']['FULL1220_PROJECTION_INPUT']).parent/'records'/item['recording_file']).read_bytes())
            turns=[]
            for i,ob in enumerate(record['result']['observations'] or [],1):
                message=ob['turn']['message'];reply=ob['turn']['response']['reply']
                turns.append({'turn_index':i,'input':message,
                    'output':self.overrides.get((item['trace_id'],i),reply),
                    'original_input_sha256':digest(message.encode()),'original_reply_sha256':digest(reply.encode())})
            result['rows'].append({'trace_id':item['trace_id'],'recording_sha256':item['recording_sha256'],'turns':turns})
        if self.run_mutation:self.run_mutation(result,job,kwargs)
        target=Path(kwargs['env']['FULL1220_PROJECTION_OUTPUT'])
        encoded=wire(result)
        with target.open('xb') as stream:stream.write(encoded)
        receipt=b'    full1220_content_projection_test.go:174: FULL1220_PROJECTION_OUTPUT_SHA256='+digest(encoded).encode()+b'\n'
        if self.after_output:self.after_output(target)
        return subprocess.CompletedProcess(command,0,receipt if self.stdout_override is None else self.stdout_override,b'')

    def project(self,f=None,output=None,execute=True):
        self.api();f=f or self.f;args=list(arguments(f))
        args[-1]['projection_helper_sha256']=getattr(self.adapter,'_PROJECTION_HELPER_SHA256','0'*64)
        runtime={'go_executable':str(self.go),'go_sha256':digest(self.go.read_bytes()),'module_cache':str(self.cache)}
        with patch.object(self.adapter.subprocess,'run',side_effect=self.fake_go):
            return self.adapter.project_window_content(*args,source_root=str(self.source),
                output_dir=str(output or self.root/'projection'),runtime=runtime,execute=execute)

    def plan(self,f=None):return self.adapter.build_window_score_plan(*arguments(f or self.f))

    def client(self,plan,proof):
        projection=proof.as_dict();by_trace={row['trace_id']:row for row in projection['rows']}
        payloads={}
        slots=self.f['manifest']['scope']['slots']
        for trace,binding in plan['trace_bindings'].items():
            root='root-'+trace
            common={'run_id':plan['provenance']['scope_sha256'],'case_key':binding['case_key'],
                'arm':binding['arm'],'evidence_kind':'recorded_run',
                'pair_id':plan['provenance']['scope_sha256']+'/'+plan['window_id']+f"/ordinal-{binding['ordinal']:06d}/pass-{binding['pass']}",
                'block_id':plan['window_id'],'arm_order':','.join(slots[binding['ordinal']-1]['arm_order']),
                'attributes.langfuse.release':plan['provenance']['archive_commit']}
            payloads[trace]=[{'id':root,'traceId':trace,'name':'evaluation.task','parentObservationId':None,
                'environment':'evaluation','metadata':{**common,'status':binding['original_status'],
                    'structural_pass':str(binding['original_structural_pass']).lower(),
                    'task_success':'true' if binding['original_status']=='passed' else 'false' if binding['original_status'] in ('failed','setup_failed') else 'unknown'}}]
            for turn in by_trace[trace]['turns']:
                payloads[trace].append({'id':root+'-turn-'+str(turn['turn_index']),'traceId':trace,
                    'parentObservationId':root,'name':'agent.turn','environment':'evaluation',
                    'metadata':{**common,'turn_index':str(turn['turn_index'])},
                    'input':turn['input'],'output':turn['output']})
            payloads[trace].append({'id':root+'-llm','traceId':trace,'parentObservationId':root+'-turn-1',
                'name':'llm.attempt','environment':'evaluation','metadata':common,'input':None,'output':None})
        class ReadOnlyFake:
            def __init__(self):self.calls=[];self.payloads=payloads;self.failure=None
            def observations(self,trace):
                self.calls.append(trace)
                if self.failure:raise self.failure
                return copy.deepcopy(self.payloads[trace])
            def request(self,*args,**kwargs):raise AssertionError('No general request or POST allowed')
        return ReadOnlyFake()

    def test_fixed_helper_projection_binds_source_record_runtime_and_no_environment_credentials(self):
        proof=self.project();projection=proof.as_dict();plan=self.plan()
        self.assertEqual(projection['schema'],'full1220-content-projection.v1')
        self.assertEqual(len(projection['rows']),4)
        command,kwargs=self.commands[0]
        self.assertEqual(command[1:],['test','-mod=readonly','-buildvcs=false','-v','./internal/observability',
            '-run','^TestFull1220FrozenContentProjection$','-count=1','-timeout=2m'])
        self.assertEqual(kwargs['env']['SDKROOT'],str(self.sdk))
        self.assertEqual(kwargs['env']['GOPROXY'],'off');self.assertEqual(kwargs['env']['GOWORK'],'off')
        self.assertFalse(any('KEY' in key or 'TOKEN' in key or 'SECRET' in key for key in kwargs['env']))
        result=self.adapter.validate_window_traces(plan,proof,self.client(plan,proof))
        self.assertTrue(result['all_pass']);self.assertEqual(result['verified_turns'],8)
        self.assertFalse(result['publish_ready']);self.assertFalse(result['native_llm_payload_io_claimed'])

    def test_plain_handwritten_projection_even_with_its_sha_is_not_evidence(self):
        proof=self.project();raw=proof.as_dict();raw['projection_sha256']=digest(wire(raw))
        with self.assertRaises(ValueError):self.adapter.validate_window_traces(self.plan(),raw,self.client(self.plan(),proof))

    def test_projection_tamper_and_different_plan_rejected_before_any_client_read(self):
        proof=self.project();plan=self.plan();client=self.client(plan,proof)
        raw=proof.as_dict();raw['rows'][0]['turns'][0]['output']='forged'
        with self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,raw,client)
        altered=copy.deepcopy(plan);altered['rows'][0]['ordinal']=999
        with self.assertRaises(ValueError):self.adapter.validate_window_traces(altered,proof,client)
        self.assertEqual(client.calls,[])

    def test_source_extra_file_changed_record_helper_pin_or_content_version_fails_before_go(self):
        self.api()
        for mode in ('extra','source_changed','helper_pin','content_version','record'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as out:
                f=copy.deepcopy(self.f);extra=self.source/'extra.go'
                if mode=='extra':extra.write_bytes(b'package malicious')
                if mode=='source_changed':(self.source/'go.mod').write_bytes(b'changed')
                args=list(arguments(f));args[-1]['projection_helper_sha256']=getattr(self.adapter,'_PROJECTION_HELPER_SHA256','0'*64)
                if mode=='helper_pin':args[-1]['projection_helper_sha256']='0'*64
                if mode=='content_version':
                    f['manifest']['sources']['internal/observability/content.go']='1'*64;refresh_links(f);args=list(arguments(f));args[-1]['projection_helper_sha256']=getattr(self.adapter,'_PROJECTION_HELPER_SHA256','0'*64)
                if mode=='record':args[3][next(iter(args[3]))]+=b' '
                with patch.object(self.adapter.subprocess,'run',side_effect=AssertionError('must reject before Go')):
                    with self.assertRaises(ValueError):self.adapter.project_window_content(*args,source_root=str(self.source),
                        output_dir=str(Path(out)/'new'),runtime={'go_executable':str(self.go),'go_sha256':digest(self.go.read_bytes()),'module_cache':str(self.cache)},execute=True)
                if extra.exists():extra.unlink()
                (self.source/'go.mod').write_bytes(b'module github.com/frostloom/ai-gateway\n')

    def test_original_root_git_pointer_is_validated_but_never_staged(self):
        pointer=self.source/'.git';raw=('gitdir: '+str(self.root/'archive-git-metadata')+'\n').encode()
        pointer.write_bytes(raw)
        proof=self.project()
        self.assertEqual(pointer.read_bytes(),raw)
        self.assertFalse((self.root/'projection/source/.git').exists())
        self.assertTrue(self.adapter.validate_window_traces(self.plan(),proof,self.client(self.plan(),proof))['all_pass'])

    def test_git_pointer_compatibility_rejects_directories_links_nested_and_invalid_bytes(self):
        cases=('directory','symlink','nested_file','nested_directory','relative','extra_line','oversized','missing_prefix','pycache')
        for mode in cases:
            target=self.source/'.git';parent=None
            if mode.startswith('nested_'):
                parent=self.source/'nested';parent.mkdir();target=parent/'.git'
            if mode=='directory' or mode=='nested_directory':target.mkdir()
            elif mode=='symlink':target.symlink_to(self.source/'go.mod')
            elif mode=='relative':target.write_bytes(b'gitdir: ../archive-git-metadata\n')
            elif mode=='extra_line':target.write_bytes(b'gitdir: /safe/archive\nextra\n')
            elif mode=='oversized':target.write_bytes(b'gitdir: /'+b'a'*4096+b'\n')
            elif mode=='missing_prefix':target.write_bytes(b'/safe/archive\n')
            elif mode=='pycache':
                parent=self.source/'__pycache__';parent.mkdir();target=parent/'cached.pyc';target.write_bytes(b'extra')
            else:target.write_bytes(b'gitdir: /safe/archive\n')
            try:
                with self.subTest(mode=mode),self.assertRaises(ValueError):self.project(output=self.root/('output-'+mode))
            finally:
                if target.is_symlink() or target.is_file():target.unlink()
                elif target.is_dir():target.rmdir()
                if parent:parent.rmdir()
        self.assertEqual(self.commands,[])

    def test_original_git_pointer_changes_during_helper_are_rejected(self):
        pointer=self.source/'.git';pointer.write_bytes(b'gitdir: /first/archive\n')
        self.run_mutation=lambda result,job,kwargs:pointer.write_bytes(b'gitdir: /second/archive\n')
        with self.assertRaises(ValueError):self.project()
        self.assertEqual(len(self.commands),1)

    def test_helper_original_reply_digest_and_output_provenance_are_revalidated(self):
        def corrupt(result,job,kwargs):result['rows'][0]['turns'][0]['original_reply_sha256']='0'*64
        self.run_mutation=corrupt
        with self.assertRaises(ValueError):self.project()

    def test_helper_cannot_mutate_frozen_copy_or_reuse_output_directory(self):
        self.run_mutation=lambda result,job,kwargs:(Path(kwargs['cwd'])/'go.mod').write_bytes(b'mutated')
        with self.assertRaises(ValueError):self.project()
        self.run_mutation=None
        with self.assertRaises(ValueError):self.project()

    def test_go_execution_requires_explicit_request_and_nonzero_exit_never_seals(self):
        with self.assertRaises(ValueError):self.project(execute=False)
        self.api();args=list(arguments(self.f));args[-1]['projection_helper_sha256']=self.adapter._PROJECTION_HELPER_SHA256
        with patch.object(self.adapter.subprocess,'run',return_value=subprocess.CompletedProcess([],1,b'',b'compile failed')):
            with self.assertRaises(ValueError):self.adapter.project_window_content(*args,source_root=str(self.source),
                output_dir=str(self.root/'failed'),runtime={'go_executable':str(self.go),'go_sha256':digest(self.go.read_bytes()),'module_cache':str(self.cache)},execute=True)

    def test_root_pair_pass_arm_order_release_environment_and_parent_binding(self):
        proof=self.project();plan=self.plan()
        for field,value in [('pair_id','wrong'),('arm','model-wrong'),('arm_order','wrong'),
            ('attributes.langfuse.release','0'*40),('case_key','wrong')]:
            client=self.client(plan,proof);trace=next(iter(client.payloads));client.payloads[trace][0]['metadata'][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,client)
        for key,value in [('traceId','9'*32),('environment','production'),('parentObservationId','wrong')]:
            client=self.client(plan,proof);trace=next(iter(client.payloads));client.payloads[trace][1][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,client)

    def test_langfuse_bool_or_same_canonical_string_preserves_original_boolean_status(self):
        base=copy.deepcopy(self.f)
        for status in ('failed','passed','needs_review'):
            f=partial_fixture() if status=='failed' else copy.deepcopy(base)
            if status=='failed':
                for key in ('source_root','sources','observer'):f['manifest'][key]=copy.deepcopy(base['manifest'][key])
                refresh_links(f)
            if status=='passed':
                refresh_record(f,0,lambda d:d['result'].update(status='passed',success=True))
                f['receipt']['rows'][0].update(status='passed',success=True)
                f['review']['rows'][0].update(original_status='passed',original_success=True)
                refresh_links(f)
            with self.subTest(status=status):
                proof=self.project(f,self.root/('bool-'+status));plan=self.plan(f);client=self.client(plan,proof)
                trace=next(iter(client.payloads));metadata=client.payloads[trace][0]['metadata']
                for key in ('structural_pass','task_success'):
                    if metadata[key] in ('true','false'):metadata[key]=metadata[key]=='true'
                self.assertTrue(self.adapter.validate_window_traces(plan,proof,client)['all_pass'])
                self.assertEqual(plan['rows'][0]['original_status'],status)
                self.assertEqual(plan['rows'][0]['original_success'],status=='passed')

    def test_boolean_metadata_rejects_integer_float_and_noncanonical_or_wrong_values(self):
        f=partial_fixture()
        for key in ('source_root','sources','observer'):f['manifest'][key]=copy.deepcopy(self.f['manifest'][key])
        refresh_links(f);proof=self.project(f);plan=self.plan(f)
        for key in ('structural_pass','task_success'):
            for value in (0,1,0.0,1.0,'False','FALSE','true',True,None):
                client=self.client(plan,proof);trace=next(iter(client.payloads));client.payloads[trace][0]['metadata'][key]=value
                with self.subTest(key=key,value=value),self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,client)

    def test_unknown_task_success_requires_exact_string_and_other_metadata_stays_strict(self):
        proof=self.project();plan=self.plan()
        for value in (False,True,0,1,'false','true','unknown ',None):
            client=self.client(plan,proof);trace=next(iter(client.payloads));client.payloads[trace][0]['metadata']['task_success']=value
            with self.subTest(value=value),self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,client)
        client=self.client(plan,proof);trace=next(iter(client.payloads));client.payloads[trace][0]['metadata']['status']=False
        with self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,client)

    def test_missing_or_delayed_reads_remain_unverified_without_retry(self):
        proof=self.project();plan=self.plan();client=self.client(plan,proof)
        client.failure=TimeoutError('temporary')
        result=self.adapter.validate_window_traces(plan,proof,client)
        self.assertFalse(result['all_pass']);self.assertFalse(result['publish_ready'])
        self.assertEqual(len(client.calls),len(plan['trace_bindings']))
        client=self.client(plan,proof);trace=next(iter(client.payloads));client.payloads[trace].pop(1)
        result=self.adapter.validate_window_traces(plan,proof,client);self.assertFalse(result['all_pass'])

    def test_duplicate_turn_missing_io_and_empty_string_not_null(self):
        proof=self.project();plan=self.plan();client=self.client(plan,proof);trace=next(iter(client.payloads))
        client.payloads[trace].append(copy.deepcopy(client.payloads[trace][1]))
        with self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,client)
        client=self.client(plan,proof);client.payloads[trace][1]['output']=None
        self.assertFalse(self.adapter.validate_window_traces(plan,proof,client)['all_pass'])

    def test_redacted_utf8_and_4096_wrapper_outputs_compare_go_projection_not_raw_reply(self):
        self.api()
        cases=[('sk-abcd1234 person@example.com','[REDACTED] [REDACTED]'),
            ('界'*1000,'界'*682+'[TRUNCATED]'),
            ('\\'*2048,{'truncated':True,'preview':'"'+'\\'*1499}),('','')]
        for original,projected in cases:
            with self.subTest(kind=type(projected).__name__),tempfile.TemporaryDirectory() as out:
                f=copy.deepcopy(self.f)
                def mutate(record):
                    record['result']['observations'][0]['turn']['response']['reply']=original
                    for req in record['result']['review_requirements']:
                        if req['turn_index']==0:req['reply_sha256']=digest(original.encode())
                refresh_record(f,0,mutate)
                for req in f['review']['rows'][0]['requirements']:
                    if req['turn_index']==0:req['reply_sha256']=digest(original.encode())
                trace=f['receipt']['rows'][0]['trace_id'];self.overrides[(trace,1)]=projected
                proof=self.project(f,Path(out)/'projection');plan=self.plan(f);client=self.client(plan,proof)
                self.assertTrue(self.adapter.validate_window_traces(plan,proof,client)['all_pass'])
                client.payloads[trace][1]['output']=None
                self.assertFalse(self.adapter.validate_window_traces(plan,proof,client)['all_pass'])

    def test_projection_file_tamper_after_helper_output_is_rejected_by_process_receipt(self):
        def tamper(path):
            body=json.loads(path.read_bytes());body['rows'][0]['turns'][0]['output']='handwritten forged projection'
            path.write_bytes(wire(body))
        self.after_output=tamper
        with self.assertRaises(ValueError):self.project()

    def test_missing_fixed_helper_process_output_receipt_does_not_seal_projection(self):
        self.stdout_override=b'go command returned success but no trusted helper receipt'
        with self.assertRaises(ValueError):self.project()

    def test_projection_proof_is_immutable_and_serialized_copy_cannot_replace_it(self):
        proof=self.project()
        with self.assertRaises(AttributeError):proof._raw=b'{}'

    def test_output_parent_symlink_cannot_write_inside_original_source(self):
        self.api();alias=self.root/'source-alias';alias.symlink_to(self.source,target_is_directory=True)
        with self.assertRaises(ValueError):self.project(output=alias/'new-result')
        self.assertFalse((self.source/'new-result').exists())
        self.assertEqual(self.commands,[])

    def test_real_later_window_partition_gate_survives_successful_current_trace_validation(self):
        later=later_quality_fixture(prior_quality=4)
        for key in ('sources','source_root','observer'):later['manifest'][key]=copy.deepcopy(self.f['manifest'][key])
        refresh_links(later);self.f=later
        proof=self.project();plan=self.plan();result=self.adapter.validate_window_traces(plan,proof,self.client(plan,proof))
        self.assertTrue(result['all_pass'])
        self.assertIn('prior_receipt_turn_partition_chain',result['pending_gates'])
        self.assertIn('single_publication_protocol',result['pending_gates'])
        self.assertFalse(result['publish_ready'])
        self.assertEqual(plan['denominators']['acquisition_cumulative']['quality_end_unrun_turns'],5)

    def test_prior_receipt_partition_gate_remains_pending_after_current_content_validation(self):
        proof=self.project();plan=self.plan();plan['pending_gates'].append('prior_receipt_turn_partition_chain')
        # A changed plan must be reprojected; a token from another plan cannot be
        # borrowed to clear gates, even if its trace/body projection is identical.
        with self.assertRaises(ValueError):self.adapter.validate_window_traces(plan,proof,self.client(self.plan(),proof))


class PublicationTests(unittest.TestCase):
    setUpClass = classmethod(ContentProjectionTests.setUpClass.__func__)
    api = ContentProjectionTests.api
    fake_go = ContentProjectionTests.fake_go
    project = ContentProjectionTests.project
    plan = ContentProjectionTests.plan
    client = ContentProjectionTests.client

    def setUp(self):
        ContentProjectionTests.setUp(self)
        self.registry=(self.root/'fixed-publication-registry').resolve()
        self.registry_patch=patch.object(self.adapter,'_PUBLICATION_REGISTRY_ROOT',self.registry,create=True)
        self.registry_patch.start();self.addCleanup(self.registry_patch.stop)
        old=(SCRIPT.parent/'langfuse-eval.py').read_bytes();path=self.source/'scripts/langfuse-eval.py'
        path.parent.mkdir();path.write_bytes(old)
        self.f['manifest']['sources']['scripts/langfuse-eval.py']=digest(old)
        self.f['manifest']['build_attestation_public_key']='a'*64
        self.f['manifest']['observer'].update(endpoint_sha256=digest(b'http://127.0.0.1:3000/api/public/otel/v1/traces'),project_sha256=digest(b'fake-public'))
        self.f['summary']['last_receipt_sha256']=''
        refresh_links(self.f)
        self.posted=[];self.visible={};self.verify_calls=0;self.post_error=None;self.bad_ack=False;self.on_read=None

    def pub_api(self):
        for name in ('verify_window_receipt_chain','verify_window_attestation','load_window_score_client','publish_window_scores','verify_window_scores'):
            self.assertTrue(callable(getattr(self.adapter,name,None)),name+' is missing')

    def chain(self,f=None):
        self.pub_api();f=f or self.f;plan=self.plan(f)
        entries=[{'summary_raw':wire(f['summary']),'receipt_raw':wire(f['receipt'])}]
        pins=[{'summary_sha256':digest(entries[0]['summary_raw']),'receipt_sha256':digest(entries[0]['receipt_raw'])}]
        return self.adapter.verify_window_receipt_chain(plan,wire(f['manifest']),entries,pins)

    def attest(self,f=None):
        self.pub_api();f=f or self.f;manifest=f['manifest'];binary=self.root/'actual-binary'
        if not binary.exists():binary.write_bytes(b'explicit actual binary bytes, never executed');binary.chmod(0o700)
        att={'schema':'full-window-actual-binary-attestation.v1','archive_commit':manifest['archive_commit'],
             'archive_tree':manifest['archive_tree'],'ready_manifest_sha256':digest(wire(manifest)),
             'binary_sha256':digest(binary.read_bytes()),'signature':'b'*128}
        def fake_attestation(command,**kwargs):
            job=json.loads(kwargs['input']);binding={k:att[k] for k in ('schema','archive_commit','archive_tree','ready_manifest_sha256','binary_sha256')}
            body={'schema':'full1220-attestation-verification.v1','manifest_sha256':digest(wire(manifest)),
                  'attestation_sha256':digest(wire(att)),'binary_sha256':att['binary_sha256'],
                  'canonical_binding_sha256':digest(wire(binding)),'signature_verified':True}
            return subprocess.CompletedProcess(command,0,wire(body),b'')
        with patch.object(self.adapter.subprocess,'run',side_effect=fake_attestation):
            return self.adapter.verify_window_attestation(wire(manifest),wire(att),digest(wire(att)),str(binary.resolve()),
                runtime={'go_executable':str(self.go),'go_sha256':digest(self.go.read_bytes())},
                output_dir=str(self.root/('attestation-'+str(len(list(self.root.glob('attestation-*')))))),execute=True)

    def test_attestation_selects_only_matching_v6_helper(self):
        old=self.attest();self.assertEqual(old.as_dict()['helper_sha256'],self.adapter._ATTESTATION_HELPER_SHA256)
        f=copy.deepcopy(self.f);f['manifest']['schema']='full-window-actual-ready.v6';f['manifest']['scope']['schema']='full-window-scope.v6'
        new=self.attest(f);self.assertEqual(new.as_dict()['helper_sha256'],self.adapter._ATTESTATION_V6_HELPER_SHA256)
        f['manifest']['scope']['schema']='full-window-scope.v5'
        with self.assertRaises(ValueError):self.attest(f)

    def bound_client(self,plan,proof):
        self.pub_api();fake=self.client(plan,proof);test=self
        class Delegate:
            def __init__(self,c):self.c=c
            def observations(self,trace):
                if test.on_read:test.on_read(trace)
                return fake.observations(trace)
            def request(self,path,body=None):
                if body is not None:
                    test.posted.append(copy.deepcopy(body))
                    if test.post_error:raise test.post_error
                    test.visible[body['id']]=copy.deepcopy(body)
                    return {} if test.bad_ack else {'id':body['id']}
                raise AssertionError('verify_scores should be the only score GET boundary')
            def verify_scores(self,scores):
                test.verify_calls+=1
                for score in scores:
                    if test.visible.get(score['id'])!=score:raise ValueError('delayed or mismatching score')
        settings=self.root/'evaluation-client.json'
        if not settings.exists():
            settings.write_bytes(wire({'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT':'http://127.0.0.1:3000/api/public/otel/v1/traces',
                'AGENT_LANGFUSE_ENVIRONMENT':'evaluation','LANGFUSE_PUBLIC_KEY':'fake-public','LANGFUSE_SECRET_KEY':'fake-secret'}));settings.chmod(0o600)
        with patch.object(self.adapter,'_old_langfuse_client_class',return_value=Delegate):
            return self.adapter.load_window_score_client(wire(self.f['manifest']),str(settings))

    def gates(self):
        self.pub_api();proof=self.project(output=self.root/('publication-projection-'+str(len(list(self.root.glob('publication-projection-*'))))));plan=self.plan();return plan,proof,self.chain(),self.attest(),self.bound_client(plan,proof)

    def publish(self,values,execute=True):
        plan,projection,chain,attestation,client=values
        return self.adapter.publish_window_scores(plan,projection,chain,attestation,client,execute=execute)

    def test_publish_requires_all_proofs_and_explicit_execute_before_post(self):
        values=self.gates()
        with self.assertRaises(ValueError):self.publish(values,False)
        for index in (1,2,3):
            bad=list(values);bad[index]={}
            with self.subTest(index=index),self.assertRaises(ValueError):self.publish(bad)
        self.assertEqual(self.posted,[])
        result=self.publish(values);self.assertTrue(result['all_verified'])
        self.assertEqual(len(self.posted),len(values[0]['scores']))

    def test_http_payload_excludes_internal_logical_identity_but_intent_keeps_it(self):
        values=self.gates();self.publish(values)
        for score in self.posted:self.assertNotIn('logical_identity',score.keys())
        scope=self.registry/values[0]['provenance']['scope_sha256']
        intents=[json.loads(path.read_bytes()) for path in scope.glob('logical-*.intent.json')]
        self.assertEqual(len(intents),len(self.posted))
        self.assertTrue(all(type(event['logical_identity']) is str for event in intents))

    def test_duplicate_publish_and_review_or_id_change_never_resend(self):
        values=self.gates();self.publish(values);count=len(self.posted)
        with self.assertRaises(ValueError):self.publish(values)
        self.f['review']['review_revision']=999
        updated=self.gates()
        with self.assertRaises(ValueError):self.publish(updated)
        altered=list(updated);altered[0]=copy.deepcopy(updated[0]);altered[0]['scores'][0]['id']='9'*64
        with self.assertRaises(ValueError):self.publish(altered)
        self.assertEqual(len(self.posted),count)

    def test_timeout_or_bad_ack_is_unknown_and_only_verify_is_allowed(self):
        values=self.gates();self.post_error=TimeoutError('sent then timeout')
        with self.assertRaises(ValueError):self.publish(values)
        count=len(self.posted)
        with self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(len(self.posted),count)
        result=self.adapter.verify_window_scores(values[0],values[4]);self.assertFalse(result['all_verified'])

    def test_bad_ack_does_not_repost_even_when_score_eventually_appears(self):
        values=self.gates();self.bad_ack=True
        with self.assertRaises(ValueError):self.publish(values)
        count=len(self.posted);self.bad_ack=False
        self.adapter.verify_window_scores(values[0],values[4])
        self.assertEqual(len(self.posted),count)

    def test_delayed_verify_performs_only_reads_and_uses_original_intent_metadata(self):
        values=self.gates();self.publish(values);saved=copy.deepcopy(self.visible);self.visible.clear()
        result=self.adapter.verify_window_scores(values[0],values[4]);self.assertFalse(result['all_verified'])
        count=len(self.posted);self.visible.update(saved)
        self.assertTrue(self.adapter.verify_window_scores(values[0],values[4])['all_verified'])
        self.assertEqual(len(self.posted),count)

    def test_intent_disk_failure_causes_zero_post_and_accepted_failure_cannot_retry(self):
        values=self.gates();original=self.adapter._publication_event
        def fail(handle,name,event):
            if name.startswith('score-'):raise OSError('intent fsync failure')
            return original(handle,name,event)
        with patch.object(self.adapter,'_publication_event',side_effect=fail),self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(self.posted,[])
        with self.assertRaises(ValueError):self.publish(values)

    def test_accepted_event_failure_after_post_is_never_resent(self):
        values=self.gates();original=self.adapter._publication_event
        def fail(handle,name,event):
            if name.endswith('.accepted.json'):raise OSError('accepted fsync failure')
            return original(handle,name,event)
        with patch.object(self.adapter,'_publication_event',side_effect=fail),self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(len(self.posted),1)
        with self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(len(self.posted),1)

    def test_registry_directory_relocation_before_post_fails_closed(self):
        values=self.gates();scope=values[0]['provenance']['scope_sha256'];called=[]
        def relocate(trace):
            if called:return
            called.append(trace);path=self.registry/scope;path.rename(self.registry/(scope+'-moved'));path.mkdir(mode=0o700)
        self.on_read=relocate
        with self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(self.posted,[])
        self.on_read=None
        with self.assertRaises(ValueError):self.publish(values)

    def test_concurrent_publish_loser_has_zero_post(self):
        values=self.gates();entered=threading.Event();release=threading.Event()
        def wait(trace):entered.set();release.wait(5)
        self.on_read=wait
        with ThreadPoolExecutor(max_workers=2) as pool:
            first=pool.submit(self.publish,values);self.assertTrue(entered.wait(5))
            with self.assertRaises(ValueError):self.publish(values)
            release.set();self.assertTrue(first.result(timeout=5)['all_verified'])
        self.assertEqual(len(self.posted),len(values[0]['scores']))

    def test_binary_tamper_and_client_project_change_reject_before_post(self):
        values=self.gates();(self.root/'actual-binary').write_bytes(b'tampered')
        with self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(self.posted,[])
        settings=self.root/'bad-client.json';settings.write_bytes(wire({'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT':'http://127.0.0.1:3000/api/public/otel/v1/traces',
            'AGENT_LANGFUSE_ENVIRONMENT':'evaluation','LANGFUSE_PUBLIC_KEY':'other','LANGFUSE_SECRET_KEY':'fake'}));settings.chmod(0o600)
        with self.assertRaises(ValueError):self.adapter.load_window_score_client(wire(self.f['manifest']),str(settings))

    def test_current_receipt_chain_tamper_and_missing_previous_entries_reject(self):
        self.pub_api();f=self.f;plan=self.plan();entries=[{'summary_raw':wire(f['summary']),'receipt_raw':wire(f['receipt'])}]
        pins=[{'summary_sha256':digest(entries[0]['summary_raw']),'receipt_sha256':digest(entries[0]['receipt_raw'])}]
        wrong=copy.deepcopy(entries);wrong[0]['receipt_raw']+=b' '
        with self.assertRaises(ValueError):self.adapter.verify_window_receipt_chain(plan,wire(f['manifest']),wrong,pins)
        with self.assertRaises(ValueError):self.adapter.verify_window_receipt_chain(plan,wire(f['manifest']),[],[])

    def test_remaining_accepted_marker_blocks_repost_after_partial_evidence_loss(self):
        values=self.gates();self.publish(values);count=len(self.posted)
        scope=self.registry/values[0]['provenance']['scope_sha256']
        for pattern in ('logical-*.intent.json','score-*.intent.json','*.sent.json'):
            for path in scope.glob(pattern):path.unlink()
        with self.assertRaises(ValueError):self.publish(values)
        self.assertEqual(len(self.posted),count)
        self.assertFalse(self.adapter.verify_window_scores(values[0],values[4])['all_verified'])

    def test_missing_one_original_intent_and_link_cannot_verify_remaining_subset_as_complete(self):
        values=self.gates();self.publish(values)
        scope=self.registry/values[0]['provenance']['scope_sha256']
        path=next(scope.glob('logical-*.intent.json'));event=json.loads(path.read_bytes());path.unlink()
        (scope/('score-'+event['score']['id']+'.intent.json')).unlink()
        result=self.adapter.verify_window_scores(values[0],values[4])
        self.assertFalse(result['all_verified'])

    def test_original_batch_manifest_is_durable_before_first_logical_intent(self):
        values=self.gates();original=self.adapter._publication_event;seen=[]
        def check(handle,name,event):
            if name.startswith('logical-'):
                target=handle.scope/('window-%06d.batch.json'%values[0]['window_index'])
                self.assertTrue(target.exists(),'complete batch inventory must precede first logical intent')
                batch=json.loads(target.read_bytes());self.assertEqual(len(batch['scores']),len(values[0]['scores']));seen.append(name)
            return original(handle,name,event)
        with patch.object(self.adapter,'_publication_event',side_effect=check):self.assertTrue(self.publish(values)['all_verified'])
        self.assertEqual(len(seen),len(values[0]['scores']))

    def test_new_review_with_fewer_metrics_verifies_all_original_batch_payloads(self):
        values=self.gates();self.publish(values);count=len(values[0]['scores'])
        self.f['review']['review_revision']=999;self.f['review']['rows'][0]['pass_score_publish_eligible']=False
        changed=self.plan();self.assertLess(len(changed['scores']),count)
        result=self.adapter.verify_window_scores(changed,values[4])
        self.assertTrue(result['all_verified']);self.assertEqual(result['scores_checked'],count)

    def test_complete_nonzero_prior_quality_chain_with_current_partial_and_unstarted(self):
        self.pub_api();prior=partial_fixture(quality=True);current=later_quality_fixture(prior_quality=1)
        for key in ('source_root','sources','observer','build_attestation_public_key'):
            current['manifest'][key]=copy.deepcopy(self.f['manifest'][key])
        prior['manifest']=copy.deepcopy(current['manifest'])
        prior['receipt'].update(scope_sha256=current['manifest']['scope_sha256'],cleanup=True,
            environment_cleanup=True,observer=True,advanced=True,payload_verified=True)
        prior['summary'].update(last_receipt_sha256='',closed=False,acquisition_done=False,next_window_index=2)
        recalculate_counts(prior)
        for row,review in zip(current['receipt']['rows'],current['review']['rows']):
            row['trace_id']='e'+row['trace_id'][1:];review['trace_id']=row['trace_id']
            index=current['receipt']['rows'].index(row)
            refresh_record(current,index,lambda d,trace=row['trace_id']:[ob['turn']['response']['telemetry'].__setitem__('langfuse_trace_id',trace) for ob in d['result']['observations']])
        def partial(record):
            result=record['result'];result['observations']=result['observations'][:1]
            result['review_requirements']=[req for req in result['review_requirements'] if req['turn_index']==0]
            result.update(status='failed',structural_pass=False,errors=['budget_exhausted']);record['disposition']='halt'
        refresh_record(current,2,partial)
        current['receipt']['rows'][2].update(status='failed',structural_pass=False,observed_turns=1,finished=False,cleanup=False,observer=False,disposition='halt')
        review=current['review']['rows'][2];review.update(original_status='failed',original_structural_pass=False,
            original_errors=['budget_exhausted'],observed_turns=1,finished=False,semantic_task_verdict='not_assessable',task_verdict='failed',pass_score_publish_eligible=False)
        review['requirements']=[req for req in review['requirements'] if req['turn_index']==0]
        for req in review['requirements']:req['verdict']='not_assessable'
        last=current['receipt']['rows'][3];del current['records'][last['recording_file']]
        last.update(attempted=False,finished=False,observed_turns=0,trace_id='',recording_file='',recording_sha256='',
            status='not_started',disposition='',cleanup=False,observer=False,structural_pass=False)
        current['review']['rows'][3]={**{key:last[key] for key in ('ordinal','pass','case_key','arm')},
            'original_status':'not_started','original_success':False,'task_verdict':'not_started',
            'semantic_task_verdict':'not_started','trace_id':None,'recording_file':None,'recording_sha256':None,
            'requirements':[],'pass_score_publish_eligible':False}
        current['receipt']['status']='stopped';recalculate_counts(current)
        for key in ('attempted_arms','finished_arms','observed_turns','quality_end_unrun_turns'):
            current['summary'][key]+=prior['summary'][key]
        current['summary'].update(unattempted_arms=7320-current['summary']['attempted_arms'],
            global_unrun_turns=11772-current['summary']['observed_turns']-current['summary']['quality_end_unrun_turns'])
        current['summary']['last_receipt_sha256']=digest(wire(prior['summary']));refresh_links(current)
        self.f=current;plan=self.plan();entries=[{'summary_raw':wire(f['summary']),'receipt_raw':wire(f['receipt'])} for f in (prior,current)]
        pins=[{kind+'_sha256':digest(entry[kind+'_raw']) for kind in ('summary','receipt')} for entry in entries]
        chain=self.adapter.verify_window_receipt_chain(plan,wire(current['manifest']),entries,pins)
        self.assertEqual(chain.as_dict()['cumulative']['observed_turns'],11)
        self.assertEqual(chain.as_dict()['cumulative']['quality_end_unrun_turns'],2)
        self.assertEqual(plan['denominators']['window']['unattempted_arms'],1)
        proof=self.project();client=self.bound_client(plan,proof)
        self.assertTrue(self.publish((plan,proof,chain,self.attest(),client))['all_verified'])
        for row in self.posted:
            if row['metadata']['original_status']=='failed':self.assertNotEqual(row['value'],'passed')
        for mutation in ('link','component','missing'):
            bad=copy.deepcopy(entries);badpins=copy.deepcopy(pins)
            if mutation=='missing':bad=bad[1:];badpins=badpins[1:]
            else:
                summary=json.loads(bad[1]['summary_raw'])
                if mutation=='link':summary['last_receipt_sha256']=pins[0]['receipt_sha256']
                else:summary['observed_turns']+=1;summary['quality_end_unrun_turns']-=1
                bad[1]['summary_raw']=wire(summary);badpins[1]['summary_sha256']=digest(bad[1]['summary_raw'])
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):self.adapter.verify_window_receipt_chain(plan,wire(current['manifest']),bad,badpins)

    def test_verify_without_publication_does_not_create_registry_or_post(self):
        values=self.gates();self.assertFalse(self.registry.exists())
        result=self.adapter.verify_window_scores(values[0],values[4])
        self.assertFalse(result['all_verified']);self.assertFalse(self.registry.exists());self.assertEqual(self.posted,[])
        paths={}
        for name in ('manifest','summary','receipt','review'):
            path=self.root/('cli-'+name+'.json');path.write_bytes(wire(self.f[name]));paths[name+'_path']=str(path)
        directory=self.root/'cli-records';directory.mkdir()
        for name,raw in self.f['records'].items():(directory/name).write_bytes(raw)
        job={'schema':'full1220-window-score-job.v1',**paths,'records_dir':str(directory),
             'expected_hashes':arguments(self.f)[-1],'client_settings_path':str(self.root/'evaluation-client.json')}
        jobfile=self.root/'cli-job.json';jobfile.write_bytes(wire(job))
        with patch.object(self.adapter,'load_window_score_client',return_value=values[4]),patch('sys.stdout',new_callable=io.StringIO):
            self.assertEqual(self.adapter.main(['verify','--job',str(jobfile)]),2)
        self.assertFalse(self.registry.exists());self.assertEqual(self.posted,[])

    def test_cli_default_plan_does_not_load_credentials_or_write_registry(self):
        self.pub_api();self.assertTrue(callable(getattr(self.adapter,'main',None)))
        with patch.object(self.adapter,'load_window_score_client',side_effect=AssertionError('read-only default')):
            self.assertEqual(self.adapter.main([]),0)
        self.assertFalse(self.registry.exists())



def v4_fixture(base=None):
    f = fixture() if base is None else base; m = f['manifest']; scope = m['scope']
    m['schema'] = 'full-window-actual-ready.v4'; scope['schema'] = 'full-window-scope.v4'
    scope['baseline_evidence_sha256'] = '12a9d584ea9508a9fc7701532ad9214b0c743a8e13d17829432fac0d6b48a20c'
    scope['budget_approval_sha256'] = 'd5062e6f39f9ae7c9a8461121f9ba907c798100d7a5428af72da459595d2d554'
    declaration = json.loads((SCRIPT.parents[1]/'eval/paired-pilots/2026-10-29-cache-price/declaration.json').read_bytes())
    scope['width'] = 28
    # The compact score fixture retains its four recorded arms and full schedule.
    for window, cap in zip(scope['windows'], declaration['caps']): window.update(cap)
    scope['windows'] += [dict(id='cap-only-%03d'%i, first_ordinal=3661, last_ordinal=3660, **cap)
                         for i, cap in enumerate(declaration['caps'][2:], 3)]
    m['baseline_evidence'] = {'path': '/offline/stopped-v3.json', 'sha256': scope['baseline_evidence_sha256']}
    m['historical_claims'] = {'full1220-first-actual-20261028': '52bbf6e7edf8b66099141db5ac7c6782e45b8afc3bcd9145ea1fd51f936eca48', 'full1220-v2-20261028/window-000001': 'c02cf79e01ad6727cf6bfe9c6157aa74c43ab68cedcb622457a2acb94aacb682', 'full1220-v2-20261028/window-000002': '518af94e74565c7f21e3d89da226e25563ad29756a04100a11d55755e6e91820', 'full1220-v2-20261028/window-000003': 'fd4ccb5cc9b2fed837383d532271d34a4a2dc3c15062b044dc411a5a12124ae6', 'full1220-v2-20261028/window-000004': '76bc6395aaf66ce6cc5569fd440802b6517110975618cc9773fd3e9b0e1a72b9', 'full1220-v2-20261028/window-000005': 'f2eb059f5bf55dbab4d0328633de7e98b029cca441cd26b584f7b96cead46988', 'full1220-v2-20261028/window-000006': '0f074c6761a898c46ba831b4a96f9818d27093955a5fb42ae8e662f745b173aa', 'recharge-cancel-20261024': 'a9ce7a22d79e4e42e8ded0b932c34648df3db0d0574ad0710c098d2b02cbc8f4', 'recharge-confirm-20261023': '2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc', 'recharge-confirm-diagnostic-retest-20261023': '23047a143039f719531cfe403f1769abda7a6091b2ee51cecf6184eb4768e654', 'recharge-supplement-20261024': '824e1e44882b1043bba833ec6af5b88980d6d89b4ce8e4023fd12c68c98fb11c', 'refund-cap-20261024': '9a026381dea69bc1feb9712d58a41e305877701e1355ba2464543aaaa0857b30', 'v5-repeated-cohort-20261024': 'c7f14ee3b310cec23cf71b7dc004fe73b29ec5e8efa4b6c4167eb91159c8dd58', 'v5-repeated-export-diagnostic-20261025': '8c63988dbd31b09cb523c7e0b067f5978ad0ee91f5736f0fc581752544161e6a', 'v5-repeated-receiver-20261027': 'f09de5bb9ab4147f140f54db7a25c5f419b7a4e48137d63c1782ea7c38d768b3', 'v5-six-case-cohort-20261024': 'b49808feb6a124c60c7f3fd88a7965589368a0ce8d8f4336e94ebaee9a025d34', 'full1220-v3-20261028/window-000001': '5cb4f10bd990e073b71ac039d7d09670d732de72cb2831422444dc0e525373e1', 'full1220-v3-20261028/window-000002': 'b46344d00c277ce8c3d7a4ad317fd23d97f77a2f0e5d50960422affd51535dd4', 'full1220-v3-20261028/window-000003': '37d8e559963bf116dbafe8c18f059e77125f8be8cc604fe5d6d4b2b822f26088', 'full1220-v3-20261028/window-000004': 'd9cb74a2376dc827744cc929eda9e39547b03ed0a20fa0ea5e8bb8aca0ba262d', 'full1220-v3-20261028/window-000005': '735f6ad0fb8a3173f6d1db6fc012260fd9040c4357b00b16824ab28df4c95998', 'full1220-v3-20261028/window-000006': '9ab2594ef283fa01446e55d7c5598397076c9ef0ee9e88cbd5e5efb2a1ca5493', 'full1220-v3-20261028/window-000007': '503ca8aaa9a4ae807dc4bd98b196414222083092751e5177ca92ce12361df926', 'full1220-v3-20261028/window-000008': '54c227dccb982dcf80b19d9603b6d8eba6765a365463cbaf23ff5259547a1cc7', 'full1220-v3-20261028/window-000009': '0f698f0bf29550cee7052d9ca04f94ef76592942f858b4d84e44a7651f5df6b1'}
    m['provider_limits'] = {
        'model': {'model':'deepseek-flash','max_request_bytes':32768,'max_input_tokens':36864,'max_output_tokens':128,
                  'input_micro_per_million':2000000,'output_micro_per_million':8000000,'cache_hit_input_micro_per_million':40000},
        'jev': {'model':'jev-1.13.0','max_request_bytes':4096,'max_input_tokens':64000,'max_output_tokens':1000000,
                'input_micro_per_million':42000,'output_micro_per_million':0}}
    m['scope_sha256'] = digest(wire(scope)); f['receipt']['scope_sha256'] = m['scope_sha256']
    f['review']['bindings']['scope_sha256'] = m['scope_sha256']; refresh_links(f)
    return f


class V4CompatibilityTests(unittest.TestCase):
    setUpClass = classmethod(WindowScorePlanTests.setUpClass.__func__)
    plan = WindowScorePlanTests.plan
    reject = WindowScorePlanTests.reject
    def test_v4_plan_records_configuration_only_cache_accounting_provenance(self):
        try: plan = self.plan(v4_fixture())
        except ValueError as error: self.fail('Valid epoch4 cache accounting must be accepted: '+str(error))
        pricing = plan['provenance']['configured_cache_accounting']
        self.assertEqual(pricing['schema'], 'full-window-accounting.v2')
        self.assertEqual(pricing['model_cache_hit_input_micro_per_million'],40000)
        self.assertEqual(pricing['cache_absent_settlement'],'all_input_at_miss_rate_without_observed_cache_split')
        self.assertFalse(pricing['proves_cache_usage']); self.assertFalse(pricing['proves_knowledge_usage'])
        self.assertEqual(len(plan['scores']),12)
        self.assertTrue(all(s['metadata']['configured_cache_accounting']==pricing for s in plan['scores']))

    def test_v4_configuration_does_not_launder_negative_unknown_or_held_pass(self):
        for quality in (False,True):
            f=v4_fixture(partial_fixture(quality));plan=self.plan(f);trace=f['receipt']['rows'][0]['trace_id']
            first=[score for score in plan['scores'] if score['traceId']==trace]
            self.assertTrue(first);self.assertFalse(any(score['value']=='passed' for score in first))
            self.assertEqual(plan['rows'][0]['semantic_task_verdict'],'failed' if quality else 'not_assessable')
            self.assertFalse(plan['publish_ready'])
        f=v4_fixture()
        for row in f['review']['rows']:row['pass_score_publish_eligible']=False
        refresh_links(f);plan=self.plan(f)
        self.assertEqual(len(plan['scores']),4)
        self.assertTrue(all(score['name'].endswith('original_status') for score in plan['scores']))

    def test_v4_rejects_coherent_wrong_baseline_claim_price_caps_and_epoch(self):
        mutations = [lambda m: m['scope'].__setitem__('schema','full-window-scope.v3'),
            lambda m: m['scope'].__setitem__('baseline_evidence_sha256','0'*64),
            lambda m: m['scope'].__setitem__('budget_approval_sha256','0'*64),
            lambda m: m['baseline_evidence'].__setitem__('sha256','0'*64),
            lambda m: m['historical_claims'].pop(next(iter(m['historical_claims']))),
            lambda m: m['historical_claims'].__setitem__(next(iter(m['historical_claims'])),'0'*64),
            lambda m: m['scope']['windows'][0].__setitem__('model_cap_micro_cny',387795),
            lambda m: m['scope']['windows'][0].__setitem__('jev_cap_micro_usd',5275),
            lambda m: m['provider_limits']['model'].pop('cache_hit_input_micro_per_million')]
        for provider,field,value in [('model','cache_hit_input_micro_per_million',0),('model','input_micro_per_million',1),
            ('model','output_micro_per_million',1),('jev','cache_hit_input_micro_per_million',1),('jev','output_micro_per_million',1),
            ('jev','input_micro_per_million',1),('model','max_output_tokens',512)]:
            mutations.append(lambda m,p=provider,k=field,v=value:m['provider_limits'][p].__setitem__(k,v))
        for mutate in mutations:
            f=v4_fixture(); mutate(f['manifest']); m=f['manifest'];m['scope_sha256']=digest(wire(m['scope']))
            f['receipt']['scope_sha256']=m['scope_sha256'];f['review']['bindings']['scope_sha256']=m['scope_sha256'];refresh_links(f)
            with self.subTest(mutate=mutate):self.reject(f)

    def test_old_epoch_plan_json_and_ids_remain_byte_identical(self):
        expected = {2: 'da0f9b2bb04ae9cfa0d5bb2110de385d4f59521a8a15990885269f080767d4cb', 3: '3edbaa382494a6b6483f4a7c165299c7e4ac9a20495a5f2776b2a0a88bb2229e'}
        for epoch in (2,3):
            f=fixture(); m=f['manifest'];m['schema']='full-window-actual-ready.v%d'%epoch;m['scope']['schema']='full-window-scope.v%d'%epoch
            m['scope_sha256']=digest(wire(m['scope']));f['receipt']['scope_sha256']=m['scope_sha256'];f['review']['bindings']['scope_sha256']=m['scope_sha256'];refresh_links(f)
            raw=json.dumps(self.plan(f),sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()
            self.assertEqual(digest(raw),expected[epoch])

    def test_old_epoch_cannot_enable_nonzero_cache_price(self):
        for epoch in (2,3):
            f=fixture();f['manifest']['schema']='full-window-actual-ready.v%d'%epoch;f['manifest']['scope']['schema']='full-window-scope.v%d'%epoch
            f['manifest']['provider_limits']={'model':{'cache_hit_input_micro_per_million':40000},'jev':{}}
            m=f['manifest'];m['scope_sha256']=digest(wire(m['scope']));f['receipt']['scope_sha256']=m['scope_sha256'];f['review']['bindings']['scope_sha256']=m['scope_sha256'];refresh_links(f)
            self.reject(f)


class ZeroSuffixCompatibilityTests(unittest.TestCase):
    setUpClass = classmethod(WindowScorePlanTests.setUpClass.__func__)
    plan = WindowScorePlanTests.plan
    reject = WindowScorePlanTests.reject

    @staticmethod
    def zero_quality_fixture():
        f=fixture();row=f['receipt']['rows'][0];review=f['review']['rows'][0]
        def negative(record):
            record['result'].update(status='failed',structural_pass=False,errors=['quality negative'])
            record['disposition']='halt'
        refresh_record(f,0,negative)
        row.update(status='failed',structural_pass=False,disposition='halt',exclude_complete_task_comparison=True)
        row['quality_end']=copy.deepcopy(partial_fixture(quality=True)['receipt']['rows'][0]['quality_end'])
        row['quality_end'].update(observed_turns=2,unrun_turns=0)
        review.update(original_status='failed',original_structural_pass=False,original_errors=['quality negative'],
                      semantic_task_verdict='failed',task_verdict='failed',pass_score_publish_eligible=False)
        for req in review['requirements']:req['verdict']='failed'
        f['receipt']['quality_end_arms']=1
        f['manifest']['sources']['internal/taskbench/window_collector.go']='9131a3bc61c7cef89c1bda02dabffffe5d85bbebd48e9834afb542553cd200d7'
        recalculate_counts(f)
        # Production windowAcquisitionReceipt uses omitempty for these int zeros.
        f['receipt'].pop('quality_end_unrun_turns',None);f['receipt'].pop('global_unrun_turns',None)
        refresh_links(f);return f

    def test_known_go_producer_zero_quality_suffix_omission_preserves_negative(self):
        f=self.zero_quality_fixture()
        try:plan=self.plan(f)
        except ValueError as error:self.fail('Known Go omitempty zero suffix must be accepted: '+str(error))
        self.assertEqual(plan['denominators']['window']['quality_end_unrun_turns'],0)
        self.assertEqual(plan['denominators']['window']['global_unrun_turns'],0)
        self.assertEqual(plan['rows'][0]['original_status'],'failed')
        self.assertTrue(plan['rows'][0]['exclude_complete_task_comparison'])
        self.assertFalse(any(score['value']=='passed' for score in plan['scores']
                             if score['traceId']==f['receipt']['rows'][0]['trace_id']))
        self.assertNotIn('quality_end_unrun_turns',f['receipt']);self.assertNotIn('global_unrun_turns',f['receipt'])

    def test_actual_one_observed_one_defined_quality_row_allows_only_omitted_top_zeros(self):
        f=self.zero_quality_fixture();scope=f['manifest']['scope'];receipt=f['receipt']
        scope['slots'][0]['user_turns']=1;scope['slots'][-1]['user_turns']=2
        for row in receipt['rows'][:2]:row.update(defined_turns=1,observed_turns=1)
        receipt['rows'][0]['quality_end']['observed_turns']=1
        receipt.update(planned_current_turns=6,observed_turns=6)
        try:self.adapter._planned_rows(f['manifest'],receipt)
        except ValueError as error:self.fail('Actual one-turn Go zero-suffix receipt shape rejected: '+str(error))

    def test_unknown_or_absent_producer_cannot_default_missing_suffix_to_zero(self):
        for producer in (None,'0'*64):
            f=self.zero_quality_fixture()
            if producer is None:f['manifest']['sources'].pop('internal/taskbench/window_collector.go')
            else:f['manifest']['sources']['internal/taskbench/window_collector.go']=producer
            refresh_links(f);self.reject(f)
            # Explicit, consistent integer zeros preserve prior unknown-source behavior.
            f['receipt'].update(quality_end_unrun_turns=0,global_unrun_turns=0);refresh_links(f);self.plan(f)

    def test_nonzero_count_quality_or_global_suffix_must_be_explicit(self):
        for missing in ('quality_end_arms','quality_end_unrun_turns'):
            f=partial_fixture(quality=True)
            f['manifest']['sources']['internal/taskbench/window_collector.go']='9131a3bc61c7cef89c1bda02dabffffe5d85bbebd48e9834afb542553cd200d7'
            del f['receipt'][missing];refresh_links(f);self.reject(f)
        f=partial_fixture(quality=True);r=f['receipt'];r['rows'][1].update(observed_turns=1,finished=False)
        r.update(finished_arms=3,observed_turns=6);r.pop('global_unrun_turns')
        f['manifest']['sources']['internal/taskbench/window_collector.go']='9131a3bc61c7cef89c1bda02dabffffe5d85bbebd48e9834afb542553cd200d7'
        with self.assertRaises(ValueError):self.adapter._planned_rows(f['manifest'],r)

    def test_explicit_zero_suffix_stays_type_sensitive_and_row_suffix_is_required(self):
        for key in ('quality_end_unrun_turns','global_unrun_turns'):
            for value in (False,0.0,'0',None,1):
                f=self.zero_quality_fixture();f['receipt'][key]=value;refresh_links(f)
                with self.subTest(key=key,value=value):self.reject(f)
        f=self.zero_quality_fixture();del f['receipt']['rows'][0]['quality_end']['unrun_turns'];refresh_links(f);self.reject(f)


class ClosedArchiveInventoryTests(unittest.TestCase):
    setUpClass = classmethod(ContentProjectionTests.setUpClass.__func__)

    def setUp(self):
        ContentProjectionTests.setUp(self)
        self.root=self.root.resolve();self.source=self.source.resolve()
        self.f['manifest']['source_root']=str(self.source);refresh_links(self.f)
    project = ContentProjectionTests.project
    api = ContentProjectionTests.api
    fake_go = ContentProjectionTests.fake_go

    def extra(self):
        path=self.source/'scripts/__pycache__/full1220-window-scores.cpython-312.pyc'
        path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(b'fixture pyc never executed'+b'x'*129661)
        return [{'path':path.relative_to(self.source).as_posix(),'sha256':digest(path.read_bytes()),'size':path.stat().st_size}]

    def inventory(self,extras):
        self.assertTrue(callable(getattr(self.adapter,'_verify_closed_archive_inventory',None)),
                        'Closed archive exact inventory verifier is missing')
        return self.adapter._verify_closed_archive_inventory(self.source,self.f['manifest']['sources'],extras)

    def test_closed_inventory_exact_extra_shape_and_no_default_waiver(self):
        extras=self.extra();self.inventory(extras)
        with self.assertRaises(ValueError):self.project()
        self.assertEqual(self.commands,[])
        self.assertTrue((self.source/extras[0]['path']).exists())

    def test_closed_inventory_rejects_more_files_empty_directories_and_links(self):
        extras=self.extra();self.inventory(extras)
        for name,directory in [('scripts/__pycache__/other.pyc',False),('extra-empty',True)]:
            target=self.source/name
            if directory:target.mkdir()
            else:target.write_bytes(b'extra')
            with self.subTest(name=name),self.assertRaises(ValueError):self.inventory(extras)
            if directory:target.rmdir()
            else:target.unlink()
        link=self.source/'scripts/__pycache__/linked.pyc';link.symlink_to(self.source/extras[0]['path'])
        with self.assertRaises(ValueError):self.inventory(extras)

    def test_closed_inventory_rechecks_changed_extra_and_signed_source(self):
        extras=self.extra();self.inventory(extras);path=self.source/extras[0]['path'];original=path.read_bytes()
        path.write_bytes(b'z'+original[1:])
        with self.assertRaises(ValueError):self.inventory(extras)
        path.write_bytes(original);self.inventory(extras)
        (self.source/'go.mod').write_bytes(b'changed signed source')
        with self.assertRaises(ValueError):self.inventory(extras)


class ClosedP4WitnessTests(unittest.TestCase):
    setUpClass = classmethod(ContentProjectionTests.setUpClass.__func__)
    fake_go = ContentProjectionTests.fake_go
    P4 = SCRIPT.parents[1]/'.tmp/paired-live-private/full1220-actual-v4-freeze-20261029'

    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory();self.addCleanup(self.temporary.cleanup)
        self.root=Path(self.temporary.name).resolve()
        self.witness=self.P4/'closed-archive-extra-witness.private.json'
        review=self.P4/'semantic-review-window-000001.json'
        if not self.witness.is_file() or not review.is_file():self.skipTest('Exact private stopped P4 witness/review unavailable')
        self.witness_raw=self.witness.read_bytes();self.spec={'path':str(self.witness.resolve()),'sha256':digest(self.witness_raw)}
        self.manifest_raw=(self.P4/'actual-manifest.json').read_bytes();self.manifest=json.loads(self.manifest_raw)
        self.source=Path(self.manifest['source_root']);self.f={'manifest':self.manifest}
        summary=(self.P4/'records/window-000001-summary.json').read_bytes();receipt_raw=(self.P4/'records/window-000001/receipt.json').read_bytes()
        receipt=json.loads(receipt_raw);records={r['recording_file']:(self.P4/'records/window-000001'/r['recording_file']).read_bytes() for r in receipt['rows'] if r['attempted']}
        review_raw=review.read_bytes();pins={'manifest_sha256':digest(self.manifest_raw),'summary_sha256':digest(summary),
            'receipt_sha256':digest(receipt_raw),'review_sha256':digest(review_raw),'records':{n:digest(b) for n,b in records.items()},
            'projection_helper_sha256':'57847bf1ba02864c918261813ea45f68dc57f4dbd5d06349cc0dd661e225cdfc'}
        self.args=(self.manifest_raw,summary,receipt_raw,records,review_raw,pins)
        self.go=self.root/'go';self.go.write_bytes(b'mocked compiler boundary only');self.go.chmod(0o700)
        self.cache=self.root/'modules';self.cache.mkdir();sdk=self.root/'MacOSX26.5.sdk';sdk.mkdir()
        self.sdk_patch=patch.object(self.adapter,'_PROJECTION_SDKROOT',str(sdk));self.sdk_patch.start();self.addCleanup(self.sdk_patch.stop)
        self.overrides={};self.commands=[];self.run_mutation=None;self.after_output=None;self.stdout_override=None

    def witness_api(self):
        self.assertTrue(callable(getattr(self.adapter,'_validate_closed_archive_extra_witness',None)),
                        'Pinned closed-archive witness verifier is missing')

    def validate(self,spec=None):
        self.witness_api()
        return self.adapter._validate_closed_archive_extra_witness(self.spec if spec is None else spec,self.manifest_raw,self.source)

    def project(self,spec=None,with_witness=True):
        self.witness_api();kwargs={'closed_archive_extra_witness':self.spec if spec is None else spec} if with_witness else {}
        with patch.object(self.adapter.subprocess,'run',side_effect=self.fake_go):
            return self.adapter.project_window_content(*self.args,source_root=str(self.source),output_dir=self.root/'projection',
                runtime={'go_executable':str(self.go),'go_sha256':digest(self.go.read_bytes()),'module_cache':str(self.cache)},execute=True,**kwargs)

    def test_actual_p4_pinned_witness_stages_only_signed_sources_and_keeps_go_bytes(self):
        evidence=self.validate();self.assertTrue(evidence['not_actual_ready'])
        self.assertEqual(evidence['phase'],'post_stop_content_projection')
        proof=self.project();provenance=proof.archive_source_provenance()
        self.assertTrue(provenance['checked_before_and_after']);self.assertTrue(provenance['not_actual_ready'])
        self.assertEqual(provenance['witness_sha256'],self.spec['sha256'])
        self.assertEqual(proof.as_dict(),json.loads((self.root/'projection/projection.json').read_bytes()))
        self.assertEqual(digest((self.root/'projection/projection.json').read_bytes()),proof._raw_sha)
        self.assertNotIn('archive_source_provenance',proof.as_dict())
        self.assertFalse((self.root/'projection/source/scripts/__pycache__').exists())
        self.assertTrue((self.source/'scripts/__pycache__/full1220-window-scores.cpython-312.pyc').exists())
        self.assertEqual(len(self.commands),1)
        plan=self.adapter.build_window_score_plan(*self.args)
        result=self.adapter.validate_window_traces(plan,proof,ContentProjectionTests.client(self,plan,proof))
        self.assertTrue(result['all_pass']);self.assertEqual(result['archive_source_provenance'],provenance)
        provenance['not_actual_ready']=False
        self.assertTrue(proof.archive_source_provenance()['not_actual_ready'])

    def test_actual_p4_without_witness_fails_before_any_helper(self):
        with self.assertRaises(ValueError):self.project(with_witness=False)
        self.assertFalse((self.root/'projection').exists());self.assertEqual(self.commands,[])

    def test_witness_wrong_sha_unknown_fields_live_exit_or_open_terminal_rejected(self):
        self.witness_api()
        with self.assertRaises(ValueError):self.validate({**self.spec,'sha256':'0'*64})
        link=self.root/'witness-link.json';link.symlink_to(self.witness)
        with self.assertRaises(ValueError):self.validate({'path':str(link),'sha256':self.spec['sha256']})
        for field in ('manifest_sha256','source_map_sha256','source_root','owner_exit','terminal','unknown'):
            doc=json.loads(self.witness_raw)
            if field in ('owner_exit','terminal'):
                other=self.root/(field+'.json');other.write_bytes(wire({'exit_code':0,'relaunch_allowed':True} if field=='owner_exit' else {'closed':False}))
                doc[field]={'path':str(other),'sha256':digest(other.read_bytes())}
            else:doc[field]='wrong'
            path=self.root/(field+'-witness.json');path.write_bytes(wire(doc))
            with self.subTest(field=field),self.assertRaises(ValueError):self.validate({'path':str(path),'sha256':digest(path.read_bytes())})
        self.assertEqual(self.commands,[])

    def test_witness_mutation_during_helper_cannot_seal_projection(self):
        copy=self.root/'witness-copy.json';copy.write_bytes(self.witness_raw);spec={'path':str(copy),'sha256':digest(copy.read_bytes())}
        self.run_mutation=lambda *args:copy.write_bytes(self.witness_raw+b' ')
        with self.assertRaises(ValueError):self.project(spec)
        self.assertEqual(len(self.commands),1)
        self.assertFalse((self.root/'projection/archive-source-provenance.json').exists())

    def test_cli_forwards_only_explicit_job_witness_and_never_loads_client_on_rejection(self):
        self.witness_api();plan=self.adapter.build_window_score_plan(*self.args)
        job={'chain':[],'attestation_path':str(self.P4/'actual-manifest.json.binary-attestation.json'),
             'attestation_sha256':'d36949ed8ccd36e8cef61e58c94b8b268feec7e34dbec672c47546c4c85a2496',
             'binary_path':str(self.P4/'full-window-actual-v4'),'runtime':{},'attestation_output':str(self.root/'sig'),
             'projection_output':str(self.root/'projection'),'closed_archive_extra_witness':self.spec}
        with patch.object(self.adapter,'_read_score_job',return_value=(job,self.args,plan)),             patch.object(self.adapter,'verify_window_receipt_chain'),patch.object(self.adapter,'verify_window_attestation'),             patch.object(self.adapter,'project_window_content',side_effect=ValueError('test stop before client')) as projected,             patch.object(self.adapter,'load_window_score_client',side_effect=AssertionError('client must remain unread')),             patch('sys.stderr',new_callable=io.StringIO):
            self.assertEqual(self.adapter.main(['publish','--execute','--job','explicit-test-job']),2)
        self.assertEqual(projected.call_args.kwargs['closed_archive_extra_witness'],self.spec)


def v5_fixture(base=None):
    f=v4_fixture(base);m=f['manifest'];scope=m['scope']
    m['schema']='full-window-actual-ready.v5';scope['schema']='full-window-scope.v5'
    baseline='2f8c83546a2918b85801160f521465c2f53e040073b3590e1ecde53f278ef340'
    scope['baseline_evidence_sha256']=baseline;m['baseline_evidence']={'path':'/offline/stopped-v4.json','sha256':baseline}
    m['historical_claims'].update({
        'full1220-v4-20261029/window-000001':'58d63365bd94f562642a7e39138da6b347d90b02c283c3aa661274d523b2784e',
        'full1220-v4-20261029/window-000002':'388914ae974e3c3218060997c6c289b92492d8d489b12beee79be1fad6e4dace',
        'full1220-v4-20261029/window-000003':'412231c60c4ea4ac8df02377a2dde3f065dfdd84fb73fadaab91bc73ec169dd1'})
    # Literal approved largest-remainder vector: no private file dependency.
    caps=[(375675, 5167), (687114, 5167), (620628, 5167), (652122, 5167), (596132, 5167), (620628, 5167), (585634, 5167), (512149, 5167), (326686, 5167), (302191, 5167), (344182, 5167), (400171, 5167), (501651, 5167), (417667, 5167), (428165, 5167), (302191, 5167), (333684, 5167), (302191, 5167), (400171, 5167), (536644, 5167), (638124, 5166), (536644, 5166), (526147, 5166), (526147, 5166), (512149, 5166), (536644, 5166), (501651, 5166), (484154, 5166), (512149, 5166), (459658, 5166), (435163, 5166), (375675, 5166), (333684, 5166), (477156, 5166), (368677, 5166), (393172, 5166), (400171, 5166), (501651, 5166), (400171, 5166), (428165, 5166), (536644, 5166), (568138, 5166), (620628, 5166), (519147, 5166), (309188, 5166), (333684, 5166), (302191, 5166), (386174, 5166), (536644, 5166), (620628, 5166), (554140, 5166), (526146, 5166), (526146, 5166), (512149, 5166), (536644, 5166), (501651, 5166), (501651, 5166), (494652, 5166), (477156, 5166), (428165, 5166), (368677, 5166), (351181, 5166), (459658, 5166), (386174, 5166), (393172, 5166), (386174, 5166), (501651, 5166), (410669, 5166), (428165, 5166), (526146, 5166), (568138, 5166), (620628, 5166), (484154, 5166), (477156, 5166), (687114, 5166), (687114, 5166), (578635, 5166), (638124, 5166), (568138, 5166), (585634, 5166), (428165, 5166), (326686, 5166), (344182, 5166), (309188, 5166), (452660, 5166), (484154, 5166), (435163, 5166), (452660, 5166), (442163, 5166), (358180, 5166), (344182, 5166), (459658, 5166), (400171, 5166), (393172, 5166), (368677, 5166), (501651, 5166), (417667, 5166), (428165, 5166), (519147, 5166), (568138, 5166), (620628, 5166), (501651, 5166), (442163, 5166), (704611, 5166), (669617, 5166), (596132, 5166), (620628, 5166), (585634, 5166), (585634, 5166), (442163, 5166), (326686, 5166), (333684, 5166), (316187, 5166), (442163, 5166), (494652, 5166), (417667, 5166), (386174, 5166), (302191, 5166), (333684, 5166), (302191, 5166), (470158, 5166), (554140, 5166), (620628, 5166), (536644, 5166), (501651, 5166), (554140, 5166), (512149, 5166), (536644, 5166), (501651, 5166), (477156, 5166), (386174, 4400)]
    for window,(model,jev) in zip(scope['windows'],caps):
        window.update(model_cap_micro_cny=model,jev_cap_micro_usd=jev)
    m['scope_sha256']=digest(wire(scope));f['receipt']['scope_sha256']=m['scope_sha256']
    f['review']['bindings']['scope_sha256']=m['scope_sha256'];refresh_links(f);return f


class V5CompatibilityTests(unittest.TestCase):
    setUpClass = classmethod(WindowScorePlanTests.setUpClass.__func__)
    plan = WindowScorePlanTests.plan
    reject = WindowScorePlanTests.reject

    def test_v5_exact28_and_new_caps_select_configuration_only_accounting2(self):
        try:plan=self.plan(v5_fixture())
        except ValueError as error:self.fail('Explicit epoch5 and fixed28 cache accounting must be accepted: '+str(error))
        cost=plan['provenance']['configured_cache_accounting']
        self.assertEqual(cost['schema'],'full-window-accounting.v2')
        self.assertEqual(cost['model_total_cap_micro_cny'],62000000);self.assertEqual(cost['jev_total_cap_micro_usd'],676000)
        self.assertEqual(cost['model_cache_hit_input_micro_per_million'],40000)
        self.assertEqual(cost['model_input_micro_per_million'],2000000);self.assertEqual(cost['model_output_micro_per_million'],8000000)
        self.assertEqual(cost['jev_input_micro_per_million'],42000);self.assertEqual(cost['jev_output_micro_per_million'],0)
        self.assertFalse(cost['proves_cache_usage']);self.assertFalse(cost['proves_knowledge_usage'])
        self.assertEqual(cost['cache_absent_settlement'],'all_input_at_miss_rate_without_observed_cache_split')
        self.assertEqual(len(plan['scores']),12);self.assertFalse(plan['publish_ready'])

    def test_v5_rejects_coherent_old25_wrong_claim_descriptor_caps_epoch_and_price(self):
        def old25(m):
            m['historical_claims']={key:value for key,value in m['historical_claims'].items() if not key.startswith('full1220-v4-')}
        def oldbaseline(m):
            m['scope']['baseline_evidence_sha256']=m['baseline_evidence']['sha256']='12a9d584ea9508a9fc7701532ad9214b0c743a8e13d17829432fac0d6b48a20c'
        def oldcaps(m):
            for window,old in zip(m['scope']['windows'],v4_fixture()['manifest']['scope']['windows']):
                for key in ('model_cap_micro_cny','jev_cap_micro_usd'):window[key]=old[key]
        def swapcaps(m):
            a,b=m['scope']['windows'][:2];a['model_cap_micro_cny'],b['model_cap_micro_cny']=b['model_cap_micro_cny'],a['model_cap_micro_cny']
        def oldepoch(m):m['schema']='full-window-actual-ready.v4';m['scope']['schema']='full-window-scope.v4'
        mutations=[old25,oldbaseline,oldcaps,swapcaps,oldepoch,
            lambda m:m['historical_claims'].pop('full1220-v4-20261029/window-000003'),
            lambda m:m['historical_claims'].__setitem__('full1220-v4-20261029/window-000003','0'*64),
            lambda m:m['baseline_evidence'].__setitem__('sha256','0'*64),
            lambda m:m['scope'].__setitem__('budget_approval_sha256','0'*64),
            lambda m:m['scope'].__setitem__('schema','full-window-scope.v4'),
            lambda m:m.__setitem__('schema','full-window-actual-ready.v4'),
            lambda m:m['provider_limits']['model'].__setitem__('cache_hit_input_micro_per_million',0),
            lambda m:m['provider_limits']['model'].pop('cache_hit_input_micro_per_million'),
            lambda m:m['provider_limits']['model'].__setitem__('input_micro_per_million',2000001),
            lambda m:m['provider_limits']['model'].__setitem__('output_micro_per_million',8000001),
            lambda m:m['provider_limits']['jev'].__setitem__('input_micro_per_million',42001),
            lambda m:m['provider_limits']['jev'].__setitem__('output_micro_per_million',1),
            lambda m:m['provider_limits']['jev'].__setitem__('cache_hit_input_micro_per_million',1)]
        for mutate in mutations:
            f=v5_fixture();m=f['manifest'];mutate(m);m['scope_sha256']=digest(wire(m['scope']))
            f['receipt']['scope_sha256']=m['scope_sha256'];f['review']['bindings']['scope_sha256']=m['scope_sha256'];refresh_links(f)
            with self.subTest(mutate=mutate):self.reject(f)

    def test_v5_negative_unknown_and_independently_held_pass_stay_unchanged(self):
        for quality in (False,True):
            f=v5_fixture(partial_fixture(quality));plan=self.plan(f);trace=f['receipt']['rows'][0]['trace_id']
            self.assertEqual(plan['rows'][0]['semantic_task_verdict'],'failed' if quality else 'not_assessable')
            self.assertEqual(plan['rows'][0]['strict_task_verdict'],'failed')
            self.assertFalse(any(s['value']=='passed' for s in plan['scores'] if s['traceId']==trace))
        f=v5_fixture()
        for row in f['review']['rows']:row['pass_score_publish_eligible']=False
        refresh_links(f);self.assertEqual(len(self.plan(f)['scores']),4)

    def test_v4_plan_json_ids_and_configuration_provenance_remain_byte_identical(self):
        raw=json.dumps(self.plan(v4_fixture()),sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()
        self.assertEqual(digest(raw),'66105d3489ca14dcf316ab79194642c3c374d2ce03c7c07f0229d49c178a412a')



class CompletedStateV6Tests(unittest.TestCase):
    setUpClass = classmethod(WindowScorePlanTests.setUpClass.__func__)

    def test_v6_has_separate_fixed_signature_helper(self):
        helper = SCRIPT.parent / 'helpers/full1220_attestation_v6.go.txt'
        self.assertTrue(helper.is_file(), 'v6 signature helper required')
        self.assertEqual(digest(helper.read_bytes()), self.adapter._ATTESTATION_V6_HELPER_SHA256)
        self.assertEqual(digest((SCRIPT.parent/'helpers/full1220_attestation.go.txt').read_bytes()), self.adapter._ATTESTATION_HELPER_SHA256)
        self.assertIn(b'full-window-actual-ready.v6', helper.read_bytes())

    def test_v6_fixed_configuration_and_mutations(self):
        root=Path('/Users/a1/项目/ai-gateway/.tmp')
        caps=root/'full1220-v6-design-20261029/caps.json'; claims=root/'stopped-v5-proof-work/claim32-map.json'
        if not caps.is_file() or not claims.is_file(): self.skipTest('Private v6 declaration fixture unavailable')
        m=v5_fixture()['manifest']; scope=m['scope']
        scope.update(schema='full-window-scope.v6', baseline_evidence_sha256='b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1', budget_approval_sha256='ef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce')
        m['baseline_evidence']['sha256']=scope['baseline_evidence_sha256'];m['historical_claims']=json.loads(claims.read_bytes())
        for window,cap in zip(scope['windows'],json.loads(caps.read_bytes())): window.update(cap)
        output=self.adapter._score_cache_accounting(m,'v6')
        self.assertEqual((output['model_total_cap_micro_cny'],output['jev_total_cap_micro_usd']),(85000000,600000))
        for field in ('baseline_evidence_sha256','budget_approval_sha256'):
            altered=copy.deepcopy(m);altered['scope'][field]='0'*64
            with self.assertRaises(ValueError):self.adapter._score_cache_accounting(altered,'v6')
        for mutate in (lambda x:x['historical_claims'].pop(next(iter(x['historical_claims']))), lambda x:x['scope']['windows'][0].__setitem__('model_cap_micro_cny',1), lambda x:x['provider_limits']['model'].__setitem__('input_micro_per_million',1)):
            altered=copy.deepcopy(m);mutate(altered)
            with self.assertRaises(ValueError):self.adapter._score_cache_accounting(altered,'v6')

    def test_v6_planner_binds_envelope_and_records(self):
        root=Path('/Users/a1/项目/ai-gateway/.tmp')
        caps=root/'full1220-v6-design-20261029/caps.json'; claims=root/'stopped-v5-proof-work/claim32-map.json'
        if not caps.is_file() or not claims.is_file(): self.skipTest('Private v6 declaration fixture unavailable')
        f=v5_fixture();m=f['manifest'];scope=m['scope'];m['schema']='full-window-actual-ready.v6'
        scope.update(schema='full-window-scope.v6', baseline_evidence_sha256='b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1', budget_approval_sha256='ef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce')
        m['baseline_evidence']['sha256']=scope['baseline_evidence_sha256'];m['historical_claims']=json.loads(claims.read_bytes())
        scope['bindings'].update(customer_revision='50e3c8dab14b4b6cc25322fd340a274e6970200b',policy_sha256='333c69bbb7506bd0c9237891cce8ddca720174974bf3f85753dd62339f368f15')
        for window,cap in zip(scope['windows'],json.loads(caps.read_bytes())): window.update(cap)
        envelope=wire({'basis':[{'path':'/offline/approval','sha256':scope['budget_approval_sha256']}]});m['envelope']={'path':'/offline/envelope','sha256':digest(envelope)};scope['bindings']['request_envelope_sha256']=digest(envelope)
        for name,raw in f['records'].items():
            record=json.loads(raw);record['policy_sha256']=self.adapter._COLLECTION_POLICY_SHA256;f['records'][name]=wire(record)
            for row in f['receipt']['rows']:
                if row['recording_file']==name: row['recording_sha256']=digest(f['records'][name])
            for row in f['review']['rows']:
                if row['recording_file']==name: row['recording_sha256']=digest(f['records'][name])
        m['scope_sha256']=digest(wire(scope));f['receipt']['scope_sha256']=m['scope_sha256'];f['review']['bindings']['scope_sha256']=m['scope_sha256'];refresh_links(f)
        self.assertIn('epoch6_evidence', inspect.signature(self.adapter.build_window_score_plan).parameters)
        plan=self.adapter.build_window_score_plan(*arguments(f),epoch6_evidence={'envelope_raw':envelope,'sidecars':{}})
        self.assertEqual(plan['provenance']['budget_approval_sha256'],scope['budget_approval_sha256'])
        self.assertFalse(plan['publish_ready'])
        with self.assertRaises(ValueError): self.adapter.build_window_score_plan(*arguments(f),epoch6_evidence={'envelope_raw':b'{}','sidecars':{}})
        f['manifest']['scope']['bindings']['customer_revision']='0'*40
        f['manifest']['scope_sha256']=digest(wire(f['manifest']['scope']));f['receipt']['scope_sha256']=f['manifest']['scope_sha256'];f['review']['bindings']['scope_sha256']=f['manifest']['scope_sha256'];refresh_links(f)
        with self.assertRaises(ValueError): self.adapter.build_window_score_plan(*arguments(f),epoch6_evidence={'envelope_raw':envelope,'sidecars':{}})

    def test_content_projection_forwards_v6_evidence(self):
        self.assertIn('epoch6_evidence', inspect.signature(self.adapter.project_window_content).parameters)

    def test_late_fact_mismatch_is_a_recorded_failure(self):
        self.assertTrue(hasattr(self.adapter, 'validate_completed_record_state'), 'completed state validator missing')
        record = json.loads(next(iter(fixture()['records'].values())))
        record['policy_sha256'] = self.adapter._COLLECTION_POLICY_SHA256
        record['result'].update(status='failed', structural_pass=True, success=False, errors=['false fact'])
        record['issues'] = [{'code':'fact_mismatch','turn':1,'error_index':0,'disposition':'continue_recorded'}]
        record['result_sha256'] = digest(wire(record['result']))
        self.assertEqual(self.adapter.validate_completed_record_state(wire(record)), 'recorded_negative')

    def test_all_source_quality_origins_and_bindings(self):
        for code in self.adapter._COLLECTION_QUALITY_CODES:
            record = json.loads(next(iter(fixture()['records'].values())))
            record['policy_sha256'] = self.adapter._COLLECTION_POLICY_SHA256
            record['result'].update(status='failed', structural_pass=code in ('semantic_negative','fact_mismatch'), success=False, errors=['negative'])
            record['issues'] = [{'code':code,'turn':1,'error_index':0,'disposition':'continue_recorded'}]
            record['result_sha256'] = digest(wire(record['result']))
            with self.subTest(code=code):
                self.assertEqual(self.adapter.validate_completed_record_state(wire(record)), 'recorded_negative')
                for field in ('issues','result_sha256'):
                    bad=copy.deepcopy(record);bad.pop(field)
                    with self.assertRaises(ValueError): self.adapter.validate_completed_record_state(wire(bad))
                for field,value in (('status','passed'),('success',True),('errors',[])):
                    bad=copy.deepcopy(record);bad['result'][field]=value;bad['result_sha256']=digest(wire(bad['result']))
                    with self.assertRaises(ValueError): self.adapter.validate_completed_record_state(wire(bad))

    def test_original_r2_records_replay_without_regrading(self):
        root = Path('/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v5-r2-freeze-20261029/records')
        if not root.is_dir(): self.skipTest('Original private R2 records unavailable')
        files = [p for w in ('window-000001','window-000002','window-000003') for p in sorted((root/w).glob('arm-*.json'))]
        self.assertEqual(len(files),168)
        states = [self.adapter.validate_completed_record_state(p.read_bytes()) for p in files]
        self.assertEqual(states.count('recorded_negative'),5)

    def test_malformed_empty_issue_and_error_types_reject(self):
        record=json.loads(next(iter(fixture()['records'].values())));record['policy_sha256']=self.adapter._COLLECTION_POLICY_SHA256
        for field,value in (('issues',None),('errors','')):
            bad=copy.deepcopy(record)
            if field == 'issues': bad[field]=value
            else: bad['result'][field]=value;bad['result_sha256']=digest(wire(bad['result']))
            with self.subTest(field=field), self.assertRaises(ValueError): self.adapter.validate_completed_record_state(wire(bad))

    def test_v6_quality_publication_fails_closed_before_other_gates(self):
        plan={'provenance':{'epoch':'v6'}, 'rows':[{'quality_end':{'unrun_turns':0}}]}
        with self.assertRaisesRegex(ValueError, 'quality.*proof'):
            self.adapter.publish_window_scores(plan, None, None, None, None, execute=True)

    def test_unbound_error_or_unknown_origin_rejects(self):
        self.assertTrue(hasattr(self.adapter, 'validate_completed_record_state'), 'completed state validator missing')
        record = json.loads(next(iter(fixture()['records'].values())))
        record['policy_sha256'] = self.adapter._COLLECTION_POLICY_SHA256
        record['result'].update(status='failed', structural_pass=False, success=False, errors=['unexpected'])
        record['issues'] = [{'code':'unclassified','turn':1,'error_index':0,'disposition':'halt'}]
        record['result_sha256'] = digest(wire(record['result']))
        with self.assertRaises(ValueError): self.adapter.validate_completed_record_state(wire(record))

if __name__ == '__main__': unittest.main()
