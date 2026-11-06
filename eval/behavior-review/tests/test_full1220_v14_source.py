"""Pure recording-state checks only; no source helper or actual authority."""
import hashlib
import json
from pathlib import Path
import types
import unittest

HERE = Path(__file__).resolve().parents[1]
SUFFIX_POLICY = '3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213'
PREFIX_POLICY = '604c68b1c192c17594ba422b85367579cabd60c31623af6a9632278e401ae970'


def wire(value):
    return json.dumps(value, separators=(',', ':')).encode()


def recording(*, prefix=False, negative=False):
    result = {'errors': ['semantic negative'] if negative else None,
              'observations': [{}] if negative else [],
              'structural_pass': True, 'success': False, 'setup_error': False,
              'review_requirements': [{}], 'status': 'failed' if negative else 'needs_review'}
    return {'policy_id': 'full-customer-recording-policy.v1' if prefix else 'full-customer-recording-preview-failure.v2',
            'policy_sha256': PREFIX_POLICY if prefix else SUFFIX_POLICY,
            'result_sha256': hashlib.sha256(wire(result)).hexdigest(),
            'disposition': 'continue_recorded',
            'issues': [{'code': 'semantic_negative', 'turn': 1, 'error_index': 0, 'disposition': 'continue_recorded'}] if negative else [],
            'result': result}


class Source(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        path = HERE/'full1220-v14-source.py'; cls.module = types.ModuleType('pure14source')
        if path.exists(): exec(compile(path.read_bytes(), str(path), 'exec'), cls.module.__dict__)

    def setUp(self):
        self.assertTrue(hasattr(self.module, 'record_header'), 'V14 source rules missing')
        self.m = self.module

    def test_suffix_and_original_prefix_have_separate_policy_checks(self):
        self.assertEqual(self.m.validate_completed_record_state(wire(recording())), 'recorded_nonnegative')
        self.assertEqual(self.m.validate_original_prefix_record_state(wire(recording(prefix=True))), 'recorded_nonnegative')
        with self.assertRaises(ValueError): self.m.validate_completed_record_state(wire(recording(prefix=True)))
        with self.assertRaises(ValueError): self.m.validate_original_prefix_record_state(wire(recording()))

    def test_recorded_negative_and_hold_are_preserved(self):
        self.assertEqual(self.m.validate_completed_record_state(wire(recording(negative=True))), 'recorded_negative')
        data = recording(); data['disposition'] = 'halt'
        self.assertEqual(self.m.record_header(wire(data))['disposition'], 'halt')
        with self.assertRaises(ValueError): self.m.validate_completed_record_state(wire(data))

    def test_private_fields_and_wrong_hash_are_rejected(self):
        for key, value in (('private_capture', {}), ('result_sha256', '0'*64), ('policy_sha256', PREFIX_POLICY)):
            data = recording(); data[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): self.m.record_header(wire(data))

    def test_issue_types_and_false_success_are_rejected(self):
        data = recording(negative=True); data['issues'][0]['error_index'] = False
        with self.assertRaises(ValueError): self.m.validate_completed_record_state(wire(data))
        data = recording(negative=True); data['result']['success'] = True
        data['result_sha256'] = hashlib.sha256(wire(data['result'])).hexdigest()
        with self.assertRaises(ValueError): self.m.validate_completed_record_state(wire(data))

    def test_document_duplicate_nonfinite_and_lossless_fragment_checks(self):
        for value in (b'{"x":1,"x":2}', b'{"x":NaN}', b'{"x":Infinity}', b'{"x":1}{}', b'{"x":1,}', b'[]'):
            with self.subTest(value=value), self.assertRaises(ValueError): self.m._Document(value)
        document = self.m._Document(b'{ "x" : { "space" : " a b " , "n" : 1.00 } }')
        self.assertEqual(document.fragment('x'), '{"space":" a b ","n":1.00}')

    def test_document_accepts_only_json_trailing_whitespace(self):
        self.m._Document(b'{"x":1} \t\r\n')
        for suffix in (b'\v', b'\x1c', '\u00a0'.encode()):
            with self.subTest(suffix=suffix), self.assertRaises(ValueError): self.m._Document(b'{"x":1}'+suffix)


if __name__ == '__main__': unittest.main()
