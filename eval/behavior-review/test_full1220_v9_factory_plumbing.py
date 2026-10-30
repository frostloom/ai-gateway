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
        self.root=Path(self.temp.name).resolve();self.w=load('full1220-v9-window.py')
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
raw=Path(os.environ['FULL1220_V9_SOURCE_INPUT']).read_bytes();job=json.loads(raw)
out=json.dumps({'schema':'full1220-source-v9-replay.v1','input_sha256':hashlib.sha256(raw).hexdigest(),'binding':job['binding']},separators=(',',':')).encode()
with Path(os.environ['FULL1220_V9_SOURCE_OUTPUT']).open('xb') as f:f.write(out)
line='    full1220_source_v9_test.go:140: FULL1220_V9_SOURCE_OUTPUT_SHA256='+hashlib.sha256(out).hexdigest()
print(line)
if Path(os.environ['FULL1220_V9_SOURCE_INPUT']).parent.name=='bad':print(line)
'''
        h,selected=self.helper('source',code)
        (self.source/'go.mod').write_bytes(b'UNIT MOCK source')
        hashes={'go.mod':hashlib.sha256(b'UNIT MOCK source').hexdigest()}
        manifest={'schema':'full-window-actual-ready.v9','source_root':str(self.source),'sources':hashes,'scope':{},'scope_sha256':'a'*64,'execution_sha256':'b'*64}
        mr=json.dumps(manifest).encode();rows=[{'recording_file':'arm.json','recording_sha256':hashlib.sha256(b'{}').hexdigest()}]
        rr=json.dumps({'schema':'full-window-acquisition-receipt.v8','scope_sha256':'a'*64,'execution_sha256':'b'*64,'rows':rows}).encode()
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
            result=self.w.verify_source_v9(mr,rr,{'arm.json':b'{}'},{},pins,source_root=str(self.source),source_hashes=hashes,
                output_dir=self.root/'good',execute=True,context=context)
            self.assertEqual(result,{'unit_mock':True,'not_source_proof':True})
            self.assertEqual(len(constructed),1)
            with self.assertRaises(ValueError):
                self.w.verify_source_v9(mr,rr,{'arm.json':b'{}'},{},pins,source_root=str(self.source),source_hashes=hashes,
                    output_dir=self.root/'bad',execute=True,context=context)
            self.assertEqual(len(constructed),1)
            self.assertTrue((self.root/'helper-supervision/active.json').exists())

    def test_capture_adapter_deferred_private_stdin_and_original_receipt(self):
        s=load('full1220-v9-scores.py');secret='h'*32
        digest=hashlib.sha256(json.dumps({'hash_key':secret},sort_keys=True,separators=(',',':')).encode()).hexdigest()
        code='''import os,sys,json,hashlib
from pathlib import Path
assert Path.cwd().name=='observability'
assert hashlib.sha256(sys.stdin.buffer.read()).hexdigest()=='''+repr(digest)+'''
raw=Path(os.environ['FULL1220_PROJECTION_INPUT']).read_bytes()
out=json.dumps({'schema':'full1220-content-projection.v1','input_sha256':hashlib.sha256(raw).hexdigest(),'rows':[]},separators=(',',':')).encode()
with Path(os.environ['FULL1220_PROJECTION_OUTPUT']).open('xb') as f:f.write(out)
print('    full1220_content_projection_v8_test.go:218: FULL1220_PROJECTION_OUTPUT_SHA256='+hashlib.sha256(out).hexdigest())
'''
        h,selected=self.helper('capture',code)
        content=(HERE.parents[1]/'internal/observability/content.go').read_bytes()
        target=self.source/'internal/observability/content.go';target.parent.mkdir(parents=True);target.write_bytes(content)
        settings=self.root/'synthetic-settings.json';settings.write_text(json.dumps({'observer':{'enabled':True,'capture_content':True,'environment':'evaluation','release':'commit','hash_key':secret}}));settings.chmod(0o600)
        manifest={'source_root':str(self.source),'sources':{'internal/observability/content.go':hashlib.sha256(content).hexdigest()},
            'private_settings_sha256':hashlib.sha256(settings.read_bytes()).hexdigest(),'archive_commit':'commit',
            'observer':{'enabled':True,'capture_content':True,'sample_rate':1,'environment':'evaluation','release':'commit'}}
        context=types.SimpleNamespace(root=self.root,manifest=manifest,helper=h)
        module=types.SimpleNamespace(load_freeze_binding=lambda *a:context,Evidence=self.w.Evidence)
        proof=types.SimpleNamespace(_module=module,payload_raw=b'{"packets":[]}') # explicitly NOT _ScoreActualProof
        checked=[]
        def check(payload,job,raw,records):checked.append(payload['input_sha256']);self.assertEqual(payload['input_sha256'],hashlib.sha256(raw).hexdigest())
        with patch.object(s,'build_window_score_plan',return_value={'unit_mock':True}),patch.object(s,'_projection_job',return_value={}),\
             patch.object(s,'_check_projected_output',side_effect=check),patch.object(self.p,'select_helper',return_value=selected),\
             patch.object(s,'_ContentProjection',return_value={'unit_mock':True,'not_content_proof':True}):
            result=s.project_window_content(json.dumps(manifest).encode(),b'{}',b'{}',{},b'{}',{'projection_helper_sha256':s._PROJECTION_HELPER_SHA256},
                source_root=str(self.source),output_dir=self.root/'capture',execute=True,actual_proof=proof,private_settings_path=settings)
        self.assertEqual(result,{'unit_mock':True,'not_content_proof':True});self.assertEqual(len(checked),1)
        raw=b''.join(p.read_bytes() for p in (self.root/'helper-supervision').rglob('*') if p.is_file())
        self.assertNotIn(secret.encode(),raw)
        reference=json.loads((self.root/'capture/process.json').read_bytes())
        receipt=json.loads(Path(reference['path']).read_bytes())
        self.assertTrue(receipt['accepted']);self.assertEqual(receipt['phase'],'capture');self.assertTrue(receipt['cleanup']['confirmed'])

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
        auth=json.loads((HERE/'fixtures/v9-contracts/go-delegated-authority.json').read_bytes())['authorization']
        auth['human_reply']['path']=str(self.root/'reply.json')
        context=types.SimpleNamespace(root=self.root,reader=m.Evidence(self.root),manifest=manifest,
            authority_raw=json.dumps(auth).encode(),binding={'verifier_sha256':'e'*64})
        for name in ('window-000002','foreign-sibling'):
            def source(*a,**kw):(records/name).mkdir();return {'unit_mock':True,'not_source_proof':True}
            with patch.object(m,'_selected',lambda:None),patch.object(m,'load_freeze_binding',return_value=context),\
                 patch.object(m,'verify_summary_chain',return_value={'total_permanent':{}}),\
                 patch.object(m,'FROZEN_MANIFEST_SHA256',hashlib.sha256(mr).hexdigest()),\
                 patch.object(m,'verify_source_v9',side_effect=source),\
                 patch.object(m,'_replay_completed_window',return_value={'total_permanent':{}}),\
                 patch.object(m,'_VerifiedWindow',return_value={'unit_mock':True,'not_window_proof':True}) as constructor:
                with self.assertRaises(ValueError):
                    m._verify_actual_window(self.root,binding_sha256='a'*64,window_index=1,summary_sha256=hashlib.sha256(sr).hexdigest(),helper_output_dir=self.root/'unused')
                constructor.assert_not_called()
            (records/name).rmdir()


if __name__=='__main__':unittest.main()
