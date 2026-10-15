import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def load():
    spec = importlib.util.spec_from_file_location('observed_pilot', ROOT / 'scripts/prepare-observed-customer-pilot.py')
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    return m


class ObservedPilotTests(unittest.TestCase):
    def test_preserves_primary_cases_and_budgets_without_authorizing(self):
        m = load()
        p = m.prepare(ROOT)
        old = m.base.prepare(ROOT)
        for key in ('primary', 'batches', 'boundary_scenarios', 'model_attempt_ceiling', 'model_reservation_micro_cny'):
            self.assertEqual(p[key], old[key])
        self.assertEqual(p['source_cases'], 1220)
        self.assertFalse(p['ready_for_paid_run'])
        self.assertIsNone(p['authorized_micro_cny'])
        self.assertEqual(p['model_calls'], 0)
        self.assertEqual(p['jev_calls'], 0)

    def test_binds_observability_and_score_sync_code(self):
        p = load().prepare(ROOT)
        hashes = p['source_bindings']['runtime_files_sha256']
        for path in ('internal/observability/content.go', 'internal/observability/evaluation.go',
                     'scripts/langfuse-eval.py', 'internal/taskbench/paired_report.go'):
            self.assertIn(path, hashes)
        self.assertTrue(p['observability_requirements']['capture_content'])
        self.assertEqual(p['observability_requirements']['sample_rate'], 1)
        self.assertFalse(p['observability_requirements']['llm_judge_enabled'])


if __name__ == '__main__':
    unittest.main()
