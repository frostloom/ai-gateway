# Langfuse 客服评测工作台验收

实施从 2026-10-15 延续至 2026-10-16。目标是把冻结任务、已有输出的评分、人工审核和提示词接入工作台，为之后的真实配对评测提供证据链。本次没有外部模型、JEV 或自动裁判调用，没有重跑 1220 条。

## 已接入的内容

| 功能 | 实际内容 | 解释边界 |
|---|---|---|
| 数据集 | 1220 个唯一主任务，固定 ID，含问题、各轮输入、profile、首轮合理动作、回答要求、完整终态及不变量 | 冻结开发集，AI 事后标注，独立审核 pending；导入不等于执行或独立金标 |
| 评分配置 | 7 个规则配置、3 个人工审核配置；复核已有 56 个规则分数 | 结构通过、局部事实匹配不等于整段回答正确；分类标签不能取平均当准确率 |
| 人工标注 | 8 个既有 local_stub 完整任务臂进入队列，0 完成、8 待处理；原有 14 轮问答可展开查看 | 必要追问属于正确行为；证据不足选 unknown；这些替身任务不能代表真实客服质量 |
| 提示词 | 当前 Go 基础 systemPrompt 的完整内容快照，摘要命名、版本 1 | 不改变模型请求，不设置 production 标签；动态 JEV 提示、待确认信息、历史和 tools 不属于该快照 |
| 对比看板 | V1/V2 各 3 个组件：任务审核状态、任务跨度 P50/P95、假模型调用数 | 限定本次冻结 local_stub release；不是新版真实成绩、成本节省或性能收益证明 |

本机入口：

