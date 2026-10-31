"""Controlled pure Python V10 score contracts; no actual proof or network IO."""
import sys
sys.dont_write_bytecode = True
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch
HERE=Path(__file__).resolve().parent

def load(name='full1220-v10-scores.py'):
    p=HERE/name
    m=types.ModuleType('controlled_v10_scores');m.__file__=str(p)
    if p.exists():exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__)
    return m

class Scores(unittest.TestCase):
    def test_v10_entry_exists_and_default_denies_before_io(self):
        m=load();self.assertTrue(callable(getattr(m,'verify_actual_score_inputs',None)), 'V10 score factory missing')
        def forbidden(*a,**kw):raise AssertionError('unauthenticated file/key/HTTP IO')
        m._regular_bytes=forbidden;m._projection_private_settings=forbidden
        self.assertEqual(m.main([]),0)
        for call in (lambda:m.verify_actual_score_inputs('/untrusted'),
                     lambda:m.build_window_score_plan(b'{}',b'{}',b'{}',{},b'{}',{},actual_proof={'verified':True}),
                     lambda:m.load_window_score_client(b'{}','/untrusted',actual_proof={'verified':True}),
                     lambda:m.project_window_content(b'{}',b'{}',b'{}',{},b'{}',{},source_root='/untrusted',output_dir='/untrusted',execute=True,actual_proof={'verified':True},private_settings_path='/untrusted')):
            with self.assertRaisesRegex(ValueError,'UNFROZEN'):call()

    def test_completion_exact_fixed_prefix_suffix_and_all_attempt_scope(self):
        m=load();self.assertTrue(callable(getattr(m,'_completion',None)), 'V10 composition validator missing')
        expected=json.loads(json.loads((HERE/'fixtures/v10-contracts/go-declarations.json').read_text())['completion']['canonical'])
        self.assertEqual(m._completion({'completion':copy.deepcopy(expected)}),expected)
        for key,value in [('prefix_score_count',2310),('prefix_positive_score_holes',0),('human_reviewed_exact_scope',True),('boundaries',0)]:
            wrong=copy.deepcopy(expected);wrong[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):m._completion({'completion':wrong})
        wrong=copy.deepcopy(expected);wrong['all_attempt']['arms']=7320
        with self.assertRaises(ValueError):m._completion({'completion':wrong})
        with self.assertRaises(ValueError):m._completion({})

    def test_fixed_capture_template_runtime_literal_and_pre_call_args(self):
        m=load();self.assertEqual(getattr(m,'_PROJECTION_HELPER_SHA256',None),'98a26427ab04797c26c88356e88800d417362e215b5fbf9c39531b2de36c41d9')
        self.assertEqual(m._PROJECTION_HELPER_RELATIVE,'internal/observability/full1220_content_projection_v8_test.go')
        raw=(HERE/'helpers/full1220_content_projection_input_v1_test.go.txt').read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(),m._PROJECTION_HELPER_SHA256)
        source=raw.decode();self.assertIn('json.Unmarshal([]byte(*callArgs),&captured)',source)

    def test_new_registry_canonical_root_and_old_scope_refusal(self):
        m=load();self.assertTrue(callable(getattr(m,'_open_publication_registry',None)))
        self.assertIn('full1220-v10-score-publications',str(m._PUBLICATION_REGISTRY_ROOT))
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve()/'registry';root.mkdir(mode=0o700)
            alias=Path(directory)/'alias';alias.symlink_to(root)
            m._PUBLICATION_REGISTRY_ROOT=alias
            with self.assertRaises(ValueError):m._open_publication_registry('a'*64,create=True)
            m._PUBLICATION_REGISTRY_ROOT=root
            with self.assertRaises(ValueError):m._open_publication_registry(m._ORIGINAL_SCOPE_SHA256,create=True)

    def test_policy_pure_vectors_equal_frozen_v9(self):
        m=load();self.assertTrue(callable(getattr(m,'_aggregate',None)))
        old=load('full1220-v9-scores.py')
        for verdicts in ([],['passed'],['failed'],['not_assessable'],['passed','failed'],['passed','not_assessable']):
            self.assertEqual(m._aggregate(verdicts),old._aggregate(verdicts))
        for text in ('a<&>\u2028', '', '中文', 'one\ntwo'):
            self.assertEqual(m._string(text),old._string(text))
        self.assertEqual(m._ACTION,old._ACTION);self.assertEqual(m._FINAL,old._FINAL)
        self.assertEqual(m.NAMESPACE,old.NAMESPACE)


