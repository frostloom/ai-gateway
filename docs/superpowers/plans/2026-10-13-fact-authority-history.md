# 事实证据补齐与历史复核计划

**目标：** 免费补齐账单/套餐/额度的有限事实核验，重新核对既有1220×3历史记录的证据边界，整理面试叙事。

**设计：** 在ConversationState追加可选的独立AnswerAuthority（omitempty，旧归档不变），捕获目录与settled账单的查询范围/完整性；额度使用现有Allowance和订阅周期。新的核验函数只判断支持的明确声明，缺来源/缺范围仍not_evaluated，不用工具回复充当独立数据库证据。旧审核数据另建只读复核视图，不覆盖标注或重算成完整任务成功率。

**约束：** 不读.env；不调用外部模型/JEV；不改1220历史原始数据/审核/分数或已有两份本地归档；不动plate_sim.py。沿用用户已确认的三项范围与无需重复询问要求；当前main=e8ab5bb。

- [x] 新增conversation_authority.go/_test.go：typed plan/bill authority、真实store读取范围、显式语法套餐月费/refresh/quota、账单token/金额/列表条数、订阅额度/周期/自动续费，负例先失败再实现。接入CheckConversationFacts与新真实agent oracle；旧无authority数据不补造事实。
- [x] 新增scripts/audit-historical-answer-evidence.py及tests：从冻结reviews/raw的SHA校验过的真实输出产生3660逐条证据复核，已有AI标签与新证据资格分开；失败不剔除、必要追问不扣分、首轮≠多轮。未恢复历史DB事实不计正确。全新版本目录拒绝覆盖，verify重算并固定来源。
- [x] 新增docs/interview/11-evaluation-story.md：从旧70%首工具局限到动作口径/任务定义/独立状态/分流与缺陷修复，引用验证证据和未证实边界，含口述版及问题回答。历史复核结果完成后填入准确统计。
- [x] 定向/Go全项目/vet/Python及旧归档verify，独立审查修复，追加CHANGELOG，提交推送。

**审查重点：** quota万数格式有舍入不能硬算精确值；catalog缺项≠不售卖；部分账单≠全部历史；时间和查询范围对齐；历史回复没有独立证据不能通过；AI审核不是人工盲审。

并行划分：主执行者负责Go事实/真实store验证；独立agent仅负责新的历史审核脚本、测试、归档和报告，互不编辑共享文件。面试文档与最终审查由主执行者完成。
