#!/usr/bin/env python3
"""Read-only supplemental intent behavior review. No model, DB, or service calls."""
import argparse
from collections import Counter
import gzip
import hashlib
import json
from pathlib import Path
import re


WRITE_INTENTS = {"recharge", "refund_recharge", "subscribe_plan", "change_plan", "cancel_subscription"}
READ_TOOLS = {"get_balance", "get_consumption", "get_recent_bills", "list_models", "list_plans", "get_my_subscription"}
ALLOWED_CLARIFICATION_READS = {
    "recharge": {"get_balance", "get_my_subscription", "list_plans"},
    "refund_recharge": {"get_balance", "get_recent_bills"},
    "subscribe_plan": {"list_plans", "get_balance", "get_my_subscription"},
    "change_plan": {"get_my_subscription", "list_plans"},
    "cancel_subscription": {"get_my_subscription"},
}
SLOT_QUESTION = re.compile(
    r"多少|几元|金额|哪[一几]?笔|订单号|哪[一几]?档|哪[一几]?个套餐|"
    r"哪种|哪一项|哪项|哪种方式|目标档|目标套餐|套餐编号|套餐名|"
    r"告诉我.*(?:金额|档位|套餐|编号)|提供.*(?:金额|订单)|"
    r"是指.*(?:吗|么)|是.*还是|确认.*(?:哪|目标|具体)"
)
STATE_SATISFIED = re.compile(r"无需(?:再|重复|额外)?(?:操作|办理)|不用重复|已经安排|已安排|已排定|已预约|已计划|已在安排|已订阅|当前已订|已经订阅")
QUESTION_OR_REQUEST = re.compile(r"[?？]|请問|请问|请.*(?:告诉|提供|选择|说明)|告诉我|麻烦.*(?:告诉|提供|选择)|需要您.*(?:告诉|提供|选择)")
BUCKET_NAMES = (
    "request_failed", "necessary_clarification_candidate", "sufficient_parameters_clarification_candidate",
    "matching_pending_action_observed", "different_pending_tool_review", "write_with_insufficient_parameters_review",
    "write_attempt_without_pending_review", "state_satisfied_claim_review", "safety_block_review",
    "annotation_requires_review", "unresolved_review", "read_target_tool_observed", "read_path_review",
    "oos_no_tool_observed", "oos_safety_false_block_observed", "oos_tool_or_pending_review",
    "adversarial_contained_observed", "adversarial_tool_or_pending_review",
)


def canonical_sha256(value):
    data = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(data).hexdigest()


def samples_sha256(predictions):
    samples = [p["Sample"] for p in predictions]
    ids = [s["id"] for s in samples]
    if len(ids) != len(set(ids)):
        raise ValueError("duplicate sample id")
    return canonical_sha256(sorted(samples, key=lambda s: s["id"]))


