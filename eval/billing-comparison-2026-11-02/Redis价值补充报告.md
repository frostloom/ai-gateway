# Redis 在当前计费链路中的价值：补充测试

## 结论

Redis 在当前项目的余额**读取**上有实测价值：本机单热账户、8 并发、三轮 2 秒闭环读取的吞吐量中位数，当前 Redis 投影为 10,732 次/秒，测试候选的 MySQL 余额行查询为 5,382 次/秒。这个结果只代表热读微基准，不能当作生产容量，也不能证明混合工作负载的总体收益。

在同一独立环境的现金写入测试中，MySQL 候选此前更快；两项结果并不矛盾：读路径和完整计费周期做的事不同。现有写链路在持 MySQL 锁时仍按账本重建 Redis 投影，不能把 Redis 原子操作直接等同于无需 MySQL 往返。

更合理的下一步是评估 **MySQL 权威余额与事务记账、Redis 作为可重建读缓存** 的完整方案，包括缓存失效、版本/顺序、充值订阅、异常恢复和一致性要求；本次没有实现或验证该组合，不能称其已经安全。

## 代码中的实际用途

- 余额读：`internal/billing/store/mysql.go` 的 `GetBalance` 和 `getBalance` 直接 GET Redis 余额键；键不存在时当前代码返回 0，**不会在该读函数中自动重建**。Redis 投影可由账本重建（`cash.go` 的 `rebuildCash`、`mysql.go` 的 `RebuildBalance`），权威值来自 `plan.go` 的 `LedgerBalance`。
- 现金操作：`cash.go` 的 `lockCash` 先取得 MySQL 租户 named lock，再把 owner token 写入 Redis；`runCashLua` 核对 token，防止已经丢失 MySQL 锁的旧会话继续写。`lua/reserve.lua` 和 `lua/settle.lua` 分别原子检查/更新余额及请求守卫；这既是当前系统正确性机制的一部分，也带来 Redis 故障依赖。不能只删除 Redis 调用，必须用新的事务、锁和恢复规则替代。
- 计费之外：网关请求去重 `internal/gateway/middleware/idempotency.go`、滑动窗口限流 `ratelimit.go`、认证限流 `auth_rate.go` 和 Agent 会话热态 `internal/agent/chat.go` 也用 Redis。这些功能未在本实验中评估或改造，不应因计费候选而删除 Redis 服务。

## 热读对比

真实 `store.Store.GetBalance` 与测试目录中的 `candidate.balance`，分别查询已预置的同一账户 Redis 键和 MySQL 主键行；每个请求只读一次，不含预占或写入。Go 1.25.12，独立 MySQL 8.0.40、Redis 7.4.11 容器和随机 loopback 端口，数据库客户端池最多 40；每组 2 秒、三次运行，统计闭环完成数，包含排空。表内延迟为每次操作墙钟时间的三轮中位数，吞吐量也取三轮中位数。两方案读取的分别是独立合成账户，其他本机服务继续运行，先 A 后 B，故仍有运行顺序与资源波动。

| 并发 | 方案 | 读取/秒中位数 | 平均延迟 ms | P95 ms | P99 ms | 错误总数 |
|---:|:---:|---:|---:|---:|---:|---:|
| 1 | A Redis | 2,344 | 0.43 | 0.63 | 1.42 | 0 |
| 1 | B MySQL | 1,058 | 0.94 | 1.36 | 2.89 | 0 |
| 8 | A Redis | 10,732 | 0.74 | 1.34 | 2.80 | 0 |
| 8 | B MySQL | 5,382 | 1.49 | 2.18 | 3.60 | 0 |
| 32 | A Redis | 26,757 | 1.20 | 1.91 | 2.94 | 0 |
| 32 | B MySQL | 9,802 | 3.26 | 5.85 | 10.41 | 0 |

完整每轮数据在 `redis-value-final-output.txt`。数据不含网关、鉴权或模型服务，也没有与大量写入同时竞争，且仅一个热账户；不覆盖读多写少的实际比例、网络分布、多实例或长时尾延迟。A 的 GET 与 B 的 GORM 主键读是各自真实测试接口，但并未隔离客户端、协议和服务端因素，不能把差距全部归于数据库引擎。

## 服务中断演练

只操作脚本新建的独立容器，测试直接调用 Store，不代表整个网关的可用性。

