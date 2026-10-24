# 充值取消案例离线验收

本轮推进原六案例中的 `recharge-002/cancel`，保持冻结定义：申请充值100元、生成预览、明确取消。只新增验收测试与证据，没有改客服运行逻辑、部署或发起真实模型/JEV；历史实际累计仍为27模型/6JEV。

真实 Handler＋隔离业务夹具配合 loopback OpenAI替身、FakeJEV，捕获两臂完整执行：

| 路径 | 替身模型调用 | 结果 |
| --- | --- | --- |
| 正常预览后取消 | 2 | 两臂结构通过，保留needs_review |
| 首次虚报成功、一次纠正后预览，再取消 | 4 | 两臂结构通过，保留needs_review |
| 同一纠正路径沿用旧3次上限 | 3 | 第二臂HTTP失败，明确保留failed |

所有路径逐项比较本租户与控制租户权威快照，余额、账本、订单、充值、订阅和套餐额度不变。成功取消路径中，预览pending绑定recharge/100元；取消轮清除pending，不调用模型、不产生工具trace，权威审计恰好一条 `cancel_confirmation` / `cancelled` / `confirmed=false`。没有第二笔业务写入，也没有为补齐任务而重试超预算路径。

HTTP采集器对没有trace的回复保持action=unreviewed，现有评分器允许这类动作等待独立语义复核。因此没有把无写状态或取消审计直接升级为整个任务通过；正常报告仍needs_review、Success=false。本轮独立审查核对了测试和实际捕获，但不能证明真实模型理解能力。

新测试初次误将local_stub地址设置为非loopback，被现有门禁在创建journal前拒绝；修正为literal loopback。随后将取消动作错误要求为自动cancel标签，经实际证据及既有HTTP采集契约核对，改为保留unreviewed并验证审计和状态。两者是验收配置/断言修正，不是客服产品缺陷修复。未设置真实费率已验证，也不把本地替身当成实际DeepSeek序列化与计费验证；loopback不会触发官方主机的thinking覆盖配置。

## 真实测评候选

独立单案例候选采用共享10模型/¥0.337920声明预留＋1JEV/$0.002688声明预留，128输出token、16384请求字节/输入token包络。这里只是离线proposal，allow_paid=false、ready_for_paid_run=false、零调用，没有新执行授权/固定claim/真实计费绑定。原3次上限在纠正路径下不足的失败证据保留。

已批准¥100+$1总预算不重复询问。现有两次充值确认scope的永久预留合计¥0.675840+$0.005376不释放；本候选尚未领取或扣减新的永久预留。后续若推进真实取消测评，应先绑定独立范围、旧累计预留、最终源码/合同和本轮收费包络，再明确批准实际执行，不能复用已消耗的确认claim。

## 回归边界

首次全项目Go回归中的 `TestConsumerKafkaE2E` 发布reserve事件时收到Kafka `Not Leader For Partition`。当前Kafka容器healthy，测试创建topic后没有等待leader就绪；这仅支持启动/元数据时序假设，不确认根因，也没有修改Kafka配置或生产重试机制。首次失败日志保留，后续复验单独归档，不宣称首次全量全绿。

首次全量17包通过（含taskbench），仅该Kafka测试失败；全量结束后一次独立复验 `go test ./internal/billing/consumer -run '^TestConsumerKafkaE2E$' -count=1 -v` 通过且非skip，未再次完整重跑全项目。独立审查无阻断，隔离库残留0，178份旧私密证据和旧claim摘要均未变。

资料见 `eval/paired-pilots/2026-10-24-recharge-cancel-offline/`；原始本地journal/报告/预算在被忽略的 `.tmp/paired-live-private/recharge-cancel-offline-20261024/`。
