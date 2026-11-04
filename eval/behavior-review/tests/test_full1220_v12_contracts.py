"""Synthetic shape codec only. No signature/authority claim."""
import copy,json,hashlib,unittest
from test_full1220_v12_window import load,HERE
class Contracts(unittest.TestCase):
 def setUp(self):
  self.m=load('contracts');self.a=json.loads((HERE/'fixtures/v9-contracts/go-delegated-authority.json').read_bytes())['authorization']
  self.a['schema']='full-window-delegated-execution-authorization.v12';self.a['budget_declaration_sha256']=self.m._BUDGET_SHA;self.a['retry_policy_sha256']=self.m._RETRY_SHA;self.a['completion_sha256']=self.m._COMPLETION_SHA;self.a['executor_equivalence_sha256']='f'*64;self.a['parent']={'model_micro_cny':195000000,'jev_micro_usd':3000000};self.a['private_settings_sha256']='a'*64;self.a['spool_root']='/controlled/spool'
  self.a['observer']={'observation_receipt_boundary':'durable_local','spool_root_sha256':hashlib.sha256(b'/controlled/spool').hexdigest(),'spool_run_id':'b'*64,'spool_max_bytes':4<<30,'enabled':True,'capture_content':True,'sample_rate':1,'environment':'evaluation','release':'c'*40,'export_timeout_ms':10000,'queue_size':4096,'endpoint_sha256':'d'*64,'project_sha256':'e'*64}
 def test_shape_only_not_authority(self):
  parsed=self.m.parse_execution_authorization(json.dumps(self.a).encode());self.assertEqual(parsed,self.a)
  self.assertNotIn(b'signature',self.m.authorization_signing_bytes(json.dumps(self.a).encode()))
 def test_observer_and_old_epoch_rejected(self):
  for mutate in (lambda a:a.update(schema='full-window-delegated-execution-authorization.v10'),lambda a:a['observer'].update(sample_rate=True),lambda a:a['observer'].update(queue_size=4097),lambda a:a['observer'].update(spool_max_bytes=1),lambda a:a.update(spool_root='/foreign'),lambda a:a['observer'].update(observation_receipt_boundary='remote'),lambda a:a['observer'].update(untrusted=True)):
   a=copy.deepcopy(self.a);mutate(a)
   with self.assertRaises(ValueError):self.m.parse_execution_authorization(json.dumps(a).encode())
 def test_duplicates_nan_signature_and_scope_review(self):
  for raw in (json.dumps(self.a).encode()[:-1]+b',"schema":"duplicate"}',json.dumps({**self.a,'human_reviewed_exact_scope':True}).encode(),json.dumps({**self.a,'signature':'0'*2}).encode()):
   with self.assertRaises(ValueError):self.m.parse_execution_authorization(raw)
if __name__=='__main__':unittest.main()



class GenuineGoAuthorityData(unittest.TestCase):
 def test_actual_go_struct_canonical_bytes_without_authority_claim(self):
  m=load('contracts');vector=json.loads((HERE/'fixtures/v12-contracts/go-authority-data.json').read_bytes())
  self.assertIs(vector['data_only'],True)
  raw=json.dumps(vector['authorization']).encode()
  self.assertEqual(m.authorization_signing_bytes(raw),vector['signing_bytes'].encode())
  self.assertEqual(hashlib.sha256(m.authorization_signing_bytes(raw)).hexdigest(),vector['signing_sha256'])
  self.assertEqual(vector['authorization']['signature'],'0'*128)
