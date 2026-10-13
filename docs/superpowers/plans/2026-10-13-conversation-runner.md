# 完整会话测试与离线评分 Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans; execute inline with one final independent review.

**Goal:** 将已冻结1220来源、4291场景接到逐轮测试与只读离线复核，区分必要追问、确认、取消、幂等和最终业务状态。

**Architecture:** 在现有internal/taskbench旁新增独立conversation适配器，保留旧runner/score/live及历史绑定。Driver负责发送预先定义的用户续句并取权威快照；ReplayDriver仅读取本地记录，不含网络或密钥配置。结构评分与回答审核分别报告，缺证据不能通过。

**Tech Stack:** Go、现有store/Snapshot结构、本地JSON、unittest与Go测试；无新增依赖。

**Spec:** 本文记录用户已确认的范围：完整会话测试器与离线评分回归，不执行付费测评，完成验证与记录后推送。

## Constraints and rulings

- 历史1220输出/定义/指标不得覆盖，不读取.env，不修改旧付费开关和默认task-eval入口。
- 每个场景独立身份、账户与状态；只核验声明的业务终态，未知字段必须阻塞。
- 不用模型评审答案。原自然语言事实与续轮answer约束需绑定原始回复的独立审核；未审核标pending，不能凭关键词或成功自述放行。
- 最终回答也要求审核，不只检查首轮；模型/用量缺失不可解释为零成本。
- Replay是记录复核，不等于新执行；替身数据不得当作V1/V2准确率或JEV收益。
- 先覆盖所有定义的加载与计划，再用代表性真实隔离DB场景证明终态链路，不逐条创建1220账户。

## Tasks

- [x] 1. Loader/runner：先失败测试；核验冻结manifest与文件哈希，展开4291场景、3071完整任务，显式续句与订单占位解析，Driver逐轮失败保留分母。
- [x] 2. Scorer：先失败测试；逐轮路由/工具/确认绑定/无越权写入/取消/重复确认、权威余额和账本/订单/订阅/额度/外部对照；完整语义证据缺失不通过。
- [x] 3. Read-only replay CLI：本地记录、版本与内容SHA、独占输出；不接受URL或在线模式。错误、缺轮、重复场景、回复审核篡改失败回归。
- [x] 4. Real-store integration：复用六模板隔离库，真实store写操作与本地路由替身，验证确认/取消/重复/退款上限/换套餐/退订；不宣称真实agent理解测评。
- [x] 5. Review/verification：Go focused/full、Python/offline archive checks、独立审查；记录面试证据和剩余语义审定边界，再推送。

## Review focus

必要追问不算错误；首轮diagnostic不进完整任务分母；setup失效与服务失败分别统计；缺少最终快照/审核/跨账户对照不得通过；预览参数换绑与重复执行即使余额看似正确也失败。

Ledger rulings：语义审核缺失保留needs_review而非自动判对；本轮实际agent HTTP适配不接付费服务，使用Driver接口与真实store替身实证，明确记录未完成部分。审查指正额度/诊断/遥测/初始化边界后均添加失败回归并修复；固定SHA保护新采集版本，采集代码哈希不限制未来生产升级。

最终证据：Go全项目测试/vet通过、Python96项通过、六项归档verify通过、独立最终审查无阻塞发现。30真实store分支记录已冻结并离线复核；回答审核未冒充完成，模型/JEV调用0。
