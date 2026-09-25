# M6 压测教学：怎么打并发、怎么读报告、怎么发现真 bug

> 前置：[05-m5.md](./05-m5.md)（M5 已做压测，本篇把「怎么做压测」讲透）
> 本篇回答三个问题：**① 怎么给一个 HTTP 服务打并发？② 打出来的 P50/P95/P99 和
> req/s 是什么意思？③ 为什么本项目压测不是报吞吐，而是带三类一致性断言——并且真的
> 用压测揪出了 3 个新 bug？**
> 本篇不是纸面教程：下面所有数字都是这台 Windows 机器真实跑出来的。

---

## 0. 30 秒版本

压测 = **并发 N 个请求同时打** + **持续 D 秒** + **结束后读统计 + 跑断言**。

本项目三种打法，从简单到正规：

| 打法 | 一句话 | 适合 |
|---|---|---|
| `curl` + `xargs -P` / shell 循环 | 不用装任何东西，bash 自带 | 快速试水、验证脚本里 |
| `hey` | 一个 exe，参数和输出都是行业标准 | 只测吞吐不想写代码 |
| 项目自带 `scripts/loadtest` | Go 写的，带 **余额==账本** 等一致性断言 | 正式压测（推荐） |

关键认知：**压测的价值不是「跑了多快」，而是「跑到边界后正确性还成不成立」**。
本项目每次压测都断言三件事：无错误、余额守恒、settle 无重复。这次压测又揪出
**3 个真实 bug**（`/report` 连接泄漏、loadtest 租户断言 bug、冷启动拒绝风暴），
每一个都比「我们 P99=800ms」更像面试题。

---

## 1. 为什么压测必须带不变量断言（不只看吞吐）

吞吐高但**钱算错** = 生产事故。对计费系统，「P99 多少」是次要指标，「余额对不对」
是生死指标。本项目压测结束时跑 3 个断言（见 [scripts/loadtest/main.go](../../scripts/loadtest/main.go)）：

1. **无错误**：所有响应都是 200（没有 402/429/5xx/连接错误）。
2. **余额守恒**：`Redis 余额 == initial - Σ(settled actual) - Σ(pending pre)`。
   ——用**独立 SQL 从 bills 表重算**，再和 Redis 投影对比。不超卖（余额没变负）、
   不丢钱（没多扣）、幂等（没重复结算），一条断言全验了。
3. **幂等**：同一个 `request_id` 的 settle 行数 ≤ 1（`uk_request_phase` 唯一索引兜底）。

> 单测是「程序自己说的」，压测断言是「从数据库独立重算的」——后者是终审，前者是初审。

---

## 2. 三条压测路径手把手

### 2.1 项目自带 loadtest（正式压测，推荐）

[scripts/loadtest/run.sh](../../scripts/loadtest/run.sh) 一键跑：自动把 gateway 用高限流
阈值重启（默认 600/min 撑不住）、清掉残留对账进程、拉一个对账常驻并发跑、然后跑压测
+ 三断言。

```bash
# 默认 300 并发 × 60s
bash scripts/loadtest/run.sh

# 参数化：并发、时长
bash scripts/loadtest/run.sh 200 60s
bash scripts/loadtest/run.sh 100 30s
```

输出长这样（200 并发 × 60s 实测）：

```
===== 压测结果 =====
并发 200 × 1m0s：总请求 12515，吞吐 208.6 req/s，错误 0
延迟(ms)：P50=938.5  P95=1455.7  P99=1718.0  平均=965.9
  HTTP 200 = 12515
[PASS] 请求全部 200，无 402/429/5xx/连接错误
[账本] initial=10000000 - settled=7568191 - pending=0 = 2431809 ; Redis balance=2431809
[PASS] Redis 余额 == 账本求和
[幂等] 重复 settle 的 request_id 数量 = 0
[PASS] settle 恰好一次，无重复 delta
loadtest: 3 PASS / 0 FAIL
```

直接读 `go run ./scripts/loadtest -c 100 -d 30s` 也行（不带 run.sh 的服务管理）。

