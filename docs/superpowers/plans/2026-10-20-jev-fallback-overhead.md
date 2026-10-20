# JEV 回退开销离线分析计划

> **For agentic workers:** Use superpowers:executing-plans inline; use verification-before-completion before commit and push.

**Goal:** 从已有四任务配对记录拆出逐轮路由、阶段耗时与 token，判断下一步优化依据，不发生外部推理。

**Architecture:** 新增只读脚本，先复用 audit-jev-efficiency.py 的完整来源、费用及预算校验，再生成逐轮和按路由汇总。业务动作标签与路由原因分别保留；不推断回答正确率。

**Tech Stack:** Python 标准库、unittest、已有冻结 JSON/JSONL。

**Spec:** 用户已批准的离线分析方案：拆分直读、回退、追问、确认/取消；只有证据充分才修改产品代码。

## Global Constraints

- 不读 env，不调用模型/JEV，不改冻结证据或已有报告。
- 本次限定历史四任务样本，不把必要追问、unreviewed 或回退计为错误。
- 当前 main 上继续用户授权的逐目标提交；不复用另一个功能工作树，不改无关 plate_sim.py。
- 阶段耗时不是完整任务用时；缺失阶段数据必须拒绝，不能补零。
- 模型费用仅为既有保守估算，JEV 费用未知。

## Review Focus

- 阶段耗时缺失、负数、布尔值、非有限数应拒绝。
- 阶段和超过总耗时应拒绝，残差单独列出。
- 非法直读计数与缺失回退原因不能被当作直读收益。
- unreviewed 与无推理轮不代表错误或成功，原动作标签保留。
- CLI 必须先做来源哈希与预算校验；不绕过已有审计。

### Task 1: 分析工具与离线报告

**Files:** Create scripts/analyze-jev-overhead.py; scripts/tests/test_jev_overhead_analysis.py; docs/jev-fallback-overhead-2026-10-20.md; eval/jev-offline-readiness/2026-10-20/overhead/*.

**Interfaces:** analyze_pilot(path) -> dict (verified source, turns, groups, paired rows, limitations); summarize_turns(journal) -> dict (validated phase decomposition).

- [x] 编写真实记录的逐轮守恒、无推理、回退及未知审核状态测试；编写异常耗时、非法直读及 CLI 审计失败测试。
- [x] 执行新测试，确认因脚本尚不存在失败。
- [x] 最小实现，只保留元数据、计数和耗时，不复制用户原文与回答。
- [x] 执行新测试及原费用审计测试；独立代码审查；修复重要问题。
- [x] 生成新派生报告与哈希清单，记录适用边界、提示词静态核对、后续候选方案。
- 发布：校验再生成结果一致、冻结源无差异及 diff 后，按用户授权提交并推送 main；实际提交以 Git 历史为准。

## Execution ledger

- Design: bounded extension of existing offline audit; approved by user “好的去吧”; inline execution under prior autonomous authorization.
- Pre-flight: 58f5a4e main; only unrelated plate_sim.py untracked. Existing confirmation/cancel branches already skip both providers. No current evidence warrants changing routing thresholds or prompts in this task.
- Task 1: new 6 tests RED (script missing) -> GREEN; efficiency + overhead focused suite 20/20 passed outside sandbox. Initial sandbox run had unrelated tempfile permission denial in the existing audit test.
- Final review: fresh reviewer found one P2 (reread journal not pinned after audit). Manifest and journal replacement subtests both observed RED; SHA verification of actual parsed bytes made them GREEN. No other important findings, no deferred minors.
- Final verification: 7 new tests; full Python suite 158/158 passed in 82.949 seconds after fix. No product Go/web code changed, so no redundant product build was run. Source artifact hashes and provider-call/usage budget chain are checked by the CLI.
- Scope decision: record the deterministic conversation-context precheck as a next candidate, not an implemented improvement. Current gate semantics forbid multi-turn direct reads; real cost/latency benefit remains unmeasured. No threshold/prompt changes or new inference authorized in this goal.
