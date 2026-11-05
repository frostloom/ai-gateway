"""Synthetic DATA boundaries only; never actual completion or authority."""
import importlib.util
from pathlib import Path
import unittest

HERE=Path(__file__).resolve().parents[1]
def load(name):
    path=HERE/name
    if not path.exists():return None
    spec=importlib.util.spec_from_file_location('v13_test_'+name.replace('-','_'),path)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);return module

class Composition(unittest.TestCase):
    def test_explicit_distinct_composite_factory_exists(self):
        module=load('full1220-v13-window.py')
        self.assertTrue(callable(getattr(module,'verify_actual_completion',None)),
                        'V13 needs delivered-original-prefix plus normal-suffix factory')
    def test_local_suffix_mapping_and_reject_old_scope(self):
        module=load('full1220-v13-window.py')
        self.assertTrue(callable(getattr(module,'execution_window_at',None)), 'V13 mapped accessor missing')
        if module is None:return
        scope={'schema':'full-window-scope.v13','windows':[{'id':f'window-{i+55:03d}','first_ordinal':386+i-1,'last_ordinal':386+i-1} for i in range(1,457)]}
        self.assertEqual(module.execution_window_at(scope,1)['id'],'window-056')
        self.assertEqual(module.execution_window_at(scope,456)['id'],'window-511')
        for index in (0,457,True):
            with self.assertRaises(ValueError):module.execution_window_at(scope,index)
        scope['schema']='full-window-scope.v11'
        with self.assertRaises(ValueError):module.execution_window_at(scope,1)
    def test_summary_does_not_mint_completion(self):
        module=load('full1220-v13-window.py')
        self.assertTrue(callable(getattr(module,'check_verified_completion',None)), 'opaque V13 completion check missing')
        if module is None:return
        with self.assertRaises(ValueError):module.check_verified_completion({'completed':True}, {})

if __name__=='__main__':unittest.main()

class PrefixRaw(unittest.TestCase):
    def test_complete_original_get_required(self):
        m=load('full1220-v13-window.py')
        self.assertTrue(callable(getattr(m,'parse_original_get',None)), 'original raw GET parser required')
        if not hasattr(m,'parse_original_get'):return
        import json,hashlib
        body=b'{"data":[]}'
        response={'status':200,'complete':True,'raw_sha256':hashlib.sha256(body).hexdigest(),'raw_bytes':len(body)}
        intent={'method':'GET','path':'/api/public/v2/observations?traceId='+('a'*32)}
        got=m.parse_original_get(json.dumps(intent).encode(),json.dumps(response).encode(),body)
        self.assertEqual(got[0],'observations');self.assertEqual(got[1],'a'*32)
        for key,value in [('complete',False),('status',206),('raw_bytes',len(body)+1),('raw_sha256','b'*64)]:
            changed=dict(response);changed[key]=value
            with self.assertRaises(ValueError):m.parse_original_get(json.dumps(intent).encode(),json.dumps(changed).encode(),body)
    def test_foreign_or_serialized_proof_and_unfrozen_factory_rejected(self):
        m=load('full1220-v13-window.py')
        self.assertTrue(callable(getattr(m,'check_verified_completion',None)))
        if not hasattr(m,'check_verified_completion'):return
        for value in ({'prefix':{'all_verified':True},'suffix':{'closed':True}},object(),None):
            with self.assertRaises(ValueError):m.check_verified_completion(value,{})
        with self.assertRaises(ValueError):m.verify_actual_completion()

class OriginalScoreEndpoint(unittest.TestCase):
    def test_original_v3_scores_get_not_new_api_guess(self):
        import json,hashlib
        m=load('full1220-v13-window.py');body=b'{"data":[]}'
        r={'status':200,'complete':True,'raw_sha256':hashlib.sha256(body).hexdigest(),'raw_bytes':len(body)}
        self.assertEqual(m.parse_original_get(json.dumps({'method':'GET','path':'/api/public/v3/scores?scoreIds=x'}).encode(),json.dumps(r).encode(),body)[0],'scores')