**压测前必须确认租户有余额**：60s 压测会烧掉 ~7-10M 额度（mock 每次返回几百 token）。
没钱了会正确返回 402「insufficient balance」（这是防超卖，不是 bug，但会让断言 FAIL）。
充值方法见 [5.4](#54-常见坑-4-额度被压测烧光)。

### 2.2 curl 并发（不装工具，快速试水）

Git Bash 里用 `xargs -P` 控制并发（N 个同时跑）：

```bash
seq 100 | xargs -P 20 -I{} curl -s -o /dev/null -w "%{http_code} %{time_total}\n" \
  -H "Authorization: Bearer sk-demo-8f3a2b1c9d4e5f60" \
  -H "Content-Type: application/json" \
  -d '{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":false}' \
  http://127.0.0.1:18080/v1/chat/completions | sort | uniq -c
```

`-P 20` = 20 并发，`seq 100` = 打 100 次。输出按 HTTP code 分组计数。没有 P95/P99，
适合「验证一下会不会挂」，不适合正式测延迟。

### 2.3 hey（行业标准工具）

```bash
hey -z 30s -c 100 \
  -m POST -T application/json \
  -H "Authorization: Bearer sk-demo-8f3a2b1c9d4e5f60" \
  -d '{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":false}' \
  http://127.0.0.1:18080/v1/chat/completions
```

输出自带 P50/P95/P99/吞吐/错误分布，跟 loadtest 的统计口径一致，可以直接对。
注意：hey 只报 HTTP 状态码分布，**不做余额断言**——所以它测不了「计费对不对」。

> 三个工具的关系：hey/curl 测「性能」，loadtest 测「性能 + 正确性」。面试时说清这个
> 区别，比会说 hey 更值钱。

---

## 3. 怎么读 P50 / P95 / P99 和 req/s

统计口径：把每次请求的延迟排序，取分位。**P50 = 一半请求比它快**，P95 = 95%，P99 = 99%。

| 指标 | 回答的问题 | 本项目实测（200 并发） |
|---|---|---|
| req/s | 系统吞吐上限 | 208.6 |
| P50 | 典型用户体验 | 938ms |
| P95 | 尾部用户（容易被感知慢） | 1456ms |
| P99 | 最差的那 1%（报警阈值常用） | 1718ms |
| 错误数 | 有没有 4xx/5xx/断连 | 0 |

**怎么读**（重点，面试会问）：

- **吞吐和延迟是跷跷板**：并发越高，吞吐先涨后平、延迟一直涨。本项目实测
  100 并发 P50=469ms → 200 并发 P50=938ms → 300 并发 P50=1329ms，而吞吐一直卡在
  ~210 req/s 左右。这就是「饱和点」：超过饱和点，加并发只是让所有人一起变慢。
- **P99 比 P50 重要**：P50 好看不代表体验好。用户感知的是 P95/P99。本项目真实边界是
  「200 并发时 P99=1.7s，300 并发时 P99=2.4s」——讲这个比讲「我们跑得很快」可信。
- **错误类型要分得清**（loadtest 会打印）：
  - `timeout`：请求 10s 没回 → 服务端处理不过来（**饱和**，服务还在工作）；
  - `dial-fail` / `refused`：连接建立失败 → **环境/端口问题**（Windows 临时端口、accept 队列）；
  - `HTTP 402`：余额不足 → 业务正确拒绝（不是 bug）。

---

## 4. 压测前检查清单

1. **限流阈值调高**：gateway 默认 `RATE_LIMIT_PER_MIN=600`（60/min/租户），高并发必 429。
   run.sh 已用 100000 重启 gateway。
2. **DB 连接池有限流**：billing 必须 `SetMaxOpenConns`（本项目 100），否则 gorm 默认无上限，
   压测下连接数暴涨打爆端口。
3. **http.Client 必须复用**：每次 new Client = 新连接池 = 连接风暴。包级共享 + 大连接池。
4. **日志别开 debug**：`LOG_LEVEL=info` 够用，debug 会让日志写入成为瓶颈。
5. **mock 延迟归零**：`MOCK_LATENCY_MS=0`，别让假上游成为噪音。
6. **租户有余额**：见 5.4。没钱了会 402。
7. **先热身再计时**：冷启动直接满并发，前 1 秒会有假性 refused（见 5.3）。
8. **压测期间对账怎么跑**：只跑 sweep（`-no-rebuild`），rebuild 在流量停止后跑
   （M5 的 2.6 讲过的竞态，别再踩）。

---

## 5. 常见坑（本项目全踩过，每条都有数字）

### 5.1 坑 1：`/report` 响应体没读完 → 连接不回池 → 端口耗尽（本次压测新揪出）

**现象**：压测跑 368 req/s 时，最后 10 秒 `billing` 连 MySQL 全报
`connectex: Only one usage of each socket address`，随后 gateway 返回一片 401/503。

**定位**：`netstat -ano | grep TIME_WAIT` 按目标端口分组——`8876 个 TIME_WAIT 指向
router(9102)`、`4403 个指向 MySQL(3307)`。端口全被 TIME_WAIT 占光。

**根因**（[internal/gateway/client/router.go](../../internal/gateway/client/router.go)）：
`Report()` 里 `resp.Body.Close()` 之前**没读完 Body**。Go `http.Transport` 的规则是——
**连接回池的前提是响应体读到了 EOF**。不读完就 Close，连接直接关闭进 TIME_WAIT，不回池。
而 router 的 `/report` 恰好返回了非空 body `{"ok":true}`，于是一次请求 → 一次 `/report`
→ 一个新连接。368 req/s × 60s ≈ 22000 个连接，超过 Windows 临时端口总量（16384）。

**修复**：
```go
// 读完 body 再 Close，连接才回池
io.Copy(io.Discard, resp.Body)
resp.Body.Close()
```

**为什么之前没炸**：M5 的压测只到 212 req/s（billing 池 50 被卡住），60s 内 1.2 万个
连接刚好压在端口总量之下；连接池 50→100 后吞吐摸到 368（假性，见 6），跨过悬崖才暴露。
**这印证了一条真理：压测要逐渐加压到「真炸」为止，炸出来的才是真 bug。**

### 5.2 坑 2：loadtest 的余额断言没按租户过滤（多租户数据一进来就假 FAIL）

**现象**：seed 造了 4 个租户后，压测断言 2 老是
`期望 3695077，实际 4724481`，差 1,029,404。

**根因**：断言 SQL 是
`SELECT SUM(actual_quota) FROM bills WHERE phase='settle'...` —— **没加 `tenant_id` 过滤**。
单租户时代没问题；多租户后把 acme/startup/trial 的 settled 也算进 demo 的账，对不上
demo 自己的 Redis 余额。

**修复**：`WHERE tenant_id=?` 过滤 + `-tenant` flag 指定断言目标租户。
`seed` 造多租户数据 = 多租户 bug 的试金石。

### 5.3 坑 3：冷启动直接满并发 → 前 1 秒大量 refused（测试工具问题）

**现象**：300 并发 × 60s，每次都报 ~1700 个
`connectex: No connection could be made because the target machine actively refused it`，
P99 却很正常。给错误加时间戳后一目了然：**全部集中在第 0 秒**，之后 59 秒零错误。

**根因**：run.sh 刚重启 gateway，300 个 worker 同时建连，accept 队列冷启动瞬间溢出，
多余的连接被 OS 用 RST 拒绝。**连接根本没到应用层，所以计费不受影响**，但断言 1 会 FAIL。

**修复**：loadtest 加 2 秒热身阶段（16 并发先打一会儿），把 accept 回路和连接池跑热再计时。

**这告诉你**：`connection refused` 不一定是服务挂了——先看时间分布，再下结论。

### 5.4 坑 4：额度被压测烧光 → 返回 402（业务正确，但会让断言 FAIL）

60s 压测烧 ~7-10M 额度。demo 租户 `initial_quota=1000万`，跑两轮就光了。
充值（把余额投影按账本公式重算，`initial_quota` 是账本源头，改它两边自动一致）：

```bash
# 1) 改账本源头
"/c/Program Files/MySQL/MySQL Server 8.0/bin/mysql.exe" -h127.0.0.1 -P3307 -uroot -proot ai_gateway \
  -e "UPDATE tenants SET initial_quota=1000000000 WHERE id=1"

# 2) 按公式重建 Redis 投影（initial - Σsettled - Σpending）
./bin/reconciler.exe -once -no-sweep
```

> 别只 SET Redis 余额——那会跟账本对不上，断言 2 立刻 FAIL。
> 正确姿势永远是从账本源头改，再重建投影。

### 5.5 坑 5（M5 记录过的）：每请求 new `http.Client` + gorm 池无上限 → 端口耗尽

见 [05-m5.md](./05-m5.md) 2.5。复习要点：`http.Client` 并发安全、连接池在 Transport 里，
必须复用；DB 池必须有上限。

### 5.6 坑 6（M5 记录过的）：RebuildAll 与在途 Reserve 竞态漏 pre

见 [05-m5.md](./05-m5.md) 2.6。重建余额是维护操作，压测/高峰期间别跑。

---

## 6. 教学路径：50 → 100 → 200 → 300 逐步加压（本机实测）

按这个顺序跑，边跑边开面板（`http://localhost:18080/`）看余额实时跳：

| 并发 | 时长 | 总请求 | req/s | P50 | P95 | P99 | 错误 | 结论 |
|---|---|---|---|---|---|---|---|---|
| 100 | 30s | 6359 | 212.0 | 469ms | 715ms | 801ms | 0 | 正常区（M5 基线） |
| 200 | 60s | 12515 | 208.6 | 938ms | 1456ms | 1718ms | 0 | 已进入延迟爬升区 |
| 300 | 60s | 12795 | 213.2 | 1329ms | 2064ms | 2394ms | 0 | 饱和：吞吐不涨，延迟翻倍 |

**读这张表**：并发 100→300，吞吐几乎没涨（212→213），P50 却翻了三倍（469→1329）。
这就是「饱和点」。本项目的吞吐天花板 ~210 req/s，瓶颈**不是 MySQL 连接池**（把池
50→100 实测吞吐不变），而是**端到端全链路延迟**：每请求 4 次 gRPC + Redis Lua 原子扣减
+ MySQL 落库 + 流式 mock 往返，加起来 ~1s。

**连接池 50→100 的实验结论**（面试可以讲）：
- 涨池后吞吐不变（瓶颈不在这），但把吞吐顶到假性 368 req/s 后暴露了 5.1 的 `/report`
  连接泄漏 bug；
- 所以「性能优化先找瓶颈」不是口号——**先加压测找真瓶颈，再决定优化哪里**。

**面试怎么讲这次压测**（口述版）：
> 「我把压测从 100 加到 200、300 并发。吞吐卡在 ~210 req/s 不涨，P50 从 469ms 涨到
> 1329ms——这是全链路延迟决定的饱和点，瓶颈在计费链路的串行落库，不在连接池（我试过
> 把池从 50 提到 100，吞吐没动）。但压力加到 368 req/s 时炸出一个真 bug：`/report`
> 响应体没读完就 Close，连接不回池，60 秒攒了 8876 个 TIME_WAIT 打爆 Windows 临时端口。
> 修复后 200/300 并发全量 3 类断言 PASS：无错误、Redis 余额==账本求和、settle 零重复。」

这套话术同时展示了：会加压、会定位、会修、会验证、知道瓶颈在哪。

---

## 7. 面试 Q&A（附参考回答）

### Q1：压测用什么工具？为什么不用现成的 Jmeter/hey？

三种都用过，各有分工：`hey`/curl 快速看性能（吞吐、延迟分位）；项目自己的
`scripts/loadtest` 额外带**三类一致性断言**——压测结束从 MySQL 独立重算余额跟 Redis 对比。
对计费系统，只报吞吐不验账等于白压。

### Q2：P50=469ms 算快还是慢？

单看数字没意义，要看**业务语境**：网关后面是真 LLM，一次生成要几秒，网关侧 1s 以内的
P99 对体验影响很小。真正该问的是「延迟从哪来的」——本项目是 mock 上游的往返 + 计费链路
串行落库。面试讲「瓶颈在计费链路的串行化，吞吐饱和在 ~210 req/s」比报一个数字强。

### Q3：为什么 200 并发的吞吐（208）比 100 并发（212）还略低？

已经过了饱和点。吞吐天花板由全链路延迟决定：`并发 / 平均延迟`。100 并发时平均延迟
475ms → 212 req/s；200 并发时平均延迟 966ms → 208 req/s。**加并发只会增加排队延迟，
不会越过由延迟决定的吞吐上限**——除非优化延迟本身（分片、批量落库）。

### Q4：压测期间对账任务在跑，不会干扰吗？

不会，因为 sweep 只处理「超龄 pending」（`created_at < now - 宽限期`），压测 60s 内的
账单远不够宽限，sweep 是 no-op。它用 billing 幂等 RPC，重复执行安全。真正要避开的是
**rebuild**（用账本覆盖 Redis 的维护操作），必须等流量停了再跑——压测的断言阶段
自动做这件事。

### Q5：connection refused 一定是服务挂了？

不一定。本项目 300 并发冷启动时，第 0 秒刷了 1700 个 refused，之后 59 秒零错误——
是 accept 队列冷启动溢出，连接没到应用层。**先看错误的时间分布再下结论**：集中在开头
是冷启动，全程持续才是真挂了或端口耗尽。

### Q6：压测烧光余额返回 402，算 bug 吗？

不算，那是防超卖的**正确行为**（余额 < 预扣额 → Reserve 返回 INSUFFICIENT → 网关 402）。
但压测断言「无错误」会因此 FAIL，所以压测前要确保租户有钱。这也从侧面验证了
**余额不会变负**——钱花到 0 就拒了，不会超卖成负数。

---

## 8. 你自己讲一遍（口述抽查）

1. 压测打并发有哪三种方式？（curl 循环 / hey / 项目自带 loadtest）
2. P50/P95/P99 分别回答什么问题？吞吐和延迟什么关系？（跷跷板，过了饱和点吞吐不涨延迟涨）
3. 本项目压测断言哪三件事？（无错误 / 余额==账本 / settle 零重复）
4. 本次压测揪出哪 3 个新 bug？（/report 连接泄漏 / loadtest 租户断言 / 冷启动 refused）
5. /report 连接泄漏的根因一句话？（响应体没读完连接不回池）
6. 为什么 50→100 连接池实验是值得的？（没提升吞吐=证明瓶颈不在这，但逼出了真 bug）
