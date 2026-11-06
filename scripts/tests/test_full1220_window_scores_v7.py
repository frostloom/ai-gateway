"""Portable v7 score compatibility; no real Go, credentials, network or registry."""
import sys
sys.dont_write_bytecode = True
import copy
import inspect
import json
import unittest
import tempfile
import subprocess
import io
from pathlib import Path
from unittest.mock import patch
import test_full1220_window_scores as old

BASELINE = 'ccf528106789daf05a61c8d8f4e5f72d3a29eea65d78a6f81afb85f4e1ea45ad'

def fixture(adapter, quality=False, zero=False):
    f, evidence = old.quality_v6_fixture(adapter, quality=quality, zero=zero)
    m=f['manifest'];scope=m['scope']
    declaration=json.loads((old.SCRIPT.parent/'tests/fixtures/full1220-v7-declarations.json').read_bytes())
    m['schema']='full-window-actual-ready.v7';scope['schema']='full-window-scope.v7'
    scope['baseline_evidence_sha256']=m['baseline_evidence']['sha256']=BASELINE
    m['historical_claims']=declaration['claims']
    m['observer']={'export_timeout_ms':10000}
    for window,cap in zip(scope['windows'],declaration['caps']):window.update(cap)
    rebind(f)
    return f,evidence

def rebind(f):
    m=f['manifest'];m['scope_sha256']=old.digest(old.wire(m['scope']))
    f['receipt']['scope_sha256']=m['scope_sha256'];f['review']['bindings']['scope_sha256']=m['scope_sha256']
    old.refresh_links(f)