# Pure controlled data; these local seals exercise the durable publication core
# only. They are deliberately never passed to an actual public proof factory.
def controlled_publication(m, root, *, score_count=2):
    trace='d'*32; scope='a'*64; manifest='b'*64
    provenance={'manifest_sha256':manifest,'scope_sha256':scope,'epoch':'v10'}
    binding={'ordinal':386,'pass':1,'case_key':'controlled/only','arm':'model','recording_sha256':'c'*64,
             'expected_defined_turns':1,'expected_observed_turns':1,'original_status':'needs_review',
             'original_structural_pass':True,'turn_metadata':[{'turn_index':1}]}
    scores=[]
    for metric in ('semantic_task','strict_task')[:score_count]:
        logical={'scope_sha256':scope,'window_id':'window-056','pass':1,'ordinal':386,'arm':'model','trace_id':trace,'metric':m.NAMESPACE+metric}
        raw=m._json_bytes(logical).decode()
        scores.append({'id':m._sha(raw.encode()),'logical_identity':raw,'traceId':trace,'name':m.NAMESPACE+metric,
            'value':'failed','dataType':'CATEGORICAL','environment':'evaluation','metadata':{
            'ordinal':386,'pass':1,'arm':'model','original_status':'needs_review','original_structural_pass':True,
            'review_sha256':'e'*64}})
    plan={'schema':'full1220-window-score-plan.v1','provenance':provenance,'window_index':1,'window_id':'window-056',
          'trace_bindings':{trace:binding},'scores':scores,'pending_gates':[]}
    job={'rows':[{'trace_id':trace,'metadata':{}}]}
    payload={'rows':[{'trace_id':trace,'turns':[{'turn_index':1,'input':'message','output':'reply','tools':[]}]}],'native_packets':[]}
    projection=m._ContentProjection(m._PROJECTION_SEAL,m._json_bytes(payload),plan,job)
    chain=m._PublicationProof(m._PUBLICATION_SEAL,'receipt_chain',{'manifest_sha256':manifest,'chain_sha256':'f'*64},plan)
    binary=root/'controlled-binary';binary.write_bytes(b'controlled-no-execution')
    attestation=m._PublicationProof(m._PUBLICATION_SEAL,'attestation',{'manifest_sha256':manifest,
        'binary_path':str(binary),'binary_sha256':m._sha(binary.read_bytes()),'attestation_sha256':'1'*64})
    class ControlledInner:
        def __init__(self):self.posts=[];self.gets=0;self.readbacks=0;self.fail_post=False;self.fail_verify=False;self.tamper=None
        def observations(self,identity):
            self.gets+=1
            return [{'id':'root','name':'evaluation.task','traceId':trace,'environment':'evaluation','metadata':{
                'status':'needs_review','structural_pass':True,'task_success':'unknown'}},
                {'id':'turn','name':'agent.turn','traceId':trace,'environment':'evaluation','metadata':{'turn_index':1},
                 'parentObservationId':'root','input':'message','output':'reply'}]
        def request(self,path,payload):
            self.posts.append(copy.deepcopy(payload))
            reg=m._PUBLICATION_REGISTRY_ROOT/scope
            assert (reg/(payload['id']+'.sent.json')).is_file(), 'sent must be durable before POST'
            assert len(list(reg.glob('logical-*.intent.json')))==score_count,'all intents before first POST'
            if self.tamper:self.tamper(reg)
            if self.fail_post:raise TimeoutError('controlled unknown result')
            return {'id':payload['id']}
        def verify_scores(self,payloads):
            self.readbacks+=1
            if self.fail_verify:raise ValueError('controlled exact metadata mismatch')
            assert payloads==self.posts
    inner=ControlledInner();client=m._WindowScoreClient(m._PUBLICATION_SEAL,inner,manifest)
    return plan,projection,chain,attestation,client,inner

