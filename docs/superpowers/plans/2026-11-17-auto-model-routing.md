# auto 模型路由 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在网关数据面实现 `model:"auto"`：中间件用 JEV 判定任务复杂度，把请求体改写为 `deepseek-v4-flash` 或 `deepseek-v4-pro` 后走现有链路。

**Architecture:** 纯逻辑集中在新的 `internal/autoroute` 包（配置/状态提取/改写/JEV 熔断/选档决策），gin 中间件放在 `internal/gateway/middleware`（挂在 APIKey→Idempotency→RateLimit 之后），`cmd/gateway/main.go` 只做装配。handler、计费、router 一律不改。

**Tech Stack:** Go 1.25 · gin · 既有 `internal/jev` 客户端（含 Fake）· `internal/pkg/metrics`（prometheus）。

**Spec:** `docs/superpowers/specs/2026-11-16-auto-model-routing-design.md`（已批准版；计划从 spec 立论，执行者两份都读）

## Global Constraints

- 档位默认：便宜档 `deepseek-v4-flash`、强档 `deepseek-v4-pro`；阈值 1.5（`score >= 阈值` 选强档）；判定失败兜底 `cheap`；判定超时 2000ms；state 上限 8192 字节；熔断 3 次失败 / 冷却 30s；`AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE` 默认开、`AUTO_ROUTE_UPGRADE_CHEAP_UNAVAILABLE` 默认关；shadow 实际服务便宜档；`AUTO_ROUTE_MODE` 默认 `off`。
- 复杂度问题定义（唯一判定题）：`complexity`，score 型，`criteria = ["简单","一般","复杂","极复杂"]`，取期望分数（0–3）。
- 非 `auto` 请求零变化：body 字节、headers、行为（反目标，任何任务不得破坏）。
- 不新增数据库表；不改 handler / billing / router；配置只走环境变量。
- `off` 态只校验 mode 本身；`shadow`/`on` 态配置错误启动即失败（fail-fast）。
- 本机跑 Go 测试用 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk`。
- 每个 Task 结束提交一次；提交作者 `frostloom <278895495+frostloom@users.noreply.github.com>`，消息以 `feat:`/`test:`/`chore:` 开头，结尾加 `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`。
- L2/L3 付费评测不在本计划内（另行授权）。

## Review Focus

以下输入类/故障模式 spec 有要求但没有单一任务测试天然覆盖，已各自钉进所属任务：

1. 非 auto 请求 body 字节逐位不变且不出现任何 `X-Auto-*` 头 → Task 6 测试。
2. 改写后 `ContentLength` 未同步 → handler/上游按旧长度截断 body → Task 6 测试断言 `c.Request.ContentLength == len(newBody)`。
3. `messages[].content` 为 OpenAI 多模态数组（`[{type,text},…]`）时提取器不得失败 → Task 2 测试。
4. JEV 返回 score 越界（<0 或 >3）或缺 `complexity` 答案 → 按 decode 兜底，不得 panic/透传 → Task 5 测试。
5. 熔断器并发读写 → `go test -race` 下无竞争 → Task 4 测试。
6. `"stream":true` 的 auto 请求改写后 stream 标志与 SSE 语义不受影响 → Task 6 测试。

---

### Task 1: autoroute 配置与 jev.ProxyTransport 导出

**Files:**
- Create: `internal/autoroute/config.go`
- Test: `internal/autoroute/config_test.go`
- Modify: `internal/jev/jev.go`（在 `jevTransport` 旁加导出包装）

**Interfaces:**
- Produces:
  - `type Mode string`；常量 `ModeOff Mode = "off"`、`ModeShadow Mode = "shadow"`、`ModeOn Mode = "on"`
  - `type Config struct { Mode Mode; CheapTier, StrongTier string; Threshold float64; FailMode string; ShadowServe string; TimeoutMS int; MaxInputBytes int; BreakerFailures int; BreakerCooldownS int; DegradeStrongUnavailable, UpgradeCheapUnavailable bool; JEVEndpoint, JEVAPIKey, JEVModel string }`
  - `func ConfigFromEnv(getenv func(string) string) (Config, error)` — 环境变量名见 spec 配置表；`getenv` 为 nil 时直接用 `os.Getenv`
  - `func (c Config) FallbackTier() string` — FailMode=="strong" 时返回 StrongTier，否则 CheapTier
  - jev 包新增 `func ProxyTransport() http.RoundTripper`（包装现有 `jevTransport()`）

- [ ] **Step 1: 写失败测试 `config_test.go`**

用例（表驱动）：空环境 → 全默认值（Mode=off、两档、1.5、cheap、2000、8192、3、30、degrade=true、upgrade=false、ShadowServe=CheapTier、JEVModel="jev-latest"、JEVEndpoint=jev.DefaultEndpoint）；`mode=off` 时不要求 JEV_API_KEY、其余非法值也不报错；`mode=shadow`/`on` 时以下各返回 error：缺 `JEV_API_KEY`、`AUTO_ROUTE_TIERS` 只有一档或两档相同、`AUTO_ROUTE_THRESHOLD` ≤0 或 >3 或非数字、`AUTO_ROUTE_FAIL_MODE` 非 cheap/strong、`AUTO_ROUTE_SHADOW_SERVE` 不在两档中、`AUTO_ROUTE_TIMEOUT_MS` ≤0 或 >60000、`AUTO_ROUTE_MAX_INPUT_BYTES` ≤0 或 >1048576、`AUTO_ROUTE_BREAKER_FAILURES` <1、`AUTO_ROUTE_BREAKER_COOLDOWN_S` <1、`JEV_ENDPOINT` 形状非法（如带 userinfo 的 https）；`AUTO_ROUTE_MODE` 未知值 → error（含 off 态）；`AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE=0` → false、`=1` → true（upgrade 同理）。

- [ ] **Step 2: 跑测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/autoroute/ -run TestConfig -v`
Expected: FAIL（包/函数不存在）

