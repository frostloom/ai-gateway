"""Synthetic V13 protocol boundaries only; no helpers or paid proof are run."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import types
import unittest

HERE=Path(__file__).resolve().parents[1]

def load(name):
    path=HERE/('full1220-v13-'+name+'.py')
    if not path.exists():raise AssertionError('V13 '+name+' implementation missing')
    module=types.ModuleType('test_v13_'+name.replace('-','_'));module.__file__=str(path)
    sys.modules[module.__name__]=module
    exec(compile(path.read_bytes(),str(path),'exec'),module.__dict__)
    return module

def declarations():return json.loads((HERE/'fixtures/v13-contracts/go-declarations.json').read_bytes())

def scope(m):
    s=json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes());v=declarations()
    s.update(schema='full-window-scope.v13',baseline_evidence_sha256=m.BASELINE,collection_policy_sha256=v['collection_policy_sha256'])
    s['bindings']['policy_sha256']=v['owned_policy_sha256']
    s['budget_declaration']=json.loads(v['budget']['canonical']);s['budget_declaration_sha256']=v['budget']['sha256']
    s['retry_declaration']=json.loads(v['retry']['canonical']);s['retry_policy_sha256']=v['retry']['sha256']
    s['completion_v13']=json.loads(v['completion']['canonical']);s['windows']=s['windows'][55:]
    s['observation']={'schema':'full-window-observation-declaration.v11','private_settings_sha256':'a'*64,'spool_root':'/controlled/spool','spool_run_id':'b'*64,'spool_max_bytes':4<<30,'observation_receipt_boundary':'durable_local','observer_identity_sha256':'c'*64}
    return s

class Contracts(unittest.TestCase):
    def test_exact_go_signing_vector_is_data_only(self):
        m=load('contracts');v=json.loads((HERE/'fixtures/v13-contracts/go-authority-data.json').read_bytes())
        self.assertIs(v['data_only'],True)
        out=m.authorization_signing_bytes(json.dumps(v['authorization']).encode())
        self.assertEqual(out,v['signing_bytes'].encode());self.assertEqual(hashlib.sha256(out).hexdigest(),v['signing_sha256'])
        self.assertLess(out.index(b'collection_policy_sha256'),out.index(b'completion_sha256'))
    def test_wrong_policy_epoch_history_and_extra_fields_rejected(self):
        m=load('contracts');v=json.loads((HERE/'fixtures/v13-contracts/go-authority-data.json').read_bytes())['authorization']
        for mutate in (lambda x:x.update(collection_policy_sha256='a'*64),lambda x:x.pop('collection_policy_sha256'),lambda x:x.update(schema='full-window-delegated-execution-authorization.v12'),lambda x:x.update(budget_declaration_sha256='047ba0eb6c92ced4c2aeaee842c0e03a6afcdb246277dd6a4bff3149526cedb0'),lambda x:x.update(preview_failures=[])):
            x=copy.deepcopy(v);mutate(x)
            with self.subTest(x=x),self.assertRaises(ValueError):m.authorization_signing_bytes(json.dumps(x).encode())

class Window(unittest.TestCase):
    def setUp(self):self.m=load('window');self.n=self.m.dependency('native')
    def test_exact_scope_and_all_original_denominators(self):
        s=scope(self.m);self.m._scope(s,self.n.wire(s),self.n)
        self.assertEqual(self.m.execution_totals(s),{'Pairs':3275,'Arms':6550,'DefinedUserTurns':10504,'Boundaries':0,'ModelCap':110191257,'JEVCap':1818872})
        self.assertEqual((self.m.HISTORICAL_MODEL,self.m.HISTORICAL_JEV),(79974331,845904))
        for i in (1,55,56,456):self.assertEqual(self.m.execution_window_at(s,i)['id'],f'window-{i+55:03d}')
    def test_missing_wrong_policy_old_completion_rejected(self):
        for mutate in (lambda s:s.pop('collection_policy_sha256'),lambda s:s.update(collection_policy_sha256='a'*64),lambda s:s['bindings'].update(policy_sha256='a'*64),lambda s:s.update(completion_v12={}),lambda s:s.update(schema='full-window-scope.v12')):
            s=scope(self.m);mutate(s)
            with self.assertRaises((ValueError,KeyError)):self.m._scope(s,self.n.wire(s),self.n)
    def test_unfrozen_and_foreign_modules_never_proof(self):
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):self.m.verify_actual_cohort(object())
        for x in ({'all_verified':True},None,object()):
            with self.assertRaises(ValueError):self.m.check_verified_cohort(x)
            with self.assertRaises(ValueError):self.m.require_quality([{'preview_failure':{}}],x,{})
    def test_sidecar_names_and_hash_join_without_sql_interpretation(self):
        raw=b'private SQL capture, parsed only by genuine Go helper'
        row={'ordinal':393,'arm':'jev_selective','recording_sha256':'b'*64,'preview_failure':{'policy_sha256':self.m.OWNED_POLICY_SHA256,'recording_sha256':'b'*64,'sidecar_file':'preview-failure-000393-jev_selective.json','sidecar_sha256':self.m.sha(raw)}}
        self.assertEqual(self.m._sidecar_names([row]),{row['preview_failure']['sidecar_file']:self.m.sha(raw)})
        for mutate in (lambda r:r['preview_failure'].update(recording_sha256='a'*64),lambda r:r['preview_failure'].update(policy_sha256='a'*64),lambda r:r['preview_failure'].update(sidecar_file='../private.json'),lambda r:r.update(quality_end={}),lambda r:r.pop('preview_failure')):
            r=copy.deepcopy(row);mutate(r)
            with self.assertRaises((ValueError,KeyError)):self.m._sidecar_names([r])
    def test_all_normal_rows_require_genuine_source_replay(self):
        for row in ({},{'preview_failure':{}},{'quality_end':{}}):
            with self.assertRaises(ValueError):self.m.require_quality([row])


class RecordingAndCheckpoint(unittest.TestCase):
    def fixture(self):
        m=load('window');n=m.dependency('native');s=scope(m);slot=s['slots'][385];s['windows']=[dict(s['windows'][0],last_ordinal=386)]
        rows=[];files={'records/receipt.json':b'{}','records/payload-manifest.json':b'{}'}
        for pos,arm in enumerate(slot['arm_order'],1):
            trace=f'{pos:032x}';tm={'langfuse_trace_id':trace,'llm_calls':1,'jev_calls':0,'reply_repairs':0,'total_latency_ms':1,'runtime':{'model':'deepseek-flash','protocol':'anthropic','routing_mode':arm,'max_output_tokens':128,'thinking_mode':'disabled','reply_repair_limit':1}}
            result={'case':{'source_case_id':slot['case_key'].split('/')[0],'scenario':{'id':slot['case_key'].split('/')[1],'turns':[{}]*slot['user_turns']}},'observations':[{'turn':{'status':200,'latency_ms':1,'response':{'telemetry':tm}}} for _ in range(slot['user_turns'])],'status':'failed','structural_pass':False,'success':False,'errors':['original action negative'],'setup_error':False,'http_failures':0,'review_requirements':[],'duration_ms':2}
            record={'policy_id':'full-customer-recording-preview-failure.v2','policy_sha256':m.COLLECTION_POLICY_SHA256,'result_sha256':m.sha(n.wire(result)),'disposition':'continue_recorded','issues':[{'code':'unexpected_action','turn':1,'error_index':0,'disposition':'continue_recorded'}],'result':result}
            raw=n.wire(record);name=f'arm-000386-{arm}.json';sidecar=f'preview-failure-000386-{arm}.json';private=b'UNIT private SQL payload, never a normal record'
            row={k:slot[k] for k in ('ordinal','pass','case_key')};row.update(arm=arm,attempted=True,finished=True,cleanup=True,observer=True,defined_turns=slot['user_turns'],observed_turns=slot['user_turns'],trace_id=trace,recording_file=name,recording_sha256=m.sha(raw),status='failed',structural_pass=False,success=False,disposition='continue_recorded',duration_ms=3,preview_failure={'policy_sha256':m.OWNED_POLICY_SHA256,'recording_sha256':m.sha(raw),'sidecar_file':sidecar,'sidecar_sha256':m.sha(private)})
            rows.append(row);files['records/'+name]=raw;files['records/'+sidecar]=private
        receipt={'rows':rows,'quality_negative_arms':2,'observed_turns':2*slot['user_turns'],'planned_current_turns':2*slot['user_turns']}
        return m,n,s,receipt,files
    def test_private_sidecar_digest_is_joined_to_checkpoint_evidence(self):
        from unittest.mock import patch
        m,n,s,r,files=self.fixture();calls=[]
        # UNIT seam only: SQL Go replay proof is mocked, never minted here.
        with patch.object(m,'_check_source_proof',side_effect=lambda proof,binding:calls.append((proof,binding))):
            e,_,_=m._rows(files,r,s,1,n,m.dependency('source'),object(),{'unit_only':True})
        self.assertEqual(len(calls),1)
        self.assertEqual([x['PreviewFailure'] for x in e],[x['preview_failure'] for x in r['rows']])
        self.assertEqual(r['quality_negative_arms'],2)
        self.assertNotIn(b'private SQL',n.wire(e))
    def test_replaced_missing_or_extra_sidecars_fail_before_checkpoint(self):
        from unittest.mock import patch
        for change in ('bytes','missing','extra','recordref','both'):
            m,n,s,r,files=self.fixture();row=r['rows'][0];name='records/'+row['preview_failure']['sidecar_file']
            if change=='bytes':files[name]+=b'changed'
            elif change=='missing':del files[name]
            elif change=='extra':files['records/orphan-private.json']=b'private'
            elif change=='recordref':row['preview_failure']['recording_sha256']='a'*64
            else:row['quality_end']={}
            with self.subTest(change=change),patch.object(m,'_check_source_proof',lambda *args:None),self.assertRaises((ValueError,KeyError)):
                m._rows(files,r,s,1,n,m.dependency('source'),object(),{})
    def test_original_negatives_and_private_record_boundary_preserved(self):
        m,n,s,r,files=self.fixture();raw=files['records/'+r['rows'][0]['recording_file']]
        self.assertEqual(m._completed_record_state(raw),'recorded_negative')
        record=json.loads(raw);record['result']['success']=True;record['result_sha256']=m.sha(n.wire(record['result']))
        with self.assertRaises(ValueError):m._completed_record_state(n.wire(record))
        record=json.loads(raw);record['preview_failures']=[{'orders':'private'}]
        with self.assertRaises(ValueError):m._record_header(n.wire(record))
        record=json.loads(raw);record['policy_id']='full-customer-recording-policy.v1'
        with self.assertRaises(ValueError):m._record_header(n.wire(record))

class OperatorAndDependencies(unittest.TestCase):
    def test_all_code_pins_load_and_helper_pin_is_current(self):
        m=load('window')
        for name in m.DEPENDENCIES:m.dependency(name)
        self.assertEqual(m.SOURCE_HELPER_SHA256,m.sha((HERE/m.SOURCE_HELPER).read_bytes()))
        self.assertEqual(m.CAPTURE_INPUT_SCHEMA,'full1220-content-projection-input.v1')
        # Go-generated frozen DATA vectors bind every independent Python consumer.
        # A stale score literal must fail even when synthetic records are built
        # with that same stale module constant.
        v=declarations();authority=json.loads((HERE/'fixtures/v13-contracts/go-authority-data.json').read_bytes())
        self.assertIs(v['data_only'],True);self.assertIs(authority['data_only'],True)
        collection=v['collection_policy_sha256'];owned=v['owned_policy_sha256']
        self.assertEqual(authority['authorization']['collection_policy_sha256'],collection)
        self.assertEqual(m.COLLECTION_POLICY_SHA256,collection)
        self.assertEqual(m.OWNED_POLICY_SHA256,owned)
        self.assertEqual(m.dependency('codec')._COLLECTION_POLICY_SHA,collection)
        self.assertEqual(load('scores')._COLLECTION_POLICY_SHA256,collection)
    def test_owner_inactive_and_exact_command_boundary(self):
        m=load('owner-template')
        def no(*a,**kw):raise AssertionError('default owner IO')
        m.Path=no;m.run=no;self.assertEqual(m.main([]),0)
        m=load('owner-template')
        for raw in (b'{"operation":"retry"}',b'{"operation":"run_window","expected_index":true,"prior_receipt_sha256":""}',b'{"operation":"close","operation":"status"}'):
            with self.assertRaises(ValueError):m.command(raw,1,'')
    def test_456_final_token_requires_checkpoint_and_never_457(self):
        m=load('window');raw=b'UNIT original checkpoint456';claims=['a'*64]*456
        m._verify_advance_token(456,m.sha(raw),claims,raw)
        for index,token,cs in ((456,'a'*64,claims),(456,m.sha(raw),claims+['b'*64]),(457,m.sha(raw),claims)):
            with self.assertRaises(ValueError):m._verify_advance_token(index,token,cs,raw)


if __name__=='__main__':unittest.main()
