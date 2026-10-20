# 新版路由的必要追问与采集链路准备（2026-10-20）

本轮继续零供应商调用。没有读取 `.env`、核验渠道连通性或费率，没有启动 1220 真实评测。本次只扩展既有本地配对测试，使用脚本化 HTTP 模型、fake JEV、专用隔离账单账户及本地 OTLP 接收器；没有改客服生产逻辑、冻结定义或历史分数。

## 这轮补齐什么

原 12 项小评测计划已经含必要追问定义，但 `TestAgentPairOwnedEnvironmentLocalReadAndWrite` 只跑余额直读、范围后续对话、明确充值确认、明确充值取消四项。现在增加两项，使它们经过真实 Handler、预算/配对日志、独立状态 oracle、报告重算和 OTLP 采集的同一链路。

| 本地覆盖任务 | 正确的过程要求 | 独立状态校验 |
|---|---|---|
| balance-001/answer_complete | 首轮读取现金余额 | 本人余额与账本一致，无写入 |
| balance-020/explicit_scope_followup | 歧义处理及明确现金范围的后续对话 | 不把现金当 token；不是缺金额追问覆盖 |
| recharge-002/confirm | 预览后确认 | 确认后一次充值 100 元 |
| recharge-002/cancel | 预览后取消 | 清除 pending、无写入 |
| recharge-001/supplement_then_confirm（新增） | 追问金额 → 补充 100 元 → 预览 → 确认 | 追问/预览不改余额；最终增加 100 元 |
| refund-001/balance_cap（新增） | 追问信息 → 申请退 100 元 → 预览 → 确认 | 初始余额 25 元，最终只退 25 元，账本一致 |

两组使用不同租户。每项都检查首轮 selective 调用一次 JEV、model 从不调用 JEV；后续轮两组都不调用 JEV，且无 JEV 错误。通过指针检查遥测字段确实存在，不能把字段缺失当零调用。selective 后续仍调用模型时，原因必须是 `conversation_context`。

## 必要追问怎样记分

必要性来自冻结定义的 `clarification_is_necessary_not_failure`，不是看回答有问号就认定正确。HTTP 记录器只根据工具 trace 产生 read/preview/execute；无工具文本仍为 `unreviewed`，语义审核保留 `pending_independent_review`。

新增断言检查两条链路的 `necessary_clarifications` 各有一个首轮槽位、动作仍未审核、结构校验通过且无事实/HTTP 错误。整任务状态仍是 `needs_review`；Langfuse task_success 仍是 `unknown`。这证明追问不会被当作错误，也不会在未审核时被当作成功。没有发布追问成功率或模型任务成功率。

## 采集与离线配置

本地采集数量从固定四项计数改为与实际报告核对：每个已完成实验臂对应 task span；每个观察轮对应 turn span；预算记录的上游模型尝试对应 generation span。仍检查问答内容、任务关联、零丢弃、零导出失败以及未审核状态。它验证 OTLP 采集链路，不证明正在运行的 Langfuse 项目已收到这次数据。

使用现有 `cmd/paired-eval -mode preflight` 重新生成离线配置，生产执行代码版本为 `f585547`，期望 `routing_revision=selective-context-prefilter-v1`。配置仍列出原有 **12 项候选任务**；上表六项是本次本地执行覆盖及下一次小样本优先考虑项，不表示已批准执行六项或十二项。两者不可混称。

新配置和证据位于 `eval/jev-offline-readiness/2026-10-20/clarification-readiness/`，没有覆盖旧 preflight。模型名、公共网关和 JEV 型号仅沿用历史提案，未检查密钥、可用性或当前报价。`ready_for_paid_run=false`、`allow_paid=false`，没有可触发付费的执行入口。

## 真实调用前仍需确认的条件

1. 固定实际执行任务子集、双方相同模型/提示词/工具/输出上限及同一路由版本，创建全新 journal；不得续跑旧版本日志。
2. 核验对应渠道模型和 JEV 的有效费率及账户费用，以独立币种给出硬上限；JEV 不默认免费。调用次数、输入字节/token、输出和金额上限都由预算守卫约束。
3. 获得明确的小样本费用授权后才调用。当前提案不设已批准金额；24 次是原计划建议的调用上限，不保证 12 项/24 臂全部完成，不能当完成次数。
4. 实跑前单独检查本地 Langfuse 项目与配置；采集故障只重传/补同步评分，不重跑模型。无在线 LLM judge。完整报告要保留失败、未跑、未审核和必要追问分母。

## 开发与面试记录

先加入两条完整链路并观察失败，再补齐脚本化供应商对缺金额/退款的响应及正确工具；没有通过改产品提示词、修改冻结期望或放松 oracle 让测试通过。排查发现 `unreviewed` 是原有独立审核边界，保留它而不是写成自动识别成功。

本轮可以说明“定义已存在”和“经过真实执行/报告/采集链路验证”是两种完成度，已经补齐后者的局部覆盖。尚未证明真实模型能正确追问、总体准确率提升、JEV 置信度校准或总成本下降。最终测试、审查和哈希结果随同目录 manifest 保存。

最终回归：`go test ./internal/taskbench ./cmd/paired-eval -p 1 -count=1 -json -timeout 5m`，108 个顶层测试通过、零失败、零跳过；两个包耗时分别为 150 秒和 6.536 秒。`go vet` 两个包通过。六项本地任务的 12 个实验臂均通过结构校验并保留待审核状态；本地 OTLP 收到 12 个 task、26 个 turn、20 个 generation，零丢弃、零导出失败。

独立只读审查未发现阻塞问题。后续可增强采集检查，按 trace ID、case、实验臂逐项核对 span，避免汇总计数掩盖错归属；当前仍是汇总计数与关联字段检查，不应宣称已完成逐臂归属审计。
