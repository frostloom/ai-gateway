# 改进清单

日期：2026-10-10。范围：只读复核 `main`（`b857a95`），没有改运行时代码。

结论先说：余额路径的 Lua 预扣、幂等结算和「有 usage marker 才补结算」这套骨架是成立的。下面列的是对照代码、测试和 README「已知边界」之后，仍然值得下一轮动手的具体缺口。已在 README 里明确接受的限制会标成「文档已声明」，避免当成未发现的新缺陷。

## 下一轮最值得做的 5 件

按「改动面小到中等、杠杆高、能单独合并」排序。

1. **流式断连不要全额退款，首字节前失败要立刻失败。** 见 P0-1、P0-2。先加 `httptest` 假上游，再改 `relayStream`。
2. **Reserve 在 MySQL 插入失败时补偿 Redis。** 见 P0-3。README 已写成有补偿，代码没有。补一条 Lua 和一条失败注入测试即可。
3. **预扣被取整成 0，或补扣会把余额打穿时，不要把账单永远留在 pending。** 见 P0-4。先写一条 store 测试锁住现状，再定「最小预扣 / 记短欠并结清」策略。
4. **控制面默认只听本机，并给 router / billing admin 加内部令牌。** 见 P0-5。`/route` 现在会把上游 key 明文返回。
5. **限流同一毫秒只算一次，HALF-OPEN 探测要在同一把锁里占位。** 见 P1-1、P1-2。两处都是十几行加竞态测试。

## P0

### P0-1 已转发的流式内容在 usage 帧之前断连，对账会全额退款

**为什么重要：** 计费正确性是这个仓库的核心。OpenAI 兼容 SSE 的 usage 在最后一帧。客户端可以把正文读完再断开，此时 `usageSeen == nil`，账单保持 pending；宽限期过后 reconciler 因没有 `usage_reported_at` 执行 Reverse。README 写的是「结算按上游实际产出算，与请求是否断连无关」，代码只在解析到 usage 帧时成立。

**位置：**

- `internal/gateway/handler/chat.go`：`relayStream` 写失败直接 `break`，函数末尾无 usage 则留下 pending
- `internal/billing/reconciler/reconciler.go`：`SweepOnce` 对无 marker 的 pending 一律 `Reverse`
- 测试只覆盖帧解析：`internal/gateway/handler/chat_test.go` 的 `TestParseUsageFrame`。没有断连计费测试

**下一步：** 用 `httptest` 假上游推几帧内容后断开客户端。断连时若已经转发过内容，用已转发文本估算 completion 并走现有 `ReportUsage` + `Settle`，让对账补结算而不是全退。纯拨号失败不要走这条路径（见 P0-2）。

### P0-2 流式在首字节前失败仍可能返回 200，且不 failover、不立即 Reverse

**为什么重要：** 非流式在拨号失败或 5xx 时会换 provider，全部失败则 `reverseWithBackground` 并返回 502。流式在 `streamClient.Do` 失败时只 `Report(false)` 然后 `return`。函数开头已经设置了 `Content-Type: text/event-stream`，没有写状态码，gin 默认 200 空体。额度要锁到对账宽限期（启动示例是 600 秒）才退回。

**位置：** `internal/gateway/handler/chat.go` 的 `relayStream`（约 226–258 行）对比 `forwardNonStream`（约 159–214 行）。

**下一步：** 首字节前的拨号失败 / 5xx：与非流式一样最多换 2 次 provider；仍然失败则立即 Reverse，并写 502 JSON，不要先承诺 SSE。流已经开始之后保持「不重试上游」的现有规则。

### P0-3 Lua 扣减成功但 reserve 行没落下时，没有补偿

**为什么重要：** README 和 M1 里程碑写的是「Lua 扣减成功 → 插 reserve 行失败 → 反向 Lua 补偿」。`Reserve` 在非重复键插入失败时直接返回 `Unavailable`，不退 Redis、不删 `billing:req:{request_id}`。`SweepOnce` 只扫已有的 pending 行，看不见这笔孤儿扣款。唯一兜底是 `RebuildBalance`，而 `Reconciler.Run` 自己写明 rebuild 与在途 Reserve 竞态，压测期间必须 `-no-rebuild`。调用方若不重试，投影会一直少这块钱。

**位置：**

