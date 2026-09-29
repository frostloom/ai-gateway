# AI 模型网关 / 计费平台（Go 微服务）

> **一句话**：一个 OpenAI 兼容的多租户模型网关，核心是**计费正确性**——并发下额度不超卖、
> 崩溃后账不丢不重、坏上游自动熔断切换。对标 new-api / one-api，并修掉了它们三个真实缺陷。

- **语言/框架**：Go 1.25 · gin · gRPC · go-redis/v9 · GORM(MySQL) · Prometheus · **前端 web/（Vite+Vue3+TS）**
- **服务**：gateway（API 网关 + Web 面板）· billing（计费 + admin 观测）· router（路由/熔断）· mock-provider（故障注入假上游）· **agent（AI 智能客服，第 6 服务，接入 JEV 判断引擎）** · reconciler · event-consumer
- **存储**：MySQL（权威账本）· Redis（余额投影 / 会话态 / 限流 / 缓存）· Kafka（计费事件流，M10）
- **Web 页面**：`/` 管理面板（X-Admin-Token）· `/portal` 用户自助页（Bearer 租户 key，含 AI 客服聊天）——独立前端工程构建，go:embed 托管，无 CDN
- **AI 客服安全性**：执行层不信任 LLM（tenant 只来自验证过的 key、写操作必须用户确认、参数服务端重校验）
  + **JEV「System One」判断引擎**（每轮先过一道判定，按置信度分层拦截/警告/预判，见 [docs/interview/10](docs/interview/10-intent-eval-and-jev.md)）
- **评测**：30 条功能用例（离线确定性，2 秒跑完）+ **1220 条意图路由评测集**（12 类 × 100 条 × 9 种说法维度，量化 JEV 前后差异）
- **面试文档**：[docs/interview/](docs/interview/)（Go 入门 + 分布式概念 + 每里程碑 Q&A + 项目故事 + 压测教学 + M7 AI 客服 + M10 意图评测与 JEV）

---

## 架构

```
外部客户端
  ├─ OpenAI 兼容 HTTP /v1/chat/completions（含 SSE 流式）→ gateway 数据面
  ├─ / 管理面板 dashboard.html（X-Admin-Token）→ gateway
  └─ /portal 用户自助页 portal.html（Bearer 租户 key）→ gateway portal 组
                       ▼
              ┌─────────────────────────────┐
              │ gateway :18080   (gin)      │ API Key 鉴权 · 幂等键 · 限流(Redis Lua)
              │ 鉴权 → 幂等 → 限流 → 转发     │ 转发 · SSE 中继 · 流式计数
              │ /portal/* → billing admin   │ POST /portal/chat → agent（透传 Bearer）
              └──────┬────────────────┬─────┘
        gRPC（控制面） │                │ HTTP（选路）
                     ▼                ▼
       ┌─────────────────────┐  ┌────────────────────────┐
       │ billing :9101       │  │ router :9102            │ 加权随机选 provider
       │ Reserve/Settle/     │  │ 熔断 CLOSED→OPEN→HALF-   │ + 排除已试节点(failover)
       │ Reverse/Report      │  │ OPEN→CLOSED · AutoBan    │ + /breakers /providers
       │ (Redis Lua 原子)    │  └──────┬─────────────────┘
       │ admin :9103（HTTP）  │         │
       └──────┬──────────────┘         │ HTTP/SSE（数据面流式）
              │ MySQL 3307 · Redis 6381 ▼
              │            ┌───────────────────────────────┐
              │            │ mock-provider :9201/9202/9203 │ MOCK_FAIL_RATE / MOCK_LATENCY_MS
              └────────────┤ mock-a w=5 · mock-b w=3 · mock-c w=1 │
              │            └───────────────────────────────┘
              │
              ▼  agent :9105（第 6 服务，AI 智能客服）
                 POST /chat：ValidateAPIKey 绑定 tenant（执行层强制）
                 → LLM 意图路由（DeepSeek function calling）→ 工具执行/确认挂起
                 → billing admin :9103（租户作用域）· 会话 Redis · 审计 MySQL

reconciler（对账）: 周期扫描滞留 pending 账单 → Reverse 全退 / 补 Settle（幂等）
renewer（续费器，billing 内）: 扫到期 auto_renew 订阅 → 延下一周期并应用 pending 改套餐（M9 起只延续「额度资格」，不再入账钱）
```

