"""Pure V8 window tests; optional genuine Go export, never actual authority."""
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

HERE = Path(__file__).resolve().parent

def load():
    p = HERE/'full1220-v8-window.py'
    m = types.ModuleType('v8_window_test'); m.__file__ = str(p)
    exec(compile(p.read_bytes(), str(p), 'exec'), m.__dict__)
    return m

class Boundaries(unittest.TestCase):
    def test_default_cli_no_io(self):
        m=load()
        class Forbidden:
            def __getattr__(self, key): raise AssertionError('default touched IO')
        m.os=Forbidden();m.Path=Forbidden()
        self.assertEqual(m.main([]),0)

    def test_actual_cannot_be_selected_by_data(self):
        m=load()
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):
            m.verify_actual({'approved':True,'schema':'full-window-actual-ready.v8'})

    def test_paths_and_link_identity(self):
        m=load()
        with tempfile.TemporaryDirectory() as d:
            p=Path(d).resolve();(p/'x').write_bytes(b'x')
            r=m.Evidence(p)
            self.assertEqual(r.read('x',hashlib.sha256(b'x').hexdigest()),b'x')
            for name in ('../x','/x','./x','a//x','a/../x'):
                with self.subTest(name=name),self.assertRaises(ValueError):r.read(name)
            os.link(p/'x',p/'hard')
            with self.assertRaises(ValueError):r.read('x')
            (p/'hard').unlink();(p/'link').symlink_to('x')
            with self.assertRaises(ValueError):r.read('link')

    def test_recheck_replacement_and_closed_namespace(self):
        m=load()
        with tempfile.TemporaryDirectory() as d:
            p=Path(d).resolve();(p/'x').write_bytes(b'x');r=m.Evidence(p)
            r.read('x');r.inventory({'x'})
            (p/'replacement').write_bytes(b'x');os.replace(p/'replacement',p/'x')
            with self.assertRaises(ValueError):r.recheck()
            (p/'orphan').write_bytes(b'')
            with self.assertRaises(ValueError):r.inventory({'x'})

    def test_quality_marker_never_proof_and_zero_no_io(self):
        m=load()
        self.assertIsNone(m.require_quality([], {'verified':True}))
        with self.assertRaises(ValueError):m.require_quality([{'quality_end':{}}], {'verified':True})

    def test_quality_noop_without_any_files_or_runtime(self):
        m=load()
        def forbidden(*args,**kwargs):raise AssertionError('no-quality accessed file/helper/runtime')
        m.Evidence=forbidden;m.dependency=forbidden
        self.assertIsNone(m.verify_quality_v8(b'bad manifest',b'{"rows":[]}',{}, {}, {},
            source_root=object(),source_hashes=object(),output_dir=object()))
        with self.assertRaisesRegex(ValueError,'explicit'):
            m.verify_source_v8(b'',b'',{},{},{},source_root=object(),source_hashes=object(),output_dir=object())

    def test_json_proof_cannot_be_rehydrated(self):
        m=load()
        with self.assertRaises(ValueError):m._SourceProof(object(),{'verified':True})
        with self.assertRaises(ValueError):m._check_source_proof({'verified':True},{})

    def test_actual_context_and_summary_json_cannot_mint_proof(self):
        m=load()
        with self.assertRaises(ValueError):m._ActualContext(object(),None,None,None,None,None)
        with self.assertRaises(ValueError):m._VerifiedWindow(object(),{},{},None)
        with self.assertRaises(ValueError):m.check_verified_window({'verified':True},{})
        with self.assertRaises(ValueError):m.verify_summary_chain([b'{"verified":true}'],[b'{}'],{},'0'*64)
        raw=b'{"verified":true}'
        with self.assertRaises(ValueError):m.verify_summary_chain([raw],[b'{}'],{},hashlib.sha256(raw).hexdigest())

    def test_exclusive_report_preserves_existing_bytes(self):
        m=load()
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'report';m._exclusive(path,b'first')
            with self.assertRaises(FileExistsError):m._exclusive(path,b'second')
            self.assertEqual(path.read_bytes(),b'first')

    def test_source_rules_are_exact_reviewed_definitions(self):
        import ast
        current=ast.parse((HERE/'../../scripts/full1220-window-scores.py').read_bytes())
        new=ast.parse((HERE/'full1220-v8-source.py').read_bytes())
        for name in ('validate_completed_record_state','_Document'):
            old_node=next(n for n in current.body if getattr(n,'name','')==name)
            new_node=next(n for n in new.body if getattr(n,'name','')==name)
            self.assertEqual(ast.dump(old_node),ast.dump(new_node))

