import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


def tool():
    spec = importlib.util.spec_from_file_location('runtime_profiles', ROOT / 'scripts/prepare-runtime-customer-profiles.py')
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    return m


class RuntimeProfilesTests(unittest.TestCase):
    def test_integer_recharge_reproduces_all_balances_and_preserves_read_oracles(self):
        m = tool()
        source = json.loads(m.SOURCE.read_bytes())
        out = m.build(source)
        self.assertEqual(set(out['accounts']), set(source['accounts']))
        for name, p in out['accounts'].items():
            old = source['accounts'][name]
            self.assertEqual(p['paid_order']['amount_cent'], p['paid_order']['amount_money'] * 100)
            self.assertEqual(p['initial_quota_cent'] + p['paid_order']['amount_cent'] - sum(b['amount_cent'] for b in p['cash_bill_fixture']), p['balance_cent'])
            self.assertEqual(p['balance_cent'], old['balance_cent'])
            self.assertEqual(p['tool_expected'], old['tool_expected'])
            self.assertEqual(p['subscription'], old['subscription'])
        self.assertEqual(source['accounts']['no_subscription']['paid_order']['amount_cent'], 500250)

    def test_invalid_balances_or_consumption_rejected(self):
        m = tool()
        source = json.loads(m.SOURCE.read_bytes())
        source['accounts']['low_balance']['balance_cent'] = 2501
        with self.assertRaises(ValueError):
            m.build(source)

    def test_archive_cannot_be_overwritten_or_tampered(self):
        m = tool()
        with tempfile.TemporaryDirectory(dir=ROOT) as temp:
            out = Path(temp) / 'profiles'
            m.export(out)
            self.assertEqual(m.verify(out)['profiles'], 6)
            with self.assertRaises(FileExistsError):
                m.export(out)
            (out / 'profiles.json').write_text('{}', encoding='utf8')
            with self.assertRaises(ValueError):
                m.verify(out)


if __name__ == '__main__':
    unittest.main()