**两个数据面**：gateway↔billing 走 gRPC（控制面，计费），gateway↔router→mock-provider 走
HTTP/SSE（数据面，流式）。结算按上游实际产出算，与请求是否断连无关。
**AI 客服是网关的「一个用户」**：agent 用租户 API key 鉴权、按租户隔离、写操作走同一套计费核心——
它自身的安全不靠 LLM 判断，靠执行层强制（见 [07-m7.md](docs/interview/07-m7.md)）。

---

## 对标 new-api：三个缺陷 → 本项目三个修复（最强面试亮点）

| new-api 的缺陷 | 后果 | 本项目的修复 |
|---|---|---|
| 钱包扣减 `check-then-act`，无 `WHERE quota>=N` 守卫 | 并发超卖成负余额 | [reserve.lua](internal/billing/store/mysql.go)：Redis Lua 原子「判余额≥N 再 DECRBY」 |
| 退款非幂等（源码注释明说不能重试） | 重试双倍退款 | `uk_request_phase (request_id, phase)` 唯一索引 + Redis `SETNX` 守卫，重复结算/冲正 no-op |
| 无对账：预扣后结算前崩溃 | 额度永久卡住 | [reconciler](internal/billing/reconciler/reconciler.go)：扫描滞留 pending，无 marker 全退 / 有 marker 补结算 |

> 详述见 [00-project-story.md](docs/interview/00-project-story.md)。

---

## 核心设计

### 计费 Saga（bills 状态机）

```
Reserve（Redis Lua 原子扣减 + 落库）→ [pending]
   ├─ ReportUsage（幂等写 marker）→ [pending + marker]
   │      └─ Settle（幂等）→ [settled]（按实际 delta 多退少补）
   └─ Reverse（幂等）→ [reversed]（Redis 退回预扣）

Reserve 内部微 Saga：Lua 扣减成功 → 插 reserve 行失败 → 反向 Lua 补偿
```

- **Redis balance 是缓存投影，MySQL bills 是权威账本**；对账定时「从账本重建投影」兜底。
- **幂等**：`uk_request_phase` 唯一索引（权威）+ `billing:req/delta:{request_id}` SETNX（缓存层）+ 状态守卫
  `UPDATE ... WHERE status='pending'`，`RowsAffected==1` 才算成功。
- **绝不复用请求 ctx**：Settle 用独立 `context.WithTimeout(context.Background(), 10s)`，客户端断连也不丢账。

### 对账（M4）

```
扫描: reserve 行 status=pending 且 created_at < now - 最大流时长 - 宽限期
  pending + 无 usage_reported_at → 流从未产出完整结果 → Reverse 全退
  pending + 有 usage_reported_at → 流已跑完但结算丢失 → 补 Settle
```

### 路由 / 熔断（M5）

```
CLOSED →(5 连败)→ OPEN →(cooldown)→ HALF-OPEN(1 个探测) → 成功回 CLOSED / 失败回 OPEN
AutoBan: 累计 20 连败 → providers.status=2（写库，重启不失效）→ 人工解除
failover: 只在首个字节前失败重试，重试带 exclude 排除已试节点，流中途失败绝不重发上游
```

### AI 智能客服 + 充值/订阅（M7/M9）

```
用户自助页 /portal（Bearer 租户 key）→ POST /portal/chat → agent :9105
管理后台   /        （admin 登录态）  → POST /admin/chat  → agent :9105（gateway 换发内部凭证）
  agent = 网关的第 6 服务、也是网关的「一个用户」：key 鉴权 → tenant 只来自验证过的 key
  两条身份路径（都在 agent 执行层解析，LLM 永远无法覆盖 tenant）：
    1) 租户自助：Authorization Bearer <租户 key> → billing ValidateAPIKey 派生 tenant
    2) 管理面板：gateway 已过 AdminSession 中间件，转发时带共享密钥头 X-Agent-Internal
       + 目标租户 X-Agent-Tenant（管理员可切换视角，默认 ADMIN_DEFAULT_TENANT）
       密钥不匹配一律回落租户 key 鉴权 → 直接打 :9105 伪造管理员会 401
  LLM（DeepSeek v4-flash, function calling）只负责「意图 → 工具/参数」，不负责安全
  9 个工具（4 读即时执行 / 5 写必须二次确认），全租户作用域，参数服务端重新校验
  写操作：算 preview 挂起（pending_action 存 Redis 会话）→ 用户「确认/取消」→ 确认才执行
  每笔工具执行写 agent_audit_log（含 confirmed/preview/result/guard）——可追责
  回复必须是纯自然语言：禁止 ⚙ / 工具名 / 原始输出整段贴出（prompt 规则 + sanitizer 双保险）

余额公式（唯一改动点，4 处一致）：
  balance = initial_quota + Σ(topups) − Σ(pending pre) − Σ(settled actual)
  topups（入账流水，ref 唯一索引 = 幂等提交点）是正余额的权威账本

话费式口径：one_time 买断已停售；recurring 退订=当期不退只停下期续费；
充值退款量 = min(订单额度, 当前余额)，绝不退成负；下单≠支付≠入账（状态机幂等）

M9 订阅 = GPT Plus 式（不是「钱买钱」）：月费只换「额度资格」，不入账钱。
  每 N 小时（默认 5）各模型档次滚动的 token 额度：额度内调用免费（funding=subscription，
  pre=0 对账本完全隐形），超额拒绝等刷新（402 RESERVE_SUB_QUOTA），未用不累积；
  窗口「刷新」是涌现的——消费账单 created_at 滑出窗口额度自动回归，无状态无续费器动钱。
```

