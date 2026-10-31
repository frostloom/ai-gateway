"""Controlled declaration/proof boundaries, not actual collection or authority."""
import copy
import hashlib
import json
from pathlib import Path
import types
import unittest

HERE=Path(__file__).resolve().parent

def load():
    path=HERE/'full1220-v10-window.py'
    if not path.exists():return None
    m=types.ModuleType('v10_window_unit');m.__file__=str(path)
    exec(compile(path.read_bytes(),str(path),'exec'),m.__dict__);return m

class Window(unittest.TestCase):
    def setUp(self):
        self.m=load();self.assertIsNotNone(self.m,'V10 window consumer is missing')
        self.n=self.m.dependency('native')
        old=json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes())
        self.scope=old.get('scope',old)
        vectors=json.loads((HERE/'fixtures/v10-contracts/go-declarations.json').read_bytes())
        self.scope['schema']='full-window-scope.v10'
        self.scope['baseline_evidence_sha256']=self.m.BASELINE
        self.scope['windows']=self.scope['windows'][55:]
        for field,key in [('budget_declaration','budget'),('retry_declaration','retry'),('completion','completion')]:
            self.scope[field]=json.loads(vectors[key]['canonical'])
        self.scope['budget_declaration_sha256']=vectors['budget']['sha256']
        self.scope['retry_policy_sha256']=vectors['retry']['sha256']
    def check(self,scope=None):
        scope=self.scope if scope is None else scope
        self.m._scope(scope,self.n.wire(scope),self.n)
    def test_exact_suffix_preserves_complete_slots(self):
        self.check();self.assertEqual(len(self.scope['slots']),3660)
        self.assertEqual(self.m.execution_window_at(self.scope,1)['id'],'window-056')
        self.assertEqual(self.m.execution_window_at(self.scope,1)['first_ordinal'],386)
        self.assertEqual(self.m.execution_window_at(self.scope,456)['id'],'window-511')
        self.assertEqual(self.m.execution_window_at(self.scope,456)['last_ordinal'],3660)
        for index in (0,457,True):
            with self.assertRaises(ValueError):self.m.execution_window_at(self.scope,index)
    def test_prefix_and_shifted_range_rejected(self):
        for mutate in (lambda s:s['windows'].__setitem__(0,{**s['windows'][0],'id':'window-001'}),
                       lambda s:s['windows'][0].__setitem__('first_ordinal',1),
                       lambda s:s['slots'][0].__setitem__('case_key','foreign'),
                       lambda s:s['windows'][0].__setitem__('model_cap_micro_cny',1),
                       lambda s:s['completion']['stopped_overhead'].__setitem__('scores_held',False),
                       lambda s:s.__setitem__('schema','full-window-scope.v9')):
            s=copy.deepcopy(self.scope);mutate(s)
            with self.assertRaises(ValueError):self.check(s)
    def test_default_unfrozen_zero_path_access(self):
        def forbidden(*a,**k):raise AssertionError('path/helper/settings accessed')
        self.m.Path=forbidden;self.m.Evidence=forbidden
        self.assertEqual(self.m.main([]),0)
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):self.m.verify_actual(object())
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):
            self.m.verify_source_v10(b'',b'',{},{},{},source_root=object(),source_hashes=object(),output_dir=object(),execute=True)
    def test_json_cannot_mint_opaque_proofs(self):
        for fn,args in ((self.m._SourceProof,(object(),{})),(self.m._ActualContext,(object(),None,None,None,None,None)),
                        (self.m._VerifiedWindow,(object(),{},{},None)),(self.m.check_verified_window,({'verified':True},{}))):
            with self.assertRaises(ValueError):fn(*args)
    def test_102_holds_and_parent_are_separate(self):
        self.assertEqual(self.m._permanent(self.n.amounts(1,2),[],self.n),self.n.amounts(66472062,631116))
        self.assertEqual(self.scope['budget_declaration']['parent'],self.n.amounts(185000000,3000000))
    def test_final456_token_is_checkpoint_no_claim457(self):
        raw=b'controlled-final-checkpoint';digest=hashlib.sha256(raw).hexdigest()
        self.m._verify_advance_token(456,digest,['c'*64]*456,raw)
        for claims,token in ((['c'*64]*457,digest),(['c'*64]*456,''),(['c'*64]*456,'c'*64)):
            with self.assertRaises(ValueError):self.m._verify_advance_token(456,token,claims,raw)
        self.m._verify_advance_token(1,'b'*64,['a'*64,'b'*64],b'controlled-cp')
    def test_quality_empty_never_loads_runtime(self):
        def forbidden(*a,**k):raise AssertionError('zero quality IO')
        self.m.dependency=forbidden;self.m.Evidence=forbidden
        self.assertIsNone(self.m.verify_quality_v10(b'bad',b'{"rows":[]}',{},{},{},source_root=object(),source_hashes=object(),output_dir=object()))

    def summaries(self,count):
        # Controlled JSON contract vectors only; no records/proofs/real claims.
        receipts=[];summaries=[];arms=observed=0;prior=''
        runtime={k:0 for k in ('logical_model_calls','logical_jev_calls','native_model_intents','native_jev_intents',
                'model_retry_intents','jev_retry_intents','native_settlements','native_unknown','native_pending')}
        runtime.update({k:self.n.amounts(0,0) for k in ('configured_known','unknown_held','pending_held')})
        runtime.update(unknown_intents=[],total_permanent=self.n.amounts(66472061,631114))
        for index in range(1,count+1):
            w=self.scope['windows'][index-1];done=index==456;nextordinal=w['last_ordinal']+1
            a=2*(w['last_ordinal']-w['first_ordinal']+1)
            t=sum(2*x['user_turns'] for x in self.scope['slots'][w['first_ordinal']-1:w['last_ordinal']])
            arms+=a;observed+=t
            r={'schema':'full-window-acquisition-receipt.v10','evidence_kind':'recorded_run','status':'collected_window',
               'window_index':index,'window_id':w['id'],'completion':self.scope['completion'],
               'advanced':True,'cleanup':True,'environment_cleanup':True,'observer':True,'payload_verified':True,
               'attempted_arms':a,'finished_arms':a,'unrun_arms':0,'observed_turns':t,
               'accounting':{'model_intents':0,'jev_intents':0},'v8':copy.deepcopy(runtime),
               'payload_manifest_sha256':'a'*64,'ledger_sha256':'b'*64}
            rr=self.n.wire(r);receipts.append(rr)
            summary={'completion':self.scope['completion'],'v8':copy.deepcopy(runtime),'evidence_kind':'recorded_run',
               'acquisition_done':done,'closed':done,'next_window_index':0 if done else index+1,
               'last_receipt_sha256':prior,'planned_pairs':3275,'planned_arms':6550,'defined_turns':10504,'boundaries_excluded':0,
               'attempted_arms':arms,'finished_arms':arms,'failed_attempted_arms':0,'unattempted_arms':6550-arms,
               'observed_turns':observed,'quality_end_unrun_turns':0,'global_unrun_turns':10504-observed,
               'model_attempts':0,'jev_attempts':0,'window_index':index,'report_sha256':self.m.sha(rr),
               'payload_manifest_sha256':'a'*64,'ledger_sha256':'b'*64,
               'first_unrun_ordinal':0 if done else nextordinal,
               'first_unrun_arm':'' if done else self.scope['slots'][nextordinal-1]['arm_order'][0],
               'future_unrun_from_ordinal':0 if done else nextordinal,'report_status':'collected_window'}
            sr=self.n.wire(summary);summaries.append(sr);prior=self.m.sha(sr)
        return summaries,receipts
    def test_summary_primary_suffix_counts_and_final456_closed_no457(self):
        summaries,receipts=self.summaries(456)
        result=self.m.verify_summary_chain(summaries,receipts,self.scope,self.m.sha(summaries[-1]))
        final=json.loads(summaries[-1]);self.assertEqual(final['attempted_arms'],6550)
        self.assertEqual(final['observed_turns'],10504);self.assertEqual(final['boundaries_excluded'],0)
        self.assertTrue(final['acquisition_done']);self.assertEqual(final['next_window_index'],0)
        self.assertEqual(final['completion']['primary']['arms'],7320)
        self.assertEqual(final['completion']['all_attempt']['arms'],7330)
        self.assertEqual(result['total_permanent'],self.n.amounts(66472061,631114))
        with self.assertRaises(ValueError):self.m.verify_summary_chain(summaries+[summaries[-1]],receipts+[receipts[-1]],self.scope,self.m.sha(summaries[-1]))
    def test_summary_old_schema_stopped_wrong_local_declaration_or_partition_rejects(self):
        for field,value in [('schema','full-window-acquisition-receipt.v8'),('advanced',False),('window_id','window-001'),('window_index',True)]:
            summaries,receipts=self.summaries(1);r=json.loads(receipts[0]);r[field]=value;receipts[0]=self.n.wire(r)
            summary=json.loads(summaries[0]);summary['report_sha256']=self.m.sha(receipts[0]);summaries[0]=self.n.wire(summary)
            with self.subTest(field=field),self.assertRaises(ValueError):
                self.m.verify_summary_chain(summaries,receipts,self.scope,self.m.sha(summaries[-1]))
        for field,value in [('planned_arms',7320),('defined_turns',11772),('boundaries_excluded',1851),('last_receipt_sha256','f'*64)]:
            summaries,receipts=self.summaries(1);summary=json.loads(summaries[0]);summary[field]=value;summaries[0]=self.n.wire(summary)
            with self.subTest(field=field),self.assertRaises(ValueError):self.m.verify_summary_chain(summaries,receipts,self.scope,self.m.sha(summaries[-1]))

    def test_only_exact_existing_capture_and_codec_artifacts_are_allowed(self):
        for name in ['eval/behavior-review/fixtures/v10-contracts/go-declarations.json', 'eval/behavior-review/fixtures/v10-contracts/go-delegated-authority.json', 'eval/behavior-review/full1220-capture-input-operator.py', 'eval/behavior-review/full1220-capture-input-v1.py', 'eval/behavior-review/helpers/full1220_content_projection_input_v1_test.go.txt', 'eval/behavior-review/test_full1220_capture_input_operator.py', 'eval/behavior-review/test_full1220_capture_input_projection.py', 'eval/behavior-review/test_full1220_capture_input_v1.py']:
            self.assertTrue(self.m._executor_only_path(name),name)
        for name in ("eval/behavior-review/fixtures/v10-contracts/customer.json","eval/behavior-review/full1220-capture-input-v2.py","eval/behavior-review/helpers/full1220_content_projection_unknown_test.go.txt","internal/agent/llm.go"):
            self.assertFalse(self.m._executor_only_path(name),name)

    def test_equivalence_exact_diff_rejects_customer_change_and_unlisted_or_stale_change(self):
        # Toy source maps isolate the data comparator, never actual source proof.
        old={'internal/agent/llm.go':'a'*64,'internal/taskbench/window_actual_v10.go':'b'*64}
        new={**old,'internal/taskbench/window_actual_v10.go':'c'*64}
        digest=self.m.sha(self.n.wire(dict(sorted(old.items()))))
        manifest={'sources':new,'scope':{'completion':{'original_sources_sha256':digest}}}
        e={'schema':'full-window-executor-equivalence.v10','original_sources_sha256':digest,
           'sources_sha256':self.m.sha(self.n.wire(dict(sorted(new.items())))),
           'original_sources':old,'changed_files':{'internal/taskbench/window_actual_v10.go':{'before':'b'*64,'after':'c'*64}}}
        self.m._validate_executor_equivalence(manifest,e,self.n)
        for change in ({}, {'internal/taskbench/window_actual_v10.go':{'before':'b'*64,'after':'d'*64}},
                       {**e['changed_files'],'internal/agent/llm.go':{'before':'a'*64,'after':'a'*64}}):
            with self.assertRaises(ValueError):self.m._validate_executor_equivalence(manifest,{**e,'changed_files':change},self.n)
        new['internal/agent/llm.go']='d'*64
        e['sources_sha256']=self.m.sha(self.n.wire(dict(sorted(new.items()))))
        e['changed_files']['internal/agent/llm.go']={'before':'a'*64,'after':'d'*64}
        with self.assertRaises(ValueError):self.m._validate_executor_equivalence(manifest,e,self.n)
    def test_source_and_window_types_cannot_cross_module_identity_or_raw_bindings(self):
        other=load();binding={'manifest_sha256':'a'*64,'receipt_sha256':'b'*64,'records':{},'sidecars':{}}
        # Direct private tokens here are unit fixtures, explicitly NOT genuine evidence.
        foreign=other._SourceProof(other._SOURCE_SEAL,{'inputs':binding})
        with self.assertRaises(ValueError):self.m._check_source_proof(foreign,binding)
        local=self.m._SourceProof(self.m._SOURCE_SEAL,{'inputs':binding})
        self.m._check_source_proof(local,binding)
        with self.assertRaises(ValueError):self.m._check_source_proof(local,{**binding,'receipt_sha256':'c'*64})
        with self.assertRaises(AttributeError):local._raw=b'{}'
    def test_old_manifest_rejected_before_any_raw_claim_or_record_use(self):
        scope=self.n.wire(self.scope);manifest=self.n.wire({'schema':'full-window-actual-ready.v9','evidence_kind':'local_stub'})
        files={'scope.json':scope,'synthetic-manifest.json':manifest}
        with self.assertRaisesRegex(ValueError,'manifest mode'):
            self.m.replay_completed_window(files,{k:self.m.sha(v) for k,v in files.items()},1)

if __name__=='__main__':unittest.main()
