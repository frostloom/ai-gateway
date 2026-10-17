# 余额重建并发风险：调查与基线

## 范围

基于远程 main 提交 `5054b583d3cf8462224942bc5445d2dab84ae573`，核对新增在线余额重建及充值、退款恢复逻辑。本记录是调查结果，不代表并发缺陷已复现或修复，不授权模型或 JEV 付费评测。

## 已核对的执行顺序

- `Reserve` 先执行 Redis 预扣，再提交 MySQL reserve 行；现有 MySQL 会话锁只按 request ID 串行化同一请求。
- `Settle`、`Reverse` 也存在 Redis 变动与 MySQL 状态提交之间的间隙。
- `RebuildBalance` 从 MySQL 聚合余额后直接 SET Redis，没有与这些间隙协调。
- 新增的 `MeOverview` 不一致修复，以及充值、退款后的 `requireCashMatch`，会在线调用重建。
- 新增恢复函数会删除已经提交的 topup 流水并恢复订单状态；需要核验并发、提交结果不确定和请求取消时的所有权与可恢复性。

## 待复现的假设

当 Redis 已预扣而 reserve 行尚未提交时，在线重建可能把旧账本余额写回 Redis，覆盖在途预扣。应通过测试屏障固定这个执行顺序，分别核验预扣成功和插入失败补偿后的余额及账本。结算、冲正、不同订单退款的并发以及故障重试也应覆盖。

这些是代码路径推导出的风险；当前尚无新增确定性失败测试，不能把它们称为已复现缺陷。

## 环境恢复与安全边界

恢复已有 `ai-gateway-mysql` 和 `ai-gateway-redis` 容器，保留原持久卷。未启动 Kafka 或应用服务。

自动审批首次拒绝测试运行，原因是测试初始化会清理数据。核对源码及只读数据库信息后，确认：

- Store 测试使用 `ai_gateway_test`、Redis DB 1。
- 客服测试使用 `ai_gateway_agent_test`、Redis DB 3。
- 观测和 taskbench 集成测试创建随机临时数据库并使用私有 miniredis。
- 业务数据库 `ai_gateway` 和 Redis DB 0 不属于测试清理目标。

显式设置本机 Redis 地址后，重新申请执行获准。测试仍会清理上述专用测试数据，不应与另一个使用相同固定测试库的运行并发执行。

## 基线验证

PowerShell：

```powershell
$env:AGENT_LIVE_JEV='0'
$env:REDIS_ADDR='127.0.0.1:6381'
go test ./internal/jev ./internal/agent ./internal/taskbench ./internal/billing/store -count=1
```

退出码 0，四个包均通过：JEV 3.871s、agent 53.445s、taskbench 124.638s、store 92.306s。本次使用现有离线替身与本地测试服务，未发起真实模型/JEV 评测。这不是全仓测试或并发风险已消除的证明。

## 下一阶段

向用户提交的短设计为：在现有 Store 内复用 MySQL 会话锁，按租户协调现金变动与重建；故障时保留权威流水，避免撤销其他并发操作。设计待确认，产品代码和测试尚未改动。批准后按失败测试、最小修复、回归、独立审查的顺序完成；每个独立验收目标提交推送。
