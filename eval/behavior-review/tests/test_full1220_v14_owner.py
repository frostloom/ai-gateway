import sys
sys.dont_write_bytecode=True
from pathlib import Path
import types
import unittest
import tempfile


def load():
    p=Path(__file__).resolve().parents[1]/'full1220-v14-owner-template.py';m=types.ModuleType('v14_owner_test');m.__file__=str(p.resolve())
    exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__);return m

class Owner(unittest.TestCase):
    def test_default_no_io(self):
        m=load()
        def fail(*a,**k):raise AssertionError('default IO')
        m.Path=fail;m.run=fail
        self.assertEqual(m.main([]),0)
    def test_unfrozen_early(self):
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):load().run('a'*64)
    def test_technical_context_cannot_read_settings_or_spawn(self):
        m=load()
        with tempfile.TemporaryDirectory(prefix='v14-owner-') as temp:
            root=Path(temp).resolve();(root/'owner-preparation').mkdir()
            m.ROOT=str(root);m.__file__=str(root/'owner-preparation/run-owner.py')
            technical=object();verifier=types.SimpleNamespace(load_freeze_binding=lambda *a:technical)
            calls=[]
            def require_actual(value):
                calls.append(value)
                raise ValueError('actual signed timing policy required')
            verifier._require_actual_context=require_actual
            m._load=lambda root:verifier
            with self.assertRaisesRegex(ValueError,'actual signed timing policy required'):m.run('a'*64)
            self.assertEqual(calls,[technical])
            self.assertFalse((root/'actual-owner-launch-intent.json').exists())
    def test_explicit_order_and_duplicate_rejection(self):
        m=load();raw=b'{"operation":"run_window","expected_index":2,"prior_receipt_sha256":"abc"}'
        self.assertEqual(m.command(raw,2,'abc')['expected_index'],2)
        for index,prior in [(1,'abc'),(2,'def')]:
            with self.assertRaises(ValueError):m.command(raw,index,prior)
        for raw in [b'{"operation":"close","operation":"run_window"}',b'{"operation":"run_window","expected_index":true,"prior_receipt_sha256":""}',b'{"operation":"retry"}',b'{"operation":"status","approved":true}']:
            with self.assertRaises(ValueError):m.command(raw,1,'')
if __name__=='__main__':unittest.main()
