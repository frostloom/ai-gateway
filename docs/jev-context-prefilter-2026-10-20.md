# Selective 多轮上下文预过滤（2026-10-20）

## 已批准范围

基于 [离线开销分析](jev-fallback-overhead-2026-10-20.md)，用户批准：对已确定不能直读的多轮会话，在 JEV 之前直接回退模型；先用本地测试验证安全边界与调用次数。本轮没有授权真实模型/JEV 请求，1220 全量继续延后。

这是现有路由的小范围改动。仅 `jev_selective` 且 session 非空、已包含多条历史消息或存在 pending_action 时，跳过 JEV 前置判定。正常 process 已先追加当前用户消息，因此普通首轮只有 1 条，已有会话至少有 2 条。

首轮仍保留 JEV 判定及原置信度门槛；model、hint、legacy 模式不改变。nil/空 session 的内部 evaluateTurn 调用保留原行为，不扩大优化边界。明确确认/取消仍先走原确定性状态机；其他待确认上下文交给模型，但 pending 不丢失，不替换写操作。

## 实现与可观测性

复用同一个上下文排除函数于 evaluateTurn 和 fastPathReason，避免出现“主动跳过却记为 judge_unavailable”。跳过时 route reason 为 `conversation_context`，JEV 调用/错误/用量缺失及阶段耗时均为零。`jev_enabled` 继续表示配置启用，是否实际调用看 `jev_calls`。

运行时新增共享 `routing_revision=selective-context-prefilter-v1`，表示这版路由实现；是否使用 selective 仍看 `routing_mode`。旧记录缺此字段，不能认为已经使用新优化。同版 model/selective 可以配对，新旧实现混用会被既有严格元数据比较拒绝。共享 prompt、工具 schema、模型输出限制和执行契约哈希不变；不改冻结记录来补新字段。

## 验证路径

- 新增模式矩阵：model/selective/hint/legacy × nil/空/首轮/多轮/pending；仅 selective 的多轮和 pending 跳过。
- 脚本化模型、计数 JEV 和本地测试账单服务验证：多轮只读仍调用同一工具，缺金额仍追问，待确认解释不执行；模型收到的完整消息与工具 schema 和 model 基线完全一致。
- 确认仍执行一次，取消不写入；都不调用任一推理供应商。复用既有首轮直读、判定故障、退款/充值/订阅确认与取消等回归。
- 运行 agent/taskbench 回归及静态检查，记录原始测试事件和哈希；不把 fake 运行耗时当供应商性能。

## 证据边界

本地测试可以证明该条件下不再发 JEV 请求、执行边界不变。历史 balance-020 第二轮的 1 次 JEV、1235/186 token、409.92 ms 只说明此优化的来源；不能把旧数据重标为新实测收益，也不能据此宣布整体提速、总体质量提升或总成本下降。

## 开发记录

- 起点：main `6f51070`；无关 `plate_sim.py` 保留。
- TDD：模式测试先观察 selective/multi 与 selective/pending 的 JEV 调用数 1 != 0；模型输入/工具和运行时标记测试也先失败。确认/取消夹具使用 JSON 数值类型及现有支持的明确命令，避免把夹具错误当产品问题。
- 独立审查发现 1 项重要兼容问题：最初的 selective 专属布尔元数据会使严格配对比较拒绝两组结果。新增真实 Handler 元数据配对回归，先观察同版配对失败及混版未被拒绝，再改为共享 routing_revision；两项回归通过。不放宽评测器比较规则，不豁免实验条件差异。
- 同版 runtime 可配对、跨臂及单臂多轮旧新版混合均须拒绝。已有旧版冻结记录可继续自身离线审核，但不能与新运行结果混合或在新实现下续跑旧 journal。新实跑应重新准备并固定配置及代码版本。
- 审查没有次要遗留项。真实供应商性能/总体质量、历史记录迁移和既有确认/退款实现全面重审未作为本次证明范围；本次仅核对调用顺序及通过相关回归，不把范围外内容视作已验收。
- 最终测试统计和命令见 `eval/jev-offline-readiness/2026-10-20/context-prefilter/manifest.json`，日志完整保留。本次不涉及 Go 客服/评测模块之外的功能，不重新构建前端。
- 最终 `go test ./internal/agent ./internal/taskbench -p 1 -count=1 -json -timeout 5m`：215 个顶层测试通过，0 失败；仅 TestLiveJevSemanticsDiagnostic 按零费用约束跳过。574 个包含子测试的通过事件不作为独立样本量。agent 包 50.510 秒、taskbench 包 129.119 秒；这是回归运行时间，不是客服响应性能。
- `go vet ./internal/agent ./internal/taskbench` 通过。新增 6 个顶层回归均通过；真实 Handler 的 model/selective 本地配对测试也通过。未发生真实模型/JEV 推理、Langfuse 在线评分或 1220 付费评测。