1. **停止 Redis：** A 在 Redis 不可用时，余额读取和新预占均返回错误，未生成被阻断请求的账单；B 不使用 Redis，成功完成现金预占、用量和结算。重启 Redis 后显式调用 A 的 `RebuildBalance`，A 先前在途预占后的余额为 900 分；B 结算后为 950 分，账本检查通过。该测试没有验证 Redis 丢失所有持久文件或故障期间全部可能的跨系统写入点。
2. **停止 MySQL：** 预先存在的 A Redis 余额仍能读到 1000 分，B 的 MySQL 余额查询失败。只证明缓存命中可读，**没有验证该值在故障期间的新鲜度**；A 的预占仍需 MySQL 锁和账本，不能由此说写入可用。Redis 掉线时整个网关其他功能也可能受影响，B 测试结果不代表完整应用无需 Redis。

首次 Redis 重启演练因 Docker 随机端口重新分配、夹具继续使用旧端口而失败；原始记录 `redis-value-output.txt` 保留。修复夹具获取新端口后，最终 `TestRedisOutageValue`、`TestBalanceReadValue`、`TestMySQLDownCachedRead` 均通过，日志 `redis-value-final-output.txt`。没有模拟网络分区、Redis 主从切换、MySQL 提交成功响应丢失、断电或真实业务流量。

## 复现

从仓库根目录运行以下命令。脚本只停止自己创建的名称带 `billing-comparison-` 前缀的容器，并在退出时清理；需 Docker、Go 和所用本地镜像。会先运行已选现有账务回归，再运行中断及读负载测试。

```sh
BILLING_COMPARE_REDIS_VALUE=1 ./eval/billing-comparison-2026-11-02/run-conflict.sh
```

上一轮计费完整周期与候选未覆盖规则见 `对比报告.md`。本轮未修改生产配置、使用生产数据、调用模型或部署。

后续 MySQL 事务加 Redis 读缓存的原型、并发与恢复边界和混合负载见 `组合方案验证报告.md`。

## 预占开始前 Redis 中断及同 ID 重试

`bash eval/billing-comparison-2026-11-02/run-redis-reserve-recovery.sh` 创建专用 MySQL/Redis 容器，在真实 Store 调用预占之前实际停止该 Redis，验证请求返回 Unavailable、该请求无账单；重启后测试**显式调用 RebuildBalance**，再使用原请求 ID 和参数调用 `ReserveWithReplaySignal` 两次。race 模式三轮结果相同：首次 OK、再次 DUPLICATE，Redis 余额和账本均为 900，指定 reserve Outbox 1 条。输出 `redis-reserve-recovery-fixed-output.txt`。合成数据，无网关、模型或 Outbox relay。

第一次执行 `redis-reserve-recovery-output.txt` 第一轮通过、后两轮失败：容器重启改变随机宿主端口，测试下一轮仍用旧地址；已更新测试以使用重启后的实际端口并重新执行三轮，原失败保留。这仅覆盖调用前 Redis 已不可达、恢复后显式重建的情况；不覆盖 Redis 已执行命令但响应丢失、重建失败、并发业务、自动恢复、MySQL 或 Redis 持久性故障，不能据此断定所有故障都能安全重试。

## Redis 已扣款但客户端结果丢失的定向模拟

`bash eval/billing-comparison-2026-11-02/run-redis-lost-reserve-reply.sh` 在隔离 MySQL/Redis 使用真实 Store。测试 hook 先让目标 Redis Lua 实际执行成功，再在客户端丢弃成功结果并返回合成错误；这模拟结果不确定，**不是实际网络断连**。三轮 race 测试均观察到：首次返回 Unavailable 时 Redis 余额已从 1000 变为 900、guard 存在、MySQL 尚无该请求账单；同一请求重试后得到 OK，再次重试为 DUPLICATE，最终余额与账本均为 900，指定 reserve Outbox 1 条。原始记录 `redis-lost-reserve-reply-output.txt`。

结果说明该受控切点存在一段 Redis 投影与账本不一致的窗口，随后相同请求通过当前 orphan guard 修复路径可收敛；不证明无人重试时自动恢复，也不覆盖网络传输层重试、Redis 宕机/故障切换、并发充值或重建、不同请求、进程死亡及 Outbox relay。模型和完整网关均未参与，不能据此声称端到端安全。

### 其他请求先于原请求重试

