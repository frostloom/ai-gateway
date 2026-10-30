"""Offline V9 score adapter contracts. Never mint actual or Go proof fixtures."""
import sys
sys.dont_write_bytecode = True
import copy
import hashlib
import json
from pathlib import Path
import types
import tempfile
import os
import unittest
from unittest.mock import patch
HERE=Path(__file__).resolve().parent

def load():
    p=HERE/'full1220-v9-scores.py'
    m=types.ModuleType('v9_scores_test');m.__file__=str(p)
    exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__)
    return m

class Scores(unittest.TestCase):
    def test_prebuilt_projection_rejects_untrusted_proof_before_key_or_runtime(self):
        m=load()
        def forbidden(*a,**kw):raise AssertionError('untrusted capture read key/runtime')
        m._projection_private_settings=forbidden;m._regular_bytes=forbidden
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):
            m.project_window_content(b'{}',b'{}',b'{}',{},b'{}',{},source_root='/untrusted',output_dir='/untrusted',
                execute=True,actual_proof={'verified':True},private_settings_path='/untrusted')

    def test_routing_metadata_original13_plus_conditional10(self):
        m=load()
        observation={'turn':{'status':200,'response':{'pending_confirm':False,'telemetry':{
            'turn_stage':'reply','llm_calls':1,'jev_calls':1,'jev_errors':0,'llm_usage_missing':0,'jev_usage_missing':0,
            'jev_fast_path':False,'jev_fast_path_reason':'','jev_intent':'get_balance','jev_intent_confidence':.95,
            'jev_enabled':True,'jev_blocked':False,'jev_short_circuit':False,'jev_route_shape':'single_read',
            'jev_shape_confidence':.97,'jev_legit_probability':.91,'jev_risk_score':1.69,'jev_risk_confidence':.98,
            'jev_fast_path_eligible':False,'jev_fast_path_tool':'get_balance'}}}}
        expected=m.turn_observer_metadata(observation,1)
        self.assertEqual(len(expected),23)
        self.assertEqual(expected['jev_risk_score'],1.69)
        actual={k:(str(v).lower() if type(v) is bool else str(v)) for k,v in expected.items()}
        actual.pop('jev_fast_path_reason') # only empty values may be absent
        m.validate_turn_observer_metadata(actual,expected)
        wrong=dict(actual,jev_risk_score='invalid')
        with self.assertRaises(ValueError):m.validate_turn_observer_metadata(wrong,expected)
        observation['turn']['response']['telemetry']['jev_risk_score']=2.01
        observation['turn']['response']['telemetry']['jev_route_shape']='secret text'
        observation['turn']['response']['telemetry']['jev_fast_path_tool']='unknown_safe_slug'
        invalid=m.turn_observer_metadata(observation,1)
        for key in ('jev_risk_score','jev_route_shape','jev_fast_path_tool'):self.assertEqual(invalid[key],'invalid')
        observation['turn']['response']['telemetry']['jev_calls']=0
        zero=m.turn_observer_metadata(observation,1)
        self.assertEqual(len(zero),13)
        with self.assertRaises(ValueError):m.validate_turn_observer_metadata({**zero,'jev_risk_score':0},zero)


    def test_projection_receipt_uses_genuine_v8_filename_once_and_exact_bytes(self):
        m=load();raw=b'{"controlled":"projection"}\n'
        digest=hashlib.sha256(raw).hexdigest().encode()
        line=b'    full1220_content_projection_v8_test.go:231: FULL1220_PROJECTION_OUTPUT_SHA256='+digest+b'\n'
        stdout=b'=== RUN   TestFull1220FrozenContentProjection\n'+line+b'--- PASS: TestFull1220FrozenContentProjection (0.01s)\nPASS\n'
        self.assertIsNone(m._verify_projection_receipt(stdout,raw))
        self.assertIsNone(m._verify_projection_receipt(stdout.replace(b'\n',b'\r\n'),raw))
        for bad_stdout,bad_raw in ((stdout,raw+b' '),(stdout+line,raw),
                (stdout.replace(b'_v8_test.go',b'_test.go'),raw),
                (stdout.replace(digest,b'0'*64),raw)):
            with self.subTest(stdout=bad_stdout,raw=bad_raw),self.assertRaises(ValueError):
                m._verify_projection_receipt(bad_stdout,bad_raw)

    def test_default_and_untrusted_inputs_never_touch_io(self):
        m=load()
        class Forbidden:
            def __getattr__(self,key):raise AssertionError('unauthenticated IO')
        m.Path=Forbidden();m.os=Forbidden()
        self.assertEqual(m.main([]),0)
        for call in (lambda:m.verify_actual_score_inputs('/untrusted'),
                     lambda:m.load_window_score_client(b'{}','/untrusted',actual_proof={'verified':True}),
                     lambda:m.build_window_score_plan(b'{}',b'{}',b'{}',{},b'{}',{},actual_proof={'verified':True})):
            with self.assertRaises(ValueError):call()

    def test_metadata_native_identity_count_parent_and_no_io(self):
        m=load()
        packet={'sequence':1,'identity':{'native_attempt':2,'provider':'model','model':'deepseek-flash','case_key':'case/one','pass':1,'arm':'model','turn':1,'attempt':1,'body_sha256':'a'*64,'bytes':42},'scope_sha256':'b'*64,'claim_sha256':'c'*64,'window_id':'window-001','response':{'complete':True,'available':True,'status':200}}
        row={'name':'window.native','id':'native-span','traceId':'d'*32,'parentObservationId':'logical','metadata':{'native_schema':'full-window-native.v8','native_id':'fb39b82843c6775ff878c217b8a32d8609d9f4c1048b9de1a05686fdd95e9f5c','native_sequence':1,'native_attempt':2,'provider':'model','body_sha256':'a'*64,'request_bytes':42,'native_content_mode':'metadata_only','claim_sha256':'c'*64,'case_key':'case/one','pass':1,'arm':'model','turn':1,'logical_attempt':1,'native_outcome':'settled','response_status':200,'response_bytes':0,'response_complete':True,'response_available':True}}
        parents=[{'id':'turn','name':'agent.turn','parentObservationId':'root','metadata':{'turn_index':1}}, {'id':'logical','name':'llm.attempt','parentObservationId':'turn','metadata':{}}]
        self.assertEqual(m.validate_native_observations([row]+parents,[packet]),1)
        for mutate in ('input','output','identity','parent','count','attempt'):
            rows=copy.deepcopy([row]+parents)
            if mutate in ('input','output'):rows[0][mutate]='sentinel'
            elif mutate=='identity':rows[0]['metadata']['native_id']='foreign'
            elif mutate=='parent':rows[0]['parentObservationId']='turn'
            elif mutate=='count':rows.append(copy.deepcopy(row))
            else:rows[0]['metadata']['native_attempt']=True
            with self.subTest(mutate=mutate),self.assertRaises(ValueError):m.validate_native_observations(rows,[packet])

    def test_tool_content_requires_exact_turn_phase_and_capture(self):
        m=load()
        turns=[{'turn_index':1,'tools':[{'name':'tool.get_balance','phase':'read','input':{},'output':'余额'}]}]
        rows=[{'id':'turn','name':'agent.turn','metadata':{'turn_index':1}},
              {'id':'tool','name':'tool.get_balance','parentObservationId':'turn','metadata':{'phase':'read'},'input':{},'output':'余额'}]
        self.assertTrue(m.validate_tool_observations(rows,turns))
        for field,value in [('output','wrong'),('input',{'secret':'leak'}),('parentObservationId','foreign')]:
            bad=copy.deepcopy(rows);bad[1][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):m.validate_tool_observations(bad,turns)
        missing=copy.deepcopy(rows);missing[1].pop('output')
        self.assertFalse(m.validate_tool_observations(missing,turns))
        with self.assertRaises(ValueError):m.validate_tool_observations(rows+rows[1:],turns)

    def test_settings_identity_pin_and_capture_switches(self):
        m=load()
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'synthetic-eval-settings.json'
            raw=b'{"observer":{"enabled":true,"capture_content":true,"environment":"evaluation","release":"commit","hash_key":"hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh"}}'
            path.write_bytes(raw);path.chmod(0o600)
            manifest={'private_settings_sha256':hashlib.sha256(raw).hexdigest(),'archive_commit':'commit'}
            self.assertEqual(m._projection_private_settings(manifest,path),(raw,'h'*32))
            wrong=copy.deepcopy(manifest);wrong['private_settings_sha256']='0'*64
            with self.assertRaises(ValueError):m._projection_private_settings(wrong,path)
            path.chmod(0o644)
            with self.assertRaises(ValueError):m._projection_private_settings(manifest,path)
            path.chmod(0o600)
            off=raw.replace(b'"capture_content":true',b'"capture_content":false');path.write_bytes(off)
            manifest['private_settings_sha256']=hashlib.sha256(off).hexdigest()
            with self.assertRaises(ValueError):m._projection_private_settings(manifest,path)

    def test_once_registry_retains_first_uncertain_intent(self):
        m=load()
        with tempfile.TemporaryDirectory() as directory:
            m._PUBLICATION_REGISTRY_ROOT=Path(directory).resolve()/'publications'
            handle=m._open_publication_registry('a'*64,create=True)
            try:
                m._publication_event(handle,'score.intent.json',{'outcome':'unknown'})
                with self.assertRaises(FileExistsError):m._publication_event(handle,'score.intent.json',{'outcome':'replace'})
                self.assertEqual((handle.scope/'score.intent.json').read_bytes(),b'{"outcome":"unknown"}')
            finally:handle.close()

    def test_original_negative_and_requirement_policy_on_genuine_records(self):
        m=load();root=HERE/'../../.tmp/window-v8-runtime-20261030/fixtures/window-001/records'
        if not root.is_dir():self.skipTest('genuine export not present')
        counts={}
        for p in root.glob('arm-*.json'):
            raw=p.read_bytes();doc=m._Document(raw);record=doc.value
            state=m.validate_completed_record_state(raw);counts[state]=counts.get(state,0)+1
            self.assertEqual(m._requirements(doc),record['result']['review_requirements'])
        self.assertEqual(counts,{'recorded_negative':40,'recorded_nonnegative':16})

if __name__=='__main__':unittest.main()
