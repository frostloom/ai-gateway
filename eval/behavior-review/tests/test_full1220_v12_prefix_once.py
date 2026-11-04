"""Synthetic once/ACK/stored-GET integrity; these files confer no authority."""
import hashlib,json,tempfile,unittest
from pathlib import Path
from test_full1220_v12_composition import load


def wire(v):return json.dumps(v,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()
def digest(v):return hashlib.sha256(wire(v)).hexdigest()

class OriginalOnce(unittest.TestCase):
    def fixture(self,root):
        m=load('full1220-v12-window.py');s=m.dependency('prefix_scores');trace='a'*32
        b={'ordinal':1,'pass':1,'case_key':'frozen/case','arm':'model','recording_sha256':'b'*64,'expected_defined_turns':1,'expected_observed_turns':1,'positive_score_suppressed':False}
        logical={'scope_sha256':m.ORIGINAL_SCOPE_SHA256,'window_id':'window-001','pass':1,'ordinal':1,'arm':'model','trace_id':trace,'metric':'full1220.v1.original_status'}
        identifier=digest(logical);logical_text=wire(logical).decode()
        planned={'id':identifier,'logical_identity':logical_text,'traceId':trace,'name':'full1220.v1.original_status','value':'failed','dataType':'CATEGORICAL','comment':'original negative','metadata':{'pass':1,'ordinal':1,'arm':'model','original_status':'failed','original_structural_pass':False}}
        plan={'schema':'full1220-window-score-plan.v1','window_index':1,'window_id':'window-001','provenance':{'manifest_sha256':m.ORIGINAL_MANIFEST_SHA256,'scope_sha256':m.ORIGINAL_SCOPE_SHA256},'trace_bindings':{trace:b},'scores':[planned]}
        identity={k:b[k] for k in ('ordinal','pass','case_key','arm','recording_sha256','expected_defined_turns','expected_observed_turns')}
        batch={'schema':'full1220-score-batch.v1','scope_sha256':m.ORIGINAL_SCOPE_SHA256,'manifest_sha256':m.ORIGINAL_MANIFEST_SHA256,'window_id':'window-001','window_index':1,'plan_sha256':digest(plan),'trace_source_identity':{trace:identity},'scores':[{k:planned[k] for k in ('id','logical_identity','traceId','name')}]};batch['batch_sha256']=digest(batch)
        payload={k:v for k,v in planned.items() if k!='logical_identity'};payload['metadata']={**payload['metadata'],'plan_sha256':digest(plan)}
        event={'schema':'full1220-score-intent.v1','logical_identity':logical_text,'score':payload,'scope_sha256':m.ORIGINAL_SCOPE_SHA256,'window_id':'window-001','batch_sha256':batch['batch_sha256']};event['intent_sha256']=digest(event)
        files={'window-000001.batch.json':batch,'logical-'+identifier+'.intent.json':event,'score-'+identifier+'.intent.json':{'logical_sha256':identifier,'intent_sha256':event['intent_sha256']},identifier+'.sent.json':{'intent_sha256':event['intent_sha256'],'may_have_been_sent':True},identifier+'.accepted.json':{'intent_sha256':event['intent_sha256'],'ack_id':identifier}}
        for name,v in files.items():(root/name).write_bytes(wire(v))
        got={k:v for k,v in payload.items() if k!='traceId'};got['subject']={'kind':'trace','id':trace}
        return m,s,plan,[got],identifier
    def test_original_negative_once_ack_and_v3_payload_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();m,s,p,rows,_=self.fixture(root)
            self.assertEqual(m._prefix_once(p,m.Evidence(root),rows,s),(1,[]))
    def test_no_missing_ack_get_or_changed_negative_metadata(self):
        for change in ('ack','GET','value','trace','metadata','batch'):
            with self.subTest(change=change),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve();m,s,p,rows,identifier=self.fixture(root)
                if change=='ack':(root/(identifier+'.accepted.json')).unlink()
                elif change=='GET':rows=[]
                elif change=='value':rows[0]['value']='passed'
                elif change=='trace':rows[0]['subject']['id']='c'*32
                elif change=='metadata':rows[0]['metadata']['original_status']='needs_review'
                else:(root/'window-000001.batch.json').write_bytes(b'{"scores":[]}')
                with self.assertRaises((ValueError,KeyError,FileNotFoundError)):m._prefix_once(p,m.Evidence(root),rows,s)

class PositiveHole(unittest.TestCase):
    fixture=OriginalOnce.fixture
    def test_positive_hole_is_absent_from_original_registry_and_get(self):
        for present in ('neither','registry','GET'):
            with self.subTest(present=present),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve();m,s,p,rows,_=self.fixture(root)
                trace=next(iter(p['trace_bindings']));p['trace_bindings'][trace]['positive_score_suppressed']=True
                # Only the frozen input plan SHA changes in this synthetic fixture.
                batch=json.loads((root/'window-000001.batch.json').read_bytes());batch.pop('batch_sha256');batch['plan_sha256']=digest(p);batch['batch_sha256']=digest(batch)
                identifier=p['scores'][0]['id'];ep=root/('logical-'+identifier+'.intent.json');event=json.loads(ep.read_bytes());event.pop('intent_sha256');event['batch_sha256']=batch['batch_sha256'];event['score']['metadata']['plan_sha256']=digest(p);event['intent_sha256']=digest(event)
                (root/'window-000001.batch.json').write_bytes(wire(batch));ep.write_bytes(wire(event))
                (root/('score-'+identifier+'.intent.json')).write_bytes(wire({'logical_sha256':identifier,'intent_sha256':event['intent_sha256']}))
                (root/(identifier+'.sent.json')).write_bytes(wire({'intent_sha256':event['intent_sha256'],'may_have_been_sent':True}));(root/(identifier+'.accepted.json')).write_bytes(wire({'intent_sha256':event['intent_sha256'],'ack_id':identifier}))
                rows[0]['metadata']['plan_sha256']=digest(p)
                logical={'scope_sha256':m.ORIGINAL_SCOPE_SHA256,'window_id':'window-001','pass':1,'ordinal':1,'arm':'model','trace_id':trace,'metric':'full1220.v1.semantic_task'};hole=digest(logical)
                if present=='registry':(root/('logical-'+hole+'.intent.json')).write_bytes(b'{}')
                if present=='GET':rows.append({'id':hole,'subject':{'kind':'trace','id':trace}})
                if present=='neither':self.assertEqual(m._prefix_once(p,m.Evidence(root),rows,s),(1,[hole]))
                else:
                    with self.assertRaises(ValueError):m._prefix_once(p,m.Evidence(root),rows,s)
