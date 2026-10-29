"""Pure codec tests; fixtures are synthetic, never actual authorization."""
import sys
sys.dont_write_bytecode = True
import copy
import importlib.util
import json
from pathlib import Path
import unittest

MODULE = Path(__file__).with_name('full1220-v8-contracts.py')

def binding():
    return {'schema':'full-window-execution-authorization.v8','human_authorized':True,
            'retry_policy_approved':True,'human_reply':{'path':'/synthetic/回复<&>.json','sha256':'a'*64},
            'manifest_sha256':'b'*64,'source_sha256':'c'*64,'binary_sha256':'d'*64,
            'scope_sha256':'e'*64,'execution_sha256':'f'*64,
            'budget_declaration_sha256':'ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725',
            'retry_policy_sha256':'6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63',
            'parent':{'model_micro_cny':140000000,'jev_micro_usd':1100000},
            'owner_id':'synthetic-甲<&>\u2028\u2029-end','single_owner':True,'no_relaunch':True}

def wire(obj):
    return json.dumps(obj,ensure_ascii=False,separators=(',',':')).encode()

# Actual Go-emitted synthetic vector; copied verbatim as bytes, never reads its reply path.
# Origin SHA256 43883969ea1e0a8ffaf1f02823429f5878623cb56502d2c28d303ae3db4153a1
GO_VECTOR_RAW = b'{\n  "authorization": {\n    "schema": "full-window-execution-authorization.v8",\n    "human_authorized": true,\n    "retry_policy_approved": true,\n    "human_reply": {\n      "path": "/private/var/folders/sw/_x387kld25bgsp22s4n5rfjm0000gn/T/TestWindowV8RuntimeSignedAuthority2443032728/001/synthetic-human-reply.txt",\n      "sha256": "152789f17eb111106e4dc13b68e47d2fc7e858716ac757a385e9fb412087b171"\n    },\n    "manifest_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "source_sha256": "2ad4bf3b43ad0cbf5942e45e97ce3d99b03de3a88957ac634f5267f2ee19fc6e",\n    "binary_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "scope_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "execution_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "budget_declaration_sha256": "ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725",\n    "retry_policy_sha256": "6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63",\n    "parent": {\n      "model_micro_cny": 140000000,\n      "jev_micro_usd": 1100000\n    },\n    "owner_id": "synthetic-test-owner",\n    "single_owner": true,\n    "no_relaunch": true,\n    "signature": "879ba725b88a40c4c616da38b0cddd318d22956b45aff2148d5a7f3c19d07737f3d09da9ddff151edee5c733b78da3286edbf5297ca221e2bfbf496ab621120d"\n  },\n  "binding": {\n    "schema": "full-window-execution-authorization.v8",\n    "human_authorized": true,\n    "retry_policy_approved": true,\n    "human_reply": {\n      "path": "/private/var/folders/sw/_x387kld25bgsp22s4n5rfjm0000gn/T/TestWindowV8RuntimeSignedAuthority2443032728/001/synthetic-human-reply.txt",\n      "sha256": "152789f17eb111106e4dc13b68e47d2fc7e858716ac757a385e9fb412087b171"\n    },\n    "manifest_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "source_sha256": "2ad4bf3b43ad0cbf5942e45e97ce3d99b03de3a88957ac634f5267f2ee19fc6e",\n    "binary_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "scope_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "execution_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",\n    "budget_declaration_sha256": "ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725",\n    "retry_policy_sha256": "6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63",\n    "parent": {\n      "model_micro_cny": 140000000,\n      "jev_micro_usd": 1100000\n    },\n    "owner_id": "synthetic-test-owner",\n    "single_owner": true,\n    "no_relaunch": true\n  },\n  "canonical_binding_utf8": "{\\"schema\\":\\"full-window-execution-authorization.v8\\",\\"human_authorized\\":true,\\"retry_policy_approved\\":true,\\"human_reply\\":{\\"path\\":\\"/private/var/folders/sw/_x387kld25bgsp22s4n5rfjm0000gn/T/TestWindowV8RuntimeSignedAuthority2443032728/001/synthetic-human-reply.txt\\",\\"sha256\\":\\"152789f17eb111106e4dc13b68e47d2fc7e858716ac757a385e9fb412087b171\\"},\\"manifest_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"source_sha256\\":\\"2ad4bf3b43ad0cbf5942e45e97ce3d99b03de3a88957ac634f5267f2ee19fc6e\\",\\"binary_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"scope_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"execution_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"budget_declaration_sha256\\":\\"ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725\\",\\"retry_policy_sha256\\":\\"6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63\\",\\"parent\\":{\\"model_micro_cny\\":140000000,\\"jev_micro_usd\\":1100000},\\"owner_id\\":\\"synthetic-test-owner\\",\\"single_owner\\":true,\\"no_relaunch\\":true}",\n  "evidence_kind": "synthetic_controlled_fixture_not_authority",\n  "public_key_hex": "dcdee69fb8577203b8d6128945e5f06e8420df739fb92a492c6d1dfab3eed533",\n  "signature_hex": "879ba725b88a40c4c616da38b0cddd318d22956b45aff2148d5a7f3c19d07737f3d09da9ddff151edee5c733b78da3286edbf5297ca221e2bfbf496ab621120d"\n}'

class CodecTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not MODULE.exists(): raise AssertionError('V8 codec not implemented')
        spec=importlib.util.spec_from_file_location('v8_contracts',MODULE)
        cls.codec=importlib.util.module_from_spec(spec);spec.loader.exec_module(cls.codec)

    def test_canonical_go_field_order_nested_and_escaping(self):
        b=binding(); b['human_reply']=dict(reversed(list(b['human_reply'].items())))
        b['parent']=dict(reversed(list(b['parent'].items())))
        b=dict(reversed(list(b.items())))
        raw=self.codec.canonical_execution_binding(wire(b))
        expected=('{'+'"schema":"full-window-execution-authorization.v8","human_authorized":true,'
          '"retry_policy_approved":true,"human_reply":{"path":"/synthetic/回复\\u003c\\u0026\\u003e.json","sha256":"'+'a'*64+'"},'
          '"manifest_sha256":"'+'b'*64+'","source_sha256":"'+'c'*64+'","binary_sha256":"'+'d'*64+'",'
          '"scope_sha256":"'+'e'*64+'","execution_sha256":"'+'f'*64+'",'
          '"budget_declaration_sha256":"ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725",'
          '"retry_policy_sha256":"6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63",'
          '"parent":{"model_micro_cny":140000000,"jev_micro_usd":1100000},'
          '"owner_id":"synthetic-甲\\u003c\\u0026\\u003e\\u2028\\u2029-end","single_owner":true,"no_relaunch":true}').encode()
        self.assertEqual(raw,expected)

    def test_signature_excluded_hex_not_base64_and_no_capability(self):
        b=binding(); a={**b,'signature':'AB'*64}
        data=self.codec.parse_execution_authorization(wire(a))
        self.assertEqual(type(data),dict)
        self.assertEqual(data,a)
        self.assertEqual(self.codec.canonical_execution_binding(wire(b)),self.codec.authorization_signing_bytes(wire(a)))
        for sig in ('A'*127,'gg'*64,'YWJjZA==',None,12):
            with self.subTest(sig=sig),self.assertRaises(ValueError):
                self.codec.parse_execution_authorization(wire({**b,'signature':sig}))

    def test_strict_json_duplicate_unknown_missing_and_nonfinite(self):
        good=wire(binding())
        bad=[b'{"schema":0,"schema":1}',good[:-1]+b',"owner_id":"x"}',
             good.replace(b'"sha256":',b'"path":"other","sha256":'),
             good+b'{}',b'\xef\xbb\xbf'+good,good.replace(b'140000000',b'NaN'),
             good.replace(b'140000000',b'Infinity'),good.replace(b'140000000',b'140000000.0'),
             good.replace(b'140000000',b'14e7'),good.replace(b'140000000',b'true'),
             good.replace(b'140000000',b'9223372036854775808'),b'\xff',b'['*1000+b']'*1000,
             good.replace(b'synthetic-',b'\\ud800'),good.replace(b'"owner_id":',b'"Owner_ID":')]
        for raw in bad:
            with self.subTest(raw=raw[:100]),self.assertRaises(ValueError):
                self.codec.canonical_execution_binding(raw)
        for change in ('missing','unknown','nested_unknown','nested_missing'):
            b=binding()
            if change=='missing':del b['owner_id']
            if change=='unknown':b['extra']=False
            if change=='nested_unknown':b['human_reply']['extra']=False
            if change=='nested_missing':del b['parent']['jev_micro_usd']
            with self.subTest(change=change),self.assertRaises(ValueError):self.codec.canonical_execution_binding(wire(b))

    def test_exact_flags_digests_schema_and_parent(self):
        for key,value in [('schema','full-window-execution-authorization.v7'),
                          ('human_authorized',False),('human_authorized',1),
                          ('retry_policy_approved',False),('single_owner',False),('no_relaunch',False),
                          ('manifest_sha256','B'*64),('binary_sha256','a'*63),
                          ('source_sha256',None),('budget_declaration_sha256','a'*64),
                          ('retry_policy_sha256','b'*64),('parent',{'model_micro_cny':130000000,'jev_micro_usd':1000000})]:
            b=binding();b[key]=value
            with self.subTest(key=key,value=value),self.assertRaises(ValueError):self.codec.canonical_execution_binding(wire(b))
        for value in ('bad','A'*64,None,1):
            b=binding();b['human_reply']['sha256']=value
            with self.subTest(reply=value),self.assertRaises(ValueError):self.codec.canonical_execution_binding(wire(b))

    def test_owner_utf8_byte_bound_and_go_trimspace(self):
        for owner in ('',' x','x\n','x\rX','\u0085x','x\u3000','甲'*43):
            b=binding();b['owner_id']=owner
            with self.subTest(owner=owner),self.assertRaises(ValueError):self.codec.canonical_execution_binding(wire(b))
        for owner in ('甲'*42+'ab','\x1cx','x\x00y','x\x1f'):
            b=binding();b['owner_id']=owner
            self.assertIsInstance(self.codec.canonical_execution_binding(wire(b)),bytes)

    def test_no_file_claim_and_detached_result(self):
        b=binding();b['human_reply']['path']='/definitely/nonexistent/evidence.json'
        a={**b,'signature':'00'*64};data=self.codec.parse_execution_authorization(wire(a))
        self.assertEqual(data,a)  # Shape only: an all-zero signature is not verified.
        data['human_reply']['path']='changed'
        self.assertEqual(a['human_reply']['path'],'/definitely/nonexistent/evidence.json')

    def test_actual_go_emitted_synthetic_vector(self):
        import hashlib
        self.assertEqual(hashlib.sha256(GO_VECTOR_RAW).hexdigest(),'43883969ea1e0a8ffaf1f02823429f5878623cb56502d2c28d303ae3db4153a1')
        v=json.loads(GO_VECTOR_RAW)
        self.assertEqual(v['evidence_kind'],'synthetic_controlled_fixture_not_authority')
        # Reverse decoded input to catch relying on incoming object order.
        raw=wire(dict(reversed(list(v['authorization'].items()))))
        self.assertEqual(self.codec.authorization_signing_bytes(raw),v['canonical_binding_utf8'].encode())
        self.assertEqual(self.codec.parse_execution_authorization(raw)['signature'],v['signature_hex'])

    def test_candidate_declarations_cannot_be_execution_authorization(self):
        for schema in ('full-window-budget-declaration.v1','full-window-retry-declaration.v1'):
            with self.subTest(schema=schema),self.assertRaises(ValueError):
                self.codec.parse_execution_authorization(wire({'schema':schema,'human_authorized':False}))

    def test_input_bound_and_exact_bytes_type(self):
        for raw in ('{}',bytearray(b'{}'),b' '*65537):
            with self.subTest(kind=type(raw)),self.assertRaises(ValueError):self.codec.canonical_execution_binding(raw)

if __name__=='__main__':unittest.main()