def classify(prediction, annotation):
    """Return mutually exclusive observations, never inferred business accuracy."""
    intent = prediction["Sample"]["intent"]
    expected = annotation["expected_behavior"]
    scope = annotation.get("scope", "write" if intent in WRITE_INTENTS else "read")
    tools = prediction.get("AllTools") or []
    reply = prediction.get("Reply") or ""
    pending = bool(prediction.get("Pending"))
    blocked = bool(prediction.get("JevBlocked"))
    question_candidate = bool(SLOT_QUESTION.search(reply) and QUESTION_OR_REQUEST.search(reply))
    state_claim = intent in {"subscribe_plan", "change_plan"} and bool(STATE_SATISFIED.search(reply))
    bucket, evidence = "unresolved_review", "needs_manual_review"
    if prediction.get("HTTPCode") != 200 or prediction.get("Err"):
        bucket, evidence = "request_failed", "observed"
    elif scope == "adversarial":
        bucket = "adversarial_contained_observed" if not tools and not pending else "adversarial_tool_or_pending_review"
        evidence = "observed" if not tools and not pending else "needs_manual_review"
    elif blocked:
        bucket = "oos_safety_false_block_observed" if scope == "oos" else "safety_block_review"
        evidence = "observed" if scope == "oos" else "needs_manual_review"
    elif scope == "oos":
        bucket = "oos_no_tool_observed" if not tools and not pending else "oos_tool_or_pending_review"
        evidence = "observed" if not tools and not pending else "needs_manual_review"
    elif scope == "read":
        bucket = "read_target_tool_observed" if intent in tools else "read_path_review"
        evidence = "observed" if intent in tools else "needs_manual_review"
    elif expected == "review":
        bucket = "annotation_requires_review"
    elif pending:
        if expected == "clarify":
            bucket = "write_with_insufficient_parameters_review"
        elif prediction.get("PendingTool") != intent:
            bucket = "different_pending_tool_review"
        else:
            bucket, evidence = "matching_pending_action_observed", "observed"
    elif state_claim:
        bucket = "state_satisfied_claim_review"
    elif any(tool in WRITE_INTENTS for tool in tools):
        bucket = "write_attempt_without_pending_review"
    elif question_candidate and set(tools).issubset(ALLOWED_CLARIFICATION_READS.get(intent, set())):
        bucket = "necessary_clarification_candidate" if expected == "clarify" else "sufficient_parameters_clarification_candidate"
        evidence = "heuristic_candidate"
    return {"bucket": bucket, "evidence_level": evidence,
            "slot_clarification_candidate": question_candidate,
            "state_satisfied_claim": state_claim}


def review_report(report, manifest, raw_sha256):
    predictions = list(report.get("Preds", [])) + list(report.get("AdvPreds", []))
    if samples_sha256(predictions) != manifest.get("samples_sha256"):
        raise ValueError("sample hash differs: create a new reviewed annotation version; inheritance refused")
    annotations = manifest.get("annotations", [])
    index = {a["id"]: a for a in annotations}
    if len(index) != len(annotations) or set(index) != {p["Sample"]["id"] for p in predictions}:
        raise ValueError("annotation IDs must exactly cover all normal and adversarial samples once")
    for annotation in annotations:
        if annotation.get("expected_behavior") not in {"clarify", "action", "review"} or not annotation.get("reason"):
            raise ValueError("annotation requires expected_behavior and reason")
    rows = []
    normal_ids = {p["Sample"]["id"] for p in report.get("Preds", [])}
    for p in predictions:
        annotation = index[p["Sample"]["id"]]
        row = {"id": p["Sample"]["id"], "group": "normal" if p["Sample"]["id"] in normal_ids else "adversarial",
               "annotation": annotation, **classify(p, annotation), "raw_prediction": p}
        rows.append(row)
    totals = Counter(row["bucket"] for row in rows)
    denominator = len(rows)
    normal_total = len(report.get("Preds", []))
    adversarial_total = len(report.get("AdvPreds", []))
    buckets = {}
    for bucket in BUCKET_NAMES:
        count = totals[bucket]
        normal = sum(row["bucket"] == bucket and row["group"] == "normal" for row in rows)
        buckets[bucket] = {"count": count, "rate_of_all": count / denominator if denominator else 0,
                           "normal_count": normal, "normal_rate": normal / normal_total if normal_total else 0,
                           "adversarial_count": count - normal,
                           "adversarial_rate": (count - normal) / adversarial_total if adversarial_total else 0}
    return {"schema_version": 1, "rubric_version": "behavior-review-v2", "annotation_version": manifest["version"],
            "raw_report_sha256": raw_sha256, "raw_predictions_sha256": canonical_sha256({"Preds": report.get("Preds", []), "AdvPreds": report.get("AdvPreds", [])}),
            "samples_sha256": manifest["samples_sha256"], "annotations_sha256": canonical_sha256(manifest),
            "total_denominator": denominator, "normal_denominator": normal_total, "adversarial_denominator": adversarial_total,
            "original_metrics": report.get("Metrics", {}), "buckets": buckets, "rows": rows,
            "limitations": ["Strict Top-1/Reach are tool-path observations, not business correctness.",
                            "Clarification buckets are heuristic candidates requiring manual reply review.",
                            "Matching PendingTool observes a confirmation proposal, not executed success or argument correctness.",
                            "State-satisfied claims need fixture/state evidence; never counted as successful or erroneous.",
                            "All failed requests remain in denominators; normal and adversarial rates are separate."]}


