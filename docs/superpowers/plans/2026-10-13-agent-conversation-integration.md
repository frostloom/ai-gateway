# 真实客服链路与离线事实核验实施计划

> 使用 superpowers:executing-plans 在当前会话执行；测试先行，完成后独立审查。

**目标：** 不访问外部模型/JEV，连接真实客服HTTP、会话、工具和数据库，补确定性事实核验及V1/V2可比条件。

**设计：** 新增可注入http.Handler的ConversationDriver，初始化与数据库oracle由独立接口提供。测试使用真实agent.Handler、真实billing admin、真实临时MySQL与私有Redis；模型HTTP只指向本地httptest，JEV使用脚本替身。动作无工具时标记unreviewed，由测试/记录审核者补标签，不通过期望动作反推实际行为。事实核验只对明确可解析的现金余额/账本、订单、订阅与确认金额字段给出结论，无法解析仍待审核，不能替代语义审核。对照配置固定模型、提示词/工具预算、种子定义和执行校验，仅允许路由模式差异；本地结果不能发布JEV效果。

**约束：** 不读.env；不访问付费接口；不修改1220历史归档和既有30条归档；不修改无关plate_sim.py；完成检查推送main。沿用用户已确认的三项范围与无需重复询问要求。

## 任务与验证

- [x] HTTP适配器：新增conversation_http.go及_test.go。接口ConversationOracle(Setup/State/Pending)、NewHandlerConversationDriver(http.Handler,oracle)。用失败测试验证实际HTTP响应/原始trace与telemetry、oracle pending、失败响应保留、无工具动作待审。复用RunConversation，不能向期望标签伪造观测。
- [x] 真实链路回归：新增conversation_agent_test.go。真实admin+agent+本地LLM；高置信只读JEV绕过、低置信回退、退款/充值确认前不写、追问后预览、取消/重复确认。数据库模板复用隔离测试设施，明确替身不验证模型准确率；核对本地请求数与telemetry。
- [x] 事实核验：新增conversation_facts.go及_test.go，明确现金/账本、订单号和订阅plan/续费的有限规则；确认金额核验依据pending，完成金额依据账本差额。错误已知事实使任务失败，缺失或超出语法仍待审核。与审核摘要分离，不回写旧分数。
- [x] 对照契约：新增conversation_comparison.go及_test.go，验证model/jev_selective、相同定义/案例顺序/profile/模型/提示词/工具schema/输出预算/执行层版本，拒绝缺项、不同条件与synthetic冒充真实；只有双方同范围、有效任务无待审才可比较成功率。真实耗时与本地重放耗时隔离。
- [x] 验证与面试记录：Go全项目test与vet、旧归档verify、新归档重放，记录真实运行/替身边界及新增代码位置；独立审查修复后提交推送。

**审查重点：** 无工具回复不可冒充clarify；HTTP失败不能丢计费遥测；oracle来源不能使用响应自报；余额与额度单位混淆；确认预览不能算实际成功；不同预算不能比较收益。

**执行记录：** 初始main=5412a67；工作区除plate_sim.py无改动。使用当前checkout，避免复制本地数据库环境；全部新增测试使用临时隔离库，不操作用户账户。