- `internal/billing/store/mysql.go`：`Reserve` 第 3 步事务失败分支（约 270–281 行），以及函数头注释里的「残留风险」
- `internal/billing/store/lua/reserve.lua`：guard TTL 24 小时，重试会走 `EXISTS` 返回「已扣过」且不再扣；但若调用方永不重试，guard 仍在、账本无行
- `internal/billing/reconciler/reconciler.go`：`RebuildAll` / `Run` 的竞态说明

**下一步：** 插入失败且不是 1062 时，跑一条补偿 Lua：仅当 `billing:req:{id}` 的值等于本次 request_id 时 `DEL` guard 并 `INCRBY` 预扣额。补一条「Lua 已成功、插入返回错误、余额回到原值、同一 request 可以重新预扣」的测试。然后把 README 的表述改成与代码一致。

### P0-4 预扣取整为 0，或结算补扣被拒绝时，账单会永久 pending

**为什么重要：** 两段逻辑叠在一起会白嫖，并且对账修不好。

1. `quota.CostCent` 把「分 / 百万 token」做整数除法。`quota.go` 自己的例子：500+500 token、输入 100 分/百万、输出 200 分/百万，成本是 0。`ReserveCostCent` 再乘 1.2 仍可能是 0。
2. 请求不带 `max_tokens` 时，`chat.go` 把 `MaxCompletionTokens` 留成 0，预扣不含输出。
3. `reserve.lua` 在 `pre == 0` 时 `bal < 0` 不成立，0 余额也能预扣成功。
4. 事后 `settle.lua` 只在补扣时检查余额，不够就返回 -1，且**不**写入 delta guard。`applyDelta` 把它变成 `FailedPrecondition`。`Settle` 因此不会把 reserve 行改成 settled。
5. 对账看到 usage marker 就再调 `Settle`，于是同一条账单每次 sweep 都失败，预扣（哪怕是 0）和「已交付的用量」都悬着。仓库里没有覆盖「补扣会透支」的测试。

**位置：**

- `internal/pkg/quota/quota.go`
- `internal/gateway/handler/chat.go`：`Reserve` 调用处
- `internal/billing/store/lua/settle.lua` 第 20–22 行
- `internal/billing/store/mysql.go`：`applyDelta`、`Settle`
- `internal/billing/reconciler/reconciler.go`：有 marker 就 `Settle`

**下一步：** 先加 store 测试：余额 0、预扣 0、上报 usage 后实际金额大于 0，断言今天的行为（Settle 失败且行仍为 pending）。策略二选一，写进测试：余额路径预扣至少 1 分，不足则 `RESERVE_INSUFFICIENT`；或者 Settle 在补扣不足时记下短欠 / 允许这一次扣到账本权威值，并把行结清，避免 reconciler 死循环。整数「分」对短调用永远是 0 的问题可以同一轮用更小的记账单位，或单独作为 P1-6。

### P0-5 控制面监听所有网卡，且没有鉴权

**为什么重要：** 这不是「公网已经暴露」的断言。`docker-compose.yml` 目前只发布 MySQL、Redis、Kafka。但进程默认 `Listen` / `ListenAndServe(":"+port)`，绑的是 `0.0.0.0`。注释写「本端口仅本机/内网可达」，代码没有强制这一点。

可达之后的具体能力：

- `cmd/router/main.go` 的 `GET /route` 把 `providers.upstream_key` 放进 JSON。`POST /report` 无鉴权，可把熔断打到 OPEN 或用成功回报清零失败计数。
- `cmd/billing/main.go` 的 gRPC 使用 `grpc.NewServer()`，没有拦截器。任意调用方都能 `Reserve` / `Settle` / `Reverse` / `RebuildBalance`。
- `internal/billing/admin/admin.go` 写明鉴权由 gateway 把关。`:9103` 自身不校验身份。`tenant_id` 来自 query。同一文件里的 `POST /admin/providers` 能写入上游 key，退款、充值、改套餐也在这个 mux 上。
- 管理员会话 token 明文落在 `admin_sessions`（`internal/billing/store/auth.go` 已注明演示）。上游 key 明文落在 `providers.upstream_key`（`mysql.go` 的 `Provider` 注释已注明演示）。
- 基础设施默认口令：`docker-compose.yml` 的 MySQL `root`/`root` 发布在 `3307`，Redis 无密码发布在 `6381`，Kafka `PLAINTEXT` 发布在 `29092`。

**位置：** 上面列出的 `cmd/*/main.go`、`internal/billing/admin/admin.go`、`internal/gateway/middleware/admin.go`（`ADMIN_TOKEN` 相等即放行，环境变量为空才关闭）。

