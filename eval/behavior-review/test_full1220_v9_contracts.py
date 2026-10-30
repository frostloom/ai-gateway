"""Pure delegated V9 codec tests; shapes are NOT actual authority."""
import copy
import importlib.util
import json
import hashlib
import subprocess
from pathlib import Path
import unittest


HERE = Path(__file__).resolve().parent
REPLY = '22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57'
AUTONOMOUS = '8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95'


def binding():
    return {'schema': 'full-window-delegated-execution-authorization.v9', 'human_authorized': True,
        'retry_policy_approved': True, 'human_reply': {'path': '/fixture/回复<&>.txt', 'sha256': REPLY},
        'manifest_sha256': 'a'*64, 'source_sha256': 'b'*64, 'binary_sha256': 'c'*64,
        'scope_sha256': 'd'*64, 'execution_sha256': 'e'*64,
        'budget_declaration_sha256': 'd6584f9462b6ffbfe94acede036e0b62de536fe04aacc4bb57394ccd71184664',
        'retry_policy_sha256': '6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63',
        'parent': {'model_micro_cny': 180000000, 'jev_micro_usd': 2500000},
        'owner_id': 'synthetic-甲<&>\u2028-end', 'single_owner': True, 'no_relaunch': True,
        'autonomous_authorization': {'path': '/fixture/autonomous.txt', 'sha256': AUTONOMOUS},
        'global_budget': {'model_micro_cny': 200000000, 'jev_micro_usd': 4000000},
        'human_reviewed_exact_scope': False}


def raw(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()


class CodecTests(unittest.TestCase):
    def setUp(self):
        p = HERE/'full1220-v9-contracts.py'
        self.assertTrue(p.exists(), 'V9 codec missing')
        spec = importlib.util.spec_from_file_location('v9_codec_test', p)
        self.c = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.c)

    def test_exact_go_embedding_order_and_nested_escaping(self):
        obj = binding()
        for field in ('human_reply', 'parent', 'autonomous_authorization', 'global_budget'):
            obj[field] = dict(reversed(list(obj[field].items())))
        actual = self.c.canonical_execution_binding(raw(dict(reversed(list(obj.items())))))
        # Literal struct order: embedded V8 binding, then three new V9 fields.
        expected = ('{"schema":"full-window-delegated-execution-authorization.v9","human_authorized":true,'
            '"retry_policy_approved":true,"human_reply":{"path":"/fixture/回复\\u003c\\u0026\\u003e.txt","sha256":"'+REPLY+'"},'
            '"manifest_sha256":"'+'a'*64+'","source_sha256":"'+'b'*64+'","binary_sha256":"'+'c'*64+'",'
            '"scope_sha256":"'+'d'*64+'","execution_sha256":"'+'e'*64+'",'
            '"budget_declaration_sha256":"d6584f9462b6ffbfe94acede036e0b62de536fe04aacc4bb57394ccd71184664",'
            '"retry_policy_sha256":"6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63",'
            '"parent":{"model_micro_cny":180000000,"jev_micro_usd":2500000},'
            '"owner_id":"synthetic-甲\\u003c\\u0026\\u003e\\u2028-end","single_owner":true,"no_relaunch":true,'
            '"autonomous_authorization":{"path":"/fixture/autonomous.txt","sha256":"'+AUTONOMOUS+'"},'
            '"global_budget":{"model_micro_cny":200000000,"jev_micro_usd":4000000},"human_reviewed_exact_scope":false}').encode()
        self.assertEqual(actual, expected)

    def test_signature_data_only(self):
        obj = {**binding(), 'signature': 'ab'*64}
        self.assertEqual(self.c.parse_execution_authorization(raw(obj)), obj)
        self.assertEqual(self.c.authorization_signing_bytes(raw(obj)), self.c.canonical_execution_binding(raw(binding())))
        with self.assertRaises(ValueError):
            self.c.parse_execution_authorization(raw({**obj, 'signature': 'x'*128}))

    def test_delegation_cannot_be_relabelled_exact_human_review(self):
        for field, value in [('human_reviewed_exact_scope', True), ('human_reviewed_exact_scope', 0),
                             ('parent', {'model_micro_cny': 140000000, 'jev_micro_usd': 1100000}),
                             ('global_budget', {'model_micro_cny': 200000001, 'jev_micro_usd': 4000000}),
                             ('schema', 'full-window-execution-authorization.v8')]:
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                self.c.canonical_execution_binding(raw({**binding(), field: value}))
        for field in ('human_reply', 'autonomous_authorization'):
            obj = binding(); obj[field]['sha256'] = '0'*64
            with self.assertRaises(ValueError): self.c.canonical_execution_binding(raw(obj))

    def test_strict_json_and_scalar_types(self):
        good = raw(binding())
        for bad in (good+b'{}', b'\xef\xbb\xbf'+good, good[:-1]+b',"schema":"duplicate"}',
                    good.replace(b'180000000', b'180000000.0'), good.replace(b'180000000', b'true'),
                    good.replace(b'180000000', b'NaN')):
            with self.subTest(bad=bad[:40]), self.assertRaises(ValueError):
                self.c.canonical_execution_binding(bad)
        obj = binding(); obj['autonomous_authorization']['unknown'] = True
        with self.assertRaises(ValueError): self.c.canonical_execution_binding(raw(obj))

    def test_actual_go_synthetic_vector_canonical_and_signature(self):
        vector_raw = (HERE/'fixtures/v9-contracts/go-delegated-authority.json').read_bytes()
        self.assertEqual(hashlib.sha256(vector_raw).hexdigest(), 'dbda63e9565e64ca23e2e7d0f95ef4deb15c428bda6cf7513d39ad54100546b8')
        vector = json.loads(vector_raw)
        message = self.c.authorization_signing_bytes(raw(vector['authorization']))
        self.assertEqual(message, vector['canonical_binding_utf8'].encode())
        self.assertEqual(self.c.canonical_execution_binding(raw(vector['binding'])), message)
        node = '/Users/a1/开发工具/node/node-v24.22.0-darwin-arm64/bin/node'
        program = '''const c=require('node:crypto');let s='';process.stdin.on('data',b=>s+=b);process.stdin.on('end',()=>{const x=JSON.parse(s);const key=c.createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),Buffer.from(x.key,'hex')]),format:'der',type:'spki'});process.stdout.write(c.verify(null,Buffer.from(x.message,'hex'),key,Buffer.from(x.signature,'hex'))?'verified':'rejected');});'''
        data = {'key': vector['public_key_hex'], 'message': message.hex(), 'signature': vector['signature_hex']}
        verified = subprocess.run([node, '-e', program], input=raw(data), capture_output=True, check=True)
        self.assertEqual(verified.stdout, b'verified')
        data['message'] = (message+b' ').hex()
        rejected = subprocess.run([node, '-e', program], input=raw(data), capture_output=True, check=True)
        self.assertEqual(rejected.stdout, b'rejected')


if __name__ == '__main__': unittest.main()
