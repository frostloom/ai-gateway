"""Offline stage decomposition; no inference or environment access."""
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('jev_overhead', ROOT / 'scripts/analyze-jev-overhead.py')
overhead = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(overhead)
PILOT = ROOT / 'eval/paired-pilots/2026-10-17-jev-acceptance-v1'


class OverheadTests(unittest.TestCase):
    def journal(self):
        return [json.loads(x) for x in (PILOT / 'attempt-01/run.jsonl').read_text(encoding='utf-8').splitlines()]

    def test_real_turns_groups_and_conservation(self):
        result = overhead.analyze_pilot(PILOT)
        self.assertEqual(len(result['turns']), 14)
        self.assertEqual(sum(g['turns'] for g in result['groups'].values()), 14)
        self.assertEqual(result['groups']['jev_selective/direct_read']['llm_calls'], 0)
        self.assertEqual(result['groups']['jev_selective/direct_read']['jev_calls'], 1)
        fallbacks = [t for t in result['turns'] if t['route'].startswith('fallback:')]
        self.assertEqual(len(fallbacks), 4)
        self.assertEqual(sum(t['llm_calls'] for t in fallbacks), 5)
        self.assertEqual(sum(t['jev_input_tokens'] for t in fallbacks), 4848)
        deterministic = [t for t in result['turns'] if t['route'] == 'no_inference']
        self.assertEqual(len(deterministic), 4)
        self.assertTrue(all(t['llm_calls'] == t['jev_calls'] == 0 for t in deterministic))
        self.assertEqual(len([t for t in result['turns'] if t['recorded_action'] == 'unreviewed']), 4)
        for turn in result['turns']:
            self.assertAlmostEqual(turn['server_latency_ms'], sum(turn[k] for k in ('jev_latency_ms', 'llm_latency_ms', 'tool_latency_ms', 'other_latency_ms')), places=6)
        self.assertFalse(result['effect_claims_allowed'])
        self.assertIsNone(result['necessary_clarification_success_rate'])
        for arm in ('model', 'jev_selective'):
            totals = result['verified_efficiency']['arms'][arm]
            turns = [t for t in result['turns'] if t['arm'] == arm]
            for k in ('llm_calls', 'jev_calls', 'llm_billable_input_tokens', 'llm_output_tokens', 'jev_input_tokens', 'jev_output_tokens'):
                self.assertEqual(sum(t[k] for t in turns), totals[k])

    def test_invalid_phase_timings_are_rejected(self):
        for field in ('jev_latency_ms', 'llm_latency_ms', 'tool_latency_ms', 'total_latency_ms'):
            for value in (None, -1, float('nan'), float('inf'), True):
                journal = self.journal()
                t = next(e for e in journal if e['kind'] == 'finish')['result']['observations'][0]['turn']['telemetry_raw']
                t[field] = value
                with self.subTest(field=field, value=value), self.assertRaisesRegex(ValueError, 'latency'):
                    overhead.summarize_turns(journal)

    def test_phase_sum_cannot_exceed_total(self):
        journal = self.journal()
        t = next(e for e in journal if e['kind'] == 'finish')['result']['observations'][0]['turn']['telemetry_raw']
        t['tool_latency_ms'] = t['total_latency_ms'] + 1
        with self.assertRaisesRegex(ValueError, 'phase'):
            overhead.summarize_turns(journal)

    def test_invalid_direct_read_is_rejected(self):
        for field, value in (('llm_calls', 1), ('jev_calls', 0), ('jev_fast_path', False)):
            journal = self.journal()
            t = next(e for e in journal if e['kind'] == 'finish' and e['arm'] == 'jev_selective')['result']['observations'][0]['turn']['telemetry_raw']
            t[field] = value
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, 'direct_read'):
                overhead.summarize_turns(journal)

    def test_missing_reason_is_unknown_not_error(self):
        journal = self.journal()
        t = next(e for e in journal if e['kind'] == 'finish' and e['key'].startswith('balance-020') and e['arm'] == 'jev_selective')['result']['observations'][0]['turn']['telemetry_raw']
        t.pop('jev_fast_path_reason')
        result = overhead.summarize_turns(journal)
        self.assertIn('jev_selective/fallback:unknown', result['groups'])

    def test_invalid_sources_fail_before_decomposition(self):
        with patch.object(overhead.audit, 'audit_pilot', side_effect=ValueError('source digest mismatch')):
            with patch.object(overhead, 'summarize_turns') as summarize:
                with self.assertRaisesRegex(ValueError, 'digest'):
                    overhead.analyze_pilot(PILOT)
                summarize.assert_not_called()

    def test_post_audit_source_replacement_is_rejected(self):
        verified = overhead.audit.audit_pilot(PILOT)
        journal = self.journal()
        t = next(e for e in journal if e['kind'] == 'finish')['result']['observations'][0]['turn']['telemetry_raw']
        t['llm_latency_ms'] -= 100
        changed = '\n'.join(json.dumps(e) for e in journal).encode('utf-8')
        original_read = Path.read_bytes
        for target in ('manifest.json', 'run.jsonl'):
            def reread(path):
                if path.name == target:
                    return changed if target == 'run.jsonl' else b'{}'
                return original_read(path)
            with self.subTest(target=target), patch.object(overhead.audit, 'audit_pilot', return_value=verified):
                with patch.object(Path, 'read_bytes', autospec=True, side_effect=reread):
                    with patch.object(Path, 'read_text', return_value=changed.decode('utf-8')):
                        with self.assertRaisesRegex(ValueError, 'digest'):
                            overhead.analyze_pilot(PILOT)


if __name__ == '__main__':
    unittest.main()
