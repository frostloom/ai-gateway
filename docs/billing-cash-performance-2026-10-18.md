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

## 目标 2：最小优化

requireCashMatch 复用同租户锁内 RebuildBalance 返回的账本余额，继续 Redis 读回核对。锁、fence、恢复与持久幂等均保留。原重复查询同时隐含一次后置 SQL 存活检查，去掉后极窄失联窗口的报错时点可能变化；不能宣称全部故障返回码等价。

新增查询预算回归实际先红后绿：原实现 aggregate=6、initial=2，修改后3和1；没有用易抖动的时间阈值。新增 Redis 读回失败/不一致两种用例，在修改前后均拒绝，并确认权威账本未改。独立审查核实全部调用者均持租户锁，未发现新增 P1/P2。

`go test ./... -count=1 -json` 实际退出0，17个测试包通过、0失败；真实 Kafka E2E 已运行。付费 JEV 诊断、opt-in 压测与计时校准按开关跳过。`go vet ./...` 退出0。完整回归覆盖失联旧owner、SQL/Redis故障恢复与充值退款幂等。

## 目标 3：更长窗口与历史规模

探针增加 `BILLING_CASH_LOAD_HISTORY`，范围0..10000，单位为每租户历史扣费周期。每周期种2条聚合历史账单（reserve/settle均终态，实际50分）；用于增大扫描规模，不是完整历史请求重放测试。历史费用、预热费用及实测成功费用全部纳入独立余额预期；settle总数也包含历史。SQL/Redis核验继续保留。

两版使用同一新增探针，SHA256 `924ddd21d01069eaf097b9b47d3df5857cc03e2c695a19300bbc038bf04c5fe7`。优化前从 `1b3102d` 提取源代码至忽略目录，优化后产品为 `2f16bd0`；没有切换主工作区。开启相同 profiling、GOMAXPROCS=4、40/10连接池，窗口10s加排空，六种租户/worker组合。顺序为 before/history0、after/history0、after/history1000、before/history1000，以部分平衡时间漂移；每组合单轮，不能据此做统计显著性结论。

history1000场景在每租户预置2000行，8租户共16000行；测量中继续增加账单，更快版本也会生成更多行，所以测得的是持续增长账本的整体吞吐，不是固定行数聚合 microbenchmark。准备与预热不计入吞吐，历史种子已通过独立审查及小规模实跑。

| 历史周期/租户 | 租户 | worker | 优化前周期/秒 | 优化后周期/秒 | 单轮变化 | P99 ms（前→后） |
|---|---:|---:|---:|---:|---:|---:|
| 0 | 1 | 1 | 5.35 | 6.59 | +23.1% | 238.7 → 203.8 |
| 0 | 1 | 8 | 6.84 | 8.16 | +19.3% | 1378.4 → 1143.6 |
| 0 | 1 | 32 | 6.03 | 8.33 | +38.3% | 5486.7 → 3866.4 |
| 0 | 8 | 1 | 5.96 | 6.34 | +6.4% | 206.3 → 201.3 |
| 0 | 8 | 8 | 28.10 | 37.08 | +32.0% | 545.4 → 623.0 |
| 0 | 8 | 32 | 39.29 | 40.24 | +2.4% | 1215.5 → 1141.0 |
| 1000 | 1 | 1 | 4.73 | 6.44 | +36.1% | 237.7 → 199.1 |
| 1000 | 1 | 8 | 5.46 | 7.75 | +41.9% | 1830.6 → 1252.5 |
| 1000 | 1 | 32 | 5.54 | 7.66 | +38.2% | 5826.1 → 4507.0 |
| 1000 | 8 | 1 | 4.59 | 4.95 | +7.9% | 266.6 → 256.0 |
| 1000 | 8 | 8 | 31.26 | 30.40 | −2.7% | 333.3 → 392.8 |
| 1000 | 8 | 32 | 28.68 | 37.19 | +29.7% | 1569.0 → 1412.5 |

四轮全部退出0，每轮恰6条记录，共24组。优化前成功1855周期、优化后2132周期（合计3987），另各有54个预热周期。全部操作错误、资金核验错误、pending、重复phase及profile命令错误均为0。每个成功周期的聚合/初始额度查询从12/4减至6/2，prepare从39减至31，全部24组计数均严格核对通过。历史种子费用没有混入实测周期计数。

原始数据：[before/0](../eval/billing-readiness/2026-10-18/cash-10s-before-1b3102d-history0.jsonl)、[after/0](../eval/billing-readiness/2026-10-18/cash-10s-after-2f16bd0-history0.jsonl)、[after/1000](../eval/billing-readiness/2026-10-18/cash-10s-after-2f16bd0-history1000.jsonl)、[before/1000](../eval/billing-readiness/2026-10-18/cash-10s-before-1b3102d-history1000.jsonl)。复现沿用前述命令，duration改10s，history按0或1000设置，tag按对应文件修改。完成后清除两个开关和历史规模环境变量。

**结论：** 重复查账已减少一半，单租户本轮吞吐提高19.3%–41.9%；多租户改善不均，一组下降2.7%，另有吞吐提升但P99变差的场景，不能声称全面提速。单租户32 worker的P99仍约3.9–4.5s，提交和锁排队成本仍在。保持暂缓生产放量，需要实际租户峰值/SLO、重复长时稳定性及更大账本验收后再定容量；本轮没有部署生产或调用付费模型。

最终独立只读审查重新核算全部24组、旧5s复测及3s失败样本，核对产品锁调用链、driver计时语义和历史种子公式，未发现新增P1/P2。历史探针 `go vet ./internal/billing/store`、本地文档链接与补丁空白检查均通过。
