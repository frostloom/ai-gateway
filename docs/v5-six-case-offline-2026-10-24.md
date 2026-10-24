# 当前v5六案例统一离线验收

已将原六案例在同一当前Handler上统一运行：balance-001/answer_complete、balance-020/explicit_scope_followup、recharge-002/confirm、recharge-002/cancel、recharge-001/supplement_then_confirm、refund-001/balance_cap。原definitions/manifest、问法、轮次、画像、动作和终态要求不修改，使用原PrepareSmallPair选择后的完整案例。每条路径一个suite/journal、12臂26轮、各臂独立fixture tenant，不拼接历史不同版本运行。

当前非测试agent Go源码逐文件与已部署50e3c8dab14b4b6cc25322fd340a274e6970200b比对一致。全部观测绑定v5合同a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f、同一prompt/tools/model/protocol及原画像摘要；循环请求目标限定literal loopback http://127.0.0.1/six-v5/chat/completions，deepseek-flash/openai是本地协议标签。模型和JEV均为替身，allow_paid=false，无新付费范围/claim，不重绑定历史已消耗授权。

| 本地路径 | 完成臂/观测轮 | 替身模型 | FakeJEV | 任务failed臂 |
|---|---:|---:|---:|---:|
| normal | 12/26 | 22 | 6 | 0 |
| repair | 12/26 | 30 | 6 | 0 |
| missing-fresh-read | 12/26 | 20 | 6 | 2 |
| wrong-refund-amount | 12/26 | 22 | 6 | 2 |

正常路径每轮现金查询均有本轮成功get_balance；单轮余额的selective由高置信FakeJEV走只读快路径、0模型。现金followup的第一轮FakeJEV刻意低置信回退，这只是受控脚本选择，不代表真实置信分布。写操作缺参数先问金额，明确申请后真实预览、用户确认才一次入账或退款，取消清pending且不写钱。recharge确认/补充各入账100元一次，refund请求100元/余额25元、确认仅退25元一次。原订单/订阅、控制租户和额度保持要求，确认或取消轮模型/JEV均0。

纠正路径向明确充值/退款申请注入leading成功虚报，复用最多一次回复纠正后生成真实预览；现金followup第二轮连续遗漏读取并报5000元，两臂各纠正一次后由execution cash_read_recovery实际新读一次，权威审计可查。该恢复不是模型主动fresh read，也不是JEV收益。本轮没有新增这些运行规则，只验证现有规则在同版统一案例中的行为。

两个负例用于检查验收器：missing-fresh-read第二轮只返回沿用历史的正文、无金额断言可触发恢复，两臂均以“read action lacks a successful read trace”failed，业务状态不变；wrong-refund-amount故意将tool参数改200而原申请为100，两臂均因preview/pending参数和执行绑定不一致failed。此负例在用户确认后仍按余额cap退款25元，但最终金额相同不等于冻结任务成功。负例没有HTTP失败，不把任意传输失败当作探针命中，也不把它们计入正向任务通过数。

自动正常/纠正报告仍needs_review、Successfalse，无trace澄清/取消动作仍unreviewed。这里的测试通过表示本地验收断言通过及负例能被抓住，不是104轮真实模型评测成功、供应商语义质量或总体收益证明。四路径共94次替身模型调用/24次FakeJEV；全项目测试中其他本地调用不计入这四份捕获统计。64模型/6FakeJEV上限仅本地测试停止线，不是新付费预算提案。

完整请求本地保存，最大7280字节、max_tokens=128。literal loopback不会注入官方DeepSeek域名专用thinking字段；FakeJEV用既有authorizedTestJEV测试适配器返回jev-1.13.0及合成usage，模型usage也固定合成100输入/8输出。这些不算token测量、真实版本验证或计费证据，未来真实统一入口仍须另行捕获官方目标请求/精确绑定版本与包络。本地journal再次运行不增加依赖调用，仅验证本地持久化行为，不授权恢复真实消耗claim。

首次新测试脚本误将读取案例当成有写操作Operation字段，发生nil指针；后续固定JEV版本与Fake默认模型标识不符，被result_mismatch及停止守卫拦截；取消测试最初期待trace而现契约trace0、cancel_confirmation/cancelled审计1；错误金额负例的预期错误文字“requested”也与既有preview/pending参数绑定诊断不一致。均按原定义/源码/日志纠正本地测试设置，不改生产门禁、案例或状态要求。首次失败、局部诊断和最终通过日志分别保留。

脱敏档案eval/paired-pilots/2026-10-24-v5-six-case-offline/；原始捕获仅在被忽略.tmp/paired-live-private/v5-six-case-offline-20261024/capture-final/。源码与四路径数据摘要见source-bindings.json和acceptance.json。下一步需建立独立同版本队列受控入口与永久预算/claim链、最终执行器和请求计费绑定；本轮尚无该新入口、精确付费授权或真实同版本整套通过，不自动启动额外测评。历史真实累计仍37模型9JEV，运行客服未部署变更。

验证结果：定向四路径捕获通过，新测试在完整回归中亦通过。首次标准完整Go回归退出1、17包通过，仅既有TestConsumerKafkaE2E报Not Leader For Partition；结束后原配置单独有界复验一次通过（12.26秒、非skip），未改服务/代码/并发/锁/时限。当前只读Kafka60分区均有leader且healthy，不能还原失败时状态、不能确认根因或永久修复；首次失败保留，不声称完整全量全绿。独立审查无P1/P2，审查者MySQL沙箱访问不可用，运行证据以主代理为据。3263项旧私密文件SHA不变，隔离库0。本小目标交付范围是同版离线探针和证据，未来新真实入口仍需完整回归门禁。