**下一步：** 默认监听 `127.0.0.1`。gateway→router、gateway→billing admin 增加共享内部令牌；令牌不匹配时 `/route` 不得返回 `api_key`，`/report` 和 admin 写接口返回 401。gRPC 用同一个令牌做 unary 拦截器。不要在这一轮做 KMS；明文 key 和明文 admin session 留在 P2，避免和「先把门关上」缠在一起。

## P1

### P1-1 租户限流在同一毫秒只计一次

**为什么重要：** 滑动窗口用 `ZADD key now now`，member 就是毫秒时间戳。同一毫秒的第二个请求覆盖同一个 member，`ZCARD` 不增加。突发流量会低于 `RATE_LIMIT_PER_MIN`（默认 600）仍被放行。Redis 故障时中间件返回 500，这一侧是失败关闭，应保持。

**位置：** `internal/gateway/middleware/ratelimit.go`。没有单测。

**下一步：** member 改成「毫秒 + 单调序号」或 `INCR`。用 miniredis 在同一毫秒打入 `limit+1` 个请求，断言只有 `limit` 个通过。

### P1-2 单进程里 HALF-OPEN 可以放出多个探测

**为什么重要：** 设计是 HALF-OPEN 只放一个探测。`available` 和 `markProbe` 各拿一次锁，中间释放。两个并发 `RouteExclude` 都能看到 `probeInFlight == false` 并选中同一 provider。多实例内存态不共享是 README 已声明的边界；这个是单进程竞态，现有 `circuit_test.go` 没盖住。

**位置：** `internal/router/registry.go` 的 `RouteExclude`、`available`、`markProbe`。

**下一步：** 在持有 `r.mu` 期间完成「过滤 + 选中 + 若为 HALF-OPEN 则占住 probeInFlight」。加一个并发测试：冷却到期后 20 个并发 `Route`，只允许一个拿到该 provider，直到 `Report`。

### P1-3 AutoBan 计数在进程重启后从 0 开始

**为什么重要：** `Report` 用内存里的 `consecutiveFail` 决定 OPEN 和 ban。`persistFail` 会把 DB 列 `consecutive_fail` 加一，但启动时没有读回。router 一重启，距离 AutoBan（默认 20）的进度清零。DB 里 `status=2` 的 ban 仍然有效，未达阈值的连续失败则否。

**位置：** `internal/router/registry.go` 的 `Report`、`persistFail`、`ban`。`consecutive_fail` 只在 `Providers()` 的展示路径被读出。

**下一步：** 建立 breaker 时用 DB 的 `consecutive_fail` 做初值；或者干脆只以 DB 计数驱动 AutoBan，内存计数只驱动 5 连败熔断。补一个「写入 19 次失败后新建 Registry，再失败一次即 ban」的测试。

### P1-4 订阅额度是 check-then-act，pending 在 ReportUsage 之前不占额度

**为什么重要：** README 已声明这是 demo 可接受的小概率超卖。它和余额路径「Lua 原子判余额再扣」不是同一保证。`subscriptionConsumed` 按窗口内 reserve 行的 token 求和；token 要等 `ReportUsage` 才写入，所以并发请求在上报前都看到 consumed=0。

**位置：** `internal/billing/store/mysql.go` 的订阅分支（约 191–238 行）、`internal/billing/store/plan.go` 的 `subscriptionConsumed`。`subscription_reserve_test.go` 覆盖额度内、超额、窗口滑过，没有并发。

**下一步：** 不要先改产品语义。加一个 Redis Lua 或 `SELECT ... FOR UPDATE` 的窗口计数，预扣「估算 token」，`ReportUsage` 时改成实际值，Reverse 时释放。用与 `TestReserveConcurrentNoOversell` 同形的并发测试：额度 300、单次估算 200，两个并发只有一个 `RESERVE_OK`。

### P1-5 对账、网关编排和计费结果没有可报警的指标

**为什么重要：** Prometheus 目前主要是 HTTP 计数和 Kafka outbox/consumer（`internal/pkg/metrics/metrics.go`）。没有 reserve 拒绝、settle 失败、pending 年龄、对账 reversed/settled。reconciler 只有 JSON `/status`，进程内没有指标。直方图用默认 buckets，上限 10 秒，而网关上游超时是 2 分钟，长请求会堆进 `+Inf`。`internal/pkg/trace` 的 `WithID` / `NewID` 全仓库没有调用方；`request_id` 只在部分日志字段里。

