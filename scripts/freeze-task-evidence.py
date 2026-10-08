"""Freeze this experiment's explicit artifacts without overwriting any evidence."""
import datetime
import gzip
import hashlib
import json
import math
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "eval/experiments/2026-10-08-deepseek-flash"
SOURCES = [
    "deepseek-flash-pilot-1791453708918",
    "deepseek-flash-full-1791453809005",
    "taskbench-pilot-20261008-1826",
    "taskbench-full-1791455587436",
    "taskbench-v2-full-1791456515215",
]
ARMS = ["control-off", "candidate-off", "control-on", "candidate-on"]
LABELS = ["旧代码 / 无JEV", "优化后 / 无JEV", "旧代码 / 有JEV", "优化后 / 有JEV"]


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def write(path, raw):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("xb") as stream:
        stream.write(raw)


def json_bytes(obj):
    return (json.dumps(obj, ensure_ascii=False, indent=2) + "\n").encode()


def latencies(values):
    values = sorted(values)
    return {"count": len(values), "average_ms": sum(values) / len(values),
            "p50_ms": values[math.ceil(len(values) * .5) - 1],
            "p95_ms": values[math.ceil(len(values) * .95) - 1]}


def main():
    if OUTPUT.exists():
        raise SystemExit("Archive already exists; preserved unchanged.")
    reports = {arm: json.loads((ROOT / "eval/reports" / SOURCES[-1] / arm / "tasks.json").read_bytes()) for arm in ARMS}
    assert {d["schema"] for d in reports.values()} == {"taskbench.v2"}
    assert len({d["dataset_sha256"] for d in reports.values()}) == 1
    assert len({d["metadata"]["evaluator_binary_sha256"] for d in reports.values()}) == 1
    tenant_ids = [r["fixture"]["tenant_id"] for d in reports.values() for r in d["results"]]
    assert len(tenant_ids) == len(set(tenant_ids)) == 96
    cases = reports[ARMS[0]]["results"]
    cases = [r["case"] for r in cases]
    case_raw = json.dumps(cases, ensure_ascii=False, separators=(",", ":")).encode()
    assert digest(case_raw) == reports[ARMS[0]]["dataset_sha256"]

    # Review optional offers separately: an offer after a correct read answer is
    # not a blocked task or a required clarification. Preserve the raw heuristic.
    review = []
    for arm, report in reports.items():
        for r in report["results"]:
            if r["redundant_clarification"]:
                assert r["case"]["read"] and r["success"]
                reply = r["turns"][0]["response"]["reply"]
                review.append({"arm": arm, "case_id": r["case"]["id"], "reply": reply,
                               "reply_sha256": digest(reply.encode()),
                               "review": "optional offer after successful read; does not prevent task completion"})

    matched = {}
    for mode in ["off", "on"]:
        old = {r["case"]["id"]: r for r in reports["control-" + mode]["results"] if r["success"]}
        new = {r["case"]["id"]: r for r in reports["candidate-" + mode]["results"] if r["success"]}
        ids = sorted(old.keys() & new.keys())
        matched[mode] = {"case_ids": ids, "control": latencies([old[i]["duration_ms"] for i in ids]),
                         "candidate": latencies([new[i]["duration_ms"] for i in ids])}
    intent = {}
    for arm in ARMS:
        p, = (ROOT / "eval/reports" / SOURCES[1] / arm).glob("intent-*.json")
        intent[arm] = json.loads(p.read_bytes())["Metrics"]
    summary = {"schema": "task-experiment-comparison.v1", "task_source": SOURCES[-1],
               "task_scoring": "taskbench.v2", "same_dataset": True, "unique_tenants": 96,
               "dataset_sha256": reports[ARMS[0]]["dataset_sha256"],
               "arms": {a: {"metadata": d["metadata"], "metrics": d["metrics"]} for a, d in reports.items()},
               "matched_success_latency": matched, "intent_source": SOURCES[1], "intent_metrics": intent,
               "reviewed_optional_offers": len(review),
               "review_author": "coding agent review; not an external human annotation"}

    text = """# DeepSeek官网真实客服任务实验（2026-10-08）

新主指标是**经过独立状态核验的任务完成率**。首工具Top-1/F1/Reach继续保留作诊断，不充当业务完成率。必要追问是正常步骤：先问清金额或套餐，补全后完成原任务才算通过。所有尝试都进入分母，HTTP与fixture失败另列。

本轮使用官网`deepseek-flash`。旧实现源码为`b26642876e78255a89c87de3982888f14d6532d4`，最终优化实现为`d07e671f1ab4eaa7f1f8a5d3e3bc3711f129094b`；每组24个相同固定案例、96个不同模拟租户、同一评测器二进制与用例哈希，开关状态由实际响应遥测核验。二进制、数据集、评测器SHA256均见[机器可读对照](task-comparison.json)。旧控制二进制来源是本次会话保存的旧代码构建；源码标记由执行者记录，并非外部供应链证明。

## 最终多轮实测

| 指标 | 旧代码 / 无JEV | 优化后 / 无JEV | 旧代码 / 有JEV | 优化后 / 有JEV |
|---|---:|---:|---:|---:|
"""
    def row(label, fn):
        return "| " + label + " | " + " | ".join(fn(reports[a]["metrics"]) for a in ARMS) + " |\n"
    text += row("任务完成率", lambda m: f"{m['completed']}/{m['total']} · {100*m['completion_rate']:.2f}%")
    text += row("必要追问提供 / 应追问", lambda m: f"{m['clarification_offered']}/{m['clarification_required']}")
    text += row("追问补全后完成 / 应追问", lambda m: f"{m['clarification_completed']}/{m['clarification_required']}")
    text += row("正确预览参数 / 实际预览", lambda m: f"{m['preview_correct']}/{m['preview_observed']}")
    text += row("成功取消并清除挂起 / 取消任务", lambda m: f"{m['cancel_no_write']}/{m['cancel_cases']}")
    text += row("取消后账务没有改变（单独观测）", lambda m: f"{m['cancel_no_write_observed']}/{m['cancel_cases']}")
    text += row("重复确认无第二次写入", lambda m: f"{m['duplicate_confirm_passed']}/{m['duplicate_confirm_cases']}")
    text += row("确认前写入违规", lambda m: str(m['premature_writes']))
    text += row("HTTP / fixture失败", lambda m: f"{m['failed_http']} / {m['fixture_failures']}")
    text += row("LLM调用总数 / 实测轮次", lambda m: f"{m['llm_calls']} / {m['telemetry_turns']}")
    for label, key in [("所有尝试平均耗时（秒）", "average_ms"), ("所有尝试P50（秒）", "p50_ms"), ("所有尝试P95（秒）", "p95_ms")]:
        text += row(label, lambda m, k=key: f"{m['all_task_latency'][k]/1000:.3f}")
    text += "\n旧代码五个失败均为自然语言取消后的`final action remains pending`：充值取消、退款取消、订阅取消、改套餐取消和撤回退订操作。客服虽回复取消，Redis待执行操作没有清掉。没有发生本次账务写入，但再次确认仍可能执行。修复在执行层识别明确撤销当前操作，清除待执行状态并记录取消审计；否定、问句和新退订意图不会被吞掉。25个工具×取消说法回归验证后，真实模型四组复验显示这五项已恢复。完成率提升20.83个百分点主要来自这个真实状态机修复，不能解释成模型意图识别提高20.83个百分点。\n"
    text += "\n## 任务完成时间的比较口径\n\n计时从首个请求到最终账务核验，包含模型、工具、网络和核验开销，排除fixture准备和模拟用户思考时间。全部24个尝试的耗时列在上表；各组成功任务数量不同，原始报告也分别列成功任务耗时。为了避免样本组成不同，下面只比较两种代码都成功的相同任务。\n\n| JEV模式 | 共同成功任务 | 旧/新平均秒 | 旧/新P50秒 | 旧/新P95秒 |\n|---|---:|---:|---:|---:|\n"
    for mode, m in matched.items():
        text += f"| {mode} | {len(m['case_ids'])} | " + " | ".join(f"{m['control'][k]/1000:.3f} / {m['candidate'][k]/1000:.3f}" for k in ["average_ms", "p50_ms", "p95_ms"]) + " |\n"
    text += "\n四组同时运行，每组任务顺序执行；这是一次实际观察，受模型采样、上游负载与本机并发影响，不能当作生产SLA或JEV因果实验。费用/token消耗未独立计量，不把调用次数直接换算费用。\n"
    text += "\n## 追问统计核对\n\n必要追问四类分别为充值金额、退款金额/订单、订阅套餐、改套餐目标。四组均真正追问、补槽、预览并完成；没有把首轮未调用写工具记成任务失败。旧文本规则还命中了9条“正确回答后询问要不要其他服务”的冗余追问候选，逐条复核均为已完成读取后的可选服务提示，不是阻塞当前任务的必要追问或错误。原始候选字段保留，详情见[追问复核](clarification-review.json)，不把候选数作为确认的错误数。复核者是编码代理，不冒充外部人工金标。\n"
    text += "\n## 同模型单轮诊断（每组1200正常 + 20对抗）\n\n这是最终取消修复前的优化实现`1a7a729`与旧控制实现的对照。单轮数据用于诊断，不替代上面的最终多轮完成率；原数据、原评分保持不变。\n\n| 实现 / 模式 | 首工具Top-1 | Macro-F1 | Reach | OOS处理 | 对抗约束生效 | 正常请求被拦 | 单轮平均/P95秒 |\n|---|---:|---:|---:|---:|---:|---:|---:|\n"
    for a, label in zip(ARMS, LABELS):
        m = intent[a]
        text += f"| {label} | {100*m['top1']:.2f}% | {100*m['macro_f1']:.2f}% | {100*m['reach_rate']:.2f}% | {m['oos_correct']}/{m['oos_total']} | {m['adversarial_blocked']}/{m['adversarial_total']} | {m['normal_blocked']} | {m['avg_latency_ms']/1000:.3f}/{m['p95_latency_ms']/1000:.3f} |\n"
    text += "\n所有正常与对抗请求HTTP失败数均为0。优化有JEV组仍有2条正常业务被拦，20条对抗有1条未达到本评测的约束判定；这些失败保留，不能声称全面安全。无JEV组的对抗约束结果来自完整响应/执行守门，并不意味着JEV前置拦截。新官网模型与9月历史模型条件不同，不跨模型归因。\n"
    text += "\n## 评分版本与证据保护\n\n- 9月历史V1/V2、原401失败实验、初版诊断全部保持原字节；本目录只追加新证据。用户授权的是模拟业务数据，凭证不入归档。\n- 两案例task pilot有一次失败是外部JSON用例加载复用了默认案例字段，属于评测器错误。已用红绿测试修复；保留该探针，不能把它的1/2称作客服任务能力。\n- 首轮四组24案例采用`taskbench.v1`。订阅读取返回正确的套餐ID，却因规则仅接受名称或固定`套餐#ID`格式、未接受`#3`或`套餐 #3`等价表达而误判部分样本。原报告全部保留；修正规则为核验实际目录ID或名称、排除冲突后升级`taskbench.v2`，对四组完整重跑，最终表只使用这同一规则的新结果。\n- 评分版本与客服版本是不同维度：用户定义无JEV为V1、有JEV为V2；这里的`taskbench.v2`只是评分规则编号，不能混称客服V2。\n- 24例是开发与故障回归集，包含必要追问、完整槽位、复合读取与写入、订单退款、余额上限、下期改套餐、已满足状态、取消和重复确认。它不是未见说法的泛化测试，24/24不代表所有用户请求100%正确；某一类别的1例通过也不足以证明全面能力。\n- 后续扩展应新增冻结的独立盲测集，覆盖口语、错别字、中英文混合、任务中改金额/套餐、歧义与冲突、网络失败恢复和更多对抗；多次重复实测后再报告置信区间与时间稳定性。不要用本轮修复样本代替盲测。\n\n## 验证与复现\n\n最终代码通过`go test ./... -count=1`、`go vet ./...`，两个运行二进制构建成功；脚本PowerShell解析通过。原始报告包含每轮HTTP响应、冻结参数、账本/订单/订阅/审计快照与失败原因。运行脚本为`../../../scripts/run-task-experiment.ps1`，必须指定新目录、实际agent源提交和独立端口；仅操作新建模拟租户。\n\n离线校验：`python eval/experiments/2026-10-08-deepseek-flash/verify.py`；当前机器另外加`--check-sources`可验证忽略目录里的源文件没有改变。归档采用gzip level9、mtime0、空文件名，SHA256同时覆盖原始字节与压缩字节。\n"

    # Read only explicitly selected evidence types; never copy .env, binaries or
    # unrelated logs. Match actual configured API keys without printing them.
    secrets = []
    for line in (ROOT / ".env").read_text(encoding="utf-8-sig").splitlines():
        name, sep, value = line.partition("=")
        if sep and name.strip().endswith("API_KEY"):
            value = value.strip().strip('\"\'')
            if len(value) >= 16:
                secrets.append(value.encode())
    pending = []
    for source in SOURCES:
        directory = ROOT / "eval/reports" / source
        assert directory.is_dir(), source
        for p in sorted(directory.rglob("*")):
            if p.is_file() and p.suffix in {".json", ".md", ".txt", ".log", ".patch"}:
                raw = p.read_bytes()
                if any(s in raw for s in secrets) or re.search(rb"sk-[a-fA-F0-9]{32}(?![a-fA-F0-9])", raw):
                    raise SystemExit("Credential detected; archive creation stopped.")
                pending.append((p, raw))
    for filename, raw in [("README.md", text.encode()), ("task-comparison.json", json_bytes(summary)),
                          ("clarification-review.json", json_bytes({"author": "coding agent", "rubric": "review optional offers independently from required clarification and task failure", "results": review})),
                          ("cases.json", case_raw)]:
        write(OUTPUT / filename, raw)
    # The existing independent verifier is unchanged and also works offline here.
    verifier = (ROOT / "eval/experiments/2026-10-08/verify.py").read_bytes()
    write(OUTPUT / "verify.py", verifier)
    entries = []
    for p, raw in pending:
        relative = p.relative_to(ROOT / "eval/reports")
        compressed = gzip.compress(raw, compresslevel=9, mtime=0)
        archive = Path("raw") / Path(str(relative) + ".gz")
        write(OUTPUT / archive, compressed)
        entries.append({"source": p.relative_to(ROOT).as_posix(), "archive": archive.as_posix(),
                        "source_bytes": len(raw), "archive_bytes": len(compressed),
                        "source_sha256": digest(raw), "archive_sha256": digest(compressed)})
    supporting = [{"path": p.name, "sha256": digest(p.read_bytes())} for p in sorted(OUTPUT.iterdir()) if p.is_file()]
    write(OUTPUT / "manifest.json", json_bytes({"schema_version": 1, "archived_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
          "credential_pattern_matches": 0, "credential_review_scope": "explicit experiment roots and evidence suffix whitelist; actual configured API keys plus DeepSeek key pattern",
          "compression": {"level": 9, "mtime": 0, "filename": ""}, "files": entries, "supporting_files": supporting}))
    print(f"Frozen {len(entries)} artifacts; 96 unique final-task tenants; 4 matching dataset/evaluator hashes; no API credentials archived.")


if __name__ == "__main__":
    main()
