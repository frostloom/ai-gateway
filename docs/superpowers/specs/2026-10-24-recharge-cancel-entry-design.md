# 充值取消独立受控入口

用户已要求继续完成下一轮真实测评前的准备。本次交付固定 `recharge-cancel-20261024`，仅原冻结 `recharge-002/cancel` 两臂四轮，复用 Go `paired-live -retest`，不改客服运行策略。

独立 authorization/billing/receipt 使用 `recharge-cancel-authorization.v1`、`recharge-cancel-billing-attestation.v1`、`authorized-recharge-cancel.v1`。固定共享10模型/337920 micro CNY、1JEV/2688 micro USD；128输出、16384请求字节和输入token声明包络。绑定已部署056a760源码/35c117合同、取消离线manifest和acceptance、诊断5c2f669/manifest及已批准100元+1美元预算SHA。精确授权和计费证据默认false，未批准不能加载凭据、创建输出或claim。

创建新永久claim前，原确认和诊断重测两份claim须分别与已消费原始SHA完全一致；缺失或变化即拒绝。此前永久预留675840 micro CNY+5376 micro USD，新范围若领取后总预留1013760+8064，未分配98986240+991936。旧claim不修改，新claim独占同步写入，失败、输出/版本变化不释放或重领。本入口仅管理这三个已审查固定分配，不扩展为通用账本。调用失败停止、不自动重试、不部署。

验证须涵盖身份/案例/预算/源码/合同/计费绑定不可混用，默认拒绝执行在凭据前、两前序claim缺失/篡改拒绝且不落新claim、领取后永久保留，以及真实Handler隔离fixture在替身提供方/JEV下的完成、提供方失败、JEV失败。完成路径取消清pending、取消轮零模型、业务快照不变，自动语义仍needs_review。新范围的收费费率及请求token证明留待实际执行前核验，不把本地替身视为真实模型质量或计费证明。
