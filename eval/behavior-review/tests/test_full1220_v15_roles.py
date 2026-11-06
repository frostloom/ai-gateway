"""Inactive V15 owner/delivery/catalog boundaries; no process or network use."""
import hashlib
import json
from pathlib import Path
import sys
import types
import unittest

HERE = Path(__file__).resolve().parents[1]


def load(part):
    path = HERE/('full1220-v15-'+part+'.py')
    module = types.ModuleType('pure15_'+part.replace('-', '_')); module.__file__ = str(path)
    sys.modules[module.__name__] = module
    if path.exists(): exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module


def forbidden(*args, **kwargs):
    raise AssertionError('unauthorized IO or process')


class Roles(unittest.TestCase):
    def test_owner_inactive_default_and_unfrozen_cannot_spawn(self):
        m = load('owner-template'); self.assertTrue(hasattr(m, 'main'), 'V15 owner missing')
        m.Path = forbidden; m.subprocess = types.SimpleNamespace(Popen=forbidden)
        self.assertEqual(m.main([]), 0)
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'): m.run('a'*64)

    def test_owner_cannot_reorder_or_repeat_command(self):
        m = load('owner-template'); self.assertTrue(hasattr(m, 'command'), 'V15 owner missing')
        raw = b'{"operation":"run_window","expected_index":2,"prior_receipt_sha256":"abc"}'
        self.assertEqual(m.command(raw, 2, 'abc')['expected_index'], 2)
        for index, prior in ((1, 'abc'), (2, 'wrong'), (457, 'abc')):
            with self.assertRaises(ValueError): m.command(raw, index, prior)
        for raw in (b'{"operation":"retry"}', b'{"operation":"run_window","expected_index":true,"prior_receipt_sha256":""}',
                    b'{"operation":"status","approved":true}'):
            with self.assertRaises(ValueError): m.command(raw, 1, '')

    def test_delivery_forged_summary_cannot_read_credentials_or_send(self):
        m = load('delivery'); self.assertTrue(hasattr(m, 'ship_batch'), 'V15 delivery missing')
        m._read_settings = forbidden
        fake = {'normal': True, 'phase_timings_v15': [{'stage': 'acquisition', 'duration_ms': 1, 'deadline_remaining_ms': 1, 'outcome': 'ok'}]}
        with self.assertRaises(ValueError): m.ship_batch(verifier=None, cohort=fake, batch_index=1)
        with self.assertRaises(ValueError): m.verify_remote_window(verifier=None, cohort=fake, window_index=1, binding={}, plan={}, projection={}, readback_root='/bad', score_module=None)
        with self.assertRaises(ValueError): m.check_verified_remote_window({'all_verified': True}, {})

    def test_old14_catalog_is_rejected_before_signature_callback(self):
        m = load('helper-runtime'); self.assertTrue(hasattr(m, 'load_catalog'), 'V15 runner missing')
        data = {'schema': 'full-window-helper-build-catalog.v14', 'identity': {}, 'artifacts': [], 'build_evidence_sha256': 'a'*64, 'signature': '0'*128}
        raw = json.dumps(data).encode()
        with self.assertRaisesRegex(ValueError, 'catalog schema'):
            m.load_catalog(raw, expected_sha256=hashlib.sha256(raw).hexdigest(), trusted_public_key='b'*64,
                           expected_identity={}, verify_signature=forbidden)


if __name__ == '__main__': unittest.main()
