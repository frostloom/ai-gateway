"""Checkout rules for byte-locked offline evidence. No model or network calls."""
import importlib.util
import subprocess
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SUMMARIZE = ROOT / 'scripts/summarize-offline-intent-audit.py'

# Working-tree bytes compared with locked hashes by the CI verify commands.
LOCKED_PATHS = (
    'eval/intent-suite/manifest.json',
    'eval/intent-suite/balance.json',
    'web/scripts/gen-intent-suite.mjs',
    'scripts/customer-service-contract.py',
    'scripts/summarize-offline-intent-audit.py',
)


def load_summarize():
    spec = importlib.util.spec_from_file_location('summarize_offline_intent_audit', SUMMARIZE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class HistoricalEvidenceCheckoutTests(unittest.TestCase):
    def test_locked_sources_are_not_text_converted(self):
        completed = subprocess.run(
            ['git', 'check-attr', 'text', '--', *LOCKED_PATHS],
            cwd=ROOT, check=True, capture_output=True, text=True)
        for line in completed.stdout.splitlines():
            path, _, value = line.split(': ')
            self.assertEqual(value, 'unset', path)

    def test_windows_source_path_stays_inside_repo(self):
        tool = load_summarize()
        relative = r'eval\experiments\2026-10-08-jev-fast-path\raw\revised-full-nojev\intent.json.gz'
        path = tool.evidence_path(relative)
        self.assertEqual(path, tool.ROOT / 'eval/experiments/2026-10-08-jev-fast-path/raw/revised-full-nojev/intent.json.gz')
        self.assertTrue(path.is_relative_to(tool.ROOT))


if __name__ == '__main__':
    unittest.main()