### 前端（New API 风格的简约控制台）

```
web/（Vite + Vue3 + TS，双入口）
  设计系统：tokens.css 统一字号/间距/圆角/阴影/层级刻度（中性灰白、蓝色强调、细实线、轻阴影）
            字体系统 sans-serif + JetBrains Mono（数据/code），无需外部字体服务
            共享组件：DataTable（统一表格语言）/ Card / BaseButton / Tag / Modal
                      / Field / Pagination / ToastHost / Sparkline
            图表组件（纯 SVG，零依赖）：
                      AreaChart  双序列面积图（网格 + 坐标轴 + hover 十字线 + 数值提示）
                      KpiCard    指标卡 + 迷你趋势（折线/柱状），把「单纯数字」变成趋势
                      MiniBars   迷你柱状；RankList 排行（色块 + 占比条）
  portal.html     → /portal 用户控制台：工作台+余额卡、商品卡（元/M 双价/上下文/标签/免费标记）、
                    充值收银台（选额→下单→模拟支付→入账）、套餐对比、订单/流水、消费趋势、
                    接口调试（原「试一发」）
  dashboard.html  → / 管理后台：浅色分组侧边栏（运营/供给/系统，可折叠）+ 10 功能区
                    （总览/销售/商品/渠道/熔断/账单/订阅入账/对账/客服审计/接口调试），
                    总览与销售页用面积图 + KPI 趋势 + 双排行替代纯数字；30s 自动刷新
  客服审计（重点视图）：按工具风险分级 —— 退款=高危 / 退订·改套餐=较高 / 充值·订阅=关注
                    / 只读=常规；「被 Jev 拦截」与「应执行却未确认」单独标红并置顶；
                    预览行（pending_confirmation）标「待确认」，不误报为异常；
                    顶部风险概览 + 工具分布排行 + 租户操作量排行 + 风险口径说明
  ChatDock（共用悬浮客服层，两个页面都挂）：
    - 首次默认收起（选择记 localStorage），点击后展开右侧客服面板
    - 可拖拽（拖头部移动、拖左边缘改宽）、位置尺寸 sessionStorage 记忆
    - 收起为简洁浮标（保留未读角标），点击再展开；跨视图常驻、会话不丢
    - 任务式对话：写操作渲染「确认执行/取消」按钮条；工具执行渲染成步骤卡；
      确认后旧确认条失效并留痕（防重复执行已过期操作）
    - portal 模式走 /portal/chat（租户 key）；admin 模式走 /admin/chat（管理员代客 + 切租户）
构建：cd web && npm install && npm run build → dist/ → go:embed 进 gateway（离线可用）
```

### P2 AI 客服评测集（数据驱动回归）

- `eval/cases/*.json`：**30 条用例 × 5 维度**（意图/确认流程/安全注入/输出纯净/订阅额度），
  每条 = 脚本化假 LLM + 多轮用户输入 + 断言集合（工具路由、确认状态机、
  tenant 覆写、admin 写调用、回复纯净度、拦截原因）。
- `go run ./cmd/eval` 离线跑（假 LLM + 假 billing admin + 真 MySQL/Redis，零成本确定性回归，
  当前 **30/30 全绿**）；`-mode http` 打真 agent（真 DeepSeek）。报告留档 `eval/reports/`。