另外 `GinMiddleware` 在 `FullPath()` 为空时用原始 URL。未匹配路径会变成无界 label。

**位置：** `internal/pkg/metrics/metrics.go`、`internal/billing/reconciler/reconciler.go`、`cmd/gateway/main.go`、`cmd/billing/main.go`（billing 的 `/metrics` 在 admin mux 上，gRPC 本身没有对应延迟指标）。

**下一步：** 给 `Reserve` 的 code、`Settle`/`Reverse` 的成功与失败、`SweepOnce` 的 scanned/reversed/settled 各加一个 counter；pending 最老年龄做一个 gauge。直方图增加 30s/60s/120s bucket。路由模板为空时把 path label 收成 `unknown`。trace 包要么接到 gateway 中间件，要么删掉，不要留一个看起来已经接通的空包。

### P1-6 短调用在「分」单位下费用恒为 0

**为什么重要：** 这是计价单位问题，不一定是某一条分支写错。`CostCent` 向下取整到分。目录价是「分/百万 token」时，几千 token 的调用结算金额是 0，Redis 与账本仍然「一致」，只是没收费。`quota.go` 注释把这当成 demo 需要加大 `MOCK_COMPLETION_TOKENS` 才能看见扣费。

**位置：** `internal/pkg/quota/quota.go`。`sales_test.go` 的 `TestCostCent` 锁的是当前公式。

**下一步：** 若下一轮要让门户价格和真实调用对齐，把内部账本改成「毫分」或「micro-cent」，展示层再换算成元。这一项比 P0-4 大，不要和「pending 卡死」混在同一个提交里。

### P1-7 没有 CI，对账包和网关编排几乎没有测试

**为什么重要：** 仓库没有 `.github/workflows`，也没有 `Dockerfile`（`docker-compose.yml` 里引用 Dockerfile 的服务段是注释）。`go test ./...` 依赖本机 MySQL/Redis，不能作为每次推送的门禁。`internal/billing/reconciler` 没有任何 `_test.go`。网关聊天测试只有 SSE 帧解析。限流、熔断并发、结算透支都没有回归。

近期提交集中在客服评测（JEV 路由模式、契约、离线 prepare）。计费核心的测试集中在 `internal/billing/store/*_test.go`，没有延伸到 gateway 编排和对账分类。

**位置：** `internal/billing/reconciler/reconciler.go`、`internal/gateway/handler/chat_test.go`、`docker-compose.yml` 末尾注释。

**下一步：** 先加一个不连真实 MySQL 的 workflow：`go test` 跑 `internal/router`、`internal/pkg/quota`、`internal/jev`、`internal/gateway/...`，以及 `go vet ./...`。reconciler 用 sqlmock 或 sqlite 不现实（方言是 MySQL），用接口把「扫描结果」注入 `SweepOnce` 的分类逻辑，单测三条路径：无 marker → Reverse，有 marker → Settle，未过宽限 → 不调用。Dockerfile 可以后做，它不修复正确性。

### P1-8 幂等键在成功前就占用，并且不区分租户

**为什么重要：** `Idempotency` 在 handler 之前 `SETNX idem:{key}`，TTL 5 分钟。上游 5xx 或 402 之后，客户端按同一 key 重试会得到 409，而不是重放第一次的响应。注释已说明这是「提交去重」不是「响应重放」。key 没有拼 `tenant_id`，两个租户用同一个 `Idempotency-Key` 会互相挡住。

**位置：** `internal/gateway/middleware/idempotency.go`。`/v1` 和 `POST /portal/completions` 都挂了它（`cmd/gateway/main.go`）。

**下一步：** key 改成 `idem:{tenant_id}:{key}`。若要保持小改动，失败响应（状态码 ≥ 400，且还没开始写 SSE）时删除刚写入的 key，让客户端能重试。响应重放缓存不要在这一轮做。

## P2

### P2-1 订阅额度、熔断多实例、余额单池、模拟支付

README「已知边界」已经写明，这里只标下一步，避免重复发现：

- 熔断多实例：在 P1-2 的单进程占位做完之后，再把状态放到 Redis。不要跳过单进程竞态直接做分布式。
- 余额退款 `min(订单, 当前余额)` 可能吃到其他充值。`plan.go` 的退款路径和 `plan_test.go` 的 cap 测试是按这个口径写的。若要按订单锁资金，是单独的账本模型，不是小修补。
- 月费不接真实支付。`docs/customer-service-foundation-status-2026-10-09.md` 把在线模型和 JEV 实验继续后移，`docs/jev-pricing-check-2026-10-10.md` 说明不能把公开价目当成这个账号免费。下一轮不要启动付费全量评测。

