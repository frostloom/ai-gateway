# 充值取消独立受控入口

本轮接续[充值取消离线验收](recharge-cancel-offline-2026-10-24.md)，为原 `recharge-002/cancel` 增加固定独立入口 `recharge-cancel-20261024`。没有新增真实推理、实际claim或部署，累计仍为27模型/6JEV尝试。客服运行逻辑不变。

## 范围和绑定

复用 `paired-live -retest`，使用独立 `recharge-cancel-authorization.v1`、`recharge-cancel-billing-attestation.v1` 和 `authorized-recharge-cancel.v1`，原确认授权或计费材料不能混用。案例保持两臂各“申请充值100元、明确取消”，共四用户轮。上限共享10次模型尝试/¥0.337920声明预留＋1次JEV/$0.002688声明预留；128输出token、16384请求字节及输入token声明包络，超限停止。

固定绑定已部署客服源码056a760及35c117合同、取消离线验收manifest/acceptance、5c2f669诊断源码/manifest、已批准¥100+$1预算SHA。执行时仍需精确绑定最终审查提交和计费材料；proposal的547e537准备基线不是最终执行器版本。DeepSeek `deepseek-flash/openai` 和TypeSafe `jev-1.13.0`保持固定，仅允许合成租户、对话、工具上下文和隔离业务结果。

## 永久预留

新claim固定 `.tmp/paired-live-retests/recharge-cancel-20261024.json`。创建前，原确认claim须匹配原始SHA `2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc`，诊断重测claim须匹配原始SHA `23047a143039f719531cfe403f1769abda7a6091b2ee51cecf6184eb4768e654`。任一缺失或被改动即拒绝，且不创建新claim。档案中两份无密钥旧claim仅为历史副本，不能覆盖本机记录或代替新授权。

当前旧两项永久预留合计¥0.675840+$0.005376；新claim尚不存在。后续获批并领取取消claim后，三个固定分配累计永久预留¥1.013760+$0.008064，已批总预算下未分配¥98.986240+$0.991936。失败也不释放、换输出或源码版本不重领；并非通用多范围预算管理器，后续范围仍须汇总旧分配。

## 默认只读及执行前剩余工作

```sh
go run ./cmd/paired-live -retest \
  -authorization eval/paired-pilots/2026-10-24-recharge-cancel-entry/authorization.example.json \
  -plan eval/paired-pilots/2026-10-24-recharge-cancel-entry/plan.json \
  -billing-evidence eval/paired-pilots/2026-10-24-recharge-cancel-entry/billing.example.json
```

示例精确授权false、费率/token证据false、执行版本占位；只读不加载env、不连接业务库、不创建输出或claim。显式execute也会在凭据和fixture之前拒绝。新执行仍需核验本轮实际序列化请求及纠正路径token包络、当前费率和JEV失败全额预留证据，绑定最终归档构建，再明确批准这一次实际付费执行。总预算已批准，不重复询问；此前确认的批准已消耗，不能沿用。

## 验证

范围测试先因未知独立身份失败，再实现通过。覆盖授权/计费/源码/案例/预算不能替换、两份前序claim分别缺失或篡改拒绝、永久领取后换目录/版本拒绝、默认只读及未就绪执行不读密钥。新增集成测试初次字段名Control/Allowances误写，按实际ConversationState的Controls/Allowance修正，仅测试编译问题，未修改客服实现。

真实Handler、隔离MySQL业务fixture和本地OpenAI/JEV传输替身四路径通过：正常2次模型、纠正4次；提供方HTTP503及JEV本地EOF分别在1次模型后停止，EOF有限诊断实际落盘且无后续回退。完成路径pending清除、取消轮0模型且无工具trace、每臂cancel_confirmation/cancelled审计恰好1条，业务/控制快照与额度不变。自动报告仍needs_review，不等于真实模型质量通过。

独立审查未发现运行代码阻断，指出预算扩张测试最初只改授权；已补齐授权和计划同时扩大、两份摘要同步更新仍拒绝的反例，目标测试再次通过。

首次完整Go回归16包通过、2包失败：`internal/billing/store/TestReserveConcurrentNoOversell` 有并发请求在MySQL `GET_LOCK(?,5)`处返回request lock timed out，`internal/taskbench`整体达到默认10分钟时限，当时正在旧 `TestLocalCampaignStopsAfterSecondBatchBudgetFailure` 的fixture迁移中。栈仅说明超时当时位置，不能据此称该次迁移单独挂死。一次有界独立复验中，旧taskbench测试通过（22.79秒），并发预留测试仍失败（锁超时、insufficient=33而非90）。因此全量回归未通过，根因尚未确认；没有修改锁时限、Kafka/MySQL/Redis配置、共享服务或生产逻辑，也未继续循环重跑。本轮Kafka包通过，不证明此前Kafka时序问题已根治。

此入口的目标验收通过，综合回归保持blocked，实际付费前应先处理重复的并发预留失败，并完成最终请求/计费绑定和精确授权。全量超时遗留的唯一 `ai_gateway_pair_0573cb26e7ed50c7144f88f36b0caadd` 隔离库，其表创建时间落在本轮窗口，已单独清理；后续查询结果和历史文件完整性见档案 `validation.json`。原始日志和捕获仅存被忽略的 `.tmp/paired-live-private/recharge-cancel-entry-20261024/`。
