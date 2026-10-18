# 计费性能剖析与优化（2026-10-18）

范围来自用户批准：先采样，再最小优化，最后完整回归及更长窗口、不同账本规模复测。生产保持暂缓放量。执行计划见 [plan](superpowers/plans/2026-10-18-cash-performance.md)。

## 目标 1：剖析

产品基线 `39fd16d`，未改产品代码。新增测试层 SQL driver 包装和 Redis hook；显式设置 `BILLING_CASH_PROFILE=1` 才启用。MySQL GET_LOCK 使用原始 sql.Conn，GORM logger 无法完整统计，因此在 driver 层覆盖 prepared fallback、事务提交和结果集关闭。DriverContext/Connect(ctx) 保留取消语义，Rows 可选接口按当前固定 mysql 驱动转发。

`TestCashProfileRowTiming` 已实际通过：真实 SELECT ? 只记录一次 prepare、一次执行；持有 rows 40ms 被计入执行耗时。采样在预热后开启，在全部 worker 排空后关闭，最终核账不计入。独立审查未发现剩余 P1/P2。

**统计口径：** total_ms 是并发累计时间；stage 包含内部 SQL/Redis，不可跨层相加或当作 wall 占比。锁耗时包含网络与执行，并非纯等待。prepare 与实际执行分开，ErrSkip 不计执行。pool_wait 只代表池耗尽排队，不含建连。下面均为每成功周期累计均值，仅列无失败的复测；P99 是完整周期。

环境仍为 Go 1.25.12 / Windows、MySQL 8.0.40、Redis 7，GOMAXPROCS=4，SQL pool=40/10，专用测试库 ai_gateway_test、Redis DB1。Docker 本轮启动遇到残留 AF_UNIX socket 导致崩溃；备份临时端点目录后恢复。持久卷未重建。运行顺序与宿主环境存在噪声，不能与上轮短探针直接作严格因果对比。

| 租户 | worker | 成功周期 | 周期/秒 | P99 ms | 聚合 ms/周期 | prepare ms/周期 | commit ms/周期 |
|---|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 21 | 4.06 | 333.9 | 25.6 | 70.1 | 60.8 |
| 1 | 8 | 32 | 5.29 | 1630.2 | 23.2 | 63.0 | 72.5 |
| 1 | 32 | 55 | 5.40 | 5691.0 | 24.5 | 64.9 | 62.6 |
| 8 | 1 | 22 | 4.40 | 259.2 | 23.0 | 60.5 | 67.1 |
| 8 | 8 | 136 | 25.93 | 398.8 | 28.0 | 80.6 | 93.3 |
| 8 | 32 | 153 | 26.23 | 1656.8 | 30.8 | 89.4 | 165.3 |

复测请求窗口5s加排空，419个成功周期；全部 errors/invariant_errors/pending/duplicate_phases=0。每周期恰为12条聚合、4条 initial_quota、39次 prepare。每次 requireCashMatch 先重建后又调用 LedgerBalance，重复了3条聚合和1条初始额度查询；Reserve与Settle各做一次，完整周期可减少8条 SQL及其 prepare。

租户锁每周期累计耗时依表中顺序为4.3、1202.6、4464.0、3.1、6.0、763.8ms；Redis各类命令累计为17.4、14.9、15.7、15.5、21.6、23.7ms。所有场景 pool_wait=0。同租户并发时锁排队显著，多租户高并发还受提交抖动影响；SQL聚合与prepare优化只能减少其中一部分。

第一轮窗口3s也完整保留：[initial](../eval/billing-readiness/2026-10-18/cash-profile-39fd16d-initial.jsonl)。该轮测试退出1：单租户32 worker产生30次操作错误、15个pending，SQL与Redis仍一致，但未满足‘所有生命周期完成’验收，不能算通过。该场景 commit 最大约5844ms。其他五场景无操作/资金断言错误。失败证据表明提交抖动和串行排队均需考虑，不得宣称重复聚合解释了全部回退。

复测原始数据：[repeat](../eval/billing-readiness/2026-10-18/cash-profile-39fd16d-repeat.jsonl)，恰6条，退出0；不覆盖首轮失败。

```powershell
$env:AGENT_LIVE_JEV='0'
$env:REDIS_ADDR='127.0.0.1:6381'
$env:GOMAXPROCS='4'
$env:BILLING_CASH_LOAD='1'
$env:BILLING_CASH_PROFILE='1'
$env:BILLING_CASH_LOAD_DURATION='5s'
$env:BILLING_CASH_LOAD_TAG='profile-39fd16d-repeat'
go test ./internal/billing/store -run '^TestCashProfileRowTiming$' -count=1 -v -timeout 60s
go test ./internal/billing/store -run '^TestCashLoad$' -count=1 -v -timeout 180s
```

禁止与另一 Store 测试进程并行；测试会清理专用夹具。普通回归前移除 BILLING_CASH_LOAD 与 BILLING_CASH_PROFILE，避免意外附带压测。

## 下一步

复用同租户锁内 RebuildBalance 返回的账本余额，继续 Redis 读回核对。锁、fence、恢复与持久幂等均保留。原重复查询同时隐含一次后置 SQL 存活检查，去掉后极窄失联窗口的报错时点可能变化；不能宣称全部故障返回码等价，必须复跑失联/失败恢复回归。
