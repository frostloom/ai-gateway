"""Pure synthetic V14 codec tests; no real grant, signing, IO or Go proof.

Expected orders/bytes below are hand-specified contract checks. Genuine Go
interoperability vectors are a separate integration step owned by root.
"""
import hashlib
import json
from pathlib import Path
import types
import unittest

HERE = Path(__file__).resolve().parents[1]
BUDGET_REPLY = '22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57'
AUTONOMY = '8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95'
POLICY_RAW = b'{"schema":"full-window-operation-policy.v14","window_seconds":600,"model_seconds":90,"jev_seconds":10}'
POLICY_SHA = hashlib.sha256(POLICY_RAW).hexdigest()
COLLECTION = '3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213'
RETRY = '26fef0e90b466ed651b2e8bc11790d336d12d75554f14e7fd06a8d18009cf088'
BUDGET = '58f7bea973d9c401c6ae8231652a91fa30ccf26de90a8adcc4ad66b0d4539e7d'
COMPLETION = '9a2d01ba483bde017baf06f7ae66c151d99d7f4fd0cc0ed911a8e18c905a8042'


def raw(value):
    return json.dumps(value, ensure_ascii=True, separators=(',', ':')).encode()


def reverse_objects(value):
    if type(value) is dict:
        return {key: reverse_objects(item) for key, item in reversed(list(value.items()))}
    return value


def execution():
    # Synthetic bindings refer to genuine Go declaration DATA, not grants.
    return {
        'schema': 'full-window-delegated-execution-authorization.v14',
        'human_authorized': True, 'retry_policy_approved': True,
        'human_reply': {'path': '/synthetic/budget.json', 'sha256': BUDGET_REPLY},
        'manifest_sha256': 'a'*64, 'source_sha256': 'b'*64,
        'binary_sha256': 'c'*64, 'scope_sha256': 'd'*64,
        'execution_sha256': 'e'*64, 'budget_declaration_sha256': BUDGET,
        'retry_policy_sha256': RETRY,
        'parent': {'model_micro_cny': 200000000, 'jev_micro_usd': 3000000},
        'owner_id': 'synthetic-owner', 'single_owner': True, 'no_relaunch': True,
        'autonomous_authorization': {'path': '/synthetic/autonomy.json', 'sha256': AUTONOMY},
        'global_budget': {'model_micro_cny': 200000000, 'jev_micro_usd': 4000000},
        'human_reviewed_exact_scope': False, 'private_settings_sha256': 'f'*64,
        'spool_root': '/synthetic/spool',
        'observer': {
            'observation_receipt_boundary': 'durable_local',
            'spool_root_sha256': hashlib.sha256(b'/synthetic/spool').hexdigest(),
            'spool_run_id': '2'*64, 'spool_max_bytes': 4294967296,
            'enabled': True, 'capture_content': True, 'sample_rate': 1,
            'environment': 'evaluation', 'release': '3'*40,
            'export_timeout_ms': 10000, 'queue_size': 4096,
            'endpoint_sha256': '4'*64, 'project_sha256': '5'*64,
        },
        'collection_policy_sha256': COLLECTION, 'completion_sha256': COMPLETION,
        'executor_equivalence_sha256': '7'*64,
        'operation_policy_sha256': POLICY_SHA,
        'operation_policy_approval': {'path': '/synthetic/timing-approval.json', 'sha256': '8'*64},
    }


def approval():
    return {
        'schema': 'full-window-operation-policy-human-approval.v14',
        'human_policy_approved': True,
        'human_reply': {'path': '/synthetic/new-timing-reply.json', 'sha256': '9'*64},
        'manifest_sha256': 'a'*64, 'source_sha256': 'b'*64,
        'binary_sha256': 'c'*64, 'scope_sha256': 'd'*64,
        'execution_sha256': 'e'*64, 'operation_policy_sha256': POLICY_SHA,
        'human_reviewed_exact_scope': False,
    }