class OncePublication(unittest.TestCase):
    def setUp(self):
        self.m=load();self.assertTrue(callable(getattr(self.m,'_publish_window_scores',None)))
        self.temporary=tempfile.TemporaryDirectory();self.root=Path(self.temporary.name).resolve()
        self.m._PUBLICATION_REGISTRY_ROOT=self.root/'registry'
    def tearDown(self):self.temporary.cleanup()
    def fixture(self):return controlled_publication(self.m,self.root)
    def publish(self,args):return self.m._publish_window_scores(*args[:5],execute=True)
    def test_sent_before_post_all_intents_and_one_immediate_readback(self):
        args=self.fixture();result=self.publish(args)
        self.assertTrue(result['all_verified']);self.assertEqual(len(args[-1].posts),2)
        self.assertEqual(args[-1].gets,1);self.assertEqual(args[-1].readbacks,1)
        with self.assertRaises(ValueError):self.publish(args)
        self.assertEqual(len(args[-1].posts),2)
        self.assertTrue(self.m._verify_window_scores(args[0],args[4])['all_verified'])
        self.assertEqual(len(args[-1].posts),2)
    def test_unknown_post_permanently_blocks_whole_target(self):
        args=self.fixture();args[-1].fail_post=True
        with self.assertRaisesRegex(ValueError,'uncertain'):self.publish(args)
        with self.assertRaises(ValueError):self.publish(args)
        self.assertEqual(len(args[-1].posts),1);self.assertEqual(args[-1].gets,1)
        reg=self.m._PUBLICATION_REGISTRY_ROOT/args[0]['provenance']['scope_sha256']
        self.assertEqual(len(list(reg.glob('*.sent.json'))),1)
        self.assertEqual(len(list(reg.glob('*.accepted.json'))),0)
    def test_immediate_false_preserved_verify_only_no_repost(self):
        args=self.fixture();args[-1].fail_verify=True
        result=self.publish(args);self.assertFalse(result['all_verified']);self.assertTrue(result['needs_readback'])
        with self.assertRaises(ValueError):self.publish(args)
        args[-1].fail_verify=False
        self.assertTrue(self.m._verify_window_scores(args[0],args[4])['all_verified'])
        self.assertFalse(result['all_verified']);self.assertEqual(len(args[-1].posts),2)
    def test_validation_failure_consumes_operation_before_get(self):
        args=self.fixture();args[-1].observations=lambda trace:[]
        with self.assertRaisesRegex(ValueError,'content'):self.publish(args)
        with self.assertRaises(ValueError):self.publish(args)
        self.assertEqual(len(args[-1].posts),0)
        self.assertTrue((self.m._PUBLICATION_REGISTRY_ROOT/args[0]['provenance']['scope_sha256']/'window-000001.publish-operation.json').exists())
    def test_write_fsync_failure_retained_and_no_post(self):
        args=self.fixture()
        original=self.m.os.fsync
        def fail(fd):
            if any(self.m._PUBLICATION_REGISTRY_ROOT.glob('*/window-000001.publish-operation.json')):raise OSError('controlled fsync failure')
            return original(fd)
        with patch.object(self.m.os,'fsync',side_effect=fail),self.assertRaises(ValueError):self.publish(args)
        with self.assertRaises(ValueError):self.publish(args)
        self.assertFalse(args[-1].posts)
    def test_partial_write_makes_progress_or_poison_blocks_reentry(self):
        handle=self.m._open_publication_registry('a'*64,create=True)
        writes=[];original=os.write
        def partial(fd,raw):writes.append(len(raw));return original(fd,raw[:max(1,len(raw)//2)])
        try:
            with patch.object(self.m.os,'write',side_effect=partial):self.m._publication_event(handle,'controlled.intent.json',{'controlled':'x'*80})
            self.assertTrue(writes,'event must explicitly verify full durable write progress')
            self.assertEqual(json.loads((handle.scope/'controlled.intent.json').read_bytes()),{'controlled':'x'*80})
        finally:handle.close()
    def test_mutated_intent_after_first_post_stops_before_second_post(self):
        args=self.fixture()
        def tamper(reg):
            target=next(reg.glob('logical-*.intent.json'));target.write_bytes(b'{}')
        args[-1].tamper=tamper
        with self.assertRaises(ValueError):self.publish(args)
        self.assertEqual(len(args[-1].posts),1)
    def test_changed_plan_and_score_metadata_fail_verify_without_post(self):
        args=self.fixture();self.publish(args)
        altered=copy.deepcopy(args[0]);altered['scores'][0]['metadata']['extra']='changed'
        with self.assertRaises(ValueError):self.m._verify_window_scores(altered,args[4])
        self.assertEqual(len(args[-1].posts),2)

def controlled_scope(m):
    old=json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes());scope=old.get('scope',old)
    vectors=json.loads((HERE/'fixtures/v10-contracts/go-declarations.json').read_bytes())
    scope['schema']='full-window-scope.v10';scope['windows']=scope['windows'][55:]
    for field,key in [('budget_declaration','budget'),('retry_declaration','retry'),('completion','completion')]:scope[field]=json.loads(vectors[key]['canonical'])
    scope['budget_declaration_sha256']=vectors['budget']['sha256'];scope['retry_policy_sha256']=vectors['retry']['sha256']
    scope['baseline_evidence_sha256']='2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47'
    return scope

def controlled_window(m,index=1):
    scope=controlled_scope(m);scope['bindings']['executor_revision']='a'*40
    w=scope['windows'][index-1];rows=[];records={};reviews=[]
    manifest={'schema':'full-window-actual-ready.v10','evidence_kind':'actual','scope':scope,'scope_sha256':m._sha(m._json_bytes(scope)),
        'execution_sha256':'e'*64,'archive_commit':'a'*40,'archive_tree':'b'*40,'knowledge_model':'off','knowledge_jev':'off',
        'sources':{'internal/taskbench/conversation_runner.go':'c'*64,'internal/taskbench/window_collector.go':'d'*64},
        'historical_claims':[],'envelope':{'sha256':'f'*64},'private_settings_sha256':'1'*64}
    for slot in scope['slots'][w['first_ordinal']-1:w['last_ordinal']]:
        for arm in slot['arm_order']:
            ordinal=slot['ordinal'];trace=f'{ordinal*2+(arm=="model"):032x}'
            source,scenario=slot['case_key'].split('/',1);turns=[{'expected_actions':['reply'],'allowed_tools':[]} for _ in range(slot['user_turns'])]
            case={'source_case_id':source,'scenario':{'id':scenario,'turns':turns,'full_task_metric_eligible':True},'answer_assertions':[]}
            observations=[{'turn':{'message':'controlled message','status':200,'response':{'reply':'controlled reply','trace':[],
                'pending_confirm':False,'pending_tool':'','pending_preview':'','telemetry':{'langfuse_trace_id':trace,'runtime':{'routing_mode':arm}}}},
                'state':{},'action':'reply'} for _ in turns]
            result={'case':case,'fixture':{},'initial':{},'final':{},'observations':observations,'review_requirements':[],
                'errors':[],'structural_pass':True,'success':False,'setup_error':False,'status':'needs_review'}
            record={'policy_id':'full-customer-recording-policy.v1','policy_sha256':m._COLLECTION_POLICY_SHA256,
                'result':result,'result_sha256':'','disposition':'continue_recorded','issues':[]}
            result['review_requirements']=m._requirements(m._Document(m._json_bytes(record)))
            record['result_sha256']=m._sha(m._Document(m._json_bytes(record)).fragment('result').encode())
            raw=m._json_bytes(record);name=f'arm-{ordinal:06d}-{arm}.json';records[name]=raw
            row={'ordinal':ordinal,'pass':slot['pass'],'case_key':slot['case_key'],'arm':arm,'defined_turns':slot['user_turns'],
                'observed_turns':slot['user_turns'],'attempted':True,'finished':True,'cleanup':True,'observer':True,
                'structural_pass':True,'success':False,'status':'needs_review','disposition':'continue_recorded',
                'recording_file':name,'recording_sha256':m._sha(raw),'trace_id':trace}
            rows.append(row)
            requirements=[{**r,'verdict':'not_assessable' if ordinal==w['first_ordinal'] else 'passed','reason':'controlled review'} for r in result['review_requirements']]
            semantic=m._aggregate([r['verdict'] for r in requirements])
            reviews.append({**{k:row[k] for k in ('ordinal','pass','case_key','arm','trace_id','recording_file','recording_sha256')},
                'source_case':case,'original_status':'needs_review','original_structural_pass':True,'original_success':False,'original_errors':[],
                'requirements':requirements,'task_verdict':semantic,'semantic_task_verdict':semantic,'pass_score_publish_eligible':semantic=='passed'})
    receipt={'schema':'full-window-acquisition-receipt.v10','evidence_kind':'recorded_run','scope_sha256':manifest['scope_sha256'],
        'execution_sha256':manifest['execution_sha256'],'status':'collected_window','window_index':index,'window_id':w['id'],
        'rows':rows,'planned_campaign_pairs':3275,'planned_campaign_arms':6550,'planned_campaign_turns':10504,
        'planned_current_pairs':len(rows)//2,'planned_current_arms':len(rows),'planned_current_turns':sum(r['defined_turns'] for r in rows),
        'attempted_arms':len(rows),'finished_arms':len(rows),'unrun_arms':0,'observed_turns':sum(r['observed_turns'] for r in rows),
        'ledger_sha256':'2'*64,'payload_manifest_sha256':'3'*64,'completion':scope['completion']}
    prior=[s for s in scope['slots'] if 386<=s['ordinal']<w['first_ordinal']]
    cumulative=len(prior)*2+len(rows);observed=sum(s['user_turns']*2 for s in prior)+receipt['observed_turns']
    summary={'evidence_kind':'recorded_run','planned_pairs':3275,'planned_arms':6550,'defined_turns':10504,'boundaries_excluded':0,
        'window_index':index,'report_sha256':m._sha(m._json_bytes(receipt)),'report_status':'collected_window',
        'ledger_sha256':receipt['ledger_sha256'],'payload_manifest_sha256':receipt['payload_manifest_sha256'],
        'attempted_arms':cumulative,'finished_arms':cumulative,'failed_attempted_arms':0,'unattempted_arms':6550-cumulative,
        'observed_turns':observed,'quality_end_unrun_turns':0,'global_unrun_turns':10504-observed,'completion':scope['completion']}
    review={'schema':'full1220-window-independent-review.v1','method':'independent_agent_evidence_review','reviewer':'controlled-only',
        'rows':reviews,'bindings':{'manifest_sha256':m._sha(m._json_bytes(manifest)),'scope_sha256':manifest['scope_sha256'],
        'execution_sha256':manifest['execution_sha256'],'receipt_sha256':summary['report_sha256'],
        'archive_commit':manifest['archive_commit'],'archive_tree':manifest['archive_tree']}}
    raw=[m._json_bytes(x) for x in (manifest,summary,receipt)]+[records,m._json_bytes(review)]
    pins={key+'_sha256':m._sha(value) for key,value in zip(('manifest','summary','receipt','review'),raw[:3]+[raw[4]])}
    pins['records']={k:m._sha(v) for k,v in records.items()}
    return raw+[pins]

class CompositionPlan(unittest.TestCase):
    def test_first_suffix_data_plan_carries_full_and_execution_denominators(self):
        m=load();args=controlled_window(m);plan=m._build(*args,'4'*64)
        self.assertEqual(plan['window_id'],'window-056');self.assertEqual(plan['window_index'],1)
        self.assertEqual(plan['denominators']['campaign']['arms'],7320)
        self.assertEqual(plan['denominators']['execution']['arms'],6550)
        self.assertEqual(plan['denominators']['all_attempt']['arms'],7330)
        self.assertEqual(plan['denominators']['stopped_overhead']['scores_held'],True)
        self.assertEqual(plan['cumulative_turn_validation']['status'],'exact_current_window')
        self.assertEqual(plan['scores_published'],0);self.assertFalse(plan['publish_ready'])
        self.assertTrue(all(s['metadata']['ordinal']>=386 for s in plan['scores']))
        self.assertTrue(any(s['value']=='not_assessable' for s in plan['scores']))
        self.assertTrue(all(s['metadata']['metadata_scope']['prefix_scores_reference_only'] for s in plan['scores']))
        # Pure planning never manufactures the actual public process-local gate.
        m.FROZEN_ROOT='/controlled-v10';m.FROZEN_BINDING_SHA256='4'*64;m.FROZEN_VERIFIER_SHA256='5'*64
        with self.assertRaisesRegex(ValueError,'Opaque'):m.build_window_score_plan(*args,actual_proof={'all_pass':True})
    def test_last_suffix_data_plan_local456_original511_no457(self):
        m=load();args=controlled_window(m,456);plan=m._build(*args,'4'*64)
        self.assertEqual(plan['window_id'],'window-511');self.assertEqual(plan['rows'][-1]['ordinal'],3660)
        summary=json.loads(args[1]);self.assertEqual(summary['attempted_arms'],6550);self.assertEqual(summary['global_unrun_turns'],0)
        manifest=json.loads(args[0]);receipt=json.loads(args[2]);receipt['window_index']=457
        with self.assertRaises(ValueError):m._planned_rows(manifest,receipt)
    def test_old_schema_and_old_full_totals_or_prefix_map_rejected(self):
        m=load();args=controlled_window(m);manifest=json.loads(args[0]);receipt=json.loads(args[2])
        for mutate in (lambda r:r.__setitem__('planned_campaign_pairs',3660),lambda r:r.__setitem__('window_id','window-001'),
                       lambda r:r.__setitem__('window_index',True)):
            wrong=copy.deepcopy(receipt);mutate(wrong)
            with self.assertRaises(ValueError):m._planned_rows(manifest,wrong)
        manifest['schema']='full-window-actual-ready.v9';args[0]=m._json_bytes(manifest);args[-1]['manifest_sha256']=m._sha(args[0])
        with self.assertRaisesRegex(ValueError,'V10'):m._build(*args,'4'*64)
    def test_capture_reservation_new_namespace_one_target_no_alias(self):
        m=load();self.assertTrue(callable(getattr(m,'_reserve_capture',None)), 'V10 exclusive capture operation missing')
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve();m._PUBLICATION_REGISTRY_ROOT=root/'registry'
            output=root/'score-runs/window-000001/capture';output.parent.mkdir(parents=True,mode=0o700)
            plan={'provenance':{'scope_sha256':'a'*64,'manifest_sha256':'b'*64},'window_index':1,'window_id':'window-056'}
            m._reserve_capture(root,plan,output)
            with self.assertRaises((ValueError,FileExistsError)):m._reserve_capture(root,plan,output)
            with self.assertRaises(ValueError):m._reserve_capture(root,plan,root/'alternate')

class PreservedContentPolicy(unittest.TestCase):
    def test_original_requirements_hashes_and_state_match_pure_frozen_vectors(self):
        m=load();old=load('full1220-v9-scores.py');args=controlled_window(m)
        for raw in args[3].values():
            self.assertEqual(m._requirements(m._Document(raw)),old._requirements(old._Document(raw)))
            self.assertEqual(m.validate_completed_record_state(raw),old.validate_completed_record_state(raw))
    def test_original_negative_semantic_pass_remains_failed_and_positive_suppressed(self):
        m=load();args=controlled_window(m);receipt=json.loads(args[2]);summary=json.loads(args[1]);review=json.loads(args[4])
        row=receipt['rows'][0];name=row['recording_file'];record=json.loads(args[3][name]);result=record['result']
        result.update(status='failed',structural_pass=False,success=False,errors=['controlled unexpected action'])
        record['issues']=[{'code':'unexpected_action','turn':1,'error_index':0,'disposition':'continue_recorded'}]
        record['result_sha256']=m._sha(m._Document(m._json_bytes(record)).fragment('result').encode())
        raw=m._json_bytes(record);args[3][name]=raw;row.update(status='failed',structural_pass=False,recording_sha256=m._sha(raw))
        r=review['rows'][0];r.update(original_status='failed',original_structural_pass=False,original_errors=result['errors'],
            recording_sha256=m._sha(raw),task_verdict='failed',semantic_task_verdict='passed',pass_score_publish_eligible=False)
        for requirement in r['requirements']:requirement['verdict']='passed'
        args[2]=m._json_bytes(receipt);summary['report_sha256']=m._sha(args[2]);args[1]=m._json_bytes(summary)
        review['bindings']['receipt_sha256']=m._sha(args[2]);args[4]=m._json_bytes(review)
        for key,value in zip(('manifest','summary','receipt','review'),args[:3]+[args[4]]):args[-1][key+'_sha256']=m._sha(value)
        args[-1]['records'][name]=m._sha(raw)
        plan=m._build(*args,'4'*64);out=plan['rows'][0]
        self.assertEqual(out['strict_task_verdict'],'failed');self.assertEqual(out['semantic_task_verdict'],'passed')
        self.assertTrue(out['positive_score_suppressed'])
        scores=[s for s in plan['scores'] if s['traceId']==row['trace_id']]
        self.assertEqual([s['value'] for s in scores],['failed','failed'])
    def test_preview_observations_match_precall_values_and_read_execute_match_resolved(self):
        m=load();turns=[{'turn_index':1,'tools':[{'name':'tool.preview.change_plan','phase':'preview',
             'input':{'name':'original name'},'output':'preview'},
             {'name':'tool.change_plan','phase':'execute','input':{'plan_id':7},'output':'done'}]}]
        rows=[{'id':'turn','name':'agent.turn','metadata':{'turn_index':1}},
              {'id':'preview','name':'tool.preview.change_plan','parentObservationId':'turn','metadata':{'phase':'preview'},'input':{'name':'original name'},'output':'preview'},
              {'id':'execute','name':'tool.change_plan','parentObservationId':'turn','metadata':{'phase':'execute'},'input':{'plan_id':7},'output':'done'}]
        self.assertTrue(m.validate_tool_observations(rows,turns))
        wrong=copy.deepcopy(rows);wrong[1]['input']={'plan_id':7}
        with self.assertRaises(ValueError):m.validate_tool_observations(wrong,turns)
        wrong=copy.deepcopy(rows);wrong[2]['metadata']['phase']='preview'
        with self.assertRaises(ValueError):m.validate_tool_observations(wrong,turns)
        wrong=copy.deepcopy(rows);wrong[1]['input']=None
        self.assertFalse(m.validate_tool_observations(wrong,turns))
    def test_capture_off_rejects_synthetic_memory_settings_without_creating_keyfile(self):
        m=load();raw=b'{"observer":{"enabled":true,"capture_content":false,"environment":"evaluation","release":"commit","hash_key":"synthetic-inline-key-only-1234567890"}}'
        class MemoryPath:
            def __init__(self,*a):pass
            def lstat(self):return types.SimpleNamespace(st_uid=os.geteuid(),st_mode=0o100600)
        with patch.object(m,'Path',MemoryPath),patch.object(m,'_regular_bytes',return_value=raw):
            with self.assertRaisesRegex(ValueError,'capture switches'):
                m._projection_private_settings({'private_settings_sha256':m._sha(raw),'archive_commit':'commit'},'memory-only')
    def test_native_metadata_only_and_strict_direct_logical_parent(self):
        m=load();packet={'sequence':1,'identity':{'native_attempt':1,'provider':'model','model':'deepseek-flash','case_key':'controlled/only',
            'pass':1,'arm':'model','turn':1,'attempt':1,'body_sha256':'a'*64,'bytes':42},'claim_sha256':'c'*64,
            'response':{'complete':True,'available':True,'status':200}}
        row={'id':'native','name':'window.native','parentObservationId':'logical','metadata':m.native_metadata(packet)}
        parents=[{'id':'turn','name':'agent.turn','metadata':{'turn_index':1}},
                 {'id':'logical','name':'llm.attempt','parentObservationId':'turn'}]
        self.assertEqual(m.validate_native_observations([row]+parents,[packet]),1)
        for mutate in (lambda r:r.__setitem__('input','leak'),lambda r:r.__setitem__('output','leak'),
                       lambda r:r.__setitem__('parentObservationId','turn'),lambda r:r['metadata'].__setitem__('native_sequence',True)):
            wrong=copy.deepcopy(row);mutate(wrong)
            with self.assertRaises(ValueError):m.validate_native_observations([wrong]+parents,[packet])
    def test_publication_rejects_foreign_or_plain_projection_before_get_post(self):
        m=load()
        with tempfile.TemporaryDirectory() as directory:
            args=controlled_publication(m,Path(directory).resolve());m._PUBLICATION_REGISTRY_ROOT=Path(directory).resolve()/'registry'
            with self.assertRaises(ValueError):m._publish_window_scores(args[0],{'verified':True},*args[2:5],execute=True)
            self.assertEqual(args[-1].gets,0);self.assertEqual(args[-1].posts,[])
            changed=copy.deepcopy(args[0]);changed['scores'][0]['value']='passed'
            with self.assertRaises(ValueError):m._publish_window_scores(changed,*args[1:5],execute=True)
            self.assertEqual(args[-1].gets,0)

class ImmediateFalse(unittest.TestCase):
    def test_explicit_false_readback_stays_false_and_verify_only_later(self):
        m=load()
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve();m._PUBLICATION_REGISTRY_ROOT=root/'registry';args=controlled_publication(m,root)
            original=args[-1].verify_scores;args[-1].verify_scores=lambda scores:False
            result=m._publish_window_scores(*args[:5],execute=True)
            self.assertFalse(result['all_verified'],'explicit false cannot become verified success')
            self.assertFalse(m._verify_window_scores(args[0],args[4])['all_verified'])
            args[-1].verify_scores=original
            self.assertTrue(m._verify_window_scores(args[0],args[4])['all_verified'])
            self.assertFalse(result['all_verified']);self.assertEqual(len(args[-1].posts),2)

if __name__=='__main__':unittest.main()
