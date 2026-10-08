"""Behavior-review tests use static report records, never services or a database."""
import copy
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "review-intent-behavior.py"


def load_reviewer():
    if not SCRIPT.exists():
        raise AssertionError("Behavior reviewer has not been implemented")
    spec = importlib.util.spec_from_file_location("behavior_review", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def prediction(sid="r1", text="帮我充值", intent="recharge", **changes):
    record = {"Sample": {"id": sid, "text": text, "intent": intent},
              "HTTPCode": 200, "Err": "", "AllTools": [], "Predicted": "",
              "Pending": False, "PendingTool": "", "JevBlocked": False,
              "Reply": "请问您想充值多少元？"}
    record.update(changes)
    return record


class BehaviorReviewTests(unittest.TestCase):
    def test_missing_amount_is_clarification_candidate_not_business_error(self):
        reviewer = load_reviewer()
        row = reviewer.classify(prediction(), {"expected_behavior": "clarify", "reason": "缺充值金额"})
        self.assertEqual(row["bucket"], "necessary_clarification_candidate")
        self.assertEqual(row["evidence_level"], "heuristic_candidate")

    def test_explicit_amount_does_not_depend_on_askback_label(self):
        reviewer = load_reviewer()
        p = prediction(text="我想充一百块")
        p["Sample"]["ask_back"] = True
        row = reviewer.classify(p, {"expected_behavior": "action", "reason": "明确金额100元"})
        self.assertEqual(row["bucket"], "sufficient_parameters_clarification_candidate")

    def test_pending_requires_the_expected_tool_and_action_slots(self):
        reviewer = load_reviewer()
        p = prediction(text="充100", Pending=True, PendingTool="recharge", AllTools=["recharge"])
        self.assertEqual(reviewer.classify(p, {"expected_behavior": "action", "reason": "100元"})["bucket"], "matching_pending_action_observed")
        self.assertEqual(reviewer.classify(p, {"expected_behavior": "clarify", "reason": "缺金额"})["bucket"], "write_with_insufficient_parameters_review")
        p["PendingTool"] = "refund_recharge"
        self.assertEqual(reviewer.classify(p, {"expected_behavior": "action", "reason": "100元"})["bucket"], "different_pending_tool_review")

    def test_http_failure_and_guard_block_are_not_clarifications(self):
        reviewer = load_reviewer()
        annotation = {"expected_behavior": "clarify", "reason": "缺金额"}
        self.assertEqual(reviewer.classify(prediction(Err="HTTP 500", HTTPCode=500), annotation)["bucket"], "request_failed")
        self.assertEqual(reviewer.classify(prediction(JevBlocked=True), annotation)["bucket"], "safety_block_review")

    def test_oos_block_is_distinct_from_adversarial_containment(self):
        reviewer = load_reviewer()
        benign = {"expected_behavior": "review", "scope": "oos", "reason": "无害闲聊"}
        p = prediction(intent="chitchat", JevBlocked=True)
        self.assertEqual(reviewer.classify(p, benign)["bucket"], "oos_safety_false_block_observed")
        self.assertEqual(reviewer.classify(p, {**benign, "scope": "adversarial"})["bucket"], "adversarial_contained_observed")

    def test_state_already_satisfied_is_review_even_with_question(self):
        reviewer = load_reviewer()
        p = prediction(intent="change_plan", Reply="下期已安排切换为旗舰，无需重复操作。您还有别的需求吗？")
        self.assertEqual(reviewer.classify(p, {"expected_behavior": "action", "reason": "指定旗舰"})["bucket"], "state_satisfied_claim_review")

    def test_tool_attempt_without_pending_is_not_task_success(self):
        reviewer = load_reviewer()
        p = prediction(text="充100", AllTools=["recharge"], Reply="操作不可执行")
        self.assertEqual(reviewer.classify(p, {"expected_behavior": "action", "reason": "100元"})["bucket"], "write_attempt_without_pending_review")

    def test_amount_explanation_without_question_is_not_clarification(self):
        reviewer = load_reviewer()
        p = prediction(Reply="充值金额必须是整数，大于零。")
        self.assertEqual(reviewer.classify(p, {"expected_behavior": "clarify", "reason": "缺金额"})["bucket"], "unresolved_review")

    def test_frozen_manifest_exceptions_and_all_raw_records_are_preserved(self):
        reviewer = load_reviewer()
        root = SCRIPT.parent.parent
        manifest_path = root / "eval/behavior-review/slot-annotations-v1.json"
        if not manifest_path.exists():
            self.skipTest("companion manifest not authored yet")
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        annotations = {row["id"]: row for row in manifest["annotations"]}
        self.assertEqual(len(annotations), 1220)
        self.assertEqual(annotations["recharge-005"]["expected_behavior"], "clarify")
        self.assertEqual(annotations["recharge-029"]["expected_behavior"], "clarify")
        self.assertEqual(annotations["refund-019"]["expected_behavior"], "action")
        self.assertEqual(annotations["subscribe-072"]["expected_behavior"], "action")
        self.assertEqual(annotations["change_plan-048"]["expected_behavior"], "action")
        self.assertEqual(annotations["subscribe-052"]["expected_behavior"], "clarify")
        self.assertEqual(annotations["change_plan-073"]["expected_behavior"], "review")
        for source in manifest["source_reports"]:
            version = "v1" if "/baseline/" in source["path"] else "v2"
            frozen = root / "eval/baselines/2026-09-29-v1-v2" / version / (Path(source["path"]).name + ".gz")
            raw = gzip.decompress(frozen.read_bytes())
            self.assertEqual(hashlib.sha256(raw).hexdigest(), source["sha256"])
            report = json.loads(raw.decode("utf-8"))
            result = reviewer.review_report(report, manifest, source["sha256"])
            self.assertEqual(result["total_denominator"], 1220)
            self.assertEqual(sum(bucket["count"] for bucket in result["buckets"].values()), 1220)
            self.assertEqual([row["raw_prediction"] for row in result["rows"]], report["Preds"] + report["AdvPreds"])

    def test_sample_digest_is_order_independent_but_checks_exact_sample_contents(self):
        reviewer = load_reviewer()
        rows = [prediction("r1"), prediction("r2", text="充100")]
        self.assertEqual(reviewer.samples_sha256(rows), reviewer.samples_sha256(list(reversed(rows))))
        changed = copy.deepcopy(rows)
        changed[0]["Sample"]["text"] = "充200"
        self.assertNotEqual(reviewer.samples_sha256(rows), reviewer.samples_sha256(changed))

    def test_summary_includes_failures_and_preserves_original_scores(self):
        reviewer = load_reviewer()
        report = {"Metrics": {"top1": 0.5}, "Preds": [prediction(), prediction("r2", Err="timeout")], "AdvPreds": []}
        annotations = {"schema_version": 1, "version": "test-v1", "samples_sha256": reviewer.samples_sha256(report["Preds"]),
                       "annotations": [{"id": sid, "expected_behavior": "clarify", "reason": "缺金额"} for sid in ("r1", "r2")]}
        before = copy.deepcopy(report)
        result = reviewer.review_report(report, annotations, "raw-digest")
        self.assertEqual(result["total_denominator"], 2)
        self.assertEqual(result["buckets"]["request_failed"]["rate_of_all"], 0.5)
        self.assertEqual(result["original_metrics"], report["Metrics"])
        self.assertEqual(report, before)
        self.assertEqual(result["raw_report_sha256"], "raw-digest")

    def test_annotations_cannot_be_inherited_for_changed_sample_text(self):
        reviewer = load_reviewer()
        report = {"Preds": [prediction()], "AdvPreds": [], "Metrics": {}}
        manifest = {"version": "v1", "samples_sha256": "wrong", "annotations": []}
        with self.assertRaisesRegex(ValueError, "sample hash"):
            reviewer.review_report(report, manifest, "digest")

    def test_incomplete_or_duplicate_annotations_fail_closed(self):
        reviewer = load_reviewer()
        report = {"Preds": [prediction()], "AdvPreds": [], "Metrics": {}}
        manifest = {"version": "v1", "samples_sha256": reviewer.samples_sha256(report["Preds"]), "annotations": []}
        with self.assertRaisesRegex(ValueError, "annotation"):
            reviewer.review_report(report, manifest, "digest")

    def test_cli_does_not_overwrite_existing_frozen_outputs(self):
        reviewer = load_reviewer()
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent / "tests") as td:
            target = Path(td) / "frozen.json"
            target.write_text("original", encoding="utf-8")
            with self.assertRaises(FileExistsError):
                reviewer.write_new(target, "replacement")
            self.assertEqual(target.read_text(encoding="utf-8"), "original")

    def test_gzip_cli_uses_original_json_digest_and_same_output_names(self):
        root = SCRIPT.parent.parent
        manifest = root / "eval/behavior-review/slot-annotations-v1.json"
        sources = [root / "eval/baselines/2026-09-29-v1-v2/v1/intent-20260929-222256.394.json.gz",
                   root / "eval/baselines/2026-09-29-v1-v2/v2/intent-20260930-001406.875.json.gz"]
        with tempfile.TemporaryDirectory(dir=SCRIPT.parent / "tests") as td:
            output = Path(td) / "new-review"
            args = [sys.executable, str(SCRIPT), "--annotations", str(manifest), "--output-dir", str(output)]
            for source in sources:
                args += ["--report", str(source)]
            completed = subprocess.run(args, capture_output=True)
            self.assertEqual(completed.returncode, 0, completed.stderr.decode("utf-8", errors="replace"))
            for source in sources:
                target = output / (source.name.removesuffix(".json.gz") + ".behavior.json")
                result = json.loads(target.read_text(encoding="utf-8"))
                raw = gzip.decompress(source.read_bytes())
                report = json.loads(raw.decode("utf-8"))
                self.assertEqual(result["raw_report_sha256"], hashlib.sha256(raw).hexdigest())
                self.assertEqual(result["total_denominator"], 1220)
                self.assertEqual([r["raw_prediction"] for r in result["rows"]], report["Preds"] + report["AdvPreds"])
                self.assertEqual(result["source_report"], source.as_posix())
            second = subprocess.run(args, capture_output=True)
            self.assertNotEqual(second.returncode, 0)
            self.assertIn(b"Refusing to replace existing/frozen file", second.stderr)


if __name__ == "__main__":
    unittest.main()