`bash eval/billing-comparison-2026-11-02/run-redis-lost-interleave.sh` 延续上述真实 Redis 执行、客户端 hook 丢结果的注入：首请求扣减后未写账单；同租户另一请求先预占，随后首请求才重试。隔离 MySQL/Redis、race 模式三轮均通过：故障后余额 900，第二请求完成后余额和账本 900，首请求重试后均为 800，两条 reserve 账单及两条对应 Outbox。输出 `redis-lost-interleave-output.txt`。这是固定顺序，并非并发竞争；没有实际网络断连、服务崩溃或 relay，也没有验证首请求永不重试时的所有清理路径。不能据此推断其他业务操作和所有故障时序均安全。

### 同租户并发恢复

`bash eval/billing-comparison-2026-11-02/run-redis-lost-concurrent.sh` 在同一个客户端 hook 注入“Lua 已执行但结果被丢弃”之后，同时释放 8 个 Store 调用：4 个重试原请求、4 个使用不同 ID；所有请求共用同一租户。隔离 MySQL/Redis、Go race 下重复三轮，每轮原请求恰有 1 个 OK、3 个 DUPLICATE，其他 4 个各 1 个 OK；余额及账本均为 500，5 条 reserve 账单及 5 条指定 Outbox。原始输出 `redis-lost-concurrent-output.txt`。

本测试覆盖单进程、单租户、8 次调用的特定故障切点；并发启动不保证覆盖每一种锁顺序。故障是客户端 hook，不是真实网络断连，也未验证多实例、持续负载、余额不足、崩溃、其他业务操作或消息投递；不能将其当作完整恢复保证或容量测量。

### 多租户、余额不足与响应丢失后竞争

`bash eval/billing-comparison-2026-11-02/run-redis-lost-insufficient.sh` 在独立 MySQL/Redis 中为两个租户分别设置余额 250、每笔预占 100；每个租户先注入一次真实 Redis Lua 执行后客户端 hook 丢弃响应，再并发发起 4 次原请求和 4 个不同 ID（合计 16 次）。Go race 三轮：每租户恰有 2 笔 OK、2 条 reserve 账单、2 条指定 Outbox，余额和账本均为 50，无超额预占。原请求可能在其他请求耗尽可用余额后得到 insufficient；原始数据中既出现 3 duplicate/3 insufficient，也出现 0 duplicate/6 insufficient。这是合理的竞争结果，不应假定丢失结果的旧请求必然优先成功。完整输出 `redis-lost-insufficient-output.txt`。

测试仅覆盖固定金额、两个租户及单进程中受控客户端错误；未证明跨进程、真实网络分区、Redis 崩溃、订阅或真实网关的完整行为，也不代表生产并发容量。

## 结算退款差额的 Redis 结果丢失

`bash eval/billing-comparison-2026-11-02/run-redis-lost-settlement-reply.sh` 对真实 Store 的结算 Lua 使用同样的客户端 hook：Redis 实际完成退款差额，再丢弃成功结果。预占 100、实际用量 40 的合成请求在故障时 Redis 余额为 960，reserve 仍 pending 且 usage 已记录，尚无 settle 行；随后两次结算均返回 settled，最终余额和账本为 960，settle 行一条，reserve/usage/settle 三条指定 Outbox。独立 MySQL/Redis、race 模式三轮均通过，输出 `redis-lost-settlement-reply-output.txt`。

这是一次退款差额的单请求受控模拟，hook 不是实际网络故障；不覆盖补扣或余额不足短收、冲正竞争、崩溃、Redis 重启、多实例或 relay。它证明此特定切点同请求重试能够收敛，不证明无人重试时自动恢复或完整结算链路的所有时序安全。

### 补扣和短收

`bash eval/billing-comparison-2026-11-02/run-redis-lost-extra-debit.sh` 使用相同客户端 hook，覆盖结算 Lua 成功补扣后结果丢失。隔离 MySQL/Redis、race 三轮：余额充足时最终实际收取 150、余额和账本 850；余额不足时应付 250、实际收取 150、余额和账本 0，结算行 `error_code=shortfall:100`。两种情况重复结算均保持一条 settle 行和三条指定 Outbox，输出 `redis-lost-extra-debit-output.txt`。

测试中费用和余额为合成值；故障由客户端 hook 产生，不是真实网络故障，未覆盖补扣时并发充值、其他请求或进程崩溃，不能推断这些情况下的短收金额和恢复行为。
