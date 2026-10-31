import copy,json,hashlib,unittest
from test_full1220_v11_window import load,HERE
class Contracts(unittest.TestCase):
 def setUp(self):
  self.m=load('contracts');self.a=json.loads((HERE/'fixtures/v9-contracts/go-delegated-authority.json').read_bytes())['authorization']
  self.a['schema']='full-window-delegated-execution-authorization.v11';self.a['budget_declaration_sha256']=self.m._BUDGET_SHA;self.a['parent']={'model_micro_cny':195000000,'jev_micro_usd':3000000};self.a['private_settings_sha256']='a'*64;self.a['spool_root']='/controlled/spool'
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


GO_VECTOR_AUTHORIZATION_RAW=b'{"schema":"full-window-delegated-execution-authorization.v11","human_authorized":true,"retry_policy_approved":true,"human_reply":{"path":"/private/var/folders/sw/_x387kld25bgsp22s4n5rfjm0000gn/T/TestFull1220V11CodecVector512905759/001/window_v9_budget_approval.json","sha256":"22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57"},"manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_sha256":"2ad4bf3b43ad0cbf5942e45e97ce3d99b03de3a88957ac634f5267f2ee19fc6e","binary_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","scope_sha256":"72dc7e9d4178b9af2c6cc7b3e219849d741614e76e7092412d43299657b95831","execution_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","budget_declaration_sha256":"7e67e78d996109ca54df699be14c2b02956681e3891218a4da11c3cbf6e00678","retry_policy_sha256":"6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63","parent":{"model_micro_cny":195000000,"jev_micro_usd":3000000},"owner_id":"synthetic-test-owner","single_owner":true,"no_relaunch":true,"autonomous_authorization":{"path":"/private/var/folders/sw/_x387kld25bgsp22s4n5rfjm0000gn/T/TestFull1220V11CodecVector512905759/001/window_v9_autonomous_authorization.json","sha256":"8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95"},"global_budget":{"model_micro_cny":200000000,"jev_micro_usd":4000000},"human_reviewed_exact_scope":false,"private_settings_sha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","spool_root":"/private/var/folders/sw/_x387kld25bgsp22s4n5rfjm0000gn/T/TestFull1220V11CodecVector512905759/002/spool","observer":{"observation_receipt_boundary":"durable_local","spool_root_sha256":"0e5e4645cb345039b66bdd5f3644a3e152b2690bf759ecd2bf1330c3c6ae1955","spool_run_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","spool_max_bytes":4294967296,"enabled":true,"capture_content":true,"sample_rate":1,"environment":"evaluation","release":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","export_timeout_ms":10000,"queue_size":256,"endpoint_sha256":"44ea7c862c85d4f15be02750e43abcc6c41a6ad3b387ef9c0fbd7cdd388205ef","project_sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},"signature":"b38d0bf60d86164689cf1e53754c6dd524c935c598f65f8b1567415fa31288ed2749e6009609a48d6e4578135c471b4c67a73aae2c96b58a5bc89dbfaafa0505"}'
GO_VECTOR_SIGNING_BYTES_SHA256='1015cd8a44d3fbd5b0696288b5100ac5987db862d3ac9316051fa21de3a0e166'

class GenuineGoVector(unittest.TestCase):
 def test_exact_go_canonical_authority_data(self):
  m=load('contracts')
  self.assertEqual(hashlib.sha256(m.authorization_signing_bytes(GO_VECTOR_AUTHORIZATION_RAW)).hexdigest(),GO_VECTOR_SIGNING_BYTES_SHA256)
