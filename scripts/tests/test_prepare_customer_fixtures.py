"""Fixture preparation stays local and cannot manufacture historical truth."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'prepare-customer-fixtures.py'


def load():
    if not SCRIPT.exists():
        raise AssertionError('Fixture preparation exporter is missing')
    spec = importlib.util.spec_from_file_location('fixture_prep', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def case(sid, tool='get_balance', action='read', text='查询现金余额'):
    return {'id': 'contract:' + sid, 'source_case_id': sid, 'text': text,
            'original': {'id': sid, 'text': text}, 'expected_action': action,
            'allowed_next_actions': [action], 'allowed_tools': [tool],
            'missing_slots': [], 'required_facts': ['核对真实工具结果'],
            'annotation_status': 'ai_posthoc_not_independent_gold'}


class FixturePreparationTests(unittest.TestCase):
    def test_read_variants_and_blocked_cases_keep_all_source_ids(self):
        tool = load()
        cases = [case('balance-1'), case('subscription-1', 'get_my_subscription'),
                 case('refund-1', 'refund_recharge', 'preview')]
        bundle = tool.build(cases)
        self.assertEqual(len(bundle['readiness']), 3)
        self.assertEqual(len(bundle['cases']), 3)  # balance + active/absent subscription
        blocked = bundle['readiness'][2]
        self.assertEqual(blocked['status'], 'blocked')
        self.assertTrue(blocked['blockers'])
        self.assertFalse(any(r['historical_truth_recovered'] for r in bundle['readiness']))
        self.assertTrue(all(not r['database_instantiated'] for r in bundle['readiness']))
        self.assertTrue(all(r['fixture_kind'] == 'future_synthetic_development' for r in bundle['fixtures']))
        self.assertEqual([c['initial_subscription'] for c in bundle['cases'][1:]], [True, False])

    def test_ambiguous_or_parameterized_reads_are_not_promoted(self):
        ambiguous = case('ambiguous', action='clarify')
        parameterized = case('filtered')
        parameterized['missing_slots'] = ['time_range']
        mixed = case('mixed')
        mixed['allowed_tools'] = ['get_balance', 'recharge']
        bundle = load().build([ambiguous, parameterized, mixed])
        self.assertEqual(bundle['cases'], [])
        self.assertTrue(all(r['status'] == 'blocked' for r in bundle['readiness']))

    def test_duplicate_and_normalized_texts_share_partition_and_not_blind(self):
        a, b = case('a'), case('b')
        b['original']['text'] += ' #2'
        b['normalization'] = {'original_text': b['original']['text'], 'text': b['text']}
        bundle = load().build([a, b])
        first, second = bundle['readiness']
        self.assertEqual(first['text_group'], second['text_group'])
        self.assertEqual(first['partition'], second['partition'])
        self.assertEqual(bundle['summary']['independent_blind_cases'], 0)
        self.assertEqual(bundle, load().build([a, b]))

    def test_refuses_invalid_ids_and_normalization_without_proof(self):
        tool = load()
        with self.assertRaises(ValueError):
            tool.build([case('same'), case('same')])
        with self.assertRaises(ValueError):
            tool.build([case('same'), dict(case('other'), id='contract:same')])
        changed = case('changed')
        changed['text'] = 'different'
        with self.assertRaises(ValueError):
            tool.build([changed])

    def test_verify_recomputes_outputs_and_rejects_tampering_or_overwrite(self):
        tool = load()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parents[1]) as directory:
            root = Path(directory)
            source = root / 'source.json'
            source.write_text(json.dumps([case('one')]), encoding='utf-8')
            out = root / 'export'
            tool.export(source, out)
            tool.verify(source, out)
            original = (out / 'cases.json').read_bytes()
            with self.assertRaises(FileExistsError):
                tool.export(source, out)
            self.assertEqual((out / 'cases.json').read_bytes(), original)
            (out / 'cases.json').write_text('[]', encoding='utf-8')
            with self.assertRaises(ValueError):
                tool.verify(source, out)


if __name__ == '__main__':
    unittest.main()
