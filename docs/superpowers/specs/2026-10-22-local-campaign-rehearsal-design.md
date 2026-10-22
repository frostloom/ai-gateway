# 六批本地演练执行器

用户已批准继续补齐六案例逐批执行、预算限制、异常停止、日志导出和评分读回；本目标仅作本地演练，不增加付费入口。继续在 main 开发，保留已有无关改动。

新增 `RunLocalCampaign(ctx, dir, options)`，接收冻结定义目录、已拥有的隔离 fixture 环境、执行版本及既有 campaign-preflight 计划。运行器自己创建进程内 Anthropic 模型替身、FakeJEV 和本机 OTLP 接收器；不接受模型地址、密钥、JEV 工厂或外部 observer，不读取凭据环境变量。模型替身仅经预算 transport 处理合成响应，不连接真实模型。沿用真实 handler、工具、独立双臂账户及现有 RunCampaignBatch。

按计划顺序每次预留一批、打开其全新子预算、执行、重放报告、核对结构/事实/usage/OTLP，再启动下一批。语义审核维持 unknown/needs_review；本地确定性检查通过只允许继续本地演练。错误、中断、预算耗尽或导出不完整时立即停止后续批次，不自动重试、不复用输出目录、不退回预留。

输出目录必须新建，包含全新 campaign 及逐批原始账本、run 日志、重放 report.json，和聚合 receipt.json。聚合保留所有计划批次与未开始批次、失败阶段、已完成数量和本地计数；错误正文不归档。allow_paid=false、ready_for_paid_run=false，external_model_calls=0、external_jev_calls=0。声明币种预留及合成 usage 不冒充实际消费。OTLP 仅是本地接收器，不冒充实际 Langfuse 摄入。

本次以可重复的 Go 集成测试作为演练入口；不添加 CLI、付费执行、token 计数或账户管理功能。fixture 使用项目现有隔离 MySQL/Redis 机制，调用者负责关闭环境；运行器负责关闭预算、observer 和接收器。支持现有六案例；未知替身脚本必须在创建输出前拒绝。

验证要求：六批顺序完成、12臂26轮、20次合成模型尝试/6次FakeJEV、任务/轮次/generation完整对应；账户双臂隔离；原报告保持待审。压低某一批模型次数可复现中途停止，后续批次无目录/预留。其他故障测试覆盖模型失败、usage缺失、JEV失败、observer失败和报告事实错误时的编排停止（通过内部执行依赖边界注入，公共接口无故障开关）。先RED后GREEN，相关测试、全项目回归、vet、独立审查，之后提交推送并写入/读回项目记忆。