### P2-2 客服工具口径窄于用户问法

**为什么重要：** 评测契约要求模型说明能力边界，工具本身还回答不了这些问法。`get_consumption` 的描述是近 7 天扣费金额，没有 token、模型分布或自定义区间。`get_recent_bills` 是最近已结算消费，契约注释写明最多 10 笔，不是充值订单，也没有导出。`eval/annotations/2026-10-09-intent-contract-v1/cases/consumption.md` 和 `cases/bills.md` 里大量条目的期望动作是「查询并说明能力边界」。

**位置：** `internal/agent/tools.go`；契约在 `eval/contracts/2026-10-09-customer-service-v1/`。`docs/customer-service-foundation-status-2026-10-09.md` 写明：独立人工金标为 0，1220 条独立账户 fixture 未补齐，12 条边界任务只是开发材料。

**下一步：** 给消费查询加可选 `from`/`to`（仍设上限，例如 31 天）和按模型汇总；账单查询明确区分「消费账单」和「充值订单」，需要订单时走已有订单工具。不要把 1220 条在线重跑当作这项改动的验收。验收用现有离线假 LLM 用例加上边界任务里已有的消费/账单条目。

### P2-3 进程退出与对账扫描的运维缺口

**为什么重要：**

- `cmd/reconciler` 和 `cmd/event-consumer` 使用 `signal.NotifyContext`。`cmd/gateway`、`cmd/billing`、`cmd/router`、`cmd/agent` 没有。SIGTERM 会切断正在 `settleAndLog` 的请求，只能靠对账。对账本身是设计的一部分，但 gateway 至少应把已开始的 `ReportUsage`/`Settle` 的后台 context 留到超时，而不是随进程立刻消失。
- `SweepOnce` 一次 `Find` 全部过期 pending，没有 `Limit`。索引是 `idx_recon (status, usage_reported_at, created_at)`（`scripts/schema.sql`），查询条件却是 `phase + status + created_at`。phase 不在索引前缀里。积压变大时会全表过滤并在内存里放全部行。
- `ReportUsage` 在更新行数等于 0 时返回 NotFound。MySQL 默认 `ROW_COUNT()` 是「实际改变的行」。相同 token 的重试可能 0 行改变，于是幂等上报被当成「行不存在」。gateway 仍会继续 `Settle`，钱多半还能结上，但日志会记成 report 失败。

**位置：** 上述 `cmd/*/main.go`；`internal/billing/reconciler/reconciler.go` 的 `SweepOnce`；`internal/billing/store/mysql.go` 的 `ReportUsage`。

**下一步：** gateway/billing 增加 SIGTERM，HTTP `Shutdown` 给一个短宽限，已启动的结算用 `context.Background` 的现有 10 秒超时跑完。对账查询改成 `phase, status, created_at` 索引并按批 `Limit 100`。`ReportUsage` 以「行匹配到」为成功，不要以「列发生变化」为成功。

### P2-4 明文密钥和演示口令

P0-5 把门关上之后再做：admin session 只存哈希（用户 portal session 已经是 SHA-256，见 `internal/billing/store/portal_auth.go` 的 `portalTokenHash`）；`upstream_key` 列改成应用层加密或外部引用；compose 默认口令改成必填环境变量，不再把 `3307`/`6381`/`29092` 无条件绑到所有网卡。`scripts/seed` 仍提到默认 `admin-demo`，而 gateway 的 `ADMIN_TOKEN` 默认已是空字符串，两处说法会让人以为还有一个写死的后门。核对 seed 文案，避免操作者去试一个已经不存在的令牌。

## 建议不要做的事

- 不要在这一轮重跑 1220 条在线意图评测或 JEV 阈值校准。基础状态文档已经把原因和费用边界写清楚。
- 不要为了「分布式熔断」先引入一套新协调服务。P1-2 的单进程占位和 P0-5 的 `/report` 鉴权更小，也更先堵住错误流量。
- 不要把 RebuildBalance 改成默认在有流量时全量跑。代码已经说明它会和在途预扣竞态。孤儿扣款用 P0-3 的补偿解决，而不是更频繁地覆盖投影。