- [ ] **Step 3: 实现 `config.go` 与 jev.ProxyTransport**

解析规则按 Step 1 表；mode 为 shadow/on 时用 `jev.NewEvaluationClient(endpoint, key, model, http.DefaultTransport)` 做一次端点形状校验（返回的 error 原样包装，client 丢弃）。

- [ ] **Step 4: 跑测试确认通过**

Run: 同 Step 2。Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/autoroute/ internal/jev/jev.go
git commit -m "feat: add autoroute config parsing"
```

### Task 2: state 提取器

**Files:**
- Create: `internal/autoroute/state.go`
- Test: `internal/autoroute/state_test.go`

**Interfaces:**
- Produces: `func ExtractState(body []byte, maxBytes int) (state any, ok bool)` — `state` 为 `[]map[string]string`（`{"role":…,"text":…}`，按对话顺序），直接可作 jev `state` 参数；`ok=false` 表示无任何可提取文本。

- [ ] **Step 1: 写失败测试**

用例：chat 形状（`messages:[{role,content:"…"}]`）→ 逐条映射；content 为多模态数组 `[{type:"text",text:"…"},{type:"image_url"}]` → 拼接各 `text` 字段、忽略无 text 项；responses 形状 `input:"…"`（字符串）→ 单条 `role:"user"`；`input` 为数组且元素是 `{role,content}` 或字符串 → 各成一条；空 messages、空 input、两者皆缺、所有 content 无文本 → ok=false。裁剪：构造总长超 maxBytes 的多轮 messages，断言结果 JSON 序列化后 ≤ maxBytes 且保留的是**最近**的轮次（从最旧开始丢弃）。

- [ ] **Step 2: 跑测试确认失败**

Run: `SDKROOT=… go test ./internal/autoroute/ -run TestExtractState -v`。Expected: FAIL

- [ ] **Step 3: 实现**

顶层 `map[string]json.RawMessage` 探测：有 `messages` 走 chat 分支，有 `input` 走 responses 分支，都没有 → ok=false。裁剪实现：序列化后超限则从头部逐条丢弃再重序列化，直到放得下或只剩一条（单条仍超限则截断该条 text 字节）。

- [ ] **Step 4: 跑测试确认通过** → PASS

- [ ] **Step 5: 提交** `git commit -m "feat: extract conversation state for auto routing"`

### Task 3: body 改写器

**Files:**
- Create: `internal/autoroute/rewrite.go`
- Test: `internal/autoroute/rewrite_test.go`

**Interfaces:**
- Produces: `func RewriteModel(body []byte, model string) ([]byte, error)` — 输入必须是含 `model` 键的 JSON 对象；输出把 `model` 换为给定值、其余键的值**逐字节保留**（`json.RawMessage`）；JSON 无效或缺 `model` → error。

- [ ] **Step 1: 写失败测试**

断言：改写后 `model` 字段为新值；其余每个键 `json.Unmarshal` 后与原值 `bytes.Equal`（含嵌套对象、数组、数字精度保持原字节）；无效 JSON、非对象、缺 model → error；`model` 值含引号等特殊字符时正确转义。

- [ ] **Step 2: 失败** → Step 3: 实现（`map[string]json.RawMessage` + 重序列化）→ Step 4: 通过 → Step 5: 提交 `feat: rewrite the model field in raw request bodies`

### Task 4: JEV 熔断器

**Files:**
- Create: `internal/autoroute/breaker.go`
- Test: `internal/autoroute/breaker_test.go`

**Interfaces:**
- Produces:
  - `type Breaker struct{ … }`；`func NewBreaker(failures int, cooldown time.Duration) *Breaker`
  - `func (b *Breaker) Allow(now time.Time) bool` — closed 恒 true；open 且冷却未到 false；open 且冷却已到 true（半开：放行一个探测，期间后续 Allow false 直到 Record 成败）
  - `func (b *Breaker) RecordSuccess(now time.Time)` — 归零计数、closed
  - `func (b *Breaker) RecordFailure(now time.Time)` — closed 下连续失败达阈值 → open（起算 now+cooldown）；半开下失败 → 再次 open 重计冷却
  - `func (b *Breaker) State() string` — `"closed"|"open"|"half_open"`

- [ ] **Step 1: 写失败测试**

用例（传入递增的 `now`，无 sleep）：closed 时 Allow 恒 true；3 连败后 State=open、Allow=false；冷却期满后 Allow=true 一次（半开）、期间第二次 Allow=false；半开成功 → closed 且计数归零（再 1 次失败不开）；半开失败 → 重新 open 满 cooldown；成功会清零连续失败计数。并发用例：`go test -race` 下 50 goroutine 混合调用三方法。

- [ ] **Step 2: 失败** → Step 3: 实现（mutex 保护；无真实时钟依赖）→ Step 4: `go test -race ./internal/autoroute/ -run TestBreaker -v` 通过 → Step 5: 提交 `feat: add the JEV decision breaker`

### Task 5: 选档决策 Selector

**Files:**
- Create: `internal/autoroute/selector.go`
- Test: `internal/autoroute/selector_test.go`

**Interfaces:**
- Consumes: Task 1 `Config`/`FallbackTier()`、Task 2 `ExtractState`、Task 4 `Breaker`、`jev.Evaluator`（测试用 `jev.NewFake`，见 `internal/jev/fake.go`）
- Produces:
  - `type Decision struct { ServedModel string; WouldBeModel string; Applied bool; Judged bool; Score float64; Probabilities map[string]float64; LatencyMS float64; Fallback string }`
  - `type Selector struct{ … }`；`func NewSelector(cfg Config, eval jev.Evaluator) *Selector`
  - `func (s *Selector) Decide(ctx context.Context, now time.Time, body []byte) Decision`
  - `func (s *Selector) BreakerState() string`
  - Fallback 取值集合：`""`、`"jev_error"`、`"timeout"`、`"decode"`、`"breaker_open"`、`"unparseable"`

- [ ] **Step 1: 写失败测试**

用例（fake JEV 脚本化）：score 1.49 → WouldBe=CheapTier；1.5（恰等阈值）→ StrongTier；score<0 / >3 / 响应缺 `complexity` → Fallback=decode、Served=FallbackTier()、Judged=false；Evaluate 返回 error 且 `jev.FailureDetails(err).Kind=="timeout"` → Fallback=timeout，其他错误 → jev_error；熔断打开（先制造 3 次失败）→ 不再调 Evaluate（fake 计数为 0）、Fallback=breaker_open；body 无 messages/input → unparseable；shadow 模式：Served=ShadowServe、WouldBe=判定档、Applied=false；on 模式 Applied=true；ctx 已取消时行为 = jev_error 路径不 panic。

- [ ] **Step 2: 失败** → Step 3: 实现（Decide 顺序：breaker → ExtractState → `context.WithTimeout(ctx, TimeoutMS)` → Evaluate 一题 → 校验 score ∈[0,3] → 阈值比较；错误映射按 Kind：timeout→`timeout`，decode/response_read→`decode`，其余→`jev_error`；成功/失败都写熔断器）→ Step 4: 通过 → Step 5: 提交 `feat: decide the auto routing tier via jev`

### Task 6: gin 中间件核心（三态/改写/headers）

**Files:**
- Create: `internal/gateway/middleware/auto_route.go`
- Test: `internal/gateway/middleware/auto_route_test.go`

**Interfaces:**
- Consumes: Task 5 `Selector.Decide`、Task 3 `RewriteModel`、Task 1 `Config`
- Produces: `func AutoRoute(cfg autoroute.Config, sel *autoroute.Selector, routeProbe func(ctx context.Context, model string) error, log *slog.Logger) gin.HandlerFunc`（Task 6 允许 `routeProbe == nil` 表示跳过探测；`sel == nil` 仅当 `cfg.Mode == ModeOff`）

- [ ] **Step 1: 写失败测试**

gin 引擎 + 该中间件 + 桩下游（读全量 body 记录、回 200）。用例：
- 非 auto 请求：下游收到的字节与发送的**逐字节相等**；响应无任何 `X-Auto-` 前缀头；`c.Request.ContentLength` 不变。
- body 非 JSON / 缺 model：同上透传。
- `mode=off` + model=auto：400，错误体形状与网关现有 `abort` 一致（`{"error":{"message":…}}`），message 为 `model not listed (auto routing disabled)`，无下游调用。
- `mode=on`：下游 body 的 `model` 为判定档；`ContentLength == len(newBody)`；响应头 `X-Auto-Model`=服务模型、`X-Auto-Applied: true`、`X-Auto-Score`、`X-Auto-Decision-Ms` 存在。
- `mode=shadow`：下游 `model` = ShadowServe；`X-Auto-Applied: false`、`X-Auto-Would-Be`=判定档、`X-Auto-Model`=ShadowServe。
- JEV 失败兜底：`X-Auto-Fallback: jev_error`、下游 model=FallbackTier()。
- `"stream":true` 的 auto 请求：改写 model 后 `stream` 键原字节保留。

- [ ] **Step 2: 失败** → Step 3: 实现（读 body 上限 8MiB，与 handler 一致；顶层 `map[string]json.RawMessage` 取 `model`；非 auto 路径把原 body 装回 `c.Request.Body` 再 `c.Next()`；auto 路径 Decide → RewriteModel → `io.NopCloser` 装回 + 修正 ContentLength → 设头 → `c.Next()`）→ Step 4: 通过（含 `-race`）→ Step 5: 提交 `feat: add the auto routing gin middleware`

### Task 7: Route 探测降级 + 指标 + 日志

**Files:**
- Modify: `internal/gateway/middleware/auto_route.go`
- Create: `internal/pkg/metrics/auto_route.go`
- Test: `internal/gateway/middleware/auto_route_test.go`（追加）

**Interfaces:**
- Consumes: `routeProbe` 回调（返回该模型当前是否可路由的 error）；Task 5 `Selector.BreakerState()`
- Produces: 指标 `gateway_auto_decisions_total{mode,model}`、`gateway_auto_decision_latency_seconds`（histogram）、`gateway_auto_fallback_total{reason}`、`gateway_auto_degraded_total{reason}`、`gateway_auto_breaker_state`（gauge 0/1，closed=0）；新响应头 `X-Auto-Degraded`（值 `strong_unavailable` 或 `cheap_unavailable`）；日志键 `auto_route`（request_id 不在中间件可得，记 method/path/model/would_be/score/fallback/degraded/decision_ms/mode/breaker_state）

- [ ] **Step 1: 写失败测试（追加用例）**

- 判定 strong 且 probe(strong) 出错、degrade 开：下游 model=CheapTier、`X-Auto-Degraded: strong_unavailable`、`X-Auto-Model`=CheapTier。
- 同上但 degrade 关：下游 model 保持 strong（真实链路里由 handler Route 503）。
- 判定 cheap 且 probe(cheap) 出错、upgrade 关（默认）：不改写、无 Degraded 头；upgrade 开：改写 strong、`X-Auto-Degraded: cheap_unavailable`。
- probe 成功：无 Degraded 头。
- probe 只被调用一次（计数断言）；降级后不再二次 probe。

- [ ] **Step 2: 失败** → Step 3: 实现（探测在 Decide 之后、`c.Next()` 之前；指标用 promauto 按 `internal/pkg/metrics` 现有风格注册；每次请求后更新 gauge）→ Step 4: 通过（含 `-race`）→ Step 5: 提交 `feat: degrade across tiers on route failure and export auto metrics`

### Task 8: 主入口装配与回归

**Files:**
- Modify: `cmd/gateway/main.go`（两组 `RateLimit` 之后各挂一次中间件）

**Interfaces:**
- Consumes: Task 1–7 全部产物；现有 router 客户端的 `Route`（包成 `routeProbe`，只取 error）；`jev.NewEvaluationClient(cfg.JEVEndpoint, cfg.JEVAPIKey, cfg.JEVModel, jev.ProxyTransport())`（mode!=off 时构造；off 传 nil selector）

- [ ] **Step 1: 实现 wiring**

`autoroute.ConfigFromEnv(os.Getenv)` 出错 → 启动失败（现有 fatal 风格）；中间件挂 `v1` 与 `rootAPI` 两组，位置在 `RateLimit` 之后。`routeProbe` 用与 handler 相同的 router 客户端实例。

- [ ] **Step 2: 验证构建与定向测试**

Run: `SDKROOT=… go build ./... && SDKROOT=… go test ./internal/autoroute/ ./internal/gateway/... ./internal/pkg/metrics/ -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 全项目回归（项目约定窗口 20m）**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./... -count=1 -timeout=20m`
Expected: 与 main 基线一致（无新增失败；既有条件 skip 照旧）

- [ ] **Step 4: 提交** `feat: wire auto routing into the gateway entrypoint`

- [ ] **Step 5: 推送并核对远程**

```bash
git push origin main && git fetch origin main && git log origin/main --oneline -1
```
