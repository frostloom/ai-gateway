# 首批独立语义复核同步（2026-10-22）

首批的两条真实 trace 已分别增加两项评分：`independent_review_status=reviewed_pass` 与 `reviewed_task_success=passed`，共 4 项，实际读回的值、trace 关联、类型和审查元数据全部匹配。原始 14 项确定性评分已再次读回，保留执行时的 `needs_review` / `task_success=unknown`；它们描述执行时尚未审查的状态，新评分描述之后完成的独立复核。

本次只访问本机 Langfuse，没有模型或 JEV 推理。首批的检查点没有推进，第二批及后续真实请求均未执行。这些评分描述首个余额查询案例，不能推广为整体质量、延迟或成本优势。

## 证据与验证

同步前核对已提交首批 manifest 中的全部来源哈希，并使用 Go 离线报告命令从原始日志重新计算报告，结果与首批原报告完全一致。独立复核文件逐项绑定执行版本、journal、案例、路径、需求及回答哈希；实际 Langfuse 用户输入和回答内容也必须匹配日志，才能开始写入。

第一次 publish 的 4 次写入均被接受，但即时读回未通过；保留原同步回执，之后仅运行只读 verify，确认全部评分及元数据匹配。没有重发评分或模型请求。该结果不证明即时读回问题的具体根因。

公开证据位于 `eval/paired-pilots/2026-10-22-pi-review-sync/`，包含评分计划、原始同步回执、4 项新评分和 14 项旧评分的读回结果及哈希清单。原执行、原报告及原审查文件均未修改。

Python 回归共 170 项，15 项按条件跳过。新增测试覆盖错误日志/版本/trace/回答、漏项/重复/未通过审查、远程内容变化、评分元数据变化，以及 CLI 空路径和读写流程。独立代码审查发现的“空复核路径误走原评分流程”问题已先复现、再修复，并经复核通过。

## 使用方式

在现有命令上同时指定 `--review` 和 `--journal`，仅支持单份 `--report` 的 plan/publish/verify。省略复核参数时原流程保持不变；显式空值、缺少其中一个参数或混入 check/preflight 会被拒绝。

```sh
python3 scripts/langfuse-eval.py plan \
  --report PATH_TO_ORIGINAL_REPORT \
  --review eval/paired-pilots/2026-10-22-pi-first-live-result/independent-review.json \
  --journal PATH_TO_ORIGINAL_JOURNAL \
  --out NEW_SCORE_PLAN
```

publish 另需显式 `--execute`、私密 `--env-file` 与新回执路径；verify 使用新的只读输出路径。不要改写原始证据或通过重新运行推理解决评分摄入问题。评分 ID 由 trace、审查文件哈希及指标确定，独立于原确定性评分；相同审查同步不会产生不同 ID。

报告及审查文件仍是可信操作者输入，不是审查人身份或执行过程的密码学证明。本工具校验它们之间的绑定，不复刻 Go 的语义规则、完整上下文哈希或独立审查判断；使用前必须由 Go 离线工具从原 journal 生成报告，并保留不可随意重绑定的原始 manifest。
