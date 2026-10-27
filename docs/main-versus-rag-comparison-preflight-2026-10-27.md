# main方式与当前RAG方式的回答对照：基线核对（2026-10-27）

用户要求比较两种方式的回答准确率，并核对实时事实来源。本文件属于知识库任务，只在codex/customer-knowledge-rag提交。没有合并main、修改其1220套件、客服规则或评分，也没有新真实模型/JEV调用、付费claim或部署。

## 已核实的公平对照条件

从main 72d3e683710f0a96eea904440a9f78a7ce264ed2 与RAG f4e035219a256fac2db89ed6df2e1d6eb4b19da1 的真实Handler分别提取有效配置；提取器使用相同源码、显式空密钥及永远拒绝联网的Transport，不初始化业务数据库。比较DeepSeek Flash/openai、model路由、512输出tokens、thinking disabled的配置：main与RAG关闭时的全部运行元数据逐字段一致，包括prompt、工具schema、v5合同摘要。main的15个非测试agent Go文件均与读取的提交内容一致。

prompt SHA：731344ccef1bfc40456b6797e9c7cb5c5c3d628799d3e5ee8ab05f3142c6f7fa；工具SHA：3962937d7fd08af4794758a56070b7bbadfb5c3e188cf1fa539355780b9e0e17；v5合同：a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f。

RAG开启仅在有效身份中增加/改变knowledge_enabled、corpus_version、知识提示词/工具schema及扩展合同。业务工具都仍可调用；RAG并不替代当前价格、余额、订阅、账单读取。身份一致是对照前提，不是所有执行代码逐字节相同的证明；真正请求的业务状态、工具结果和所有尝试仍须记录复核。

正式回答对照应使用原72题（36知识/12未知/12动态/12安全），144任务，交替AB/BA、每臂独立隔离账户/会话和相同画像；A为main有效提示/工具策略，B仅额外开启当前RAG。暂不修检索/改知识正文，以免把“当前分支”和优化后分支混成一组。每题独立评分，报告完整正确率、无依据断言、来源引用、失败、全部usage/费用及耗时。原72题已有直接检索诊断，不声称是开发者从未见过的题；正式模型回答仍未运行。512仅是这组独立实验的统一候选输出预算，不改变main1220冻结的128输出合同，也不将独立结果冒称其1220结果。

## 实时业务数据具体来自哪里

模型不持有数据库连接、也不直接编写SQL。agent工具调用经过控制面鉴权的billing admin服务，再由Store查询：

| 工具/事实 | 路径 | 实际数据来源 |
|---|---|---|
| get_balance/现金余额 | agent.me → /admin/me → Store.MeOverview | Redis的billing:balance投影与MySQL LedgerBalance核对；不一致则重建投影，仍不一致报错 |
| list_plans/套餐价格和额度配置 | 同一MeOverview → ListPlans | MySQL套餐记录；agent再过滤在售状态 |
| get_my_subscription/订阅及剩余额度 | MeOverview → GetActiveSubscription/SubscriptionAllowance | MySQL订阅、套餐、滚动窗口内订阅reserve用量，不从RAG文本推断 |
| get_consumption/近7天扣费 | MeOverview → ConsumptionStats | MySQL已结算账单聚合 |
| get_recent_bills/近期账单 | /admin/bills → ListBills | MySQL本人账单，固定phase/status/条数过滤 |
| list_models/模型目录单价 | 模型目录admin端点 → ListModels | MySQL模型价格与在售配置 |

Redis余额是账本投影，不是唯一权威；LedgerBalance公式基于MySQL初始额度、入账、待预留和已结算扣费计算。这里的“实时”指读取平台本轮当前记录，不是即时向供应商官网询价，也不是保证服务端数据绝不滞后。部分工具共用总览，会联读多项数据，不应误写成每个工具只执行一条SQL。主模型拿本轮工具返回的文本形成回答，仍需独立检查它是否正确采用这些数据。

## 真实回答对照尚未开始

本轮完成的是可比较基线与数据源核对。knowledge-live的paid execute和AcquireClaim仍明确拒绝；现有transport、claim、collector仅支持local_stub，不能通过改布尔或换URL冒充真实执行。main新full1220窗口基础也还不提供本知识范围的正式provider能力。真实执行前仍需独立scope/版本/完整请求及usage兼容/费率/累计永久预算绑定；不能沿用已消耗claim、越过共预算或占用未协调的1220范围。

现有用户自主预授权允许就绪后执行，无需逐scope反复询问，但它不使技术门禁自动完成。因此accuracy.main和accuracy.rag目前均为null，不能说哪组更准确，63.9%直接检索召回率也不用于填充它们。下一必需实现是独立真实provider准入与共享预算协调，而非重复运行替身来冒充真实效果。

配置快照和核对回执位于eval/knowledge-reports/2026-10-27-main-baseline/；原始提取器在本工作树忽略的.tmp/knowledge-validation/2026-10-27-main-vs-rag/。未加载模型密钥、未向供应商发请求、未写质量评分。