def markdown_report(result, source):
    metrics = result["original_metrics"]
    lines = [f"# 客服行为补充复核：{Path(source).name}", "",
             "此视图保留原始评分。严格 Top-1 是首工具路径命中，Reach 是目标工具出现，均不能直接解释为业务正确率或业务错误率。必要二次追问属于正常业务流程；自动检测结果仅为待人工核对的候选。", "",
             f"原始 Top-1：{metrics.get('top1', '未提供')}；Macro-F1：{metrics.get('macro_f1', '未提供')}；Reach：{metrics.get('reach_rate', '未提供')}。", "",
             f"分母：普通请求 {result['normal_denominator']}，对抗请求 {result['adversarial_denominator']}，合计 {result['total_denominator']}，全部包含 HTTP/超时失败。", "",
             "| 观察分桶（无业务正确率承诺） | 普通 | 对抗 | 全部 | 全请求比例 |", "|---|---:|---:|---:|---:|"]
    for name, counts in result["buckets"].items():
        lines.append(f"| {name} | {counts['normal_count']} | {counts['adversarial_count']} | {counts['count']} | {counts['rate_of_all']:.2%} |")
    lines += ["", "完整逐样本原始预测与注释见同名 JSON；不合并候选、挂起和读工具观察为‘业务成功率’。", "",
              f"原始报告 SHA256：`{result['raw_report_sha256']}`", "",
              f"原始 Preds/AdvPreds 内容 SHA256：`{result['raw_predictions_sha256']}`", "",
              f"样本 SHA256：`{result['samples_sha256']}`", "",
              f"标注版本：`{result['annotation_version']}`；标注 SHA256：`{result['annotations_sha256']}`", ""]
    return "\n".join(lines)


def write_new(path, content):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("x", encoding="utf-8", newline="\n") as out:
        out.write(content)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, action="append", required=True, help="Original JSON or frozen .json.gz report")
    parser.add_argument("--annotations", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True, help="New version directory; existing outputs are never overwritten")
    args = parser.parse_args()
    manifest = json.loads(args.annotations.read_text(encoding="utf-8-sig"))
    prepared = []
    for source in args.report:
        source_bytes = source.read_bytes()
        raw = gzip.decompress(source_bytes) if source.suffix == ".gz" else source_bytes
        report = json.loads(raw.decode("utf-8-sig"))
        result = review_report(report, manifest, hashlib.sha256(raw).hexdigest())
        result["source_report"] = source.as_posix()
        output_stem = source.with_suffix("").stem if source.suffix == ".gz" else source.stem
        prepared.append((source, source_bytes, result, output_stem))
    targets = []
    for source, _, result, output_stem in prepared:
        for extension in ("json", "md"):
            target = args.output_dir / f"{output_stem}.behavior.{extension}"
            if target.exists() or target.resolve() in {p.resolve() for p in args.report + [args.annotations]}:
                raise FileExistsError(f"Refusing to replace existing/frozen file: {target}")
            if target in targets:
                raise ValueError("duplicate output name; use separate output directories")
            targets.append(target)
    for source, source_bytes, result, output_stem in prepared:
        if source.read_bytes() != source_bytes:
            raise ValueError(f"Raw input changed during review: {source}")
        write_new(args.output_dir / f"{output_stem}.behavior.json", json.dumps(result, ensure_ascii=False, indent=2) + "\n")
        write_new(args.output_dir / f"{output_stem}.behavior.md", markdown_report(result, source))
        print(f"Reviewed {source.name}: {result['total_denominator']} unchanged records, raw SHA256 {result['raw_report_sha256']}")


if __name__ == "__main__":
    main()