class Partition(unittest.TestCase):
    def test_full_tuple_union_rejects_gaps_overlap_reorder_and_unattempted(self):
        import copy
        m=load('full1220-v13-window.py');slots=[];rows=[]
        for i in range(1,3661):
            turns=2 if i<=249 or 386<=i<2363 else 1
            slot={'ordinal':i,'pass':(i-1)//1220+1,'case_key':f'case{i}','arm_order':['model','jev_selective'],'user_turns':turns};slots.append(slot)
            for arm in slot['arm_order']:
                rows.append({'ordinal':i,'pass':slot['pass'],'case_key':slot['case_key'],'arm':arm,'defined_turns':turns,'trace_id':f'{len(rows)+1:032x}','attempted':True,'finished':True,'status':'failed','semantic':'not_assessable'})
        self.assertEqual(m.validate_primary_partition(rows[:770],rows[770:],slots),{'Pairs':3660,'Arms':7320,'DefinedUserTurns':11772,'Boundaries':1851})
        for change in ('gap','overlap','order','unattempted','case','turn','trace'):
            bad=copy.deepcopy(rows)
            if change=='gap':bad.pop(770)
            elif change=='overlap':bad[770]=copy.deepcopy(bad[0])
            elif change=='order':bad[770],bad[771]=bad[771],bad[770]
            elif change=='unattempted':bad[770]['attempted']=False
            elif change=='case':bad[770]['case_key']='changed'
            elif change=='turn':bad[770]['defined_turns']+=1
            else:bad[770]['trace_id']=bad[0]['trace_id']
            with self.subTest(change=change),self.assertRaises(ValueError):m.validate_primary_partition(bad[:770],bad[770:],slots)
    def test_final456_token_is_checkpoint_never_claim457(self):
        import hashlib
        m=load('full1220-v13-window.py');claims=['a'*64]*456;raw=b'original checkpoint456'
        m._verify_advance_token(456,hashlib.sha256(raw).hexdigest(),claims,raw)
        for index,token,cs in [(456,'a'*64,claims),(456,hashlib.sha256(raw).hexdigest(),claims+['b'*64]),(457,'b'*64,claims)]:
            with self.assertRaises(ValueError):m._verify_advance_token(index,token,cs,raw)
    def test_owner_rejects_command457_even_with_matching_index(self):
        m=load('full1220-v13-owner-template.py')
        with self.assertRaises(ValueError):m.command(b'{"operation":"run_window","expected_index":457,"prior_receipt_sha256":"abc"}',457,'abc')

class OriginalScoreSubject(unittest.TestCase):
    def test_v3_trace_subject_normalized_exactly(self):
        m=load('full1220-v13-window.py')
        self.assertTrue(callable(getattr(m,'original_score_payload',None)), 'v3 subject must be compared to original traceId')
        if not hasattr(m,'original_score_payload'):return
        self.assertEqual(m.original_score_payload({'id':'x','subject':{'kind':'trace','id':'a'*32}})['traceId'],'a'*32)
        for row in ({'subject':{'kind':'observation','id':'a'*32}},{'subject':{'kind':'trace','id':'a'*32},'traceId':'b'*32}):
            with self.assertRaises(ValueError):m.original_score_payload(row)

class OpaqueModuleIdentity(unittest.TestCase):
    def test_foreign_source_and_completion_types_rejected_before_io(self):
        a=load('full1220-v13-window.py');b=load('full1220-v13-window.py')
        source=b._SourceProof(b._SOURCE_SEAL,{'inputs':{}})
        with self.assertRaises(ValueError):a._check_source_proof(source,{})
        with self.assertRaises(ValueError):a.check_verified_completion(object.__new__(b.VerifiedV13Completion),{})
        with self.assertRaises(ValueError):a.check_verified_cohort(object.__new__(b.VerifiedV13Cohort))
