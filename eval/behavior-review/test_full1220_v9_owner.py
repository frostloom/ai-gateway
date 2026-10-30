import sys
sys.dont_write_bytecode=True
from pathlib import Path
import types
import unittest


def load():
    p=Path(__file__).with_name('full1220-v9-owner-template.py');m=types.ModuleType('v9_owner_test');m.__file__=str(p.resolve())
    exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__);return m

class Owner(unittest.TestCase):
    def test_default_no_io(self):
        m=load()
        def fail(*a,**k):raise AssertionError('default IO')
        m.Path=fail;m.run=fail
        self.assertEqual(m.main([]),0)
    def test_unfrozen_early(self):
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):load().run('a'*64)
    def test_explicit_order_and_duplicate_rejection(self):
        m=load();raw=b'{"operation":"run_window","expected_index":2,"prior_receipt_sha256":"abc"}'
        self.assertEqual(m.command(raw,2,'abc')['expected_index'],2)
        for index,prior in [(1,'abc'),(2,'def')]:
            with self.assertRaises(ValueError):m.command(raw,index,prior)
        for raw in [b'{"operation":"close","operation":"run_window"}',b'{"operation":"run_window","expected_index":true,"prior_receipt_sha256":""}',b'{"operation":"retry"}',b'{"operation":"status","approved":true}']:
            with self.assertRaises(ValueError):m.command(raw,1,'')
if __name__=='__main__':unittest.main()
