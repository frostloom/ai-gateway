import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def load_tool():
    spec = importlib.util.spec_from_file_location('full_pilot_prepare', ROOT / 'scripts/prepare-full-customer-pilot.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class FullPilotPreparationTests(unittest.TestCase):
    def test_1220_unique_primary_tasks_and_separate_boundaries(self):
        p = load_tool().prepare(ROOT)
        self.assertEqual(p['source_cases'], 1220)
        self.assertEqual(len(p['primary']), 1220)
        self.assertEqual(len({c['source_case_id'] for c in p['primary']}), 1220)
        self.assertEqual(p['paired_arms'], 2440)
        self.assertEqual(len(p['boundary_scenarios']), 1851)
        self.assertFalse(p['ready_for_paid_run'])
        self.assertEqual(p['model_calls'], 0)
        self.assertEqual(p['jev_calls'], 0)
        self.assertIsNone(p['authorized_micro_cny'])
        self.assertTrue(all(c['measurement_scope'] == 'complete_task' for c in p['primary']))
        self.assertTrue(all(c['answer_review_status'] == 'pending_independent_review' for c in p['primary']))

    def test_batches_cover_exactly_once_and_respect_existing_limits(self):
        p = load_tool().prepare(ROOT)
        self.assertEqual([key for b in p['batches'] for key in b['keys']], [c['key'] for c in p['primary']])
        for b in p['batches']:
            self.assertLessEqual(len(b['keys']), 12)
            self.assertLessEqual(b['model_attempt_ceiling'], 256)
            self.assertLessEqual(b['jev_attempt_ceiling'], 256)
            self.assertEqual(b['model_reservation_micro_cny'], b['model_attempt_ceiling'] * 33792)
        self.assertEqual(sum(b['model_attempt_ceiling'] for b in p['batches']), p['model_attempt_ceiling'])
        self.assertGreater(p['model_attempt_ceiling'], 2440)

    def test_selection_rejects_missing_or_duplicate_sources(self):
        m = load_tool()
        with self.assertRaises(ValueError):
            m.select_primary([{'source_case_id': 'a', 'scenarios': []}])
        rows = m.load_definitions(ROOT)
        with self.assertRaises(ValueError):
            m.select_primary(rows + [rows[0]])


if __name__ == '__main__':
    unittest.main()
