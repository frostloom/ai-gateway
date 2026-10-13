"""Evidence eligibility and immutable historical archive regression tests."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'audit-historical-answer-evidence.py'


class HistoricalAnswerEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.exists(), 'historical evidence auditor must exist')
        spec = importlib.util.spec_from_file_location('historical_evidence', SCRIPT)
        self.audit = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.audit)

    def row(self, reply='请确认是否继续', verdict='pass', pending=False, code=200):
        pred = {'Sample': {'id': 'x-001', 'text': '查询'}, 'Reply': reply,
                'HTTPCode': code, 'Err': '', 'AllTools': [], 'Pending': pending}
        review = {'action_verdict': 'pass', 'reply_verdict': verdict,
                  'action_reason': 'prior action reason', 'reply_reason': 'prior reply reason'}
        return self.audit.normalize('x-001', 'fast', pred, review)

    def test_quantity_downgrades_eligibility_but_preserves_prior_label(self):
        row = self.row('套餐每月99元，额度100万token')
        self.assertEqual(row['status'], 'missing-historical-authority')
        self.assertEqual(row['prior_ai_review']['reply_verdict'], 'pass')
        self.assertTrue(row['prior_reply_pass_quantity_without_authority'])
        self.assertFalse(row['numeric_truth_verified'])

    def test_confirmation_is_only_first_turn_evidence(self):
        row = self.row('请确认充值50元', pending=True)
        self.assertEqual(row['status'], 'confirmation-only')
        self.assertFalse(row['full_task_verified'])

    def test_failed_reply_and_service_failure_are_retained(self):
        self.assertEqual(self.row('余额99元', 'fail')['status'], 'existing-semantic-review-fail')
        self.assertEqual(self.row('余额99元', 'pass', code=503)['status'], 'proven-service-failure')

    def test_necessary_clarification_can_retain_semantic_pass(self):
        self.assertEqual(self.row('请提供订单编号')['status'], 'existing-semantic-review-pass')

    def test_unverifiable_non_numeric_remains_missing_authority(self):
        self.assertEqual(self.row('该套餐正在销售', 'unverifiable')['status'], 'missing-historical-authority')

    def test_date_and_chinese_quantity_screening(self):
        for reply in ('到期日期2026-10-13', '余额三十元', '剩余额度一百万token', '已用20%'):
            with self.subTest(reply=reply):
                self.assertTrue(self.row(reply)['quantity_mentions'])

    def test_index_rejects_duplicates_unknown_and_changed_original(self):
        pred = {'Sample': {'id': 'x', 'text': 'original'}}
        definitions = {'x': pred['Sample']}
        with self.assertRaises(ValueError):
            self.audit.index_predictions([pred, pred], definitions)
        with self.assertRaises(ValueError):
            self.audit.index_predictions([{'Sample': {'id': 'unknown', 'text': 'original'}}], definitions)
        with self.assertRaises(ValueError):
            self.audit.index_predictions([{'Sample': {'id': 'x', 'text': 'changed'}}], definitions)

    def test_paths_reject_traversal_and_absolute(self):
        for path in ('../outside', '/outside', 'C:/outside', 'raw/../../outside', 'raw\\..\\outside'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                self.audit.safe_path(Path.cwd(), path)

    def test_pinned_sources_reject_coordinated_source_manifest_change(self):
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent / 'tests') as tmp:
            root = Path(tmp)
            (root / 'reviews.json').write_text('[]', encoding='utf-8')
            (root / 'archive-manifest.json').write_text(json.dumps({'files': [
                {'path': 'reviews.json', 'sha256': self.audit.sha(b'[]')}]}), encoding='utf-8')
            with self.assertRaises(ValueError):
                self.audit.load_sources(root)

    def test_archive_rederivation_rejects_changed_reply_and_coordinated_output(self):
        rows, sources = self.audit.load_sources()
        normalized = self.audit.derive(rows)
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent / 'tests') as tmp:
            out = Path(tmp) / 'archive'
            self.audit.build(out)
            self.audit.verify(out)
            data = self.audit.read(out / 'rows.json')
            data[0]['model_reply'] += ' tampered'
            self.audit.write_json(out / 'rows.json', data)
            manifest = self.audit.read(out / 'archive-manifest.json')
            manifest['files']['rows.json'] = self.audit.sha((out / 'rows.json').read_bytes())
            self.audit.write_json(out / 'archive-manifest.json', manifest)
            with self.assertRaises(ValueError):
                self.audit.verify(out)
        self.assertEqual(len(normalized), 3660)

    def test_build_refuses_existing_directory(self):
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent / 'tests') as tmp:
            with self.assertRaises(FileExistsError):
                self.audit.build(Path(tmp))


if __name__ == '__main__':
    unittest.main()
