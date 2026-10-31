"""Task4 isolated transport/readback contracts, never actual acquisition proof."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
HERE=Path(__file__).resolve().parent

def load():
    p=HERE/'full1220-v11-delivery.py'
    spec=importlib.util.spec_from_file_location('delivery_test',p)
    m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m

class Delivery(unittest.TestCase):
    def test_no_json_or_unfrozen_grants_credentials(self):
        m=load()
        with patch.object(m,'_read_settings',side_effect=AssertionError('credentials accessed')):
            with self.assertRaises(ValueError):m.verify_remote_window(verifier=None,cohort={'normal':True},window_index=1,binding={},plan={},projection={},readback_root='/bad',score_module=None)
            with self.assertRaises(ValueError):m.ship_batch(verifier=None,cohort={},batch_index=1)
            with self.assertRaises(ValueError):m.check_verified_remote_window({'all_verified':True},{})
    def test_exact_original_ids_and_parents(self):
        m=load();trace='1'*32;spans=[(trace,'2'*16,''),(trace,'3'*16,'2'*16)]
        rows=[{'id':'2'*16,'traceId':trace,'parentObservationId':None},{'id':'3'*16,'traceId':trace,'parentObservationId':'2'*16}]
        m._exact_coverage(spans,{trace:rows},[trace])
        for kind in ('missing','duplicate','foreign','parent','id'):
            bad=copy.deepcopy(rows)
            if kind=='missing':bad.pop()
            if kind=='duplicate':bad.append(bad[0])
            if kind=='foreign':bad[0]['traceId']='9'*32
            if kind=='parent':bad[1]['parentObservationId']='4'*16
            if kind=='id':bad[1]['id']='5'*16
            with self.subTest(kind=kind),self.assertRaises(ValueError):m._exact_coverage(spans,{trace:bad},[trace])
    def test_get_only_bounded_no_redirect_persists_original(self):
        m=load();trace='1'*32;requests=[];response={'data':[{'id':'2'*16,'traceId':trace}],'meta':{}}
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*a):pass
            def do_POST(self):raise AssertionError('POST forbidden')
            def do_GET(self):
                requests.append(self.path)
                if response.get('redirect'):
                    self.send_response(307);self.send_header('Location','/redirected');self.end_headers();return
                self.send_response(200);self.end_headers();self.wfile.write(json.dumps(response).encode())
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            with tempfile.TemporaryDirectory() as root:
                settings={'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT':f'http://127.0.0.1:{server.server_port}/api/public/otel/v1/traces','AGENT_LANGFUSE_ENVIRONMENT':'evaluation','LANGFUSE_PUBLIC_KEY':'public','LANGFUSE_SECRET_KEY':'secret'}
                client=m._ReadbackClient(settings,Path(root).resolve());self.assertEqual(client.observations(trace),response['data']);self.assertEqual(len(requests),1)
                original=(Path(root)/(trace+'.response')).read_bytes();self.assertEqual(json.loads(original),response)
                with self.assertRaises(ValueError):client.observations(trace)
            with tempfile.TemporaryDirectory() as root:
                response['redirect']=True
                with self.assertRaises(ValueError):m._ReadbackClient(settings,Path(root).resolve()).observations(trace)
                self.assertEqual(len(requests),2)
                self.assertTrue((Path(root)/(trace+'.intent.json')).exists())
                self.assertTrue((Path(root)/(trace+'.result.json')).exists())
        finally:server.shutdown();server.server_close();thread.join()
    def test_fresh_raw_identity_namespace_check(self):
        m=load()
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve();(root/'response').write_bytes(b'original')
            snap=m._Snapshot(root);snap.capture();snap.check();(root/'response').write_bytes(b'mutated!')
            with self.assertRaises(ValueError):snap.check()
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve();(root/'response').write_bytes(b'original');snap=m._Snapshot(root);snap.capture();(root/'foreign').write_bytes(b'x')
            with self.assertRaises(ValueError):snap.check()
    def test_remote_binding_and_immutable_proof(self):
        m=load()
        for value in ({},None,{'all_verified':True}):
            with self.assertRaises(ValueError):m.check_verified_remote_window(value,{})
        with self.assertRaises(ValueError):m.VerifiedRemoteWindow(None,None,None,None,None,None,None)


class IntegratedReadback(unittest.TestCase):
    def test_signed_catalog_actual_get_source_validator_opaque_proof(self):
        """Synthetic private seals replace expensive cohort acquisition ONLY.

        Catalog Ed25519 verification, chosen module/bytes, real loopback GET,
        source content validator, exact spool IDs and fresh proof are real.
        """
        import sys, types, subprocess, contextlib
        m=load()
        def module(name,path):
            mod=types.ModuleType(name);mod.__file__=str(path);sys.modules[name]=mod
            self.addCleanup(lambda:sys.modules.pop(name,None));exec(compile(path.read_bytes(),str(path),'exec'),mod.__dict__);return mod
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve()
            for name in ('verifier-preparation','score-preparation','delivery-preparation','delivery-readbacks','records','campaign','spool','source'):(root/name).mkdir(mode=0o700)
            for target,source in [('verifier-preparation/verify-window.py','full1220-v11-window.py'),('score-preparation/scores.py','full1220-v11-scores.py'),('delivery-preparation/delivery.py','full1220-v11-delivery.py')]: (root/target).write_bytes((HERE/source).read_bytes())
            # Real verifier dependencies remain the unchanged checked originals.
            v=module('task4_test_verifier',root/'verifier-preparation/verify-window.py')
            for filename,digest in v.DEPENDENCIES.values(): (root/'verifier-preparation'/filename).write_bytes((HERE/filename).read_bytes())
            s=module('task4_test_scores',root/'score-preparation/scores.py');d=module('task4_test_delivery',root/'delivery-preparation/delivery.py')
            v.FROZEN_ROOT=s.FROZEN_ROOT=d.FROZEN_ROOT=str(root);s.FROZEN_VERIFIER_SHA256=m.sha((root/'verifier-preparation/verify-window.py').read_bytes());s._cohort_verifier_module=v
            trace='1'*32;sid='2'*16;turnid='3'*16
            fields={k:'a'*64 for k in ('manifest_sha256','receipt_sha256','summary_sha256','scope_sha256','execution_sha256')}
            inputs={k:fields[k] for k in ('manifest_sha256','receipt_sha256','summary_sha256')};inputs.update(records={},sidecars={},binding_sha256='b'*64)
            source=v._SourceProof(v._SOURCE_SEAL,{'inputs':{k:val for k,val in inputs.items() if k not in ('summary_sha256','binding_sha256')}})
            window=v._VerifiedWindow(v._WINDOW_SEAL,inputs,{'window_index':1,'final_normal_cohort':True},source)
            report={'schema':'full1220-v11-final-cohort-data.v1','normal_owner_verified_separately':True,'Pairs':3660,'Arms':7320,'DefinedUserTurns':11772,'Boundaries':1851,**fields,'spans':[[trace,sid,''],[trace,turnid,sid]]}
            (root/'campaign/ledger-000001').mkdir()
            for leaf in ('claim-000001.json','checkpoint-000001.json','ledger-000001/accounting.jsonl'):(root/'campaign'/leaf).write_bytes(b'{}')
            campaign_reader,_=v._snapshot(root/'campaign')
            context=types.SimpleNamespace(reader=v.Evidence(root),source_reader=v.Evidence(root/'source'),manifest={})
            cohort=v.VerifiedV11Cohort(v._COHORT_SEAL,context,(v.Evidence(root/'records'),campaign_reader,v.Evidence(root/'spool')),[window],report);object.__setattr__(window,'_cohort',cohort)
            plan={'window_index':1,'provenance':fields,'pending_gates':[],'trace_bindings':{trace:{'expected_observed_turns':1,'original_status':'failed','original_structural_pass':False,'turn_metadata':[{'turn_index':1,'http_status':200}],'case_key':'case','pass':1,'arm':'model'}}}
            job={'rows':[{'trace_id':trace,'metadata':{'run_id':'r'}}]};payload={'rows':[{'trace_id':trace,'turns':[{'turn_index':1,'input':'原始输入','output':'原始回复','tools':[]}]}],'native_packets':[]}
            projection=s._ContentProjection(s._PROJECTION_SEAL,s._json_bytes(payload),plan,job)
            binding=s.remote_binding(plan,projection)
            rows=[{'id':sid,'traceId':trace,'parentObservationId':None,'name':'evaluation.task','environment':'evaluation','metadata':{'run_id':'r','status':'failed','structural_pass':False,'task_success':False}},{'id':turnid,'traceId':trace,'parentObservationId':sid,'name':'agent.turn','environment':'evaluation','metadata':{'run_id':'r','turn_index':1,'http_status':200},'input':'原始输入','output':'原始回复'}]
            class Handler(BaseHTTPRequestHandler):
                def log_message(self,*a):pass
                def do_GET(self):self.send_response(200);self.end_headers();self.wfile.write(m.wire({'data':rows,'meta':{}}))
            server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
            try:
                settings={'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT':f'http://127.0.0.1:{server.server_port}/api/public/otel/v1/traces','AGENT_LANGFUSE_ENVIRONMENT':'evaluation','LANGFUSE_PUBLIC_KEY':'unit-pk','LANGFUSE_SECRET_KEY':'unit-sk'}
                settings_raw=m.wire(settings);p=root/'delivery-preparation/langfuse-read-settings.json';p.write_bytes(settings_raw);p.chmod(0o600)
                context.manifest={'observer':{'endpoint_sha256':m.sha(settings['OTEL_EXPORTER_OTLP_TRACES_ENDPOINT'].encode()),'project_sha256':m.sha(b'unit-pk')}}
                cat={'schema':'full1220-v11-delivery-catalog.v1',**{k:fields[k] for k in ('manifest_sha256','scope_sha256','execution_sha256')},'verifier_sha256':s.FROZEN_VERIFIER_SHA256,'score_sha256':m.sha((root/'score-preparation/scores.py').read_bytes()),'issuer_sha256':m.sha((root/'delivery-preparation/delivery.py').read_bytes()),'helper_sha256':'c'*64,'helper_source_sha256':'d'*64,'settings_sha256':m.sha(settings_raw),'journal_root':str(root/'delivery-journal')}
                # New synthetic key only, never a real authority key or signing path.
                program="const c=require('crypto');let b=[];process.stdin.on('data',x=>b.push(x));process.stdin.on('end',()=>{const k=c.generateKeyPairSync('ed25519');console.log(JSON.stringify({key:k.publicKey.export({format:'der',type:'spki'}).subarray(12).toString('hex'),signature:c.sign(null,Buffer.concat(b),k.privateKey).toString('hex')}))});"
                signed=json.loads(subprocess.check_output([v.NODE_EXECUTABLE,'-e',program],input=m.wire(cat)));d.DELIVERY_PUBLIC_KEY=signed['key']
                (root/'delivery-preparation/catalog.json').write_bytes(m.wire({'payload':cat,'signature':signed['signature']}))
                proof=d.verify_remote_window(verifier=v,cohort=cohort,window_index=1,binding=binding,plan=plan,projection=projection,readback_root=root/'delivery-readbacks/first',score_module=s)
                self.assertTrue(d.check_verified_remote_window(proof,binding)['all_verified'])
                with self.assertRaises(AttributeError):proof._binding=b'forged'
                wrong=copy.deepcopy(binding);wrong['summary_sha256']='e'*64
                with self.assertRaises(ValueError):d.check_verified_remote_window(proof,wrong)
                path=root/'delivery-readbacks/first'/f'{trace}.response';path.write_bytes(path.read_bytes()+b' ')
                with self.assertRaises(ValueError):d.check_verified_remote_window(proof,binding)
                # A missing/changed original remote row does not mint a new proof;
                # later GET-only retries never call shipment or POST.
                rows[1]['output']='篡改回复'
                with self.assertRaises(ValueError):d.verify_remote_window(verifier=v,cohort=cohort,window_index=1,binding=binding,plan=plan,projection=projection,readback_root=root/'delivery-readbacks/changed',score_module=s)
                with patch.object(d,'_read_settings',side_effect=AssertionError('credential boundary bypass')):
                    with self.assertRaises(ValueError):d.verify_remote_window(verifier=v,cohort={'normal':True},window_index=1,binding=binding,plan=plan,projection=projection,readback_root=root/'delivery-readbacks/fake',score_module=s)
                    bad=copy.deepcopy(cat);bad['settings_sha256']='f'*64
                    (root/'delivery-preparation/catalog.json').write_bytes(m.wire({'payload':bad,'signature':signed['signature']}))
                    with self.assertRaises(ValueError):d.verify_remote_window(verifier=v,cohort=cohort,window_index=1,binding=binding,plan=plan,projection=projection,readback_root=root/'delivery-readbacks/forged',score_module=s)
            finally:server.shutdown();server.server_close();thread.join()

class ScoreModuleABI(unittest.TestCase):
    def test_score_entry_passes_exact_registered_owner_to_remote_issuer(self):
        import types,sys
        p=HERE/'full1220-v11-scores.py';s=types.ModuleType('task4_score_abi');s.__file__=str(p);sys.modules[s.__name__]=s
        try:
            exec(compile(p.read_bytes(),str(p),'exec'),s.__dict__)
            sentinel=object();received={}
            def issue(**kw):received.update(kw);return sentinel
            actual=types.SimpleNamespace(_module=object(),_proof=types.SimpleNamespace(_cohort=object()))
            with patch.object(s,'_check_actual'),patch.object(s,'_validate_projection_plan_binding'),patch.object(s,'remote_binding',return_value={'bound':True}),patch.object(s,'remote_verifier',return_value=types.SimpleNamespace(verify_remote_window=issue)),patch.object(s,'_check_remote'):
                self.assertIs(s.verify_remote_score_inputs({'window_index':511},object(),actual_proof=actual,readback_root='/unit'),sentinel)
            self.assertIs(received['score_module'],s);self.assertIs(received['verifier'],actual._module);self.assertEqual(received['window_index'],511)
        finally:sys.modules.pop(s.__name__,None)


class RangePartition(unittest.TestCase):
    def test_ordered_complete_or_stopped_range_has_no_hidden_batch(self):
        d=load();batches=[{'batch':i,'body_sha256':str(i)*64} for i in (1,2,3)]
        result={'schema':'durable-otel.delivery-range-result.v1','results':[{'schema':'durable-otel.delivery-result.v1',**batches[0],'outcome':'unknown','attempts':1}],'pending':[],'unattempted':batches[1:],'stop_reason':'context'}
        self.assertIs(d._range_result(result,batches),result)
        for kind in ('missing','reorder','duplicate','false_complete','foreign_hash'):
            value=copy.deepcopy(result)
            if kind=='missing':value['unattempted'].pop()
            elif kind=='reorder':value['unattempted'].reverse()
            elif kind=='duplicate':value['unattempted'][1]=value['unattempted'][0]
            elif kind=='false_complete':value['stop_reason']=''
            elif kind=='foreign_hash':value['results'][0]['body_sha256']='e'*64
            with self.subTest(kind=kind),self.assertRaises(ValueError):d._range_result(value,batches)

if __name__=='__main__':unittest.main()
