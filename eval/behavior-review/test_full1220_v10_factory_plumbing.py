"""UNIT MOCK trust/schema boundaries; real local process/receipt plumbing only.

No Go replay, actual freeze, window proof, source proof or capture proof is made:
crypto/context/source-case validators and final proof constructors are explicitly
replaced. These tests catch adapter wiring errors, not actual source correctness.
"""
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

HERE=Path(__file__).resolve().parent


def load(name):
    p=HERE/name;m=types.ModuleType('unit_mock_'+name.replace('-','_'));m.__file__=str(p)
    sys.modules[m.__name__]=m
    exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__)
    return m


class Plumbing(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve();self.w=load('full1220-v10-window.py')
        self.p=self.w.dependency('runner')
        self.runtime=self.root/'runtime';self.runtime.mkdir()
        self.source=self.root/'source';self.source.mkdir()
        build=self.root/'helper-build';build.mkdir();(build/'catalog.json').write_bytes(b'UNIT MOCK, NOT CATALOG');(build/'catalog.json').chmod(0o400)

    def helper(self,kind,code):
        package=self.runtime/('internal/taskbench' if kind=='source' else 'internal/observability')
        package.mkdir(parents=True,exist_ok=True)
        binary=self.root/(kind+'.fixture')
        binary.write_text('#!'+sys.executable+'\n'+code);binary.chmod(0o500)
        tree=self.p.pin_tree(self.runtime,{})
        selection=types.SimpleNamespace(binary=self.p.pin_file(binary),argv=(str(binary),),cwd=str(package))
        return {'runner':self.p,'runtime':tree,'tools':{},'catalog':{}},selection

    def test_source_adapter_actual_process_markers_and_failure_barrier(self):
        code='''import os,json,hashlib
from pathlib import Path
assert Path.cwd().name=='taskbench'
raw=Path(os.environ['FULL1220_V10_SOURCE_INPUT']).read_bytes();job=json.loads(raw)
assert job['schema']=='full1220-source-v10-job.v1' and job['local_index']==1 and job['window_id']=='window-056'
assert job['execution_totals']=={'Pairs':3275,'Arms':6550,'DefinedUserTurns':10504,'Boundaries':0,'ModelCap':110191257,'JEVCap':1818872}
out=json.dumps({'schema':'full1220-source-v10-replay.v1','input_sha256':hashlib.sha256(raw).hexdigest(),'binding':job['binding']},separators=(',',':')).encode()
with Path(os.environ['FULL1220_V10_SOURCE_OUTPUT']).open('xb') as f:f.write(out)
line='    full1220_source_v10_test.go:140: FULL1220_V10_SOURCE_OUTPUT_SHA256='+hashlib.sha256(out).hexdigest()
print(line)
if Path(os.environ['FULL1220_V10_SOURCE_INPUT']).parent.name=='bad':print(line)
'''
        h,selected=self.helper('source',code)
        (self.source/'go.mod').write_bytes(b'UNIT MOCK source')
        hashes={'go.mod':hashlib.sha256(b'UNIT MOCK source').hexdigest()}
        manifest={'schema':'full-window-actual-ready.v10','source_root':str(self.source),'sources':hashes,'scope':{'schema':'full-window-scope.v10','windows':[{'id':f'window-{i+56:03d}','first_ordinal':386,'last_ordinal':390} for i in range(456)]},'scope_sha256':'a'*64,'execution_sha256':'b'*64}
        mr=json.dumps(manifest).encode();rows=[{'recording_file':'arm.json','recording_sha256':hashlib.sha256(b'{}').hexdigest()}]
        rr=json.dumps({'schema':'full-window-acquisition-receipt.v10','scope_sha256':'a'*64,'execution_sha256':'b'*64,'window_index':1,'window_id':'window-056','rows':rows}).encode()
        n=self.w.dependency('native');leaf=self.w.dependency('source')
        class MockContext:pass
        context=MockContext();context.seal=self.w._ACTUAL_SEAL;context.helper=h;context.root=self.root;context.manifest=manifest
        dep=self.w.dependency
        def dependency(name):
            if name=='source':return types.SimpleNamespace(_Document=leaf._Document,validate_completed_record_state=lambda raw:None)
            return dep(name)
        pins=self.w.source_binding(mr,rr,{'arm.json':b'{}'},{})
        constructed=[]
        def no_proof(seal,binding):constructed.append(binding);return {'unit_mock':True,'not_source_proof':True}
        with patch.object(self.w,'_selected',lambda:None),patch.object(self.w,'_scope',lambda *a:None),patch.object(self.w,'_ActualContext',MockContext),\
             patch.object(self.w,'FROZEN_MANIFEST_SHA256',hashlib.sha256(mr).hexdigest()),patch.object(self.w,'dependency',side_effect=dependency),\
             patch.object(self.p,'select_helper',return_value=selected),patch.object(self.w,'_SourceProof',side_effect=no_proof):
            result=self.w.verify_source_v10(mr,rr,{'arm.json':b'{}'},{},pins,source_root=str(self.source),source_hashes=hashes,
                output_dir=self.root/'good',execute=True,context=context)
            self.assertEqual(result,{'unit_mock':True,'not_source_proof':True})
            self.assertEqual(len(constructed),1)
            with self.assertRaises(ValueError):
                self.w.verify_source_v10(mr,rr,{'arm.json':b'{}'},{},pins,source_root=str(self.source),source_hashes=hashes,
                    output_dir=self.root/'bad',execute=True,context=context)
            self.assertEqual(len(constructed),1)
            self.assertTrue((self.root/'helper-supervision/active.json').exists())

    def test_source_late_future_or_foreign_records_never_reaches_proof_constructor(self):
        # Explicit mock authority/summary/replay boundaries; NO actual proof is
        # created. Exercises the real wrapper across source execution boundary.
        m=self.w;records=self.root/'records';current=records/'window-000001';current.mkdir(parents=True)
        campaign=self.root/'campaign';campaign.mkdir()
        (records/'startup.json').write_text(json.dumps({'evidence_kind':'recorded_run','closed':False,'acquisition_done':False,
            'next_window_index':1,'last_receipt_sha256':'','attempted_arms':0}))
        (current/'arm.json').write_bytes(b'{}')
        receipt={'rows':[{'recording_file':'arm.json','recording_sha256':hashlib.sha256(b'{}').hexdigest()}],'ledger_sha256':'d'*64}
        rr=json.dumps(receipt).encode();(current/'receipt.json').write_bytes(rr)
        sr=json.dumps({'report_sha256':hashlib.sha256(rr).hexdigest()}).encode();(records/'window-000001-summary.json').write_bytes(sr)
        (campaign/'checkpoint-000001.json').write_text(json.dumps({'ledger_sha256':'d'*64}))
        manifest={'scope':{},'sources':{},'source_root':str(self.source)};mr=json.dumps(manifest).encode()
        (self.root/'actual-manifest.json').write_bytes(mr)
        reply=(HERE.parents[1]/'internal/taskbench/testdata/window_v9_budget_approval.json').read_bytes()
        (self.root/'reply.json').write_bytes(reply)
        auth=json.loads((HERE/'fixtures/v10-contracts/go-delegated-authority.json').read_bytes())['authorization']
        auth['human_reply']['path']=str(self.root/'reply.json')
        context=types.SimpleNamespace(root=self.root,reader=m.Evidence(self.root),manifest=manifest,
            authority_raw=json.dumps(auth).encode(),binding={'verifier_sha256':'e'*64})
        for name in ('window-000002','foreign-sibling'):
            def source(*a,**kw):(records/name).mkdir();return {'unit_mock':True,'not_source_proof':True}
            with patch.object(m,'_selected',lambda:None),patch.object(m,'load_freeze_binding',return_value=context),\
                 patch.object(m,'verify_summary_chain',return_value={'total_permanent':{}}),\
                 patch.object(m,'FROZEN_MANIFEST_SHA256',hashlib.sha256(mr).hexdigest()),\
                 patch.object(m,'verify_source_v10',side_effect=source),\
                 patch.object(m,'_replay_completed_window',return_value={'total_permanent':{}}),\
                 patch.object(m,'_VerifiedWindow',return_value={'unit_mock':True,'not_window_proof':True}) as constructor:
                with self.assertRaises(ValueError):
                    m._verify_actual_window(self.root,binding_sha256='a'*64,window_index=1,summary_sha256=hashlib.sha256(sr).hexdigest(),helper_output_dir=self.root/'unused')
                constructor.assert_not_called()
            (records/name).rmdir()


if __name__=='__main__':unittest.main()
