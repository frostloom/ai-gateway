"""Offline full-window evidence binding tests; no services or credentials."""
import copy
import hashlib
import importlib.util
import json
import unittest
import tempfile
import subprocess
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


if __name__ == '__main__': unittest.main()