- 权威回归基线：改 prompt / 工具描述 / 校验逻辑后必跑，通过率下降即回归。

真实意图对照（每组 1,220 条）：`./scripts/run-intent-ab.ps1`；独立 JEV 开/关服务，输出 JSON 明细、混淆矩阵和 Markdown 对比。评分版本 2 使用严格 Top-1/Reach、分离澄清、失败与误拦，并记录真实 LLM 请求次数和 P50/P95 延迟。复现和口径见 [意图评测手册](eval/intent-suite/RUNBOOK.md)。

### 前端验收（npm run verify）

```
cd web && npm run verify   # = vue-tsc --noEmit && vite build && node scripts/smoke.mjs
```

`scripts/smoke.mjs`（Node + jsdom）在无浏览器环境真实挂载两个前端并断言：

- 两个入口都能挂载（mounted + 渲染字节数），**运行时错误必须为 0**；
- 悬浮客服层：点击浮标展开（头部/输入框/快捷指令齐全）→ 点收起变浮标 → 点浮标再展开；
- 任务式对话全链路（mock fetch）：发消息 → 收到挂起（确认条 + 预览文案 + 工具步骤卡）
  → 点「确认执行」→ 确认条收起并留痕 → 见成功文案；
- 客服审计风险分级（用真实记录形态）：预览行标「待确认」不误报、确认行标已确认、
  退款标高危、只读标常规。

这一步能抓到「构建通过但运行时报错」以及交互/分级回归（本轮即由它发现并修掉了
「确认后旧确认条仍可点」和「审计把正常预览行误报成未确认写」两个缺陷）。

### P3 JEV 判断引擎（System One 给 System Two 上保险）

Jev = TypeSafe AI 的判定模型：state + 类型化问题（noul/choice/score）→ 带置信度的结构化判定
（~0.1s/次、约 $0.00008/次，不做文本生成）。agent 里两个落点：

```
意图预判：LLM 调用前，Jev Choice 分类意图 → 高置信(≥JEV_INTENT_MIN)向系统提示注入
          「安全预判」引导优先调用对应工具（不代行，LLM 仍是唯一执行者）
写操作守门：写工具 Preview 后、挂起确认前，Jev Noul 判定「本人合理请求 vs 注入/异常」→
          低于 JEV_GUARD_MIN 直接拦截（回复原因 + 审计 guard JSON + 指标 agent_jev_decisions_total）
          ——与「用户二次确认」双保险：确认防误操作，Jev 防注入
```

- `internal/jev/`：类型化客户端（兼容两种响应形态，容错解码）+ 假实现，单测覆盖解码/错误/fake；
  未配置 `JEV_ENDPOINT/JEV_API_KEY` 时全部静默停用，行为与旧版一致。

### M10 Kafka 计费事件流

- billing 记账方法把 bill 行 + outbox 行**同事务**提交；outbox relay 轮询投递到 `billing.events`，
  失败重试、超限改投 DLQ；event-consumer 幂等（`uk_event_id`）物化 `audit_events` 审计明细。
- `docker compose up -d kafka kafka-init`，`KAFKA_BROKERS` 空则不启用 relay。

### 可观测

- gateway（gin）与 router（net/http）共用一套 Prometheus 指标，`service` label 区分：
  `gateway_http_requests_total` / `..._request_duration_seconds`（histogram，出 P50/P95/P99）/ `..._in_flight_requests`
- `/metrics`（Prometheus 抓取）· `/breakers`（每个 provider 的熔断状态 JSON）
- 日志统一 slog，带 request_id 贯穿三服务

---

## 快速开始（Windows / Git Bash）

