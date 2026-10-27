import copy
import json
import tempfile
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('full_local_matrix', ROOT / 'scripts/full-customer-local-matrix.py')
m = importlib.util.module_from_spec(spec)

class FullLocalMatrixTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec.loader.exec_module(m)
        cls.plan = m.prepare(ROOT)

    def test_all_sources_once_and_unrun_preserved(self):
        p = self.plan
        self.assertEqual([len(w['cases']) for w in p['windows']], [100]*12+[20])
        self.assertEqual(len({r['key'] for w in p['windows'] for r in w['cases']}), 1220)
        r = m.aggregate(p, [])
        self.assertEqual(r['status'], 'incomplete_local')
        self.assertEqual(r['unrun_cases'], 1220)
        self.assertFalse(r['actual_full1220_complete'])
        self.assertEqual(r['external_model_calls'], 0)

    def test_failure_preserves_denominator_and_forbids_continuation(self):
        with tempfile.TemporaryDirectory() as temp:
            artifact=Path(temp)/'local.bin';artifact.write_bytes(b'local-test')
            row={'window_id':'window-001','return_code':1,'execution_revision':'a'*40,
                 **{k:m.reference(artifact) for k in ['binary','stdout','stderr']}}
            r=m.aggregate(self.plan,[row])
            self.assertEqual((r['checked_local_pairs'],r['failed_window_unverified_cases'],r['unrun_cases']),(0,100,1120))
            with self.assertRaises(ValueError):
                m.aggregate(self.plan,[row,{**row,'window_id':'window-002'}])

    def test_plan_tamper_rejected(self):
        for mutate in ['duplicate', 'turns', 'identity', 'paid']:
            p = copy.deepcopy(self.plan)
            if mutate == 'duplicate': p['windows'][1]['cases'][0] = p['windows'][0]['cases'][0]
            elif mutate == 'turns': p['windows'][0]['cases'][0]['turns'] = 9
            elif mutate == 'identity': p['selection_sha256'] = 'a'*64
            else: p['allow_paid'] = True
            with self.assertRaises(ValueError): m.aggregate(p, [])

    def test_raw_window_binding_and_unknown_semantic_status(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            c = self.plan['windows'][0]['cases'][0]
            window = {'id':'window-001','cases':[c]}
            binding = {k:self.plan[k] for k in ['selection_scope','selection_sha256','definition_sha256','manifest_sha256','profile_sha256']}
            revision = 'a'*40
            original_spec = importlib.util.spec_from_file_location('frozen_source_test',ROOT/'scripts/prepare-full-customer-pilot.py')
            source = importlib.util.module_from_spec(original_spec);original_spec.loader.exec_module(source)
            selected = source.select_primary(source.load_definitions(ROOT))
            source_row,scenario=next((row,sc) for row,sc in selected if row['source_case_id']+'/'+sc['id']==c['key'])
            case=m.typed_case(source_row,scenario)
            runtime={**m.RUNTIME_SHA,'execution_revision':revision,'evidence_kind':'local_stub','model':'local-full-customer',
                     'provider_protocol':'anthropic','output_budget':128,'thinking_mode':'disabled',
                     'definition_sha256':binding['definition_sha256'],'manifest_sha256':binding['manifest_sha256'],
                     'profile_sha256':{c['profile']:binding['profile_sha256']},'routing_mode':'model'}
            cfg = {'suite':{'cases':[case]}, 'selection_scope':binding['selection_scope'],
                   'selection_sha256':binding['selection_sha256'],'allow_paid':False,
                   'model':runtime,'selective':{**runtime,'routing_mode':'jev_selective'},'arm_order':['model','jev_selective']}
            contract=m.sha(m.canonical(cfg))
            m.validate_configuration(self.plan,c,cfg,revision,contract)
            for mutation in ['user','revision','contract','prompt']:
                badcfg=copy.deepcopy(cfg)
                if mutation=='user':badcfg['suite']['cases'][0]['scenario']['turns'][0]['user']='changed input'
                if mutation=='revision':badcfg['model']['execution_revision']='f'*40
                if mutation=='prompt':
                    badcfg['model']['prompt_sha256']='f'*64
                    badcfg['selective']['prompt_sha256']='f'*64
                badcontract=m.sha(m.canonical(badcfg)) if mutation!='contract' else 'f'*64
                with self.assertRaises(ValueError):m.validate_configuration(self.plan,c,badcfg,revision,badcontract)
            journal = [{'kind':'config','config':cfg}]
            for i,a in enumerate(['model','jev_selective']):
                journal += [{'kind':'start','key':c['key'],'arm':a},
                            {'kind':'finish','key':c['key'],'arm':a,'langfuse_trace_id':str(i+1)*32,
                             'budget_before':{'attempts':2*i},'budget_after':{'attempts':2*i+2},
                             'result':{'status':'needs_review','success':False,'structural_pass':True,
                                       'observations':[{}]*c['turns'],'http_failures':0,'setup_error':False}}]
            def write(path, value):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(json.dumps(value))
                return m.reference(path)
            batchdir = directory/'campaign/batch-001'
            batchdir.mkdir(parents=True)
            raw = b'\n'.join(json.dumps(e).encode() for e in journal)+b'\n'
            (batchdir/'run.jsonl').write_bytes(raw)
            report = {'evidence_kind':'local_stub','effect_claims_allowed':False,'planned_cases':1,'planned_arms':2,'finished_arms':2,'unrun_arms':0,'journal_sha256':m.sha(raw), 'rows':[{'key':c['key'],'arm':a,'status':'needs_review','structural_pass':True,'fact_errors':0,'http_failures':0,'observed_turns':c['turns'],'langfuse_trace_id':str(i+1)*32,'guarded_upstream_model_attempts':2} for i,a in enumerate(['model','jev_selective'])]}
            rr = write(batchdir/'report.json',report)
            declaration = {'arm_order':['model','jev_selective'],'id':'batch-001','key':c['key'],'configuration':cfg,'pair_run_contract':contract}
            wp = {**binding,'schema':'full-primary-local-window-plan.v1','execution_revision':revision,'allow_paid':False,'planned_user_turns':2*c['turns'],'batches':[declaration]}
            receipt = {**binding,'schema':'full-primary-local-window-receipt.v1','evidence_kind':'local_stub','execution_revision':revision,'status':'completed_local','completed_batches':1,'allow_paid':False,'ready_for_paid_run':False,'actual_full1220_complete':False,'external_model_calls':0,'external_jev_calls':0,'planned_cases':1,'planned_arms':2,'planned_user_turns':2*c['turns'],'synthetic_model_attempts':4,'checked_local_otlp':{'tasks':2,'turns':2*c['turns'],'generations':4},'batches':[{'arm_order':['model','jev_selective'],'id':'batch-001','key':c['key'],'status':'checked_local','pair_run_contract':contract,'report':report,'report_sha256':rr['sha256'],'journal_sha256':m.sha(raw)}]}
            spans = []
            for row in report['rows']:
                for name, count in [('evaluation.task',1),('agent.turn',c['turns']),('llm.attempt',2)]:
                    for _ in range(count):
                        spans.append({'span_id':f'{len(spans)+1:016x}','trace_id':row['langfuse_trace_id'],'name':name,'metadata':{'case_key':c['key'],'arm':row['arm'],'run_id':contract,'evidence_kind':'local_stub',**({'task_success':'unknown'} if name=='evaluation.task' else {})}})
            capture_ref = write(directory/'local-observations.json',{'spans':spans})
            receipt['local_otlp_sha256'] = capture_ref['sha256']
            ev = {'execution_revision':revision,'plan':write(directory/'plan.json',wp),'receipt':write(directory/'receipt.json',receipt)}
            rows, count, traces = m.checked_window(self.plan,window,ev)
            self.assertEqual((len(rows),count,len(traces)),(1,4,2))
            for field,value in [('actual_full1220_complete',True),('external_model_calls',1),('status','stopped'),('completed_batches',0),('selection_sha256','f'*64)]:
                bad = copy.deepcopy(receipt);bad[field]=value
                ev['receipt']=write(directory/'receipt.json',bad)
                with self.assertRaises(ValueError):m.checked_window(self.plan,window,ev)
            ev['receipt']=write(directory/'receipt.json',receipt)
            for field,value in [('langfuse_trace_id','f'*32),('execution_failures',[{'stage':'cleanup'}]),
                                ('result',{'status':'failed','success':False,'structural_pass':False,'observations':[]}),
                                ('budget_after',{'attempts':100})]:
                badjournal=copy.deepcopy(journal);badjournal[2][field]=value
                badraw=b'\n'.join(json.dumps(e).encode() for e in badjournal)+b'\n'
                (batchdir/'run.jsonl').write_bytes(badraw)
                badreport=copy.deepcopy(report);badreport['journal_sha256']=m.sha(badraw)
                badreportref=write(batchdir/'report.json',badreport)
                badreceipt=copy.deepcopy(receipt)
                badreceipt['batches'][0].update(report=badreport,report_sha256=badreportref['sha256'],journal_sha256=m.sha(badraw))
                ev['receipt']=write(directory/'receipt.json',badreceipt)
                with self.assertRaises(ValueError):m.checked_window(self.plan,window,ev)
            # Ordinary SHA links and contracts cannot hide substantive source changes.
            for mutation in ['user','revision','contract','prompt']:
                badcfg=copy.deepcopy(cfg)
                if mutation=='user':badcfg['suite']['cases'][0]['scenario']['turns'][0]['user']='changed input'
                if mutation=='revision':badcfg['model']['execution_revision']='f'*40
                if mutation=='prompt':
                    badcfg['model']['prompt_sha256']='f'*64
                    badcfg['selective']['prompt_sha256']='f'*64
                badplan=copy.deepcopy(wp);badplan['batches'][0]['configuration']=badcfg
                badcontract=m.sha(m.canonical(badcfg)) if mutation!='contract' else 'f'*64
                badplan['batches'][0]['pair_run_contract']=badcontract
                ev['plan']=write(directory/'plan.json',badplan)
                with self.assertRaises(ValueError):m.checked_window(self.plan,window,ev)
            ev['plan']=write(directory/'plan.json',wp)
            write(batchdir/'report.json',report)
            ev['receipt']=write(directory/'receipt.json',receipt)
            (batchdir/'run.jsonl').write_bytes(raw+b' ')
            with self.assertRaises(ValueError):m.checked_window(self.plan,window,ev)

    def test_missing_bad_duplicate_or_reordered_evidence_rejected(self):
        for ev in [[{'window_id':'window-001'}], [{'window_id':'window-002'}],
                   [{'window_id':'window-001','return_code':0,'receipt':{'path':'/missing','sha256':'0'*64}}]]:
            with self.assertRaises(ValueError): m.aggregate(self.plan, ev)

if __name__ == '__main__': unittest.main()
