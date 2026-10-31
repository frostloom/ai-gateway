"""Controlled V11 boundaries; synthetic DATA is never an actual proof."""
import copy
import json
from pathlib import Path
import types
import unittest

HERE = Path(__file__).resolve().parent

def load(name):
    p = HERE / ('full1220-v11-'+name+'.py')
    if not p.exists(): raise AssertionError('V11 '+name+' implementation missing')
    m = types.ModuleType('test_v11_'+name.replace('-', '_')); m.__file__ = str(p)
    exec(compile(p.read_bytes(), str(p), 'exec'), m.__dict__)
    return m

def scope(m):
    s=json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes())
    s['schema']='full-window-scope.v11';s['baseline_evidence_sha256']=m.BASELINE
    d=s['budget_declaration'];d['baseline_sha256']=m.BASELINE
    d['parent']={'model_micro_cny':195000000,'jev_micro_usd':3000000}
    d['historical']={'model_micro_cny':66825427,'jev_micro_usd':638655}
    d['maximum_reserved']={'model_micro_cny':194170923,'jev_micro_usd':2729954}
    d['remaining']={'model_micro_cny':829077,'jev_micro_usd':270046}
    s['budget_declaration_sha256']=m.sha(m.dependency('native').wire(d))
    s['observation']={'schema':'full-window-observation-declaration.v11','private_settings_sha256':'a'*64,'spool_root':'/controlled/spool','spool_run_id':'b'*64,'spool_max_bytes':4<<30,'observation_receipt_boundary':'durable_local','observer_identity_sha256':'c'*64}
    return s

class V11Boundaries(unittest.TestCase):
    def setUp(self): self.m=load('window');self.n=self.m.dependency('native')
    def test_full_original_schedule_and_mapping(self):
        s=scope(self.m);self.m._scope(s,self.n.wire(s),self.n)
        self.assertEqual(self.m.execution_totals(s),{'Pairs':3660,'Arms':7320,'DefinedUserTurns':11772,'Boundaries':1851,'ModelCap':123827032,'JEVCap':2037474})
        for i in (1,56,57,511):self.assertEqual(self.m.execution_window_at(s,i)['id'],f'window-{i:03d}')
    def test_old_reduced_reordered_and_composed_rejected(self):
        for mutate in (lambda s:s.update(schema='full-window-scope.v10'),lambda s:s['windows'].pop(),lambda s:s['slots'].pop(),lambda s:s.update(completion={}),lambda s:s['slots'].reverse(),lambda s:s.pop('observation')):
            s=scope(self.m);mutate(s)
            with self.assertRaises((ValueError,KeyError)):self.m._scope(s,self.n.wire(s),self.n)
    def test_no_json_or_foreign_proof(self):
        for value in ({'verified':True},None,object()):
            with self.assertRaises(ValueError):self.m.window_proof(value,1)
            with self.assertRaises(ValueError):self.m.check_verified_cohort(value)
        with self.assertRaises(ValueError):self.m.VerifiedV11Cohort(object(),None,None,None)
    def test_unfrozen_before_paths(self):
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):self.m.verify_actual_cohort(object())
    def test_live_durable_is_not_remote_or_normal(self):
        i={'spool_run_id':'b'*64,'spool_root_sha256':'c'*64,'spool_max_bytes':4<<30}
        c={'Accepted':2,'Persisted':2,'Exported':0,'Dropped':0,'Failed':0,'QueueDepth':0,'Turns':1,'SampledTurns':1,'Batches':1}
        o={'observation_receipt_boundary':'durable_local','remote_delivery':'pending','normal_closure':False,'spool':{'run_id':'b'*64,'root_sha256':'c'*64,'max_bytes':4<<30,'spans':2,'batches':1,'bytes':1,'stored_bytes':2,'last_link_sha256':'d'*64,'closed':False,'poisoned':False,'data_only':False,'durability_return_unproven':False,'owner_completion_unproven':False}}
        self.m.validate_observation_receipt(o,c,i,final=False)
        with self.assertRaises(ValueError):self.m.validate_observation_receipt(o,c,i,final=True)
        for field,value in [('Exported',2),('Failed',1),('Persisted',1),('Accepted',True)]:
            bad=dict(c);bad[field]=value
            with self.assertRaises(ValueError):self.m.validate_observation_receipt(o,bad,i,final=False)
        for field,value in [('remote_delivery','verified'),('normal_closure',True)]:
            bad=copy.deepcopy(o);bad[field]=value
            with self.assertRaises(ValueError):self.m.validate_observation_receipt(bad,c,i,final=False)

if __name__=='__main__':unittest.main()

class FinalDataBoundaries(unittest.TestCase):
    def setUp(self):self.m=load('window')
    def test_spool_exact_full_trace_parent_and_data_boundary(self):
        receipts=[{'rows':[{'trace_id':f'{i:032x}'} for i in range(1,7321)]}]
        spans=[{'trace_id':f'{i:032x}','span_id':f'{i:016x}','parent_span_id':'','start_time_unix_nano':1,'end_time_unix_nano':2} for i in range(1,7321)]
        live={'spans':7320,'data_only':False,'durability_return_unproven':False,'owner_completion_unproven':False}
        data={'summary':{**live,'data_only':True,'durability_return_unproven':True,'owner_completion_unproven':True},'spans':spans}
        self.assertEqual(len(self.m.validate_spool_coverage(data,live,receipts)),7320)
        for change in ('missing','duplicate','foreign','parent','timestamp','json_live'):
            bad=copy.deepcopy(data)
            if change=='missing':bad['spans'].pop()
            elif change=='duplicate':bad['spans'][-1]=copy.deepcopy(bad['spans'][0])
            elif change=='foreign':bad['spans'][0]['trace_id']='f'*32
            elif change=='parent':bad['spans'][0]['parent_span_id']='f'*16
            elif change=='timestamp':bad['spans'][0]['start_time_unix_nano']=3
            else:bad['summary']['data_only']=False
            with self.subTest(change=change),self.assertRaises(ValueError):self.m.validate_spool_coverage(bad,live,receipts)
    def test_closed_snapshot_rejects_changed_extra_missing_alias_and_hardlink(self):
        import tempfile,os
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();(root/'raw').write_bytes(b'original')
            reader,files=self.m._snapshot(root);self.assertEqual(files,{'raw':b'original'})
            self.m._fresh_snapshot(reader)
            (root/'extra').write_bytes(b'extra')
            with self.assertRaises(ValueError):self.m._fresh_snapshot(reader)
            (root/'extra').unlink();(root/'raw').write_bytes(b'changed')
            with self.assertRaises(ValueError):self.m._fresh_snapshot(reader)
            os.link(root/'raw',root/'link')
            with self.assertRaises(ValueError):self.m._snapshot(root)
    def test_fake_live_window_never_satisfies_final_projection(self):
        w=self.m._VerifiedWindow(self.m._WINDOW_SEAL,{}, {'final_normal_cohort':False},None)
        with self.assertRaises(ValueError):self.m.check_verified_window(w,{})