```bash
# 0) 基础设施（MySQL :3307 + Redis :6381，国内用 daocloud 镜像）
docker compose up -d mysql redis

# 1) 构建
GOPROXY=https://goproxy.cn,direct go build -o bin/ ./cmd/...

# 前端（P1 起）：先构建 web/ 产物（dist/ 是 go:embed 的部署物，须先于 go build 存在）
cd web && npm install && npm run build && cd ..

# 2) 启动（billing/router 先于 gateway；mock-provider 用环境变量配置）
BILLING_PORT=9101 BILLING_ADMIN_PORT=9103 ./bin/billing.exe &   # :9101 gRPC + :9103 admin
./bin/router.exe       &   # :9102
MOCK_PROVIDER_PORT=9201 MOCK_PROVIDER_NAME=mock-a MOCK_FAIL_RATE=0 ./bin/mock-provider.exe &
MOCK_PROVIDER_PORT=9202 MOCK_PROVIDER_NAME=mock-b MOCK_FAIL_RATE=0 ./bin/mock-provider.exe &
MOCK_PROVIDER_PORT=9203 MOCK_PROVIDER_NAME=mock-c MOCK_FAIL_RATE=0 ./bin/mock-provider.exe &
GATEWAY_PORT=18080 ./bin/gateway.exe &   # :18080（默认 8080 可能被占）
./bin/reconciler.exe -interval 30 -grace 600 &
# M7 AI 客服：真实 DeepSeek（v4-flash）。key 必填，否则 /portal/chat 返回明确错误
AGENT_LLM_API_KEY=sk-xxxxxxxx ./bin/agent.exe &   # :9105

# 3) 打开页面（admin token 默认 admin-demo，见 gateway 日志）
start http://localhost:18080/        # 管理面板 /（X-Admin-Token，无 AI 客服）
start http://localhost:18080/portal  # 用户自助页 /portal（Bearer 租户 key，含 AI 客服）

# 4) 造面板演示数据（4 租户 + 5000 条历史账单，Redis==账本一致）
go run ./scripts/seed -n 5000

# 5) 打一发
curl http://127.0.0.1:18080/v1/chat/completions \
  -H "Authorization: Bearer sk-demo-8f3a2b1c9d4e5f60" \
  -H "Content-Type: application/json" \
  -d '{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":false}'

# 6) 压测（自动重启 gateway 高限流 + 拉起对账并发跑 + 三断言）
bash scripts/loadtest/run.sh 200 60s     # 并发、时长可调
```

> 注：Git Bash 的 `kill` 杀不掉 Windows exe，一律 `taskkill //PID <pid> //F`。
> 压测教学（P50/P95/P99 怎么读、三条压测路径、六个坑）见 [06-loadtest.md](docs/interview/06-loadtest.md)。

---

## 验证结果（本机真实跑过）

| 测试 | 结果 |
|---|---|
| 单元测试（store/handler/router） | 全绿 |
| M1 并发不超卖 | 100 协程 Reserve，余额恰好归零、从不负、不足返 INSUFFICIENT |
| M2/M3 场景 | 正常扣费 / 幂等重复 409 / 余额不足 / SSE 按实际扣费 / 断连退款，全 PASS |
| M4 对账 | kill -9 网关打在 ReportUsage 前后，账单全收敛，Redis 余额 == 账本求和 |
| M5 熔断场景 9/9 | 注入失败验证 failover→OPEN→HALF-OPEN→CLOSED→AutoBan→SQL 解除→回归 |
| **压测 100 并发 × 30s** | **总请求 6359 · 212.0 req/s · 错误 0 · P50=469ms P95=715ms P99=801ms** |
| **压测 200 并发 × 60s** | **总请求 12515 · 208.6 req/s · 错误 0 · P50=938ms P95=1456ms P99=1718ms** |
| **压测 300 并发 × 60s** | **总请求 12795 · 213.2 req/s · 错误 0 · P50=1329ms P95=2064ms P99=2394ms** |
| Web 面板 | 4 租户余额/消耗 + provider 熔断卡 + 账单分页 + 「试一发」，全一致无 CDN |
| M7/M9 领域场景 | 订阅不入账（余额不变）/重复订阅被拒/充值支付/退款 min(单,余额)/gateway 真实调用走订阅额度/超额 402/窗口回拨刷新恢复/改套餐下期生效/退订当期不退，每步 Redis==账本 |
| M7/M9 单测 | store 退款边界 + 订阅额度 Reserve 全路径（额度内/超额/免费绕过/窗口恢复/MeView allowance）+ agent 运行时（假 LLM，无网络）全 PASS |
| P1 前端 | 新页面 / 与 /portal 经 go:embed 托管（断网离线），/portal/me、/v1 全链路（gateway→router→mock→billing）打通过 · 压测三断言回归 PASS |
| **P2 评测** | **离线 30/30 通过（2s）**：意图 8 / 确认 8 / 安全 7 / 纯净 4 / 额度 3 · report 留档 · go vet/test 全绿 |
| **P3 JEV** | 每轮前置判定（意图/合法性/风险三问）· 分层处置（拦截/警告/预判）· 上游契约修正（`/v1/systemone`）· 未配置静默降级 · 决策落审计 + prometheus |
| **M10 意图评测集** | **1220 条**（12 类 × 100 + 对抗 20）· 9 种说法维度 · 校验器全过 · `-sample 5` 冒烟约 10 分钟 / 全量约 1 小时 |
| 页面分离 | `/` 管理面板无 AI 客服；`/portal` 用户页用租户 key 看自己数据 + 聊天正常，错 key 被拒 |