@unittest.skipUnless(os.environ.get('TASKBENCH_V8_WINDOW_EXPORT'),'requires explicit genuine Go export')
class GenuineWindow(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.m=load();cls.root=Path(os.environ['TASKBENCH_V8_WINDOW_EXPORT']).resolve()
        cls.pin=os.environ['TASKBENCH_V8_WINDOW_INVENTORY_SHA256']
        inv=json.loads((cls.root/'export-inventory.json').read_bytes())
        cls.files={name:(cls.root/name).read_bytes() for name in inv['files']}
        cls.hashes={k:hashlib.sha256(v).hexdigest() for k,v in cls.files.items()}

    def test_real_go_completed_data_not_authority(self):
        out=self.m.verify_controlled_export(self.root,self.pin)
        self.assertEqual((out['attempted_arms'],out['finished_arms'],out['observed_turns']),(56,56,60))
        self.assertEqual((out['native_model_intents'],out['native_jev_intents'],out['logical_model_calls']),(117,28,116))
        self.assertEqual(out['unknown_held'],{'model_micro_cny':74752,'jev_micro_usd':0})
        self.assertFalse(out['execution_authority']);self.assertFalse(out['owned_completion_proved'])
        self.assertEqual(out['actual_session_summary'],'not_created_or_claimed')

    def mutation(self, name, change):
        files=dict(self.files);value=json.loads(files[name]);change(value)
        files[name]=json.dumps(value,ensure_ascii=False,separators=(',',':')).encode()
        hashes={k:hashlib.sha256(v).hexdigest() for k,v in files.items()}
        with self.assertRaises(ValueError):self.m.replay_completed_window(files,hashes,1)

    def test_real_go_public_signature_data_only(self):
        m=self.m;manifest=json.loads(self.files['synthetic-manifest.json'])
        raw=self.files['synthetic-authorization.json'];auth=json.loads(raw)
        message=m.dependency('codec').authorization_signing_bytes(raw)
        m._signature(manifest['build_attestation_public_key'],message,auth['signature'])
        with self.assertRaises(ValueError):m._signature(manifest['build_attestation_public_key'],message+b' ',auth['signature'])
        bad=('00' if auth['signature'][:2]!='00' else '01')+auth['signature'][2:]
        with self.assertRaises(ValueError):m._signature(manifest['build_attestation_public_key'],message,bad)

    def test_receipt_and_state_mutations(self):
        for k,v in [('cleanup',False),('advanced',False),('schema','full-window-acquisition-receipt.v7'),('finished_arms',55),('payload_packets',144)]:
            with self.subTest(k=k):self.mutation('records/receipt.json',lambda x,k=k,v=v:x.__setitem__(k,v))
        self.mutation('records/receipt.json',lambda x:x['v8']['unknown_held'].__setitem__('model_micro_cny',0))
        self.mutation('records/receipt.json',lambda x:x['rows'][0].__setitem__('success',True))
        self.mutation('records/receipt.json',lambda x:x['rows'].reverse())

    def test_extra_and_next_claim_mutations(self):
        self.mutation('campaign/extra-000001.json',lambda x:x.__setitem__('provider_index',2))
        self.mutation('campaign/claim-000002.json',lambda x:x.__setitem__('previous_checkpoint_sha256','a'*64))
        self.mutation('campaign/checkpoint-000001.json',lambda x:x['v8'].__setitem__('logical_model_calls',117))

    def test_missing_or_orphan_payload(self):
        for operation in ('remove','orphan'):
            files=dict(self.files)
            if operation=='remove':del files['records/native-payloads/request-000001.body']
            else:files['records/native-payloads/orphan']=b'unknown'
            hashes={k:hashlib.sha256(v).hexdigest() for k,v in files.items()}
            with self.subTest(operation=operation),self.assertRaises(ValueError):self.m.replay_completed_window(files,hashes,1)

if __name__=='__main__':unittest.main()
