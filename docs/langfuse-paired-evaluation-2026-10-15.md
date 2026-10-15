# Langfuse 与客服配对评测接入

将 `origin/codex/langfuse-observability`（6153452）合入以 c8bc4d1 为基线的 main，并补齐配对 runner 关联。目标：同一次 V1/V2 任务执行同时留下本地评测证据与 Langfuse 追踪，不增加模型请求。未启动 1220 条真实调用。

## 追踪和成绩

- 每个完整任务实验臂建立 `evaluation.task` 根追踪，多轮 `agent.turn` 为子节点，下挂 JEV、模型请求和工具操作。
- 每个节点带 `run_id`（配对配置合同 SHA）、`case_key`、`arm`、`evidence_kind`；请求及其子节点带从 1 开始的 `turn_index`。相同合同的重复实验仍以唯一 trace ID 区分单次执行。
- V1=`model`，V2=`jev_selective`。本地日志 finish 事件、每轮 telemetry 和报告保存 `langfuse_trace_id`；恢复已完成任务不新增追踪或重复模型请求。旧日志缺可选字段仍兼容。
- 根节点保存 `status`、`structural_pass`、`task_success`、必要追问数和 HTTP 失败数。`needs_review` 的成功状态是 `unknown`；必要追问、待确认不自动判错，结构通过不等于完整任务成功。
- 完整日志、独立状态 oracle、事实检查和独立回答审核仍决定正式成绩。当前未向 Langfuse Scores API 写分，结果显示在 metadata；没有启用 LLM-as-a-Judge 或其他额外付费裁判。

## 注入与费用边界

生产客服入口读取独立的 `AGENT_LANGFUSE_*` 配置，默认关闭。配对 runner 不自动读取环境或发现凭据：显式调用方通过 `observability.ConfigFromEnv` / `observability.New` 创建 observer，赋给 `AgentPairOptions.Observer`；两臂使用同一观察器。调用方负责结束时限时 `ForceFlush` / `Shutdown` 并核对 `Stats`。

正式评测要求 evaluation 环境、100% 采样。观察器为有界异步队列，记录丢弃和导出失败；无无限重试，不阻塞客服请求。观察失败不绕过费用保护、不自动重跑，但该次监控覆盖不足，不能称为完整 Langfuse 验证。既有 128 token 上限、thinking 关闭、禁止评测自适应增额重试的规则保留。

内容采集单独控制，默认关闭；启用后仅上传限长脱敏用户输入、最终回复及工具内容，不采集完整模型 prompt 或认证头。身份为 HMAC。规则脱敏不保证识别所有自由文本隐私，生产环境应单独决定是否启用；本轮只有合成账户测试。

OpenAI 缓存读取和 Anthropic 缓存字段分别处理，缺失 usage 不补零。LLM/JEV 调用计数是客户端尝试，可能包含本地预算拒绝；实际上游尝试以费用 guard 日志为准，不能直接按 span 数扣费。Langfuse 自动模型匹配或价格不代表千问代理渠道真实账单。正式报告继续以渠道定价证据和费用日志区分预留额、定价估算、未知账单；模型人民币与 JEV 美元分别展示。本轮未设置 Langfuse 价格模型，未声称 JEV 免费。

## 本机实测

复用已有 Langfuse 六容器和测试 MySQL/Redis，全部健康，未删除或重建持久卷。页面：<http://localhost:13000>。私有登录文件和项目 key 仍在原工作树 `deploy/langfuse/.local-login.txt`、`.env.agent`，均忽略 Git，未写入报告。

`TestAgentPairOwnedEnvironmentLocalReadAndWrite` 默认使用本地 OTLP 接收器。显式 `TASKBENCH_LANGFUSE_SMOKE=1` 才使用指定项目配置，且强制 localhost/127.0.0.1、evaluation、100% 采样；`TASKBENCH_LANGFUSE_SMOKE_RECEIPT` 可指定导出回执文件。

真实摄入验收 release=`langfuse-paired-integration-2026-10-15`，业务使用隔离账户、真实客服处理器、本地 Anthropic 假服务、Fake JEV：

| 案例 | 实验臂 | 验证 |
| --- | --- | --- |
| balance-001/answer_complete | V1/V2 各一臂 | V2 直接查询无 generation，V1 两次本地模型请求 |
| balance-020/explicit_scope_followup | V1/V2 各一臂 | 两轮关联，必要追问与后续查询分开记录 |
| recharge-002/confirm | V1/V2 各一臂 | 预览、确认关联，oracle 验证实际写入 |
| recharge-002/cancel | V1/V2 各一臂 | 取消挂起，业务状态无写入 |

Langfuse v2 Observations 实际查询确认 **8 个任务、14 轮、49 个 observation、12 个本地 generation**；丢弃 0、导出失败 0。八臂结构通过、语义待审核。Fake JEV 分流证明实现，不证明真实 JEV 准确率、速度或费用收益。外部模型/JEV 推理调用均为 0。

查询显式使用 `fields=core,basic,metadata,usage`。初次默认查询不返回 metadata，补充字段后核对到案例、实验组、轮次和 token；不是摄入丢失。脱敏服务端摘要、导出回执、文件 SHA、运行时代码 SHA 见 `eval/paired-pilots/2026-10-15-langfuse-local-v1/`。

验证：全项目 Go 测试、go vet、110 项 Python 测试通过，独立审查无 P1/P2。首次沙箱内 OTLP 测试因本地 HTTP 受限失败，授权本地网络后通过，未为此修改业务逻辑。

## 正式运行前

原 `2026-10-14-full-preflight-v1` 的 1220 主任务、2440 臂、1851 个独立边界分支保持归档；它绑定旧 c8bc4d1 运行时代码，不能当作加入监控后代码的运行绑定。后续生成新的版本，保留旧包及旧失败记录，不覆盖。

仍需修复后的少量真实模型/JEV验收、独立回答审核、渠道定价与分别限额，以及用户对真实推理费用的授权。监控接入不解除这些门槛。当前也没有把生产客服重启成已启用监控的实例。

官方依据：[OTel 与逐 span 属性传播](https://langfuse.com/integrations/native/opentelemetry)、[Observations v2 字段分组](https://langfuse.com/docs/api-and-data-platform/features/observations-api)、[分数写入接口](https://langfuse.com/docs/evaluation/evaluation-methods/scores-via-sdk)。