**M10 真实修掉的问题**（都是加评测集后才暴露的）：

1. **销售趋势恒为 0** —— `SalesStats` 用 `DATE_FORMAT(created_at)` 按数据库服务器时区分桶，
   而补齐循环用 `from.Truncate(24*time.Hour)`（UTC 截断），键错开一天导致真实数据全部落到占位外。
   旧断言只看 `ByDay[0]`，恰好被零值占位符骗过。已修 + 加 `TestSalesStatsByDayUsesLocalDay` 锁住。
2. **JEV 上游契约全错** —— 端点是 `/v1/systemone` 不是 `/v1/chat/completions`，
   字段是 `instructions`/`criteria` 不是 `question`/`choices`。照自己熟悉的协议猜 = 全部 404。
3. **多层超时等值配置** —— agent 内部、网关透传、评测客户端三层都设 120~300s，
   等于随机谁先超时。已改为逐层放大（300s / 10min / 600s）。
4. **`anthropicToMessage` 把思考预算耗尽报成"空内容"** —— 带 thinking 的模型会先输出长推理，
   `max_tokens` 被吃光时 content 里连一个 text 块都没有。现在识别这种情况并自动加倍预算重试。
5. **OOS 口径 bug** —— 闲聊类的"命中"是零工具调用，但期望值是字面量 `chitchat`，
   直接比较会让这类恒判错。指标和报告都要归一化（报告曾把所有正确的闲聊列进错分表）。

压测三类一致性断言（不只报吞吐，200/300 并发下全 PASS）：

```
[PASS] 请求全部 200，无 402/429/5xx/连接错误
[PASS] Redis 余额 == 账本求和（300 并发下 initial - settled - pending == Redis，精确到 1）
[PASS] settle 恰好一次，无重复 delta
```

**压测揪出的真实 bug**（面试金矿，逐步加压每个档都炸出新的）：
1. 每请求 new `http.Client` → 连接风暴打爆 Windows 临时端口 → 共享 Transport 连接池 + 限 DB 连接池修复（M5）
2. RebuildAll 与在途 Reserve 竞态漏 pre 预占 → rebuild 只在流量安静时跑（M5）
3. `/report` 响应体没读完就 Close → 连接不回池 → 60s 8876 个 TIME_WAIT 打爆端口 → `io.Copy(io.Discard, resp.Body)` 修复（200 档逼出）
4. loadtest 余额断言没按租户过滤 → 多租户 seed 数据一进来就假 FAIL → `WHERE tenant_id=?` 修复
5. 300 并发冷启动第 0 秒 accept 队列溢出刷 refused → loadtest 加 2s 热身阶段

---

## 评测（两套，互补）

```bash
# ① 功能用例：30 条，脚本化假 LLM，离线确定性，2 秒跑完（改代码后必跑）
go run ./cmd/eval

# ② 意图路由：1220 条，打真实 agent，量化路由准确率与 JEV 收益
go run ./cmd/eval -suite intent -mode http \
  -base http://127.0.0.1:18080/portal -key sk-demo-8f3a2b1c9d4e5f60 \
  -sample 5 -workers 4        # 每类抽 5 条，约 10 分钟
```

| | 功能用例 `eval/cases/` | 意图路由 `eval/intent-suite/` |
|---|---|---|
| 模型 | 「用例通过/失败」 | 「分类指标」 |
| 测什么 | 确认状态机、tenant 隔离、审计留痕、参数校验 | 路由到哪个工具（Top-1 / Macro-F1 / OOS 误调用率） |
| 规模 | 30 条 × 5 维度 | 1220 条（12 类 × 100 + 对抗 20） |
| LLM | 脚本化（确定性、零成本） | 真实（花 token） |
| 用途 | 改代码后回归 | 量化 JEV 前后差异、定位说法维度短板 |

**为什么两套都要**：假 LLM 测的是「执行层的确定性逻辑」，真 LLM 测的是「意图 → 工具的准确率」。
只测假的测不真，只测真的测不稳。

