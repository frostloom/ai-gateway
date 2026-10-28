# 新版缓存分拆结算内核

新`full-window-accounting.v2`可按完整有效的报告hit/miss对计算峰值费用：加权输入只向上取整一次，再加向上取整的输出费用。可选cache-hit费率为正且不超过未命中费率，JEV字段固定0；半缺失、负数、越界或总和不符继续拒绝。没有完整cache对时保守按全部输入计价，不能把未知当命中或免费。

旧schema1拒绝非零新费率，含hit的旧usage也仍按原全部输入峰值计价；新字段为0时不进入旧canonical JSON。原`cost()`与`Begin`算法保持，最大单次hold仍74752µCNY/2688µUSD。真实样例input1757/output33/hit1536/miss221在新费率40000/2000000/8000000下为768µCNY，旧账仍3778。

断言RED后，177项最终normal、179项含真实overlay的race通过，分别2.759/59.936秒，0失败/跳过；root再次177项通过，10.686秒，独立定向2.705秒通过，无确认P1/P2。边界、ceil及成本不超原上界、冷缓存预算、旧ticket/hold、JEV免费输出和旧JSON均覆盖。一次边界参考值算术错误仅修测试并保留日志，没有冒称产品RED。

实际25份原证明重新加载仍得到1791模型/445JEV、永久38888170/308003、余66111830/691997，9174份原文件SHA不变。独立副本仅重链已有config/intent/settle，删除原halt/close且保持unsealed；982模型/241JEV已捕获prefix的影子值879664/12316，第九窗影子prefix60386。原账4027750没有改写，不模拟未捕获后缀，不称旧窗恢复或完成。

当前campaign仍创建schema1及零cache费率，未接Actual/Open、新caps或新实际范围。本单元0新增评测推理/JEV/财务claim/评分/部署，未运行全Go或DB；新scope仍须独立接线、完整Handler/native包络、fresh fee/版本、精确归档全Go20分钟与签名冻结。客服及知识配置保持原值。

[机器证据](evidence/full1220-cache-accounting-2026-10-29.json)记录源码、实际前缀与验证摘要。[预算设计](full1220-after-v3-budget-design-2026-10-29.md)说明官方费率、候选64m/.69m和未完成门禁。
