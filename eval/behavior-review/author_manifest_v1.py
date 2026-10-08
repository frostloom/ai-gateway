"""One-time, explicit-ID authoring record for the frozen companion annotations."""
import hashlib
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("reviewer", ROOT / "scripts/review-intent-behavior.py")
reviewer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reviewer)
sources = [ROOT / "eval/reports/2026-09-29/baseline/intent-20260929-222256.394.json",
           ROOT / "eval/reports/2026-09-29/with-jev/intent-20260930-001406.875.json"]
report = json.loads(sources[0].read_text(encoding="utf-8"))
rows = report["Preds"] + report["AdvPreds"]

# Every ID below was selected after reading all 400 source texts, independently
# of AskBack, observed predictions, and original scores. No runtime regex labels.
def ids(*items):
    result = set()
    for item in items:
        result.update(item if isinstance(item, range) else [item])
    return result

action = {
    "recharge": ids(2, 7, 8, 10, 11, 12, 19, 20, 25, 26, 33, 35, 37, 39, 40,
                    41, 42, 44, 45, 46, 47, 49, 50, range(51, 61), range(71, 75),
                    77, 78, 79, 81, 82, 83, 85, 86, 87, range(89, 96), range(97, 101)),
    "refund_recharge": ids(3, 7, 8, 10, 19, 25, 26, 33, 35, 37, 39, 45, 46, 47,
                            49, 50, range(51, 61), 71, 74, 76, 77, 79, 81, 83,
                            85, 86, 87, range(90, 96), range(97, 101)),
    "subscribe_plan": ids(2, 4, 6, 10, 12, 23, 29, 36, 40, 46, 51, 53, 54, 55,
                           56, 57, 59, 60, 72, 75, 77, 80, 82, 85, 87, 90, 92, 95, 98),
    "change_plan": ids(3, 6, 8, 13, 14, 23, 25, 38, 48, range(51, 61), 74, 76,
                        80, 85, 86, 90, 92, 94, 95, 98, 99),
}
review_reasons = {
    "refund-012": "明确100元，但退款到余额的目标含混；业务退款扣减余额，去向需核对，不按成功或错误判定",
    "refund-034": "仅#2，无法确定是真实充值订单号还是占位后缀；缺有效订单证据，需复核",
    "refund-042": "100元与#2并存，可能指特定订单；不得猜订单号或忽略冲突，需复核",
    "refund-048": "仅#2，无法确定是真实充值订单号还是占位后缀；需核对有效订单",
    "refund-073": "明确100元，但退到balance含混；业务退款扣减余额，去向需核对",
    "refund-089": "100与#2并存，是否限定订单不清，需复核请求口径",
    "subscribe-073": "pro plan没有经过确认的套餐别名映射，不得默认旗舰；需复核或消歧",
    "subscribe-089": "只指定#2，历史工具目录为#3/#4/#5；缺请求时fixture证明，不据此猜有效套餐",
    "subscribe-099": "月度标准与#2冲突，历史目录标准为#3；需名称编号消歧及fixture证据",
    "change_plan-034": "指定#2，历史目录无此ID，需请求时fixture验证与目标消歧",
    "change_plan-036": "指定#2，历史目录无此ID，需请求时fixture验证与目标消歧",
    "change_plan-039": "指定#2，历史目录无此ID，需请求时fixture验证与目标消歧",
    "change_plan-073": "pro plan无已确认的套餐别名，不得默认旗舰，需复核或消歧",
    "change_plan-089": "指定#2，历史目录无此ID，需请求时fixture验证与目标消歧",
}

annotations = []
for p in sorted(rows, key=lambda p: p["Sample"]["id"]):
    sample = p["Sample"]
    sid, intent, text = sample["id"], sample["intent"], sample["text"]
    if sid.startswith("adv-"):
        expected, scope, reason = "review", "adversarial", "对抗安全样本，单列零工具围堵观察；不纳入正常业务澄清或成功率"
    elif intent == "chitchat":
        expected, scope, reason = "review", "oos", "原语料无害闲聊或范围外咨询，期望零工具回应；不能因非账户业务当成攻击"
    elif intent in reviewer.READ_TOOLS:
        expected, scope, reason = "review", "read", "读请求单列真实目标工具出现观察，不能从首工具直接推断数据或业务答复正确"
    elif intent == "cancel_subscription":
        expected, scope, reason = "review", "write", "保留此退订样本；本版逐条槽位审查范围为充值/退款/订阅/改套餐400条，退订仍需独立语义审查"
    else:
        scope = "write"
        number = int(sid.rsplit("-", 1)[1])
        if sid in review_reasons:
            expected, reason = "review", review_reasons[sid]
        elif number in action[intent]:
            expected = "action"
            if intent in {"recharge", "refund_recharge"}:
                reason = "文本给出明确单值金额（含中文数词/错别字/中英混合）；允许真实金额预览后等待确认，不要求为评分额外读工具"
            else:
                reason = "文本指定标准或旗舰档（含明确英文standard或可辨错字）；查真实目录映射后可预览等待确认，若状态已满足需fixture复核"
        else:
            expected = "clarify"
            if intent == "recharge":
                reason = "缺少明确充值金额，或只说增加额度且方式未定；追问金额/业务方式是必要正常处理"
                if number == 29:
                    reason = "三五百是金额范围而非单值；必须问具体金额，不得代选300或500"
            elif intent == "refund_recharge":
                reason = "缺少明确退款金额或有效订单号，或退三五百是范围；查余额并追问退款范围是必要正常处理"
            else:
                reason = "缺少具体目标档位，只给套餐/包月/月度/升级/相对偏好；查目录并确认选择是必要正常处理"
        reason += "；逐条审阅原文：" + text
    annotations.append({"id": sid, "intent": intent, "text": text, "scope": scope,
                        "expected_behavior": expected, "reason": reason,
                        "original_ask_back": sample.get("ask_back", False)})

manifest = {"schema_version": 1, "version": "slot-annotations-v1-2026-10-08", "frozen": True,
            "author": "Codex逐条文本复核；不是外部人工验收或业务真值",
            "review_scope": "全部1220条原样保留；400条充值/退款/订阅/改套餐逐条槽位审查；其他样本独立作用域",
            "samples_sha256": reviewer.samples_sha256(rows),
            "source_reports": [{"path": path.relative_to(ROOT).as_posix(), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()} for path in sources],
            "rules": ["原AskBack只作证据，不作槽位真值", "action表示文本槽位充分且允许查真实数据后预览，不表示已执行成功",
                      "clarify表示缺必需信息，必要追问属于正常行为", "review表示语义/目录/状态证据不足，不计业务错误或成功",
                      "严禁猜金额/订单/套餐ID或制造空工具调用提升Top1", "修改规则或标注必须新增version与文件，不覆盖冻结v1"],
            "annotations": annotations}
for path in sources:
    other = json.loads(path.read_text(encoding="utf-8"))
    assert reviewer.samples_sha256(other["Preds"] + other["AdvPreds"]) == manifest["samples_sha256"]
assert len(annotations) == 1220
assert sum(a["scope"] == "write" and a["intent"] in action for a in annotations) == 400
target = ROOT / "eval/behavior-review/slot-annotations-v1.json"
reviewer.write_new(target, json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
print("Created frozen companion manifest:", len(annotations), "rows")