数据集的调研结论与设计依据：`eval/intent-suite/DESIGN.md`
（含对 CLINC150 / Banking77 的可复用性评估 —— 结论是**只能借鉴方法论，语料必须自建**）。
操作手册：`eval/intent-suite/RUNBOOK.md`。

**JEV 前后对比**：

```powershell
$env:JEV_API_KEY=''; .\scripts\start-all.ps1 -Only agent -SkipInfra   # 关闭 JEV
go run ./cmd/eval -suite intent ... -out eval/reports/baseline-no-jev
.\scripts\start-all.ps1 -Only agent -SkipInfra                        # 恢复（读 .env）
go run ./cmd/eval -suite intent ... -out eval/reports/full-with-jev
.\scripts\compare-intent.ps1 -Baseline <基线报告.md> -Experiment <实验报告.md>
```

---

## 目录结构

```
cmd/{gateway,billing,router,mock-provider,reconciler,agent,event-consumer,eval}/main.go   服务与工具入口
internal/
  proto/billing/v1/billing.proto     计费 gRPC 契约
  gateway/{handler,middleware,client} 网关编排与三中间件
  gateway/handler/portal.go          /portal 用户自助组（key 派生 tenant + 转发）
  gateway/web/dist                   前端构建产物（Vite，go:embed all:dist 托管）
  billing/{server,store,lua,reconciler,consumer,outbox} 计费服务 + 对账 + M10 事件流
  billing/store/plan.go              M7/M9 领域：套餐（月费+每窗口额度）/订阅/充值/topups/会话/审计
  billing/admin                      计费观测 HTTP 端点（:9103，面板与 agent 数据源）
  agent/{llm,llm_protocol,tools,chat,prompt,guard} AI 客服运行时（意图路由/工具/确认状态机/审计/JEV 分层守门）
  agent/intentsuite.go               意图路由评测：加载 1220 条语料 + 分类指标计算（Top-1/Macro-F1/OOS）
  jev/                               JEV 判定模型类型化客户端（System One，+ 假实现供离线测试）
  router/{registry,circuit}          权重路由 + 熔断状态机
  pkg/{config,logger,metrics,redisx,tokens,trace,xid,quota,kafka}    公共
web/                                 独立前端工程（Vite+Vue3+TS）：双入口 portal/dashboard
eval/cases|reports                    P2 客服评测用例（30 条 × 5 维度）与报告归档
eval/intent-suite/                    M10 意图路由评测集（1220 条）+ DESIGN.md / RUNBOOK.md
scripts/{schema.sql, m1~m7, seed, loadtest, channel}  DDL + 场景脚本 + 造数据 + 压测
scripts/start-all.ps1                 Windows 一键启动（bash 走 WSL 不可用时的等价实现）
scripts/compare-intent.ps1            JEV 前后指标对比
web/scripts/{gen,verify}-intent-suite.mjs  评测集生成与校验
web/scripts/survey-datasets.mjs       公开数据集可复用性调研
docs/interview/                       面试 / 学习文档
docs/api-reference.md                 前端对接 API 契约（portal/admin/agent/计费 全量）
```

## 面试文档索引

- [00-project-story.md](docs/interview/00-project-story.md) —— 30 秒项目故事 + new-api 三缺陷对比
- [go-primer.md](docs/interview/go-primer.md) —— 本项目用到的 Go 语法/并发原语速览
- [distributed-concepts.md](docs/interview/distributed-concepts.md) —— 分布式概念白话版，每个都标本项目落点
- [01-m1.md](docs/interview/01-m1.md) ~ [05-m5.md](docs/interview/05-m5.md) —— 每里程碑：概念层→代码导读→面试 Q&A→真实验证结果→口述抽查
- [06-loadtest.md](docs/interview/06-loadtest.md) —— 压测教学：三条压测路径、P50/P95/P99 怎么读、六个坑、50→300 逐步加压实测
- [07-m7.md](docs/interview/07-m7.md) —— M7/M9 AI 智能客服 + GPT Plus 式订阅：agentic 自服务、执行层不信任 LLM、话费式退款口径、二次确认状态机、审计可追责、订阅=每窗口 token 额度
- [08-eval.md](docs/interview/08-eval.md) —— P2 AI 客服评测集：30 用例 × 5 维度、离/在线双模式、纵深断言、迭代闭环
- [09-jev.md](docs/interview/09-jev.md) —— P3 JEV 判断引擎：System One 给 System Two 上保险、意图预判 + 写操作守门、置信度阈值与审计
- [10-intent-eval-and-jev.md](docs/interview/10-intent-eval-and-jev.md) —— M10 意图路由评测集（1220 条怎么建、为什么 CLINC150/Banking77 不能用）+ JEV 分层接入 + JEV 前后指标对比 + 并发超时踩坑

