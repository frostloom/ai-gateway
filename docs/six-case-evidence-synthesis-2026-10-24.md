# 六案例历史证据汇总与下一阶段门禁

现有真实证据支持一条有限结论：单轮“查询余额”成功触发JEV只读快路径，在该次观察中少用2次模型、runner耗时较短；其余五例没有减少模型调用，选择性路径各增加1次JEV且runner耗时较长。当前不能宣称JEV整体节约成本或提升速度。六例均取自修复后最终通过的历史运行，跨6个执行器版本、5份运行合同，每例仅一次、顺序固定model后selective，存在版本差异、顺序/缓存/网络波动和成功样本选择偏差。

每例两臂原始独立语义均通过，自动needs_review/Successfalse原样保留。以下是描述性数据，不是预注册同版本队列、总体通过率或收益估计。来源manifest/review/journal/report完整SHA见source-bindings.json，派生数据见paired-observations.json。

| 原案例 | 模型调用 model→selective | selective的JEV | runner毫秒 model→selective | 合同摘要 |
|---|---:|---:|---:|---|
| balance-001/answer_complete | 2→0 | 1 | 1860.013→976.954 | d66fbde2 |
| balance-020/explicit_scope_followup | 4→4 | 1 | 4928.506→6269.223 | b92458d1 |
| recharge-002/confirm | 1→1 | 1 | 1748.848→3284.462 | 35c117c3 |
| recharge-002/cancel | 1→1 | 1 | 1414.805→2435.907 | 35c117c3 |
| recharge-001/supplement_then_confirm | 2→2 | 1 | 2118.668→2506.615 | 624a53c6 |
| refund-001/balance_cap | 2→2 | 1 | 2354.997→2481.776 | a95cacd0 |

四种时间分开记录：telemetry.total_latency_ms逐轮和是服务端处理耗时；turn.latency_ms逐轮和是客户端HTTP往返；result.duration_ms是会话驱动范围；journal.runner_elapsed_ms从factory前到driver清理后，包含账户fixture setup、状态oracle、全部轮次和driver cleanup。runner不含人工思考、campaign全局数据库准备/删除、末尾observer shutdown/export等待和Langfuse评分读回；不能冒称用户完整端到端耗时。六份历史源码均已git show核对factory/cleanup处于计时器内，见clock-provenance.json。

取消案例历史摘要存在两处标签不准：原“服务端轮次耗时”1183.130834/2089.230250ms其实是HTTP往返和；原“完整runner”1255.316792/2226.538375ms其实是会话duration。正确服务端telemetry和964.069000/1723.039875ms，正确runner1414.805167/2435.907250ms。本次明确校正标签，旧摘要、journal及manifest均不改写，任务质量、调用次数和金额结论不变。逐项对应见cancel-timing-erratum.json。

| 原案例 | model配置价（人民币） | selective配置价（人民币） | selective JEV配置价（美元） |
|---|---:|---:|---:|
| balance-001/answer_complete | 0.007452 | 0 | 0.000050442 |
| balance-020/explicit_scope_followup | 0.015662 | 0.01591 | 0.00005061 |
| recharge-002/confirm | 0.003826 | 0.003922 | 0.000050568 |
| recharge-002/cancel | 0.003826 | 0.003826 | 0.000050568 |
| recharge-001/supplement_then_confirm | 0.007424 | 0.007424 | 0.000050442 |
| refund-001/balance_cap | 0.007844 | 0.007684 | 0.000050442 |

以上按各冻结执行使用的模型峰值未命中输入¥2/输出¥8每百万token及JEV输入$0.042每百万计算，不代表当前费率或实际账单；响应缓存命中已单列，未用折扣抵扣配置价，美元人民币不相加、不选汇率制造总节省。JEV失败的送达/收费未知，无法由这些配置价得到全项目精确实际收费。

单轮余额以1次JEV完成真实get_balance、0模型；其余selective快路径轮数0。现金followup的两臂各1次回复纠正，最终fresh read来自执行层cash_read_recovery审计，各1条，不能归功于模型主动fresh read或JEV优化。当前实现快路径限首轮独立无参数且安全置信达标的get_balance/get_consumption/list_plans/get_my_subscription；写入与多轮上下文保留模型/确认流程。这解释本组写入案例未观察到调用收益，但不是未来优化效果证明。

六次成功运行合计22模型/6JEV，只是被选中运行的实际支出。历史另外三次：原现金第二批6/1、首次现金重测8/1、充值确认传输失败1/1，共15模型/3JEV；与成功运行合为37模型/9JEV。失败、清理/传输问题与其消耗均保留，不能剔除后声称项目共用22/6。预算永久预留与实际usage不同，现有新100+1预算五项永久预留¥1.824768+$0.013440不释放；更早旧授权支出不重分配到这五项里。见execution-accounting.json。

下一阶段决策：先在当前50e3c8d/v5 Handler上对原六案例统一离线验收，再建立一个固定同版本队列的独立入口和永久claim链。当前不能重跑已消耗入口或将历史六次通过当成当前v5的整套真实通过；尚未准备新的统一真实入口、授权或最终计费绑定。不要先扩真实样本量再补这些门禁。

统一验收与收益研究是两个目标。工程验收需冻结原案例/问法/画像、运行合同、提示/工具、模型版本与输出包络，保证逐轮动作/金额/租户/确认写入正确，完整内容/评分/清理可核验。收益研究需另定目标流量/样本分层（首轮无参数读取与多轮/写操作分开）、重复次数及停止线、成对顺序平衡、缓存记录、失败计入方式和时钟边界；样本量与新付费范围尚未决定，本轮不虚构次数或审批。不将安全停止算任务完成，不在看到结果后换口径挑成功样本。

本轮只做本地证据读取和派生，无新模型/JEV、claim、评分写入或部署，不修改运行逻辑。统一离线验收是下一个可执行小目标；完成后再按具体门禁判断是否需要新的真实队列和精确付费授权。
