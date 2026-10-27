# 完成100题配对

用户明确要求“我让你跑完啊”，这是对原停止后继续完成的最新授权；额外独立总上限仍¥20，不占main预算。原stopped/claim/59回答不改，不重跑已完成slot。使用既有Go执行器/隔离fixture，不修改客服/知识正文。

1. 为新completion capability验证旧claim、原recording/journal完整SHA与全部已知usage/无业务变更/清理；旧78尝试和¥0.336768保守计费继承进同一¥20父额度，不新增20元。新增固定一次continuation claim绑定原证据与新批准原话。旧claim永久保留。
2. 只执行剩余141 task /165 turn，继承已完成slot且guard禁止重复。新结果明确segment，不把141当200；最终聚合精确200唯一slot/224实际回合才完整。
3. 默认检查不执行；用户当前原话绑定approval，新source/binary冻结，local负例与原使用包回归/一次独立最终复核后执行。观测预检查，技术故障当前segment停止；不得用观测失败当回答失败或放弃目标，先处理依赖再决定后续合法窗口。
4. 原始结果只追加；剩余回答隐藏arm独立评分，与59原评分保持统一rubric。逐轮真实Langfuse读回，最终100题四类准确率、配对差异/费用/延时、原停止及批次限制全部报告，仅RAG分支。

执行取舍：completion1仅补齐K030-a即遇到明确observation_flush（2span失败/usage已知），保留这一段而不重跑K030。续跑支持不可重复的父子证据链，继承所有祖先slot/次数/费用。仅独立知识评测合同绑定observer_timeout_ms=10000，main/生产默认2秒不变，无导出重试；单条合成span验证220.066ms导出成功。原各段停止不提升，继续直到所有题目评分完毕。

completion2完成55task后再次observation_flush（203 accepted/201 exported/2failed），usage累计166/705856micro全已知。原隔离库Close读回超时保留Cleanup=false，之后本机MySQL只读所有ai_gateway_pair_ schema为0；新CLI会在任何领取前重新实际查询0并校验独立cleanup-proof SHA，不改原false。为完成100题，新独立合同显式记录observation_best_effort和collect_safe_failures：仅观测故障记录flush/counter失败，后续只读核验，不再中止模型质量数据采集；仅来源代理无技术错误且业务完整快照不变的本地参数/安全业务失败可计为质量失败继续。provider计费未知、HTTP技术故障、实际业务变化、未证实清理仍硬停止；原main与旧模式不变。
