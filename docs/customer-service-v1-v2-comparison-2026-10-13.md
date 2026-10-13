# 客服 V1/V2 对照契约（2026-10-13）

本次只实现可比性校验与离线证据重算，未调用付费模型或真实 JEV，未产生或发布真实性能数值。测试中的模型名称、hash、审核者与 token 数均为本地契约测试数据。

入口为 `taskbench.CompareConversations(suite, model, selective)`，两侧使用 `ConversationComparisonRun{Config, Results}`。V1 路由必须为 `model`，V2 必须为 `jev_selective`。配置仅允许路由模式不同。

| 固定条件 | 校验 |
| --- | --- |
| 证据类型 | 两侧相同，且仅接受 `local_stub` / `recorded_run` |
| 定义与 manifest | 非空、有效 SHA256，绑定传入 suite，两侧相同 |
| profile / seed 定义 | 每个所用 profile 均有相同 SHA256，不接受缺项或额外 profile |
| 模型与 provider protocol | 必填且相同；协议仅接受执行层支持的 `openai` / `anthropic` |
| 提示词、工具 schema | 有效 SHA256 且相同 |
| 输出预算、执行层 revision | 正数预算、非空 revision，两侧相同 |
| 案例与顺序 | 完整覆盖传入 suite，案例定义、profile、完整任务/首轮诊断范围与顺序相同 |

调用方应先通过冻结定义加载器构造 suite，再使用实验记录器写入配置。profile hash 必须覆盖完整 seed/fixture 定义；不应仅 hash 账号 ID。模型参数、提示词/工具设置、重试/温度/工具循环等运行条件应由完整配置快照及执行层 revision 固定，不能用相同 revision 掩盖运行时配置差异。该函数不能验证输入来源真实性，配置、oracle 快照和服务端遥测仍属于可信记录输入，不构成执行证明。

函数将结果转换为原始 episode，通过 `ReplayConversations` 重算；传入的 `Success`、`Status`、`ReviewRequirements`、`DurationMS` 和汇总指标均不作为评分依据。回复内容中的自报成功/耗时/token 不进入指标。录制运行不能由 `local_stub` 审核冒充独立审核。

比较在重放完成后对新生成的 results 执行 `CheckConversationFacts`，再重新汇总。识别到事实 mismatch 时，追加错误并设为 failed/Success=false，保留 StructuralPass 以区分结构正确与事实错误；即使语义审核误判 pass，错误现金余额等已知事实也不能贡献成功率。有限事实匹配不能替代语义审核，无法解析的语句不自动判为事实正确。

每轮 available runtime 遥测还须匹配声明的 routing mode、模型、协议、prompt/tool hash 与输出预算。服务端 endpoint/contract hash、thresholds 和 repair limit 的共享运行契约必须双方及各轮相同（仅 route 不同）；缺项或不同则拒绝。model 路由出现 JEV 调用、enabled/fast_path/short_circuit 标记会拒绝。任一轮缺失 runtime 时，不把声明配置当作运行验证，双方耗时、调用数与 token 比较均保持 unavailable。

只有 `recorded_run` 的双方完整任务均有效且没有待语义审核任务时，才开放成对 completion rate。失败任务仍计入分母；setup 失败导致完整覆盖条件不满足，双方质量都保持 unavailable。首轮诊断不进入完整任务成功率。`local_stub` 始终不发布质量成功率，不能用于声称 JEV 有质量、速度或成本收益。报告 `EffectClaimsAllowed` 保持 false；可比较并不自动证明改进。

耗时指标仅汇总每轮原始 response telemetry 的 `total_latency_ms`，与本地重放 `DurationMS`、用户端端到端任务耗时区分。每轮双方均存在有效非负耗时才开放；任一侧缺失时双方都保持 null。调用数也要求每轮双方有有效非负整数 `llm_calls` / `jev_calls`。提前中止或 setup 失败不允许以较短任务冒充完整任务的耗时/调用节省。

token 汇总要求每轮 LLM 与 JEV 的 calls、usage_missing、usage_responses、input_tokens、output_tokens 字段完整、非负，missing 为 0，responses 与 calls 相等。零调用也必须有明确零用量。缺失字段、部分用量、失败调用未返回 usage 均使双方 token 不可用。计数溢出会关闭指标，不估算缺失值；不计算货币成本。

验证只运行本地 Go 契约测试，覆盖条件不匹配、缺项、重复/顺序/定义变化、传入成功标记伪造、待审门槛、合成审核冒充、已审核但错误余额、runtime 不匹配/缺失、model 路由出现 JEV、耗时来源及不完整/非法遥测。完整项目验证由主任务统一执行。历史归档不修改。
