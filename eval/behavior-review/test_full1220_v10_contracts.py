"""V10 data normalization; synthetic signatures never grant execution."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import unittest

HERE = Path(__file__).resolve().parent


def raw(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()


class CodecTests(unittest.TestCase):
    def setUp(self):
        path = HERE / os.environ.get('V10_CODEC_UNDER_TEST', 'full1220-v10-contracts.py')
        spec = importlib.util.spec_from_file_location('v10_codec_test', path)
        self.c = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.c)
        self.vector = json.loads((HERE/'fixtures/v10-contracts/go-delegated-authority.json').read_bytes())

    def test_actual_go_binding_bytes_and_detached_data(self):
        vector = self.vector
        self.assertTrue(vector['blank_build_anchors_deny_actual'])
        message = vector['canonical_binding_utf8'].encode()
        self.assertEqual(hashlib.sha256(message).hexdigest(),
                         '68d80ee37151891b72d6f14d36cbcdc890f0f4dad4b7db8420bb976280e17b48')
        obj = copy.deepcopy(vector['binding'])
        for key in ('human_reply', 'parent', 'autonomous_authorization', 'global_budget'):
            obj[key] = dict(reversed(list(obj[key].items())))
        self.assertEqual(self.c.canonical_execution_binding(raw(dict(reversed(list(obj.items()))))), message)
        self.assertEqual(self.c.authorization_signing_bytes(raw(vector['authorization'])), message)
        parsed = self.c.parse_execution_authorization(raw(vector['authorization']))
        self.assertEqual(parsed, vector['authorization'])
        parsed['parent']['model_micro_cny'] = 0
        self.assertEqual(self.c.parse_execution_authorization(raw(vector['authorization'])), vector['authorization'])

    def test_fixed_parent_lineage_and_completion_cannot_be_relabelled(self):
        for field, value in (
            ('human_reviewed_exact_scope', True), ('human_reviewed_exact_scope', 0),
            ('parent', {'model_micro_cny': 180000000, 'jev_micro_usd': 2500000}),
            ('global_budget', {'model_micro_cny': 200000001, 'jev_micro_usd': 4000000}),
            ('schema', 'full-window-delegated-execution-authorization.v9'),
            ('completion_sha256', '0'*64), ('executor_equivalence_sha256', 'bad'),
            ('budget_declaration_sha256', '0'*64), ('retry_policy_sha256', '0'*64),
        ):
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.c.canonical_execution_binding(raw({**self.vector['binding'], field: value}))
        for field in ('human_reply', 'autonomous_authorization'):
            value = copy.deepcopy(self.vector['binding']); value[field]['sha256'] = '0'*64
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.c.canonical_execution_binding(raw(value))

    def test_strict_json_types_unknowns_and_utf8_owner_bound(self):
        good = raw(self.vector['binding'])
        for bad in (good+b'{}', b'\xef\xbb\xbf'+good,
                    good[:-1]+b',"schema":"duplicate"}',
                    good.replace(b'185000000', b'185000000.0'),
                    good.replace(b'185000000', b'true'),
                    good.replace(b'185000000', b'NaN'),
                    good.replace(b'185000000', b'185000000e0')):
            with self.subTest(bad=bad[:20]), self.assertRaises(ValueError):
                self.c.canonical_execution_binding(bad)
        for owner in ('x\ny', '\u2000x', 'x\u2000', '😀'*33, '\ud800'):
            with self.subTest(owner=repr(owner)), self.assertRaises((ValueError, UnicodeError)):
                self.c.canonical_execution_binding(raw({**self.vector['binding'], 'owner_id': owner}))
        bad = copy.deepcopy(self.vector['binding']); bad['human_reply']['unknown'] = True
        with self.assertRaises(ValueError): self.c.canonical_execution_binding(raw(bad))
        for signature in ('x'*128, 'ab'*63, True):
            with self.subTest(signature=signature), self.assertRaises(ValueError):
                self.c.parse_execution_authorization(raw({**self.vector['authorization'], 'signature': signature}))

    def test_real_synthetic_signature_and_tamper(self):
        message = self.c.authorization_signing_bytes(raw(self.vector['authorization']))
        node = '/Users/a1/开发工具/node/node-v24.22.0-darwin-arm64/bin/node'
        program = """const c=require('node:crypto');let s='';process.stdin.on('data',b=>s+=b);process.stdin.on('end',()=>{const x=JSON.parse(s);const key=c.createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),Buffer.from(x.key,'hex')]),format:'der',type:'spki'});process.stdout.write(c.verify(null,Buffer.from(x.message,'hex'),key,Buffer.from(x.signature,'hex'))?'verified':'rejected');});"""
        item = {'key': self.vector['public_key_hex'], 'message': message.hex(),
                'signature': self.vector['signature_hex']}
        result = subprocess.run([node, '-e', program], input=raw(item), capture_output=True, check=True)
        self.assertEqual(result.stdout, b'verified')
        item['message'] = (message+b' ').hex()
        result = subprocess.run([node, '-e', program], input=raw(item), capture_output=True, check=True)
        self.assertEqual(result.stdout, b'rejected')


if __name__ == '__main__': unittest.main()
