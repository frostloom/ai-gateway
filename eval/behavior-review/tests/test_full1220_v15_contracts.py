"""Go-generated V15 DATA interoperability and rejection tests; no authority."""
import copy
import hashlib
import json
from pathlib import Path
import types
import unittest

HERE = Path(__file__).resolve().parents[1]
POLICY_REPLY = '2560180d500f5740d102c5e55822b25dc322a9493506c3685cee473c5b6ec4af'


def wire(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()


class Contracts(unittest.TestCase):
    def setUp(self):
        path = HERE/'full1220-v15-contracts.py'
        self.m = types.ModuleType('pure15contracts')
        if path.exists(): exec(compile(path.read_bytes(), str(path), 'exec'), self.m.__dict__)
        self.assertTrue(hasattr(self.m, 'canonical_execution_binding'), 'V15 codec not implemented')
        raw = (HERE/'fixtures/v15-contracts/go-operation-wire-data.json').read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), 'e34c26ccd6e65c3621cf537cc0d0f543006427daa426fa3397ae358ae46350f8')
        self.vectors = json.loads(raw)

    def test_exact_genuine_go_messages_preserve_role_order_and_escaping(self):
        self.assertIs(self.vectors['not_authorization'], True)
        self.assertIs(self.vectors['zero_signatures_not_verified'], True)
        for role, parse in (('execution', self.m.authorization_signing_bytes), ('approval', self.m.policy_approval_signing_bytes)):
            vector = self.vectors[role]
            reversed_input = dict(reversed(list(vector['document'].items())))
            actual = parse(wire(reversed_input))
            self.assertEqual(actual, vector['go_json'].encode())
            self.assertEqual(hashlib.sha256(actual).hexdigest(), vector['sha256'])
            self.assertNotIn(b'"signature"', actual)

    def test_declarations_use_new178_and_original600_policy_data(self):
        raw = (HERE/'fixtures/v15-contracts/go-declarations.json').read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), '9a834d35f5ebc7cebf2041654b224d1f92b837210f1163f264f2a245d7aca01b')
        fixture = json.loads(raw)
        for name in ('budget', 'completion', 'operation_policy', 'retry'):
            self.assertEqual(hashlib.sha256(fixture[name+'_go_json'].encode()).hexdigest(), fixture[name+'_sha256'])
        policy = fixture['operation_policy_go_json'].encode()
        self.assertEqual(self.m.canonical_operation_policy(policy), policy)
        for schema in ('full-window-operation-policy.v15', 'full-window-operation-policy.v13'):
            data = copy.deepcopy(fixture['operation_policy']); data['schema'] = schema
            with self.assertRaises(ValueError): self.m.canonical_operation_policy(wire(data))

    def test_only_exact_approved_policy_reply_is_accepted(self):
        original = self.vectors['approval']['document']
        self.assertEqual(original['human_reply']['sha256'], POLICY_REPLY)
        for digest in ('0'*64, '22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57',
                       '8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95'):
            data = copy.deepcopy(original); data['human_reply']['sha256'] = digest
            with self.subTest(digest=digest), self.assertRaises(ValueError): self.m.parse_operation_policy_approval(wire(data))

    def test_old14_authority_and_cross_role_schemas_rejected(self):
        for role, parse in (('execution', self.m.parse_execution_authorization), ('approval', self.m.parse_operation_policy_approval)):
            data = copy.deepcopy(self.vectors[role]['document'])
            for schema in (data['schema'].replace('.v15', '.v14'), 'full-window-scope.v15'):
                data['schema'] = schema
                with self.subTest(role=role, schema=schema), self.assertRaises(ValueError): parse(wire(data))

    def test_unsigned_roles_and_phase_authority_fields_rejected(self):
        for role, parse in (('execution', self.m.parse_execution_authorization), ('approval', self.m.parse_operation_policy_approval)):
            for mutate in (lambda d: d.pop('signature'), lambda d: d.update(signature='0'*127),
                           lambda d: d.update(phase_timings_v15=[]), lambda d: d.update(human_reviewed_exact_scope=True)):
                data = copy.deepcopy(self.vectors[role]['document']); mutate(data)
                with self.subTest(role=role), self.assertRaises(ValueError): parse(wire(data))

    def test_stale_budget_completion_and_changed_policy_cannot_bind_new_authority(self):
        for key, value in (('budget_declaration_sha256', '58f7bea973d9c401c6ae8231652a91fa30ccf26de90a8adcc4ad66b0d4539e7d'),
                           ('completion_sha256', '9a2d01ba483bde017baf06f7ae66c151d99d7f4fd0cc0ed911a8e18c905a8042'),
                           ('operation_policy_sha256', '0'*64)):
            data = copy.deepcopy(self.vectors['execution']['document']); data[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): self.m.parse_execution_authorization(wire(data))

    def test_policy_paths_and_600_90_10_limits_do_not_expand(self):
        for path in ('relative', '//synthetic/reply', '/synthetic/../reply', '/synthetic/./reply', '/synthetic/reply/'):
            data = copy.deepcopy(self.vectors['approval']['document']); data['human_reply']['path'] = path
            with self.assertRaises(ValueError): self.m.parse_operation_policy_approval(wire(data))
        for key, value in (('window_seconds', 601), ('model_seconds', 91), ('jev_seconds', 11), ('window_seconds', True)):
            data = {'schema': 'full-window-operation-policy.v14', 'window_seconds': 600, 'model_seconds': 90, 'jev_seconds': 10}; data[key] = value
            with self.assertRaises(ValueError): self.m.canonical_operation_policy(wire(data))

    def test_strict_duplicate_nonfinite_unknown_and_trailing_json_rejected(self):
        raw = wire(self.vectors['approval']['document'])
        for bad in (raw+b'{}', raw.replace(b'"human_policy_approved":true', b'"human_policy_approved":NaN'),
                    raw.replace(b'"human_policy_approved":true', b'"human_policy_approved":true,"human_policy_approved":true'),
                    b'[]', b'null'):
            with self.assertRaises(ValueError): self.m.parse_operation_policy_approval(bad)


if __name__ == '__main__': unittest.main()