## 里程碑

| # | 内容 | 验收 |
|---|---|---|
| M1 | 骨架 + 原子预占（Reserve Lua + 微 Saga 补偿） | 并发 100 余额恰好归零从不负 |
| M2 | 非流式全链路（鉴权/幂等/限流 + 编排 + router + mock） | curl 一发产生 reserve+settle 两行 |
| M3 | SSE 流式（透传 + Estimator + 断连退款） | 按实际扣费，kill 客户端保持 pending |
| M4 | 对账收敛（三分类 + 幂等冲正/补结算 + 重建投影） | kill -9 后账单全收敛 |
| M5 | 路由熔断 + AutoBan + Prometheus + 压测 | 熔断全状态机验证 + P99 压测报告 |
| M6 | Web 面板 + 造数据 + 压测教学（本仓库新增） | 面板可看数据 · seed 造 4 租户千条账单 · 200/300 并发三断言 PASS |
| M7 | AI 智能客服（agentic 自服务）+ 充值/订阅（话费式计费） | 假 LLM 单测 5 + 退款边界 7 · m7 场景 7 节 · 页面分离 · 确认状态机 · 审计落库 |
| M9 | 订阅改 GPT Plus 式（月费 + 每窗口各档 token 额度，滚动刷新） + AI 客服纯自然语言输出 | store 订阅额度全路径单测 + m7 场景：订阅不入账/额度内免费/超额 402/窗口恢复 · 无 ⚙/工具名 |
| P1 | 前端重做：独立 web/ 工程（Vite+Vue3+TS）· /portal 商城化（商品卡/收银台/套餐/订单/消费/悬浮客服）· /dashboard 管理后台（深色侧栏 10 功能区） | 页面对齐全部 /portal、/admin API · go:embed 托管离线可用 · 压测三断言回归 PASS |
| P2 | AI 客服评测集：eval/cases 30 用例 × 5 维度（意图/确认/安全/纯净/额度）+ 离在线 runner + 报告 | 离线假 LLM 回归 **30/30 全绿** · go vet/test 全绿 |
| P3 | JEV 判断引擎：internal/jev 类型化客户端(fake 单测) + 客服意图预判 + 写操作守门（置信度阈值拦截 + 审计 guard + 指标） | 假 Jev 守门/注入/放行/回退单测全绿 · 未配置静默停用无回归（评测 30/30 不变） |

## 已知边界（面试可主动讲）

- 吞吐上限 ~210 req/s 由**端到端全链路延迟**决定（每请求 4 次 gRPC + Lua 原子扣减 + MySQL
  落库 + 上游往返）：并发 100→300，吞吐几乎不变而 P50 469ms→1329ms 翻三倍。把 billing
  MySQL 池 50→100 实测吞吐不变（瓶颈不在池），扩展方向：billing 按租户分片 / 批量 settle
  合并写。
- 熔断为单实例内存态，多实例需共享判定（可落到 Redis / 依赖 AutoBan 的持久层兜底）。
- mock-provider 为本地故障注入假 upstream，不接真实 LLM。
- demo 租户余额会被压测消耗（60s 烧 ~7-10M），压测前按 [06-loadtest.md](docs/interview/06-loadtest.md) §5.4 充值。
- **AI 客服依赖真实 DeepSeek**：`AGENT_LLM_API_KEY` 未填时 `/portal/chat` 明确报错（无 mock 模式，
  离线验证靠假 LLM 单测 + m7 场景脚本）。单测对 LLM 的调用面做了接口化，脚本化 tool_call 注入。
- **余额是单池**：退款 `min(单,余额)` 可能吃到别的充值额度，不做按订单资金锁定。
- **M9 订阅额度按「滚动窗口」近似而非严格计数**：消费按 bills 的 reserve 行 created_at 滑动统计，
  窗口滑过即恢复（未用不累积）；reserve→report 间隙不占额度，允许小概率并发超卖（demo 可接受）。
  超限 = 拒绝等待刷新，不做 GPT 的降级到 mini 模型。月费为模拟价，不接真支付。
- **会话热态在 Redis（TTL 30min）、持久记录在 MySQL**：跨 30 分钟的旧会话会重建，但每笔操作都
  有 agent_audit_log 可追责。
