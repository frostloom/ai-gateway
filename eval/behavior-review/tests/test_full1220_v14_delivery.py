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
HERE=Path(__file__).resolve().parents[1]

def load():
    p=HERE/'full1220-v14-delivery.py'
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


# The old V11 synthetic-cohort fixture cannot certify V14's original-prefix
# completion or separate signed timing approval. Genuine V14 composition and
# readback are validated by the root freeze/actual pipeline, not forged here.

class ScoreModuleABI(unittest.TestCase):
    def test_score_entry_passes_exact_registered_owner_to_remote_issuer(self):
        import types,sys
        p=HERE/'full1220-v14-scores.py';s=types.ModuleType('task4_score_abi');s.__file__=str(p);sys.modules[s.__name__]=s
        try:
            exec(compile(p.read_bytes(),str(p),'exec'),s.__dict__)
            sentinel=object();received={}
            def issue(**kw):received.update(kw);return sentinel
            actual=types.SimpleNamespace(_module=object(),_proof=types.SimpleNamespace(_cohort=object()),_completion=object())
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

class InitialReadbackPin(unittest.TestCase):
    def test_factory_rejects_mutation_between_get_and_final_snapshot(self):
        """Only cohort/catalog/semantic gates and HTTP connection are synthetic.

        Real client receives original bytes; real factory and checker must not
        establish provenance from a disk baseline first captured afterwards.
        No sockets, keys, signing or actual settings are used by this regression.
        """
        import types
        for mutation in ('none','bytes','same_bytes_replacement','root_replacement','foreign'):
            with self.subTest(mutation=mutation),tempfile.TemporaryDirectory() as temp:
                d=load();root=Path(temp).resolve();(root/'delivery-readbacks').mkdir()
                path=root/'delivery-readbacks/attempt';trace='1'*32;sid='2'*16
                binding={'manifest_sha256':'a'*64,'scope_sha256':'b'*64,'execution_sha256':'c'*64,'receipt_sha256':'d'*64,'summary_sha256':'e'*64,'content_helper_sha256':d._CONTENT_HELPER,'trace_ids':[trace]}
                original=d.wire({'data':[{'id':sid,'traceId':trace,'parentObservationId':None,'output':'original'}],'meta':{}})
                class Connection:
                    def __init__(self,*a,**kw):pass
                    def request(self,method,*a,**kw):self.method=method
                    def getresponse(self):
                        self_outer.assertEqual(self.method,'GET')
                        return types.SimpleNamespace(status=200,read=lambda limit:original)
                    def close(self):pass
                self_outer=self
                settings={'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT':'http://127.0.0.1:1/api/public/otel/v1/traces','LANGFUSE_PUBLIC_KEY':'unit','LANGFUSE_SECRET_KEY':'unit'}
                window=types.SimpleNamespace(_inputs=d.wire({k:binding[k] for k in ('manifest_sha256','receipt_sha256','summary_sha256')}))
                v=types.SimpleNamespace(completion_window_proof=lambda *a:window,check_verified_window=lambda *a:{'final_normal_cohort':True},_require_actual_context=lambda c:c,OPERATION_POLICY_SHA256='a'*64,check_verified_completion=lambda *a:{})
                cohort=types.SimpleNamespace(_context=types.SimpleNamespace(manifest={},authority_proof=types.SimpleNamespace(approval_sha256='b'*64)))
                binding.update(operation_policy_sha256='a'*64,operation_policy_approval_sha256='b'*64)
                class Projection:pass
                def validate(plan,projection,client):
                    self.assertEqual(client.observations(trace)[0]['output'],'original')
                    leaf=path/(trace+'.response')
                    if mutation=='bytes':leaf.write_bytes(original.replace(b'original',b'modified'))
                    elif mutation=='same_bytes_replacement':leaf.unlink();leaf.write_bytes(original)
                    elif mutation=='root_replacement':
                        import shutil
                        path.rename(path.with_name('retained-original'));shutil.copytree(path.with_name('retained-original'),path)
                    elif mutation=='foreign':(path/'foreign').write_bytes(b'foreign')
                    return {'all_pass':True}
                score=types.SimpleNamespace(_ContentProjection=Projection,_validate_projection_plan_binding=lambda *a:None,remote_binding=lambda *a:binding,validate_window_traces=validate)
                selected=(root,{},types.SimpleNamespace(recheck=lambda:None),{'scope_sha256':binding['scope_sha256'],'execution_sha256':binding['execution_sha256'],'spans':[(trace,sid,'')]})
                with patch.object(d,'_selected',return_value=selected),patch.object(d,'_read_settings',return_value=settings),patch.object(d.http.client,'HTTPConnection',Connection):
                    call=lambda:d.verify_remote_window(verifier=v,cohort=cohort,window_index=1,binding=binding,plan={'window_index':1},projection=Projection(),readback_root=path,score_module=score)
                    if mutation=='none':
                        proof=call();self.assertTrue(d.check_verified_remote_window(proof,binding)['all_verified'])
                        (path/(trace+'.response')).write_bytes(original+b' ')
                        with self.assertRaises(ValueError):d.check_verified_remote_window(proof,binding)
                    else:
                        with self.assertRaises(ValueError):call()
                        self.assertTrue((path/(trace+'.response')).exists())

if __name__=='__main__':unittest.main()