class V7PureTests(unittest.TestCase):
    setUpClass=classmethod(old.WindowScorePlanTests.setUpClass.__func__)
    def plan(self,f,e):return self.adapter.build_window_score_plan(*old.arguments(f),epoch7_evidence=e)

    def test_public_declaration_and_config(self):
        f,e=fixture(self.adapter);plan=self.plan(f,e)
        self.assertEqual(plan['provenance']['epoch'],'v7')
        self.assertEqual(plan['provenance']['baseline_evidence_sha256'],BASELINE)
        self.assertEqual(plan['provenance']['historical_claims_sha256'],'2b67c23b0b3b6c48e676fff5dfbc3587ab5f2432e96acb3824f931130fd75ede')
        c=plan['provenance']['configured_cache_accounting']
        self.assertEqual((c['model_total_cap_micro_cny'],c['jev_total_cap_micro_usd']),(84500000,600000))
        self.assertFalse(c['proves_cache_usage']);self.assertFalse(c['proves_knowledge_usage'])
        self.assertEqual(c['cache_absent_settlement'],'all_input_at_miss_rate_without_observed_cache_split')
        self.assertFalse(plan['publish_ready'])

    def test_changed_epoch_approval_claim_caps_rates_timeout_rejected(self):
        for mode in ('epoch','baseline','approval','claim','caps','cache','jev','timeout','knowledge'):
            f,e=fixture(self.adapter);m=f['manifest']
            if mode=='epoch':m['scope']['schema']='full-window-scope.v6'
            elif mode=='baseline':m['scope']['baseline_evidence_sha256']=m['baseline_evidence']['sha256']='b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1'
            elif mode=='approval':m['scope']['budget_approval_sha256']='0'*64
            elif mode=='claim':m['historical_claims'].pop('full1220-v6-20261029/window-000001')
            elif mode=='caps':m['scope']['windows'][0]['model_cap_micro_cny']+=1
            elif mode=='cache':m['provider_limits']['model'].pop('cache_hit_input_micro_per_million')
            elif mode=='jev':m['provider_limits']['jev']['cache_hit_input_micro_per_million']=1
            elif mode=='timeout':m['observer']['export_timeout_ms']=2000
            else:m['knowledge_jev']='on'
            rebind(f)
            with self.subTest(mode=mode),self.assertRaises(ValueError):self.plan(f,e)

    def test_explicit_epoch_evidence_pairing(self):
        f,e=fixture(self.adapter)
        with self.assertRaises(ValueError):self.adapter.build_window_score_plan(*old.arguments(f),epoch6_evidence=e)
        with self.assertRaises(ValueError):self.adapter.build_window_score_plan(*old.arguments(f),epoch6_evidence=e,epoch7_evidence=e)
        f,e=old.quality_v6_fixture(self.adapter,quality=False)
        with self.assertRaises(ValueError):self.adapter.build_window_score_plan(*old.arguments(f),epoch7_evidence=e)

    def test_no_quality_is_noop_without_helper_runtime_or_filesystem(self):
        f,e=fixture(self.adapter);args=old.arguments(f)
        with patch.object(self.adapter,'_regular_bytes',side_effect=AssertionError('filesystem accessed')),patch.object(self.adapter.subprocess,'run',side_effect=AssertionError('process')):
            self.assertIsNone(self.adapter.verify_window_quality_v7(args[0],args[2],args[3],args[5],epoch7_evidence=e,source_root=None,output_dir=None,runtime=None,execute=True))

    def test_zero_nonzero_owned_quality_remains_pending_negative(self):
        for zero in (False,True):
            f,e=fixture(self.adapter,quality=True,zero=zero)
            with patch.object(self.adapter.subprocess,'run',side_effect=AssertionError('process')):plan=self.plan(f,e)
            self.assertEqual(plan['provenance']['quality_v7_source_replay'],'pending')
            self.assertEqual(plan['rows'][0]['strict_task_verdict'],'failed')
            self.assertNotIn('quality_v6_inputs_sha256',plan['provenance'])
            trace=plan['rows'][0]['trace_id'];self.assertFalse(any(s['value']=='passed' for s in plan['scores'] if s['traceId']==trace))
            for fake in (None,{'verified':True,'kind':'quality_v7'}):
                with patch.object(self.adapter,'_open_publication_registry',side_effect=AssertionError('registry')),self.assertRaises(ValueError):
                    self.adapter.publish_window_scores(plan,None,None,None,None,execute=True,quality_proof=fake)

    def test_v7_helpers_separate_and_legacy_bytes_unchanged(self):
        root=old.SCRIPT.parent/'helpers'
        for name,digest in [('full1220_quality_v6_test.go.txt','c8aae02c9bf7f27d6aa7091bb94286453c4e5c2f604e78bd896c1c0f2ebba458'),('full1220_attestation_v6.go.txt','c4c9b998d7b549b2cebe598e6ef87de0aa6534ea510a9d7c4a766db40ba36b92'),('full1220_attestation.go.txt','cadae5ad17b7233b97ba9d6d24029365e10dc280223b4991987d6ca31ea58b63')]:
            self.assertEqual(old.digest((root/name).read_bytes()),digest)
        for oldname,newname,pin in [('full1220_quality_v6_test.go.txt','full1220_quality_v7_test.go.txt',self.adapter._QUALITY_V7_HELPER_SHA256),('full1220_attestation_v6.go.txt','full1220_attestation_v7.go.txt',self.adapter._ATTESTATION_V7_HELPER_SHA256)]:
            raw=(root/newname).read_bytes();self.assertEqual(old.digest(raw),pin)
            self.assertEqual(raw,(root/oldname).read_bytes().replace(b'V6',b'V7').replace(b'v6',b'v7'))
        self.assertIn('epoch7_evidence',inspect.signature(self.adapter.verify_window_quality_v7).parameters)

    def test_quality_helper_pin_fails_before_source_runtime(self):
        f,e=fixture(self.adapter,quality=True);args=old.arguments(f);args[5]['quality_helper_sha256']='0'*64
        with patch.object(self.adapter,'_regular_bytes',side_effect=AssertionError('helper read')),self.assertRaises(ValueError):
            self.adapter.verify_window_quality_v7(args[0],args[2],args[3],args[5],epoch7_evidence=e,source_root=None,output_dir=None,runtime=None,execute=True)

    def test_legacy_plan_bytes_unchanged(self):
        snapshots={'v2':'da0f9b2bb04ae9cfa0d5bb2110de385d4f59521a8a15990885269f080767d4cb','v4':'66105d3489ca14dcf316ab79194642c3c374d2ce03c7c07f0229d49c178a412a','v5':'f4194ec039221a9f055b38cb4799c83cea8182ae5a8e4f148b7b3af657172d03','v6-False-False':'ffa0025ad4680e41e4fa72f5987ee26d7035c5697b062ed28f41959c86b4dedf','v6-True-False':'37c43bffff539a7b20fc00595f3c14fd096d26de9d7d9045454bdd2641e721e4','v6-True-True':'f9dfb0221475fc8dfe4236bcaf32b35eac23c557337f08eb359eaa2b7e627ff6'}
        cases=[('v2',old.fixture(),None),('v4',old.v4_fixture(),None),('v5',old.v5_fixture(),None)]+[('v6-'+str(q)+'-'+str(z),*old.quality_v6_fixture(self.adapter,quality=q,zero=z)) for q,z in [(False,False),(True,False),(True,True)]]
        for name,f,e in cases:
            self.assertEqual(old.digest(self.adapter._json_bytes(self.adapter.build_window_score_plan(*old.arguments(f),epoch6_evidence=e))),snapshots[name])


    def test_explicit_cli_publication_forwards_evidence_and_quality_runtime(self):
        f,evidence=fixture(self.adapter,quality=True);f['manifest']['source_root']='/owned/frozen';old.refresh_links(f)
        inputs=old.arguments(f);plan=self.adapter.build_window_score_plan(*inputs,epoch7_evidence=evidence)
        with tempfile.TemporaryDirectory() as folder:
            path=Path(folder)/'attestation.json';path.write_bytes(b'{}')
            job={'chain':[],'attestation_path':str(path),'attestation_sha256':'a'*64,'binary_path':'/unused',
                 'runtime':{'sdk_settings_sha256':'b'*64},'attestation_output':'/owned/signature',
                 'projection_output':'/owned/projection','quality_output':'/owned/quality','_epoch7_evidence':evidence}
            with patch.object(self.adapter,'_read_score_job',return_value=(job,inputs,plan)), \
                 patch.object(self.adapter,'verify_window_receipt_chain'), patch.object(self.adapter,'verify_window_attestation'), \
                 patch.object(self.adapter,'verify_window_quality_v7') as quality, \
                 patch.object(self.adapter,'project_window_content',side_effect=ValueError('stop before client')) as projection, \
                 patch.object(self.adapter,'load_window_score_client',side_effect=AssertionError('client touched')), \
                 patch('sys.stderr',new_callable=io.StringIO):
                self.assertEqual(self.adapter.main(['publish','--execute','--job','owned']),2)
            self.assertIs(quality.call_args.kwargs['epoch7_evidence'],evidence)
            self.assertEqual(quality.call_args.args,(inputs[0],inputs[2],inputs[3],inputs[5]))
            self.assertEqual(quality.call_args.kwargs['output_dir'],job['quality_output'])
            self.assertIs(quality.call_args.kwargs['runtime'],job['runtime'])
            self.assertTrue(quality.call_args.kwargs['execute'])
            self.assertIs(projection.call_args.kwargs['epoch7_evidence'],evidence)

    def test_sidecars_envelope_case_and_record_mutations_rejected(self):
        for fault in ('missing','sha','cleanup','unknown','case','envelope','extra'):
            f,e=fixture(self.adapter,quality=True);row=f['receipt']['rows'][0];q=row['quality_end']
            if fault=='missing':e['sidecars'].pop(q['cleanup_file'])
            elif fault=='sha':e['sidecars'][q['candidate_file']]+=b' '
            elif fault=='extra':e['sidecars']['extra.json']=b'{}'
            elif fault=='envelope':e['envelope_raw']=old.wire({'basis':[]})
            else:
                key='cleanup' if fault=='cleanup' else 'candidate';value=json.loads(e['sidecars'][q[key+'_file']])
                if fault=='cleanup':value['session_absent']=False
                elif fault=='case':value['evidence']['record']['result']['case']['source_case_id']='foreign'
                else:
                    old.refresh_record(f,0,lambda record:record['issues'][0].update(code='unknown'))
                    value['evidence']['record']=json.loads(f['records'][row['recording_file']])
                e['sidecars'][q[key+'_file']]=old.wire(value);q[key+'_sha256']=old.digest(e['sidecars'][q[key+'_file']]);old.refresh_links(f)
            with self.subTest(fault=fault),self.assertRaises(ValueError):self.plan(f,e)

    def test_score_job_reader_loads_exact_epoch7_evidence(self):
        f,e=fixture(self.adapter,quality=True);args=old.arguments(f)
        job={'schema':'full1220-window-score-job.v1','manifest_path':'/manifest','summary_path':'/summary','receipt_path':'/receipt','review_path':'/review','records_dir':'/records','expected_hashes':args[5]}
        files={'/job':old.wire(job),'/manifest':args[0],'/summary':args[1],'/receipt':args[2],'/review':args[4],f['manifest']['envelope']['path']:e['envelope_raw']}
        files.update({'/records/'+name:raw for name,raw in {**args[3],**e['sidecars']}.items()})
        with patch.object(self.adapter,'_regular_bytes',side_effect=lambda path,*unused:files[Path(path).as_posix()]):
            loaded,inputs,plan=self.adapter._read_score_job('/job')
        self.assertEqual(loaded['_epoch7_evidence'],e);self.assertNotIn('_epoch6_evidence',loaded)
        self.assertEqual(plan,self.plan(f,e));self.assertEqual(inputs,args)

    def test_legacy_v3_bytes_and_v1_rejection_unchanged(self):
        f=old.fixture();m=f['manifest'];m['schema']='full-window-actual-ready.v3';m['scope']['schema']='full-window-scope.v3';rebind(f)
        self.assertEqual(old.digest(self.adapter._json_bytes(self.adapter.build_window_score_plan(*old.arguments(f)))),'3edbaa382494a6b6483f4a7c165299c7e4ac9a20495a5f2776b2a0a88bb2229e')
        m['schema']='full-window-actual-ready.v1';m['scope']['schema']='full-window-scope.v1';rebind(f)
        with self.assertRaises(ValueError):self.adapter.build_window_score_plan(*old.arguments(f))