class Contracts(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        path = HERE/'full1220-v14-contracts.py'
        cls.module = types.ModuleType('pure_v14_contracts')
        if path.exists():
            exec(compile(path.read_bytes(), str(path), 'exec'), cls.module.__dict__)

    def setUp(self):
        self.assertTrue(hasattr(self.module, 'canonical_execution_binding'),
                        'V14 pure execution codec is not implemented')
        self.m = self.module

    def test_operation_declaration_has_exact_canonical_bytes(self):
        self.assertEqual(self.m.canonical_operation_policy(raw(reverse_objects(json.loads(POLICY_RAW)))), POLICY_RAW)

    def test_genuine_go_declaration_bytes_and_binding_references(self):
        fixture = json.loads((HERE/'fixtures/v14-contracts/go-declarations.json').read_bytes())
        self.assertIs(fixture['not_authorization'], True)
        self.assertEqual(fixture['schema'], 'go-generated-v14-declaration-data-vectors.v1')
        expected = {'budget': BUDGET, 'completion': COMPLETION,
                    'policy': '40f1ad8fe36d38a54e43e4d7fdc319d1137c8cde6aeac2255a200a44370f83a3'}
        for name, digest in expected.items():
            with self.subTest(name=name):
                vector = fixture['vectors'][name]
                self.assertEqual(hashlib.sha256(vector['go_json'].encode()).hexdigest(), digest)
                self.assertEqual(vector['sha256'], digest)
        policy = fixture['vectors']['policy']['go_json'].encode()
        self.assertEqual(self.m.canonical_operation_policy(policy), policy)
        self.assertEqual(POLICY_SHA, fixture['vectors']['policy']['sha256'])
        data = execution()
        data['budget_declaration_sha256'] = fixture['vectors']['budget']['sha256']
        data['completion_sha256'] = fixture['vectors']['completion']['sha256']
        self.m.canonical_execution_binding(raw(data))

    def test_wrong_or_old_budget_and_completion_pins_are_rejected(self):
        for key, digest in (
            ('budget_declaration_sha256', '1'*64),
            ('budget_declaration_sha256', 'b20b0894e85bad9b3db31a5834bd8eb350895ab2b3f9ef2eae67850b095d8a3a'),
            ('completion_sha256', '6'*64),
            ('completion_sha256', '8dffa2b26461726cb5e53fbcba47ebb9f053a6ddef086b14c8e7def73d41bec7'),
        ):
            data = execution(); data[key] = digest
            with self.subTest(key=key, digest=digest), self.assertRaises(ValueError):
                self.m.canonical_execution_binding(raw(data))

    def go_wire_data(self):
        fixture_raw = (HERE/'fixtures/v14-contracts/go-operation-wire-data.json').read_bytes()
        self.assertEqual(hashlib.sha256(fixture_raw).hexdigest(),
                         '898f5a8eaf0f6f4684addb70d1d67dc66b28960b7da195e59db4176729d5e9cc')
        fixture = json.loads(fixture_raw)
        self.assertEqual(fixture['schema'], 'go-generated-v14-operation-wire-data-vectors.v1')
        self.assertIs(fixture['not_authorization'], True)
        self.assertIs(fixture['zero_signatures_not_verified'], True)
        return fixture

    def test_genuine_go_execution_message_and_sha_match(self):
        vector = self.go_wire_data()['execution']
        self.assertEqual(vector['document']['signature'], '0'*128)
        message = self.m.authorization_signing_bytes(raw(reverse_objects(vector['document'])))
        self.assertEqual(message, vector['go_json'].encode())
        self.assertEqual(hashlib.sha256(message).hexdigest(), vector['sha256'])
        self.assertEqual(vector['sha256'], '81fcf4e795009a953c4c2891623fab6b7176c7878acacec2a0adcec4cbe4cebd')
        binding = dict(vector['document']); del binding['signature']
        self.assertEqual(self.m.canonical_execution_binding(raw(binding)), message)
        self.assertIn(b'\\u003c\\u0026\\u003e', message)
        self.assertNotIn(b'"signature"', message)

    def test_genuine_go_policy_approval_message_and_sha_match(self):
        vector = self.go_wire_data()['approval']
        self.assertEqual(vector['document']['signature'], '0'*128)
        message = self.m.policy_approval_signing_bytes(raw(reverse_objects(vector['document'])))
        self.assertEqual(message, vector['go_json'].encode())
        self.assertEqual(hashlib.sha256(message).hexdigest(), vector['sha256'])
        self.assertEqual(vector['sha256'], 'a8b1d1f29844ef79a824afda4c42fc9be8cabe29ef8f1970a74b73a3397e248b')
        binding = dict(vector['document']); del binding['signature']
        self.assertEqual(self.m.canonical_operation_policy_approval(raw(binding)), message)
        binding['human_reply']['sha256'] = BUDGET_REPLY
        with self.assertRaises(ValueError): self.m.canonical_operation_policy_approval(raw(binding))

    def test_execution_order_and_new_tail_are_canonical(self):
        parsed = json.loads(self.m.canonical_execution_binding(raw(reverse_objects(execution()))))
        self.assertEqual(list(parsed), 'schema human_authorized retry_policy_approved human_reply manifest_sha256 source_sha256 binary_sha256 scope_sha256 execution_sha256 budget_declaration_sha256 retry_policy_sha256 parent owner_id single_owner no_relaunch autonomous_authorization global_budget human_reviewed_exact_scope private_settings_sha256 spool_root observer collection_policy_sha256 completion_sha256 executor_equivalence_sha256 operation_policy_sha256 operation_policy_approval'.split())
        self.assertEqual(list(parsed['operation_policy_approval']), ['path', 'sha256'])
        self.assertEqual(list(parsed['parent']), ['model_micro_cny', 'jev_micro_usd'])
        self.assertEqual(list(parsed['observer']), 'observation_receipt_boundary spool_root_sha256 spool_run_id spool_max_bytes enabled capture_content sample_rate environment release export_timeout_ms queue_size endpoint_sha256 project_sha256'.split())

    def test_approval_order_and_bytes_are_independent_of_input_order(self):
        expected = (b'{"schema":"full-window-operation-policy-human-approval.v14","human_policy_approved":true,"human_reply":{"path":"/synthetic/new-timing-reply.json","sha256":"'+b'9'*64+b'"},"manifest_sha256":"'+b'a'*64+b'","source_sha256":"'+b'b'*64+b'","binary_sha256":"'+b'c'*64+b'","scope_sha256":"'+b'd'*64+b'","execution_sha256":"'+b'e'*64+b'","operation_policy_sha256":"'+POLICY_SHA.encode()+b'","human_reviewed_exact_scope":false}')
        self.assertEqual(self.m.canonical_operation_policy_approval(raw(reverse_objects(approval()))), expected)

    def test_signature_is_shape_only_data_and_excluded_from_messages(self):
        for data, parse, message, canonical in (
            (execution(), self.m.parse_execution_authorization, self.m.authorization_signing_bytes, self.m.canonical_execution_binding),
            (approval(), self.m.parse_operation_policy_approval, self.m.policy_approval_signing_bytes, self.m.canonical_operation_policy_approval),
        ):
            with self.subTest(schema=data['schema']):
                signed = {**data, 'signature': 'AB'*64}
                result = parse(raw(signed))
                self.assertEqual(result['signature'], 'AB'*64)
                self.assertEqual(message(raw(signed)), canonical(raw(data)))
                self.assertNotIn(b'"signature"', message(raw(signed)))
                result['human_reply']['path'] = 'changed'
                self.assertEqual(parse(raw(signed))['human_reply']['path'], data['human_reply']['path'])

    def test_go_html_unicode_and_control_escaping(self):
        data = execution(); data['owner_id'] = '合成<&>\u2028\u2029\t\\"'
        value = self.m.canonical_execution_binding(raw(data))
        self.assertIn('"owner_id":"合成\\u003c\\u0026\\u003e\\u2028\\u2029\\t\\\\\\\""'.encode(), value)
        self.assertFalse(value.endswith(b'\n'))

    def test_go_trimspace_and_utf8_owner_byte_bound(self):
        data = execution(); data['owner_id'] = '\x1c'
        self.m.canonical_execution_binding(raw(data))  # Python strip differs from Go.
        for owner in ('', ' x', 'x\u0085', 'x\rinside', 'x\ninside', '界'*43):
            with self.subTest(owner=repr(owner)):
                data['owner_id'] = owner
                with self.assertRaises(ValueError): self.m.canonical_execution_binding(raw(data))
        data['owner_id'] = '界'*42+'aa'
        self.m.canonical_execution_binding(raw(data))

    def test_old_budget_and_autonomy_cannot_be_new_timing_reply(self):
        for digest in (BUDGET_REPLY, AUTONOMY):
            with self.subTest(digest=digest):
                data = approval(); data['human_reply']['sha256'] = digest
                with self.assertRaises(ValueError): self.m.canonical_operation_policy_approval(raw(data))
        self.m.canonical_execution_binding(raw(execution()))  # Their old roles remain valid.

    def test_approval_reply_path_matches_go_message_canonical_boundary(self):
        for path in ('relative/reply.txt', '//synthetic/reply.txt',
                     '/synthetic/../reply.txt', '/synthetic/./reply.txt', '/synthetic/reply.txt/'):
            data = approval(); data['human_reply']['path'] = path
            with self.subTest(path=path), self.assertRaises(ValueError):
                self.m.canonical_operation_policy_approval(raw(data))

    def test_old_or_other_role_schemas_are_rejected(self):
        for builder, codec, schemas in (
            (execution, self.m.canonical_execution_binding, ('full-window-delegated-execution-authorization.v13', 'full-window-operation-policy-human-approval.v14')),
            (approval, self.m.canonical_operation_policy_approval, ('full-window-operation-policy-human-approval.v13', 'full-window-delegated-execution-authorization.v14')),
        ):
            for schema in schemas:
                with self.subTest(schema=schema):
                    data = builder(); data['schema'] = schema
                    with self.assertRaises(ValueError): codec(raw(data))

    def test_missing_new_fields_and_unknown_fields_are_rejected(self):
        for key in ('operation_policy_sha256', 'operation_policy_approval'):
            data = execution(); del data[key]
            with self.subTest(key=key), self.assertRaises(ValueError): self.m.canonical_execution_binding(raw(data))
        for builder, codec in ((execution, self.m.canonical_execution_binding), (approval, self.m.canonical_operation_policy_approval)):
            data = builder(); data['extra'] = True
            with self.assertRaises(ValueError): codec(raw(data))
            data = builder(); data['human_reply']['extra'] = True
            with self.assertRaises(ValueError): codec(raw(data))

    def test_bad_fixed_policy_and_declaration_types_are_rejected(self):
        for key, value in (('window_seconds', 300), ('model_seconds', 91), ('jev_seconds', 11), ('window_seconds', True), ('model_seconds', 90.0), ('jev_seconds', '10'), ('schema', 'full-window-operation-policy.v13')):
            data = json.loads(POLICY_RAW); data[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError): self.m.canonical_operation_policy(raw(data))
        for builder, codec in ((execution, self.m.canonical_execution_binding), (approval, self.m.canonical_operation_policy_approval)):
            data = builder(); data['operation_policy_sha256'] = '0'*64
            with self.assertRaises(ValueError): codec(raw(data))

    def test_bool_and_integer_aliases_do_not_pass(self):
        for key, value in (('human_authorized', 1), ('retry_policy_approved', 1), ('single_owner', 1), ('no_relaunch', 1), ('human_reviewed_exact_scope', 0)):
            data = execution(); data[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): self.m.canonical_execution_binding(raw(data))
        for key, value in (('human_policy_approved', 1), ('human_reviewed_exact_scope', 0)):
            data = approval(); data[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): self.m.canonical_operation_policy_approval(raw(data))
        for key in ('sample_rate', 'spool_max_bytes', 'export_timeout_ms', 'queue_size'):
            data = execution(); data['observer'][key] = True
            with self.subTest(key=key), self.assertRaises(ValueError): self.m.canonical_execution_binding(raw(data))

    def test_amounts_observer_and_retained_roles_cannot_drift(self):
        for section, key, value in (('parent', 'model_micro_cny', 195000000), ('global_budget', 'jev_micro_usd', 3000000), ('human_reply', 'sha256', 'a'*64), ('autonomous_authorization', 'sha256', 'a'*64), ('observer', 'enabled', 1), ('observer', 'capture_content', False), ('observer', 'sample_rate', 0), ('observer', 'spool_root_sha256', 'a'*64), ('observer', 'queue_size', 4097)):
            data = execution(); data[section][key] = value
            with self.subTest(section=section, key=key), self.assertRaises(ValueError): self.m.canonical_execution_binding(raw(data))

    def test_refs_and_digests_are_exact_and_bounded(self):
        for builder, codec in ((execution, self.m.canonical_execution_binding), (approval, self.m.canonical_operation_policy_approval)):
            for digest in ('A'*64, 'a'*63, '', 3, None):
                data = builder(); data['scope_sha256'] = digest
                with self.subTest(digest=digest), self.assertRaises(ValueError): codec(raw(data))
            for path in ('', 1, None):
                data = builder(); data['human_reply']['path'] = path
                with self.subTest(path=path), self.assertRaises(ValueError): codec(raw(data))
        for root in ('relative', '//synthetic/spool', '/synthetic/../spool', '/synthetic/spool/'):
            data = execution(); data['spool_root'] = root
            with self.subTest(root=root), self.assertRaises(ValueError): self.m.canonical_execution_binding(raw(data))

    def test_strict_json_duplicate_trailing_float_nonfinite_and_utf8(self):
        valid = raw(approval())
        candidates = (valid+b'{}', b'\xef\xbb\xbf'+valid, valid.replace(b'"human_policy_approved":true', b'"human_policy_approved":true,"human_policy_approved":true'), valid.replace(b'"human_reply":{', b'"human_reply":{"path":"duplicate",'), valid.replace(b'"human_policy_approved":true', b'"human_policy_approved":NaN'), valid.replace(b'"human_policy_approved":true', b'"human_policy_approved":1.0'), valid.replace(b'/synthetic/new', b'/synthetic/\xffnew'), valid.replace(b'/synthetic/new', b'/synthetic/\\ud800new'), b'[]', b'null', b'', b' '*65537)
        for candidate in candidates:
            with self.subTest(raw=candidate[:60]), self.assertRaises(ValueError): self.m.canonical_operation_policy_approval(candidate)
        for candidate in (valid.decode(), bytearray(valid), None):
            with self.subTest(kind=type(candidate)), self.assertRaises(ValueError): self.m.canonical_operation_policy_approval(candidate)

    def test_int64_and_nesting_bounds_are_checked_before_shape(self):
        for value in (1 << 63, -(1 << 63)-1):
            data = approval(); data['human_policy_approved'] = value
            with self.assertRaisesRegex(ValueError, 'int64'): self.m.canonical_operation_policy_approval(raw(data))
        for value in (-(1 << 63), (1 << 63)-1):
            data = approval(); data['human_policy_approved'] = value
            with self.assertRaises(ValueError) as found: self.m.canonical_operation_policy_approval(raw(data))
            self.assertNotIn('int64', str(found.exception))
        nested = b'{"x":'*12+b'0'+b'}'*12
        with self.assertRaises(ValueError): self.m.canonical_operation_policy_approval(nested)

    def test_bad_or_missing_signature_is_rejected_without_crypto(self):
        for builder, parse in ((execution, self.m.parse_execution_authorization), (approval, self.m.parse_operation_policy_approval)):
            with self.assertRaises(ValueError): parse(raw(builder()))
            for signature in ('', 'a'*127, 'g'*128, None, 1):
                data = builder(); data['signature'] = signature
                with self.subTest(signature=signature), self.assertRaises(ValueError): parse(raw(data))

    def test_new_human_reply_digest_changes_message_without_claiming_approval(self):
        data = approval(); before = self.m.canonical_operation_policy_approval(raw(data))
        data['human_reply']['sha256'] = '0'*64
        after = self.m.canonical_operation_policy_approval(raw(data))
        self.assertNotEqual(before, after)  # DATA only; runtime must verify signature/real reply.


if __name__ == '__main__':
    unittest.main()
