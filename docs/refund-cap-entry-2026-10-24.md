# 退款余额上限独立受控入口

新增固定refund-cap-20261024，原refund-001/balance_cap两臂各三轮不改：“我要退款”；“不是修改旧申请；新申请从我本人账户退回充值款100元人民币，请先预览。”；“确认”。绑定已部署50e3c8dab14b4b6cc25322fd340a274e6970200b客服及v5合同a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f，退款修复离线manifest/acceptance、5c2f669诊断和100+1预算SHA。最终执行器提交另行冻结。

复用paired-live -retest，新authorization/billing/receipt v1。两臂共享最多12模型尝试/声明预留¥0.405504、1JEV/声明预留$0.002688；单请求128输出token、16384字节/输入token声明包络、thinking disabled。原请求100元、余额25元，确认前只预览、实际最多退25元一次；数量上限是停止线，不保证所有模型输出都能完成任务。

创建固定.tmp/paired-live-retests/refund-cap-20261024.json之前必须校验原确认2c9d64b9、诊断23047a14、取消a9ce7a22和补充824e1e44四份原始完整SHA。档案四份无密钥副本只用于验证摘要，不替代本机原件。任一缺失/篡改均拒绝且不创建。O_EXCL永久领取，失败/换版本/输出不重领、不自动释放或重试。不是通用预算分配器。

四项现有永久预留¥1.419264+$0.010752保持，新范围若领取则五项累计¥1.824768+$0.013440，100+1未分配¥98.175232+$0.986560。当前新claim不存在，剩余额度不授权额外付费。模板human_authorized/费率/token验证false，只读检查不加载凭据、不连接fixture、不创建输出；未就绪execute在凭据前拒绝。既有已消耗批准不复用，不重复询问总预算。

真实Handler、隔离fixture、本地custom transport在官方目标上验证完整请求thinking disabled及128输出，FakeJEV：正常4模型、纠正6模型，均仅确认后每臂退款25元一次，账本/余额0，控制租户、订单、订阅和额度不变。provider503一次后停止；JEV EOF在模型臂2次完成后、JEV臂模型前停止，eof诊断持久化，无回退/重试。重用输出目录拒绝且无新调用。自动报告needs_review/Successfalse保留，不等同真实语义通过。

正常/纠正10份请求本地捕获，最大6933字节、1634序列化JSON文本token；本轮从官方token用量页对应链接重新下载tokenizer，SHA89085f12ef79460ac5f66d1119325ddfc694b4ab209d80bbd81d35f081dc9614一致。该计数不是服务端usage或任意后续输出保证，缺usage/超包络仍停止。当前重新核对[DeepSeek价格](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)，保守使用峰值未命中输入¥2/输出¥8每百万token；[TypeSafe模型](https://docs.typesafe.ai/models) pin jev-1.13.0，输入$0.042每百万、输出免费、默认64k，未开long-context。公开费率非供应商账单，失败收费保证未知，使用全额单次预留且失败即停。

私密evaluation.env只保留本机0600，新计划settings/健康认证通过；settings_only不证明runtime绑定。JEV同代理GET models200仅只读健康，不保证真实POST成功或旧传输故障永久修复。本轮不改客服运行逻辑/部署、不新付费、无新claim，实际累计33模型8JEV。验证及完整源摘要见同日refund-cap-entry档案；最终归档执行器及私密false授权/计费材料将精确绑定最终提交，实际运行仍须新明确批准。

完整标准Go回归退出0、18包通过、零失败，隔离库残留0，3142旧私密文件/claim SHA不变。独立审查无P1/P2，审查者独立运行四项门禁测试通过，未另跑依赖MySQL的Handler和全量回归，后两项以主代理实际执行为据。
