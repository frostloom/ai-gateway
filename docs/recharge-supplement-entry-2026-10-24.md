# 补充金额充值独立受控入口

接续[修复部署验收](customer-recharge-supplement-deployment-2026-10-24.md)，新增固定 `recharge-supplement-20261024`，仅原 `recharge-001/supplement_then_confirm` 两臂各三轮：我要充值、明确申请给我本人账户充值100元人民币并先预览、确认。冻结案例原文不修改。用户总预算¥100＋$1已批，本轮不执行真实付费、不创建实际claim、不重部署客服；累计仍29模型7JEV。

## 独立身份和永久预留

复用paired-live -retest，独立authorization/billing/receipt v1，原确认、诊断或取消授权均不能混用。固定已部署客服源码262a47f25572ad77ef38f68d00fb8703d0f45e53、合同v4 SHA624a53c69b2fece1f705332ca208ee854be0588decad986d4dee42355ad46477、修复后离线manifest/acceptance、5c2f669诊断和100+1预算SHA；最终执行器提交单独绑定。

共享最多12模型尝试，声明预留¥0.405504；1次JEV，声明预留$0.002688。每个模型请求128输出token、16384字节/输入token声明包络，thinking disabled。上限是停止线，不能保证所有合法模型路径完成；正常本地4次、纠正6次不代表真实调用数或收益。

新claim固定.tmp/paired-live-retests/recharge-supplement-20261024.json。创建前须原确认2c9d64b9、诊断23047a14、取消a9ce7a22三份原始完整SHA全部一致，缺失/篡改拒绝且不创建。完整SHA及无密钥历史副本见档案；副本不替代机器现有claim。失败、换输出或版本均不能重领，预留不自动释放。非通用预算管理器。

前三项永久预留¥1.013760＋$0.008064；新claim若领取，四项累计¥1.419264＋$0.010752，总预算未分配¥98.580736＋$0.989248。当前新claim不存在，不将余款视为额外授权。

## 验证和请求材料

新范围/claim测试先因未知身份失败，再实现通过；覆盖授权/计费/模型/案例/版本/源码/合同/预算/离线/诊断摘要，coherent扩大auth+plan且重算摘要仍拒绝。三前序claim逐份缺失或篡改拒绝；临时目录领取后换版本/输出拒绝，历史原件不改写。默认只读不加载env、不连接fixture、不创建claim或输出；execute未就绪在凭据前拒绝。既有O_EXCL互斥沿用，本轮未新增并发领取专项测试。

真实Handler/隔离MySQL fixture、loopback传输替身与FakeJEV：正常4模型、纠正6模型各1FakeJEV，确认前无业务写、确认后每臂100元仅一次入账，控制状态/额度不变。provider503在1模型后停止；JEV EOF在模型臂2次完成后、JEV臂首个模型前停止，诊断eof持久化且无回退/重试。完成路径自动报告仍needs_review、Successfalse，不当成真实语义质量通过。

首次测试未登记计划准备层新身份，已按失败证据补齐；JEV失败调用数最初沿用旧取消1次，按本案例模型臂两轮修正为2。捕获overlay最初缺失输出父目录而被runner拒绝，补齐测试目录后四路径通过；不修改生产输出规则或放宽断言。

正常/纠正完整10份模型请求已本地捕获，最大6900字节、1627序列化JSON文本token。官方token用量页面当前链接的tokenizer重新下载且SHA一致。计数不等于服务端usage或所有输出的保证；缺usage或超包络必须停止。2026-10-24实际核对[DeepSeek定价](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)，以峰值未命中输入¥2、输出¥8/百万token保守预留，不假设折扣；[TypeSafe模型](https://docs.typesafe.ai/models)固定jev-1.13.0输入$0.042/百万token、输出免费、默认64k，未启用long-context。公开费率非供应商账单，失败收费保证仍未知，以全额单尝试预留且失败停止约束。

私密评测模型配置与现行客服相同，仅存0600文件。初次Langfuse检查失败，后续分层只读settings、健康与认证通过；未确认暂态失败根因。JEV同代理GET models200仅证明只读健康，不能保证POST成功。首次完整Go回归16包通过、2包失败：旧TestAuthorizedFirstBatchOneShotAndFailureStop四子场景通过后清理MySQL读超时，原TestReserveConcurrentNoOversell出现GET_LOCK(?,5)锁超时/insufficient=36而非90，taskbench整体达到原10分钟时限，当时为TestConversationAuthorityRealAgentReads/get_recent_bills_model，不能将当时栈位置当作单例根因。未改生产代码、并发、池、锁时限、共享服务配置或测试断言。结束后隔离库0。

一次独立复验中旧首批测试7.82秒通过，但原100并发预留仍失败（insufficient=37）。随后仅测试overlay复用既有脱敏SQL/Redis计时工具，保留原100并发/40连接/10闲置/5秒锁和断言，计时运行通过：tenant锁最大871.96ms（含网络往返）、SQL/Redis命令错误0，池等待累计47.39秒为并发累计量。不能由此确认此前根因或永久解决；随后未插桩原测试5.54秒通过，代码/标准并行度/默认时限保持不变再做一次完整回归，退出0、18测试包通过、0失败（taskbench 299.419秒）。当前准备门禁以此更新；首次失败和失败复验均保留，完整结果见本轮validation.json。2962项历史文件/claim摘要不变，隔离库残留0，独立代码/材料审查无P1/P2。

## 执行前门禁

公开模板human_authorized、费率/token验证保持false，只用于只读；不创建新付费范围批准。最终代码审查提交归档构建后，私密候选授权保持false，实际计费/请求证据及最终SHA精确绑定；没有确认前不得付费。只有最终检查仅剩retest_not_authorized、完整回归/清理通过，才向用户请求这一笔具体执行。真实完成后再做独立语义复核及Langfuse六轮内容/评分读回，不能用本轮替身提前替代。

脱敏档案eval/paired-pilots/2026-10-24-recharge-supplement-entry/；原始日志/捕获仅在被忽略的.tmp/paired-live-private/recharge-supplement-entry-20261024/。本轮没有改客服实现、部署或旧付费证据。