class V7FixedRunnerTests(unittest.TestCase):
    setUpClass=classmethod(old.WindowScorePlanTests.setUpClass.__func__)
    def test_mocked_fixed_process_output_and_mutation_guards(self):
        # Synthetic records have no valid SQL capture. These mocks test only
        # orchestration/sealing, never claim a genuine source-owned Go proof.
        for fault in ('valid','exit','wrong-epoch','missing-receipt','binding','record'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as folder:
                root=Path(folder).resolve();source=root/'source';source.mkdir()
                name='internal/taskbench/conversation_runner.go';target=source/name;target.parent.mkdir(parents=True);target.write_bytes(b'package taskbench\n')
                go=root/'go';go.write_bytes(b'fake pinned executable');go.chmod(0o700)
                sdk=root/'MacOSX26.5.sdk';sdk.mkdir();(sdk/'SDKSettings.json').write_bytes(b'{}')
                modules=root/'modules';modules.mkdir();output=root/'proof'
                f,e=fixture(self.adapter,quality=True);f['manifest']['source_root']=str(source);f['manifest']['sources']={name:old.digest(target.read_bytes())};rebind(f)
                args=old.arguments(f);args[5]['quality_helper_sha256']=self.adapter._QUALITY_V7_HELPER_SHA256
                runtime={'go_executable':str(go),'go_sha256':old.digest(go.read_bytes()),'sdk_settings_sha256':old.digest(b'{}'),'module_cache':str(modules)}
                def process(command,**kwargs):
                    self.assertIn('^TestFull1220QualityV7$',command);self.assertEqual(kwargs['env']['GOPROXY'],'off')
                    self.assertEqual((output/'source'/self.adapter._QUALITY_V7_HELPER_RELATIVE).read_bytes(),(old.SCRIPT.parent/'helpers/full1220_quality_v7_test.go.txt').read_bytes())
                    raw=(output/'input.json').read_bytes();job=json.loads(raw)
                    result={'schema':'full1220-quality-v7-replay.v1','input_sha256':old.digest(raw),'binding':job['binding']}
                    if fault=='binding':result['binding']['receipt_sha256']='0'*64
                    if fault=='wrong-epoch':result['schema']='full1220-quality-v6-replay.v1'
                    body=old.wire(result);(output/'quality.json').write_bytes(body)
                    if fault=='record':
                        key=next(iter(args[3]));(output/'records'/key).write_bytes(args[3][key]+b' ')
                    stdout=b'' if fault=='missing-receipt' else ('    full1220_quality_v7_test.go:99: FULL1220_QUALITY_OUTPUT_SHA256='+old.digest(body)+'\n').encode()
                    return subprocess.CompletedProcess(command,1 if fault=='exit' else 0,stdout,b'')
                with patch.object(self.adapter,'_PROJECTION_SDKROOT',str(sdk)),patch.object(self.adapter,'_quality_v6_source_identity',return_value={'mocked_identity':'test only'}),patch.object(self.adapter.subprocess,'run',side_effect=process):
                    def run():return self.adapter.verify_window_quality_v7(args[0],args[2],args[3],args[5],epoch7_evidence=e,source_root=str(source),output_dir=str(output),runtime=runtime,execute=True)
                    if fault=='valid':
                        proof=run();self.assertIs(type(proof),self.adapter._PublicationProof);self.assertEqual(proof._kind,'quality_v7');self.assertIn('quality_v7_inputs_sha256',proof.as_dict())
                    else:
                        with self.assertRaises(ValueError):run()

class V7PublicationBoundaryTests(unittest.TestCase):
    setUpClass=classmethod(old.PublicationTests.setUpClass.__func__)
    setUp=old.PublicationTests.setUp
    attest=old.PublicationTests.attest
    pub_api=old.PublicationTests.pub_api
    api=old.ContentProjectionTests.api
    fake_go=old.ContentProjectionTests.fake_go
    client=old.ContentProjectionTests.client
    bound_client=old.PublicationTests.bound_client

    def gates(self):
        f,e=fixture(self.adapter,quality=True)
        for key in ('source_root','sources','observer','build_attestation_public_key'):f['manifest'][key]=copy.deepcopy(self.f['manifest'][key])
        f['manifest']['observer']['export_timeout_ms']=10000;f['summary']['last_receipt_sha256']='';old.refresh_links(f);self.f=f
        args=list(old.arguments(f));args[-1]['projection_helper_sha256']=self.adapter._PROJECTION_HELPER_SHA256
        plan=self.adapter.build_window_score_plan(*args,epoch7_evidence=e)
        runtime={'go_executable':str(self.go),'go_sha256':old.digest(self.go.read_bytes()),'module_cache':str(self.cache)}
        with patch.object(self.adapter.subprocess,'run',side_effect=self.fake_go):
            projection=self.adapter.project_window_content(*args,source_root=str(self.source),output_dir=str(self.root/'quality-projection'),runtime=runtime,execute=True,epoch7_evidence=e)
        entries=[{'summary_raw':args[1],'receipt_raw':args[2]}];pins=[{key:args[-1][key] for key in ('summary_sha256','receipt_sha256')}]
        return plan,projection,self.attest(f),self.bound_client(plan,projection),args,entries,pins,e

    def test_v7_attestation_selects_new_fixed_helper_and_rejects_mixed(self):
        f=copy.deepcopy(self.f);f['manifest']['schema']='full-window-actual-ready.v7';f['manifest']['scope']['schema']='full-window-scope.v7'
        self.assertEqual(self.attest(f).as_dict()['helper_sha256'],self.adapter._ATTESTATION_V7_HELPER_SHA256)
        f['manifest']['scope']['schema']='full-window-scope.v6'
        with self.assertRaises(ValueError):self.attest(f)

    def test_quality_proof_kind_mutation_and_removed_markers_reject_before_registry(self):
        plan,projection,attestation,client,args,entries,pins,e=self.gates()
        _,_,_,binding=self.adapter._quality_v7_inputs(args[0],args[2],args[3],args[5],e)
        data={**binding,'quality_v7_inputs_sha256':old.digest(self.adapter._json_bytes(binding))}
        for kind in ('quality_v6','quality_v7'):
            altered=copy.deepcopy(data)
            if kind=='quality_v7':altered['rows'][0]['quality_end']['cleanup_sha256']='0'*64
            proof=self.adapter._PublicationProof(self.adapter._PUBLICATION_SEAL,kind,altered)
            with patch.object(self.adapter,'_open_publication_registry',side_effect=AssertionError('registry')),self.assertRaises(ValueError):
                self.adapter.publish_window_scores(plan,projection,None,attestation,client,execute=True,quality_proof=proof)
        for mode in ('quality','epoch','both'):
            changed=copy.deepcopy(plan)
            if mode in ('quality','both'):
                for row in changed['rows']:row.pop('quality_end',None)
            if mode in ('epoch','both'):changed['provenance'].pop('epoch')
            chain=self.adapter.verify_window_receipt_chain(changed,args[0],entries,pins)
            with patch.object(self.adapter,'_open_publication_registry',side_effect=AssertionError('registry')),self.assertRaisesRegex(ValueError,'Projection belongs to another or modified plan'):
                self.adapter.publish_window_scores(changed,projection,chain,attestation,client,execute=True)

    def test_test_only_sealed_quality_batch_retains_epoch7_binding(self):
        plan,projection,attestation,client,args,entries,pins,e=self.gates()
        _,_,_,binding=self.adapter._quality_v7_inputs(args[0],args[2],args[3],args[5],e)
        data={**binding,'quality_v7_inputs_sha256':old.digest(self.adapter._json_bytes(binding))}
        proof=self.adapter._PublicationProof(self.adapter._PUBLICATION_SEAL,'quality_v7',data)
        chain=self.adapter.verify_window_receipt_chain(plan,args[0],entries,pins)
        result=self.adapter.publish_window_scores(plan,projection,chain,attestation,client,execute=True,quality_proof=proof)
        self.assertTrue(result['all_verified'])
        batch=json.loads((self.registry/plan['provenance']['scope_sha256']/'window-000001.batch.json').read_bytes())
        self.assertEqual(batch['quality_v7_proof'],data);self.assertNotIn('quality_v6_proof',batch)

if __name__=='__main__':unittest.main()
