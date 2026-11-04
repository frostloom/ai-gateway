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

HERE=Path(__file__).resolve().parents[1]


def load(name):
    p=HERE/name;m=types.ModuleType('unit_mock_'+name.replace('-','_'));m.__file__=str(p)
    sys.modules[m.__name__]=m
    exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__)
    return m


class Plumbing(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve();self.w=load('full1220-v12-window.py')
        self.p=self.w.dependency('runner')
        self.runtime=self.root/'runtime';self.runtime.mkdir()
        self.source=self.root/'source';self.source.mkdir()
        build=self.root/'helper-build';build.mkdir();(build/'catalog.json').write_bytes(b'UNIT MOCK, NOT CATALOG');(build/'catalog.json').chmod(0o400)

    def helper(self,kind,code):
        package=self.runtime/('internal/taskbench' if kind in ('source','spool') else 'internal/observability')
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
raw=Path(os.environ['FULL1220_V12_SOURCE_INPUT']).read_bytes();job=json.loads(raw)
assert job['schema']=='full1220-source-v12-job.v1' and job['observation_receipt_boundary']=='durable_local'
out=json.dumps({'schema':'full1220-source-v12-replay.v1','input_sha256':hashlib.sha256(raw).hexdigest(),'binding':job['binding']},separators=(',',':')).encode()
with Path(os.environ['FULL1220_V12_SOURCE_OUTPUT']).open('xb') as f:f.write(out)
line='    full1220_source_v12_test.go:140: FULL1220_V12_SOURCE_OUTPUT_SHA256='+hashlib.sha256(out).hexdigest()
print(line)
if Path(os.environ['FULL1220_V12_SOURCE_INPUT']).parent.name=='bad':print(line)
'''
        h,selected=self.helper('source',code)
        (self.source/'go.mod').write_bytes(b'UNIT MOCK source')
        hashes={'go.mod':hashlib.sha256(b'UNIT MOCK source').hexdigest()}
        manifest={'schema':'full-window-actual-ready.v12','source_root':str(self.source),'sources':hashes,'scope':{},'scope_sha256':'a'*64,'execution_sha256':'b'*64}
        mr=json.dumps(manifest).encode();rows=[{'recording_file':'arm.json','recording_sha256':hashlib.sha256(b'{}').hexdigest()}]
        rr=json.dumps({'schema':'full-window-acquisition-receipt.v12','scope_sha256':'a'*64,'execution_sha256':'b'*64,'rows':rows}).encode()
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
            result=self.w.verify_source_v12(mr,rr,{'arm.json':b'{}'},{},pins,source_root=str(self.source),source_hashes=hashes,
                output_dir=self.root/'good',execute=True,context=context)
            self.assertEqual(result,{'unit_mock':True,'not_source_proof':True})
            self.assertEqual(len(constructed),1)
            with self.assertRaises(ValueError):
                self.w.verify_source_v12(mr,rr,{'arm.json':b'{}'},{},pins,source_root=str(self.source),source_hashes=hashes,
                    output_dir=self.root/'bad',execute=True,context=context)
            self.assertEqual(len(constructed),1)
            self.assertTrue((self.root/'helper-supervision/active.json').exists())

    def test_capture_adapter_deferred_private_stdin_and_original_receipt(self):
        s=load('full1220-v12-scores.py');secret='h'*32
        digest=hashlib.sha256(json.dumps({'hash_key':secret},sort_keys=True,separators=(',',':')).encode()).hexdigest()
        code='''import os,sys,json,hashlib
from pathlib import Path
assert Path.cwd().name=='observability'
assert Path(os.environ['SDKROOT']).name=='MacOSX26.5.sdk'
assert hashlib.sha256(sys.stdin.buffer.read()).hexdigest()=='''+repr(digest)+'''
raw=Path(os.environ['FULL1220_PROJECTION_INPUT']).read_bytes()
assert json.loads(raw)['schema']=='full1220-content-projection-input.v1'
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
        with patch.object(s,'_check_actual',lambda *a:None),patch.object(s,'build_window_score_plan',return_value={'unit_mock':True}),patch.object(s,'_projection_job',return_value={'schema':'full1220-content-projection-input.v1'}),\
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


    def test_spool_adapter_runs_fixed_helper_with_no_private_stdin(self):
        code='''import os,json,hashlib
from pathlib import Path
assert Path.cwd().name=='taskbench'
raw=Path(os.environ['FULL1220_V12_SPOOL_INPUT']).read_bytes();job=json.loads(raw)
assert job['schema']=='full1220-v12-closed-spool-job.v1'
out=json.dumps({'schema':'full1220-v12-closed-spool-data.v1','input_sha256':hashlib.sha256(raw).hexdigest(),'summary':{'unit_mock':True},'spans':[]},separators=(',',':')).encode()
with Path(os.environ['FULL1220_V12_SPOOL_OUTPUT']).open('xb') as f:f.write(out)
print('    full1220_source_v12_test.go:200: FULL1220_V12_SPOOL_OUTPUT_SHA256='+hashlib.sha256(out).hexdigest())
'''
        h,selected=self.helper('spool',code)
        context=types.SimpleNamespace(root=self.root,helper=h,manifest={'spool_root':str(self.root/'spool'),'observer':{'spool_run_id':'a'*64}})
        with patch.object(self.p,'select_helper',return_value=selected):
            data=self.w._verify_spool_helper(context,self.root/'spool-output')
        self.assertEqual(data['summary'],{'unit_mock':True})
        self.assertNotIn('proof',data)

if __name__=='__main__':unittest.main()