- [1220 条任务定义](http://localhost:13000/project/ai-gateway-evaluation/datasets/cmv9q5l6p0003t350aw1dtfxx/items)
- [人工标注队列](http://localhost:13000/project/ai-gateway-evaluation/annotation-queues)
- [基础提示词版本 1](http://localhost:13000/project/ai-gateway-evaluation/prompts/customer-service%2Fsystem%2F731344ccef1bfc40456b6797e9c7cb5c5c3d628799d3e5ee8ab05f3142c6f7fa)
- [本机替身对比看板](http://localhost:13000/project/ai-gateway-evaluation/dashboards/cmv9qprul001dt3507gkqxd33)

看板默认查询过去一天；之后查看这次证据需把日期切到 **2026-10-15**。V1、V2 各显示 4 条 unknown，假模型调用数为 7/5；任务跨度含初始化和状态检查，不能当用户响应耗时。真实任务耗时、响应耗时与调用次数仍应分别记录。

## 评测器、费用与调试台

现有确定性评测器在项目本地运行，结果通过 API 写入 Langfuse Scores；此次没有把它们伪装成 Langfuse 的自动 LLM 裁判。原生“评测器”页面尚未配置付费自动裁判。人工审核使用三个分类配置：完整任务结果、回答事实、追问必要性。

费用继续区分 token 用量、声明价格估算和实际渠道账单。缺渠道单价或完整 usage 时保持 unknown；没有添加猜测价格，也没有将空费用展示解释为免费。本机替身报告中的模拟预算和用量不能当真实消费。

提示词已可查看版本与关联 generation，但应用仍从 Go 代码构建请求。调试台和“运行实验”没有执行，模型连接没有新增。以后测试短提示词时应先创建候选版本，在同一任务/profile/模型/预算下比较质量、token、响应及任务耗时，经验证后再变更运行配置。

## 同步与验证机制

新增 `scripts/langfuse-workbench.py`：

- `plan` 离线加载冻结定义及四份已归档替身报告，检查 SHA 和唯一来源，生成可复核计划。
- `publish --execute` 只向显式本机 evaluation 项目写入；先核验全部旧追踪和评分。数据项固定 ID，其余资源按唯一版本名检查，内容冲突停止而不覆盖。
- 每次写入前后刷盘留回执。网络失败不自动重试；显式续传先读回，防止“服务端已接受、客户端没收到响应”导致重复。
- `verify` 独立读回全部 1220 项、10 配置、8 队列项、提示词、6 组件及布局；同时执行六个指标查询，空结果或错误不通过。
- 重复发布的 `idempotence-receipt.jsonl` 只有 plan 和 verified 两条记录，新增 POST 为 0。

本机 API 不允许 dashboard 使用 `traceId` 筛选；metadata 的 categoryOptions 又存在“保存接受、实际查询拒绝”的接口差异。最终使用固定 traceRelease、local_stub 和 arm 筛选。**run_id 实际是配置合同摘要，不是执行实例 ID**，因此另外枚举整个 release 的所有观测，追踪集合必须恰好等于冻结 8 条，分页未读完也拒绝。后续运行必须使用新 release 和新计划；本校验不是持续监控，不应往旧 release 追加记录。

Go generation 通过标准 OTel 属性关联 `customer-service/system/<SHA256>`、整数版本 1，并标记 `base_system_only`。仅 evaluation 生效；禁用观测、非法摘要或 production 不添加该关联。没有在线读取提示词、改变推理输入或新增上游请求。

除了本地替身 HTTP 集成测试，还发送了 **3 个纯观测节点**到另一个独立 release，实际读回 promptId/name/version。该探针不执行模型或工具，不进入原 8 臂看板，也不产生任务成绩。

## 操作记录与准备包

原六个 Langfuse 容器在本次开始前已退出，恢复原部署和持久卷，未删除数据。最初 Windows localhost 回退造成每次请求约 2.5 秒，127.0.0.1 同服务约 0.03 秒；主动停止首轮同步，保留未确认写入回执后显式续传。

`plan.json`、`plan-v2.json` 及 publish-1/2/3 保留为早期证据（旧计划以字节可还原 gzip 保存）。publish-3 只能证明配置读回，尚未验证实际查询，不是最终验收。浏览器发现查询不兼容后，先存档再移除仅本轮创建的 1 个错误看板和 6 个组件；任务、追踪、评分、队列和提示词均保留。最终使用 **plan-v3.json、publish-4.jsonl、readback.json 和 browser-validation.json**。

首个浏览器脚本把摘要中的“404”误判为缺页，且一次客户端跳转截到了旧内容；原记录保留为 browser-validation-first.json。最终改用定向正文和表头断言，实际检查数据集详情、提示词正文及六个正常显示的组件，页面错误 0。

新准备包为 `eval/paired-pilots/2026-10-16-langfuse-workbench-preflight-v3`，绑定当前运行代码，仍是 1220 主任务 / 2440 配对臂，1851 个边界场景单列。旧 v2 与历史 1220/3660 首轮输出不改。

直接读取默认 `.env` 的启动检查发现渠道、模型、协议和 JEV 版本不符；失败证据保留。按原有公开 `deploy/langfuse/evaluation.env.example` 覆盖后检查通过。默认 `.env` 没有被改写；之后运行必须明确加载已选渠道配置，不能把配置检查通过理解为远程密钥或推理连通性已验证。

可复用的本机只读检查示例（所有 `--out` 使用新路径）：

```powershell
python scripts/langfuse-workbench.py verify --plan eval/langfuse-workbench/2026-10-15-v1/plan-v3.json --env-file <本地项目私有配置> --out .tmp/workbench-readback-new.json
python scripts/langfuse-eval.py check --preflight eval/paired-pilots/2026-10-16-langfuse-workbench-preflight-v3 --env-file .env --env-file <本地项目私有配置> --env-file deploy/langfuse/evaluation.env.example --out .tmp/startup-new.json
```

私有配置可把 OTLP endpoint 的 hostname 设为 `127.0.0.1`，其余保持同一 Langfuse 项目。不要把密钥放进计划、命令参数或 Git。

## 验证和后续门槛

执行 Go 全量测试、go vet、Python 全量测试、浏览器和本机 API 核验；另经独立工程审查，修复布局接口、全局过滤和追踪范围问题后无剩余 P1/P2。具体命令、数量和产物摘要见归档 validation.json、manifest.json。

当前完成的是评测基础设施。完整回答的独立审核、少量真实 V1/V2 配对验收、最新渠道计价与明确金额上限仍是后续门槛；新准备包仍保持 **ready_for_paid_run=false**，没有启动 1220 条真实模型评测。

参考：[Langfuse 评测概念](https://langfuse.com/docs/evaluation/core-concepts)、[费用记录](https://langfuse.com/docs/observability/features/token-and-cost-tracking)、[指标 API](https://langfuse.com/docs/metrics/features/metrics-api)、[OTel 接入](https://langfuse.com/integrations/native/opentelemetry)。最终以本机实际读回和页面验收为准。
