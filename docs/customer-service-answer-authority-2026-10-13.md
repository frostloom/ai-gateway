# 免费独立事实核验的范围

本轮新增可选的ConversationState.AnswerAuthority，旧数据缺此字段仍保持原样。它由独立store查询获得，而不是解析工具Result再当真值；已接入本地真实agent oracle和CheckConversationFacts。无需外部模型/JEV调用，也不自动修改历史评分。

|声明|独立来源和限制|
|---|---|
|套餐名称、定期类型、月费、刷新小时、档次额度|完整目录；停售/缺失在完整目录可否决，部分目录缺项保留未知|
|本期到期、剩余额度/配额、刷新周期|有时间戳的权益快照和目录窗口；未来到期的cancelled本期有效|
|最近账单笔数、时间、模型、token、扣费和合计|本租户settle/settled前10条，总数与分页覆盖；单一成功read trace绑定默认3或明确1..10|
|近七天逐日扣费和合计|独立每日现金汇总，Tokens字段单位为分，不是token；日期限定为采集当天及前六天|

缺usage/amount字段不当零。同轮多个账单读取无法把回复唯一归属到一个查询范围，保留not_evaluated。空模型名的“?”核对的是实际展示占位，不代表识别了真实模型。万数按实际显示舍入比较，保留原始精确额度。日查询补齐零日；零现金不证明没有记录、没有token或免费使用。

有限规则只识别生产模板支持的明确语法；条件、引用和不支持的表达保留语义审核。match不是整份回答通过，缺语义审定仍needs_review；不将这些开发单测转成完整任务成绩。

新增真实本地agent读取测试覆盖list_plans/get_my_subscription/get_recent_bills/get_consumption，各含model和jev_selective两种模式。模型服务为本机httptest脚本，JEV为Fake，真实billing admin和随机MySQL测试库/私有Redis处理读取，测试结束清理。正确工具回复由独立源核对；篡改月费、周期、账单合计和七日合计必须被否决。账单工具目前不进入直接快路径；带“额度”的订阅请求触发歧义回退，测试用明确订阅读取验证既有允许路径，不改变安全门槛。

测试和评测不同：八个读分支及原十五个真实agent smoke只验证实现，不是新模型质量样本。旧30条store和15条agent归档字节未更改。本轮没有恢复当时的数据库事实，也没有重跑1220条模型请求。独立语义金标、真实配对的延迟/token/费用仍未取得。

最终验证：八个新增读分支、十五个既有agent分支和九项纯authority回归非skip通过（包级31.612秒）；随后全项目Go通过（taskbench90.496秒、store84.494秒）及go vet通过。Python107项通过（74.222秒），六项旧来源verify和一项新历史复核verify通过。独立审查另以GOPROXY=off验证九项authority、两份旧归档和全部Comparison测试通过。远程CI运行结果尚未核验。
