# API 参考（前端对接）

> 依据源码逐条抄录（gateway /portal、/admin 代理 + billing admin :9103 + router :9102 + agent :9105 + reconciler :9106）。
> 所有 HTTP 响应均 `Content-Type: application/json`（`/v1/chat/completions` stream 例外，为 SSE）。
> 金额单位：**分**（=`元 × 100`）为主；充值订单同时带 `amount_money`（元）。模型价格单位为 **分 / 百万 token**。
> 服务端口：gateway=8080、billing admin=9103、router=9102、agent=9105、reconciler=9106。
> 网关对上游的超时：admin 聚合 `adminHTTP` = 5s；`/portal/chat` 专用 `chatHTTP` = 120s（DeepSeek 多轮 tool loop）。

---

## 鉴权约定

### 1. Portal（用户自助，`/portal/*` 与 `/v1/*`）

走 `middleware.APIKey`，三种取 key 方式（优先级从高到低）：

| 方式 | 格式 |
|---|---|
| `Authorization` 头 | `Authorization: Bearer <租户 Key>`（推荐） |
| `api-key` 头 | `api-key: <租户 Key>` |
| `x-api-key` 头 | `x-api-key: <租户 Key>` |

- gateway 用 SHA-256 哈希 key 后调 billing `ValidateAPIKey` 校验；**tenant_id / api_key_id 一律由 key 派生，前端/body 传入的 tenant_id 会被网关强制覆盖**（写操作 `portalPOST` 强制 `m["tenant_id"] = key 派生值`）。
- 失败响应（`middleware.APIKey.abort`，OpenAI 风格，前端判 `error.message`）：
  - `401 {"error": {"message": "missing api key" | "invalid api key", "type": "authentication_error"}}`
- 限流：`/v1/*` 额外有 Idempotency + RateLimit（默认 600 req/min/租户）；`/portal` 仅 APIKey。

### 2. Admin 面板（`/admin/*`，除 `/admin/auth/*` 外）

走 `middleware.AdminSession`，取会话 token 的优先级：

1. Cookie `agw_admin`（登录后浏览器持有的 **HttpOnly** 会话 cookie，`Path=/`，`SameSite=Lax`）；
2. 请求头 `X-Admin-Token`（脚本/curl 用：把登录拿到的 token 放这个头）；
3. 环境变量 `ADMIN_TOKEN` 兜底（**默认值 `admin-demo`**，本机演示用；正式环境置空即关闭）。

有 token 时 gateway 转发到 billing `/admin/auth/me` 校验，失败返回：
- `401 {"error": "unauthorized"}`（无 token）
- `401 {"error": "session invalid"}`（token 无效/过期）

会话有效期：**12 小时**（billing `sessionTTL`）。登录/登出 cookie 由 billing 签发，经 gateway proxy `Set-Cookie` 透传给浏览器。

### 3. 其他

- Agent 聊天 `Authorization: Bearer <租户 Key>`（agent 自己校验，不再走 gateway 中间件）；`/portal/chat` 把请求的 `Authorization` 原样透传给 agent。
- `/admin/auth/*` 不需要登录态（登录前即可访问），但 gateway 仍会把 cookie（`agw_admin`）与 `X-Admin-Token` 透传给上游。

---

## Portal 端点（相对 `/portal`，经 gateway 代理，需租户 Key）

> 只读端点透传 `billingAdminBase = http://127.0.0.1:9103`（billing 观测/自服务端点）。写操作 POST body 中 `tenant_id` 一律被强制改写为 key 派生值。

### GET /portal/me

- 透传目标：`GET {billing}/admin/me?tenant_id=<key派生>`（billing `MeOverview`）。
- Query：无（tenant_id 网关自动带）。
- 响应 `200`：**MeView**（字段见下，金额单位分）：

```json
{
  "balance": 1000000,            // int64 实时可用（Redis 投影，分）
  "balance_cent": 1000000,       // int64 同上，显式声明单位为分（展示端 /100 显示元）
  "ledger_balance": 1000000,     // int64 账本口径（权威，分）
  "consistent": true,            // bool  两者是否一致
  "subscription": { ... } | null,   // 生效中的定期订阅（可空），见 Subscription
  "allowance": [ ... ],          // []TierRemain 订阅各档次每窗口剩余额度（token，活跃订阅才非空，omitempty）
  "refresh_hours": 5,            // int   额度滚动窗口小时数（活跃订阅套餐的；展示「每 N 小时刷新」，omitempty）
  "plans": [ ... ],              // []Plan 套餐目录
  "consumption": [ ... ],        // []DayStat 近 7 天消耗（分/日）
  "consumption_by_model": [ ... ], // []ModelStat 近 7 天按模型消耗
  "topups": [ ... ]              // []Topup 最近入账流水（20 条）
}
```

### GET /portal/plans

- 透传目标：`GET {billing}/admin/plans`（billing `ListPlans`，全量含下架，`ORDER BY id ASC`）。
- 响应 `200`：`{"items": [Plan, ...]}`。

### GET /portal/topups

- 透传目标：`GET {billing}/admin/topups?tenant_id=<key派生>`。
- 响应 `200`：`{"items": [Topup, ...]}`（固定 50 条，倒序）。

### GET /portal/recharge-orders

- 透传目标：`GET {billing}/admin/recharge-orders?tenant_id=<key派生>`。
- 响应 `200`：`{"items": [RechargeOrder, ...]}`（固定 50 条，倒序）。

### GET /portal/models

- 透传目标：`GET {billing}/portal/models?<raw query 透传>`（billing 侧只列**上架** status=0 的 SKU）。
- Query：`vendor`（可选，按厂商过滤）。
- 响应 `200`：`{"items": [Model, ...]}`（买家目录，只含启用 SKU）。

### GET /portal/consumption

- 透传目标：`GET {billing}/admin/consumption?tenant_id=<key派生>&<raw query 透传>`。
- Query：`days`（可选，整数；默认/非法 → 7）。
- 响应 `200`：`{"items": [DayStat, ...]}`（每日消耗，缺失日补 0）。

### POST /portal/recharge（建充值订单，不收款）

- 透传目标：`POST {billing}/admin/recharge`。
- Body（`tenant_id` 会被网关强制覆写）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `amount_money` | int64 | 充值金额（**模拟元**，整数收，>0） |
| `idem_key` | string 可选 | 幂等锚点；非空时直接作为 `order_no`，重复提交返回已有订单 |

- 响应 `200`：**RechargeOrder**（status=`pending_payment`，`amount_cent = amount_money × 100`）。
- 错误：`400 {"error": "..."}`（金额 ≤ 0、重复等，由 billing 文案透传）。

### POST /portal/recharge/pay（模拟支付：pending_payment → paid + 入账）

- 透传目标：`POST {billing}/admin/recharge/pay`。
- Body：

| 字段 | 类型 | 说明 |
|---|---|---|
| `order_no` | string | 要支付的充值订单号 |

- 响应 `200`：**RechargeOrder**（status=`paid`，含 `paid_at`）。幂等：已 paid 直接返回原单；非 pending 状态返回 400。
- 页面「充值」= 建单 + 支付两步，与 AI 客服等价。

### POST /portal/recharge/refund（退款）

- 透传目标：`POST {billing}/admin/recharge/refund`。两种方式二选一：

| 字段 | 类型 | 说明 |
|---|---|---|
| `tenant_id` | uint64 | 被强制覆写为 key 派生值 |
| `order_no` | string | 二选一：按订单退（min(订单额度, 余额)） |
| `amount_money` | int64 | 二选一：按金额退（元，min(金额, 余额)） |
| `idem_key` | string 可选 | 按金额退的幂等锚点（可空，服务端自生成） |

- 响应 `200`：**RefundResult**：`{"order_no": string, "amount_cent": int64(订单原金额/按金额退的目标金额,分), "refund_cent": int64(实际退回,分), "balance_after": int64(退款后余额,分)}`。
- 错误：`400 {"error": "请提供 order_no（按订单退）或 amount_money（按金额退）"}` 等。

### POST /portal/subscribe（订阅套餐，M9 起 GPT Plus 式：开通额度资格，不扣钱）

- 透传目标：`POST {billing}/admin/subscribe`。
- Body：`{"tenant_id"(强制覆写): uint64, "plan_id": uint64}`。
- 响应 `200`：**Subscription**（status=`active`，`auto_renew=true`，`cycle_start`/`cycle_end` 由套餐 `ValidityDays` 推出，`quota_granted=0`）。
- 错误：`400 {"error": "..."}`（套餐不存在/下架/仅定期订阅在售/已有生效订阅等）。

### POST /portal/change-plan（改套餐，下期生效）

- 透传目标：`POST {billing}/admin/change-plan`。
- Body：`{"tenant_id"(强制覆写): uint64, "plan_id": uint64}`。
- 响应 `200`：`{"ok": true}`。
- 错误：`400 {"error": "..."}`（无生效订阅/一次性套餐不支持/目标套餐无效等）。

### POST /portal/cancel-subscription（退订，当期不退只停续费）

- 透传目标：`POST {billing}/admin/cancel-subscription`。
- Body：`{"tenant_id"(强制覆写): uint64}`。
- 响应 `200`：**CancelResult**：`{"plan_name": string, "refund": int64(恒0), "current_cycle_end": string|null(RFC3339 时间戳)}`。
- 错误：`400 {"error": "..."}`（一次性套餐不可退订 / 无生效订阅等）。

### 错误码约定（Portal 通用）

- 鉴权失败（gateway 中间件）：`401 {"error":{"message":"missing api key"|"invalid api key","type":"authentication_error"}}`。
- body 解析失败：`400 {"error": "read body failed" | "bad json body"}`（gateway 层）。
- 上游不可达：`502 {"error": "upstream unreachable"}`（gateway proxy 层）。
- 业务校验失败：`400 {"error": "<billing 文案>"}`（billing 透传原样）。
- `/portal/chat` 特例：构建失败 `500`、agent 不可达 `502 {"error":"agent 不可达"}`，其余状态码与 body 原样透传 agent 的响应。

---

## Admin 端点（相对 `/admin`，经 gateway 代理，需登录态）

> `/admin/auth/*` 无需登录态。`/admin/*` 需 `middleware.AdminSession`（cookie / X-Admin-Token / admin-demo 兜底）。
> gateway 会把 `Set-Cookie`（登录/登出）与 `agw_admin` cookie、`X-Admin-Token` 头透传上游。
> 上游：billing=`http://127.0.0.1:9103`、router=`http://127.0.0.1:9102`、reconciler=`http://127.0.0.1:9106`、agent=`http://127.0.0.1:9105`。

### GET /admin/auth/initialized（无需登录）

- 透传目标：`GET {billing}/admin/auth/initialized`。
- 响应 `200`：`{"initialized": bool}`（系统是否已有管理员，首屏判断走 setup 还是 login）。

### POST /admin/auth/setup（首次初始化管理员；仅当尚无任何管理员时可用）

- 透传目标：`POST {billing}/admin/auth/setup`。Body：`{"username": string, "password": string}`（`len(password) >= 6`）。
- 响应 `200`：`{"username": "..."}` + `Set-Cookie: agw_admin=<token>; Path=/; HttpOnly; SameSite=Lax; Max-Age=43200`。
- 错误：`400 {"error":"用户名必填，密码至少 6 位"}`；`409 {"error":"already initialized"}`。

### POST /admin/auth/login

- 透传目标：`POST {billing}/admin/auth/login`。Body：`{"username": string, "password": string}`。
- 响应 `200`：`{"username": "..."}` + 同上 `Set-Cookie: agw_admin=...`。
- 错误：`401 {"error":"用户名或密码错误"}`（用户不存在/禁用/密码错统一文案，不泄露原因）。

### POST /admin/auth/logout

- 透传目标：`POST {billing}/admin/auth/logout`。Body：可空。
- 响应 `200`：`{"ok": true}` + `Set-Cookie: agw_admin=; Path=/; HttpOnly; Max-Age=-1`（立即过期）。

### GET /admin/auth/me（会话校验）

- 透传目标：`GET {billing}/admin/auth/me`。token 取 cookie `agw_admin` 或 `X-Admin-Token` 头。
- 响应 `200`：`{"username": string}`；`401 {"error":"unauthorized"}`。

### GET /admin/overview

- 透传目标：`GET {billing}/admin/overview`（billing `OverviewStats`，全租户账本/投影 + 全局汇总）。

```json
{
  "tenants": [ {
    "id": 1, "name": "t1",
    "initial_quota": 1000000,      // int64 初始额度（分）
    "balance": 950000,             // int64 账本口径余额（分）
    "redis_balance": 950000,       // int64 Redis 投影（live，分）
    "consistent": true,            // bool 两者一致（对账收敛观测点）
    "topups": 100000, "settled": 50000, "pending": 0,   // int64 分
    "bill_count": 12, "today_bills": 3, "today_settled": 1000  // int64
  } ],
  "totals": { "settled": int64, "pending": int64, "bills": int64,
              "today_bills": int64, "today_settled": int64 }
}
```

### GET /admin/bills（账单分页，默认 phase=reserve）

- 透传目标：`GET {billing}/admin/bills?<raw query 透传>`。
- Query：`tenant_id`（0=全部）、`phase`（空→`reserve`；`settle` 查结算行）、`status`（可选）、`limit`（默认 50，max 200）、`offset`（默认 0）。
- 响应 `200`：`{"total": int64, "items": [Bill, ...]}`（`ORDER BY id DESC`）。

**Bill**（无 json tag，wire 键 = Go 字段名）：

| wire 字段 | 类型 | 说明 |
|---|---|---|
| `ID` | uint64 | |
| `RequestID` | string | 请求幂等键（uk: request+phase） |
| `Phase` | string | `reserve` / `settle` |
| `Status` | string | `pending` / `settled` / `reversed` |
| `TenantID` | uint64 | |
| `APIKeyID` | uint64 | |
| `ProviderID` | *uint64 | 可空 |
| `Model` | string | 调用名 == 目录 model_id |
| `Funding` | string | `balance` / `subscription`（M9） |
| `PreQuota` | int64 | 预扣金额（分） |
| `ActualQuota` | *int64 | settle 实际金额（分），可空 |
| `DeltaQuota` | *int64 | actual − pre（settle 行），可空 |
| `PromptTokens` | *int64 | 可空 |
| `CompletionTokens` | *int64 | 可空 |
| `InPriceCent` | *int64 | 价格快照：输入 分/百万 token，可空 |
| `OutPriceCent` | *int64 | 价格快照：输出 分/百万 token，可空 |
| `UsageReportedAt` | *time.Time | 网关流终态上报的持久 marker，可空 |
| `ErrorCode` | string | |
| `CreatedAt` | time.Time | RFC3339 |

### GET /admin/subscription（M7 只读观测，跨租户管理视角）

- 透传目标：`GET {billing}/admin/subscription?<raw query 透传>`（query 带 `tenant_id`）。
- 响应 `200`：
  - 无订阅：`{"subscription": null, "allowance": null}`
  - 有活跃订阅：`{"subscription": <Subscription>, "allowance": [TierRemain,...]|null}`（allowance 仅定期订阅且套餐启用且配置过 tier_quota 才非空）。

### GET /admin/topups

- 透传目标：`GET {billing}/admin/topups?tenant_id=`（0=全部）。
- 响应 `200`：`{"items": [Topup, ...]}`（50 条倒序）。

### GET /admin/recharge-orders

- 透传目标：`GET {billing}/admin/recharge-orders?tenant_id=`（0=全部租户）。
- 响应 `200`：`{"items": [RechargeOrder, ...]}`（50 条倒序，tenant_id>0 才过滤）。

### GET /admin/providers（router 熔断视角）

- 透传目标：`GET {router}/providers`（router `Providers()`，直查表 + 内存熔断状态合并）。
- 响应 `200`：**数组** `[ProviderState, ...]`（注意：不是 `{items}` 包装）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uint64 | |
| `name` | string | |
| `base_url` | string | |
| `models` | string | JSON 数组串，如 `["deepseek-chat"]` 或 `["*"]` |
| `weight` | int | |
| `status` | int8 | 0=active 1=disabled 2=banned |
| `consecutive_fail` | int | 连续失败数（内存最新） |
| `breaker_state` | string | `closed` / `open` / `half_open` |
| `next_closed_at` | string | RFC3339，仅 open 时有（omitempty） |

### GET /admin/channels（渠道列表，含毛利——billing 视角）

- 透传目标：`GET {billing}/admin/providers`（billing `ListProvidersWithMargin`，`upstream_key` 已被脱敏为 `"sk-***"`）。
- 响应 `200`：`{"items": [ProviderMargin, ...]}`。

**ProviderMargin**（内嵌 Provider + 毛利三字段）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uint64 | |
| `name` | string | |
| `base_url` | string | |
| `upstream_key` | string | 响应一律 `"sk-***"`（maskKey 脱敏，不回显明文） |
| `cost_in_cent` | int64 | 上游成本：输入 分/百万 token |
| `cost_out_cent` | int64 | 上游成本：输出 分/百万 token |
| `models` | string | JSON 数组串 |
| `weight` | int | |
| `status` | int8 | 0=active 1=disabled 2=banned |
| `fail_count` | int | |
| `consecutive_fail` | int | |
| `cooldown_until` | *time.Time | 可空（omitempty） |
| `created_at` / `updated_at` | time.Time | |
| `selling_cent` | int64 | 累计售价（分，Σactual_quota） |
| `cost_cent` | int64 | 累计上游成本（分） |
| `margin_cent` | int64 | 毛利（分）= 售价 − 成本 |

### POST /admin/channels（渠道 upsert，按 name 幂等）

- 透传目标：`POST {billing}/admin/providers`。Body：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `name` | string | 是 | 渠道名（重复名更新转发参数） |
| `base_url` | string | 是 | 上游 base URL |
| `upstream_key` | string | 否 | 上游 API key |
| `models` | string | 否 | JSON 数组串，如 `["deepseek-chat"]` 或 `["*"]` |
| `weight` | int | 否 | 权重 |
| `status` | int8 | 否 | 0=active 1=disabled 2=banned |
| `cost_in_cent` | int64 | 否 | 上游成本：输入 分/百万 token |
| `cost_out_cent` | int64 | 否 | 上游成本：输出 分/百万 token |

- 响应 `200`：**Provider**（同 ProviderMargin 的 Provider 部分，key 脱敏 `"sk-***"`，含真实 `id`）。
- 错误：`400 {"error":"name and base_url required"}`。

### GET /admin/models（商品目录，卖方全量）

- 透传目标：`GET {billing}/admin/models?<raw query 透传>`。
- Query：`vendor`（可选）、`status`（可选，0=上架 1=下架）。
- 响应 `200`：`{"items": [Model, ...]}`（`ORDER BY vendor ASC, id ASC`）。

**Model**（json tags 齐全）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uint64 | |
| `vendor` | string | 厂商：DeepSeek/通义千问/... |
| `model_id` | string | 调用名（= `/v1/chat/completions` 的 model 参数），计费键，全站唯一 |
| `name` | string | 展示名 |
| `input_price_cent` | int64 | 输入价 分/百万 token |
| `output_price_cent` | int64 | 输出价 分/百万 token |
| `context_len` | int | 上下文窗口（token） |
| `tags` | string | JSON 数组串：`["文本","推理","旗舰"]` |
| `status` | int8 | 0=上架 1=下架 |
| `created_at` / `updated_at` | time.Time | RFC3339 |

### POST /admin/models（新增 SKU）

- 透传目标：`POST {billing}/admin/models`。Body：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `vendor` | string | 是 | |
| `model_id` | string | 是 | |
| `name` | string | 否 | |
| `input_price_cent` | int64 | 否 | 分/百万 token |
| `output_price_cent` | int64 | 否 | 分/百万 token |
| `context_len` | int | 否 | |
| `tags` | []string | 否 | 如 `["文本","推理"]`（网关 marshal 成 JSON 串存 `tags`） |

- 响应 `200`：**Model**。错误：`400 {"error":"model_id 与 vendor 必填"}` 或重复 model_id 等。

### POST /admin/models/status（启停 SKU）

- 透传目标：`POST {billing}/admin/models/status`。Body：`{"id": uint64, "status": int8 /* 0=上架 1=下架 */}`。
- 响应 `200`：`{"ok": true}`。

### POST /admin/models/price（改价）

- 透传目标：`POST {billing}/admin/models/price`。Body：`{"id": uint64, "input_price_cent": int64, "output_price_cent": int64}`（均 ≥ 0，否则 `400 {"error":"价格不能为负"}`）。
- 响应 `200`：`{"ok": true}`。

### GET /admin/sales（销售统计）

- 透传目标：`GET {billing}/admin/sales?<raw query 透传>`。
- Query：`from`、`to`（可选，`YYYY-MM-DD`，本地时区；缺省/非法 → 近 30 天；`to` 含当天，自动 +23:59:59）。
- 响应 `200`：**SalesStats**：

```json
{
  "summary":   { "topup_cent": int64, "consumed_cent": int64, "settled_bills": int64, "active_tenants": int64 },
  "by_model":  [ { "model_id": string, "name": string, "vendor": string, "consumed_cent": int64,
                   "calls": int64, "input_tokens": int64, "output_tokens": int64, "active_tenants": int64 } ],
  "by_vendor": [ { "vendor": string, "consumed_cent": int64, "calls": int64, "active_tenants": int64 } ],
  "by_day":    [ { "day": "YYYY-MM-DD", "consumed_cent": int64, "topup_cent": int64 } ]
}
```

> 口径：`consumed_cent` = settle 行 `actual_quota` 求和（分）；`topup_cent` = topups 正项。

### GET /admin/audit（AI 客服审计轨迹）

- 透传目标：`GET {billing}/admin/audit?<raw query 透传>`。
- Query：`tenant_id`（0=全部）、`limit`（默认 50，max 200）。
- 响应 `200`：`{"items": [AgentAuditLog, ...]}`（倒序）。

**AgentAuditLog**（无 json tag，wire 键 = Go 字段名，表名 `agent_audit_log`）：

| wire 字段 | 类型 | 说明 |
|---|---|---|
| `ID` | uint64 | |
| `SessionID` | string | |
| `TenantID` | uint64 | |
| `Tool` | string | 工具名（get_balance / recharge / ...） |
| `Args` | string | 参数 JSON 串 |
| `Confirmed` | bool | 写操作是否经过用户确认 |
| `Preview` | string | 确认时展示的预览文案 |
| `Result` | string | 执行结果 JSON/错误 |
| `CreatedAt` | time.Time | |

### GET /admin/reconcile-check（账本 vs 投影一致性）

- 透传目标：`GET {billing}/admin/reconcile-check`（基于 `OverviewStats` 过滤 inconsistent 租户）。
- 响应 `200`：

```json
{
  "checked": 3,          // int 检查的租户数
  "inconsistent": [ { "id": 2, "name": "t2", "ledger_cent": 9000, "redis_cent": 8000 } ],
  "ok": true             // bool 是否全部一致
}
```

### GET /admin/me

- 透传目标：`GET {billing}/admin/me?tenant_id=<key派生>`。响应同 **Portal GET /portal/me**（MeView）。

### GET /admin/consumption

- 透传目标：`GET {billing}/admin/consumption?tenant_id=&<raw query 透传>`。Query：`days`（默认 7）。
- 响应 `200`：`{"items": [DayStat, ...]}`。

### GET /admin/health（服务健康聚合）

- gateway 本地聚合（不转发单点），并发 GET 以下 4 个端点的 `/healthz`：
  - billing → `{billing}/healthz`；router → `{router}/healthz`；reconciler → `{reconciler}/healthz`；agent → `{agent}/healthz`
- 响应 `200`：

```json
{
  "ok": false,
  "services": {
    "billing":    { "ok": true },
    "router":     { "ok": true },
    "reconciler": { "ok": false, "error": "unreachable" },
    "agent":      { "ok": true }
  }
}
```

### 错误码约定（Admin 通用）

- 上游不可达：`502 {"error":"upstream unreachable"}`。
- 上游构建失败：`500 {"error":"bad upstream url" | "build upstream request failed"}`。
- billing 业务校验：`400 {"error":"<文案>"}` 透传。
- `/admin/auth/*` 无登录中间件；其余 `/admin/*` 未登录：`401 {"error":"unauthorized"|"session invalid"}`。

---

## 响应共用结构（billing store，逐字段抄录源码）

> 以下结构多端点复用；`Plan/Subscription/Topup/RechargeOrder` 无 json tag，**wire 键 = Go 字段名**（首字母大写）。

### Plan（billing/store/plan.go）
| Go 字段（wire 键） | 类型 | 说明 |
|---|---|---|
| `ID` | uint64 | |
| `Name` | string | 套餐名（唯一） |
| `PlanType` | string | `recurring`（目录只种定期订阅；`one_time` 保留代码不再售出） |
| `PriceMoney` | int64 | 月费（**元**，模拟价） |
| `ValidityDays` | int | 计费周期天数（续费/改套餐周期） |
| `RefreshHours` | int | 额度滚动窗口小时数（每 N 小时刷新，未用不累积；默认 5） |
| `TierQuota` | string | 每窗口每档次 token 额度，JSON 串 `{"轻量":2000000,...}` |
| `Status` | int8 | 0=active 1=disabled |
| `CreatedAt` / `UpdatedAt` | time.Time | |

### Subscription（billing/store/plan.go）
| Go 字段（wire 键） | 类型 | 说明 |
|---|---|---|
| `ID` | uint64 | |
| `TenantID` | uint64 | |
| `PlanID` | uint64 | |
| `PlanType` | string | |
| `Status` | string | `active` / `cancelled` / `expired` |
| `CycleStart` | *time.Time | recurring 本期起；one_time 为 null |
| `CycleEnd` | *time.Time | 本期止 |
| `AutoRenew` | bool | |
| `CycleNum` | int | 已续期数 |
| `QuotaGranted` | int64 | 累计授予额度（M9 起为 0，额度资格制） |
| `PendingPlanID` | *uint64 | 改套餐：下期生效 |
| `CancelledAt` | *time.Time | |
| `CreatedAt` / `UpdatedAt` | time.Time | |

### TierRemain（json tags）
| wire 字段 | 类型 | 说明 |
|---|---|---|
| `tier` | string | 档次名（旗舰>推理>视觉>长文本>文本>轻量>免费） |
| `quota` | int64 | 窗口额度（token） |
| `consumed` | int64 | 窗口已用（token） |
| `remaining` | int64 | 剩余（token，max(0, quota−consumed)） |

### RechargeOrder（billing/store/plan.go，status 枚举 = 充值状态）
| Go 字段（wire 键） | 类型 | 说明 |
|---|---|---|
| `ID` | uint64 | |
| `TenantID` | uint64 | |
| `OrderNo` | string | 订单号，唯一 = 幂等锚点 |
| `AmountMoney` | int64 | wire 层金额（**元**，整数收） |
| `AmountCent` | int64 | 入账金额（分）= AmountMoney × 100 |
| `Status` | string | `pending_payment` / `paid` / `refunded` / `cancelled` |
| `PaidAt` | *time.Time | |
| `CreatedAt` / `UpdatedAt` | time.Time | |

### Topup（billing/store/plan.go，正余额权威账本）
| Go 字段（wire 键） | 类型 | 说明 |
|---|---|---|
| `ID` | uint64 | |
| `TenantID` | uint64 | |
| `Source` | string | `recharge` / `subscription` / `refund` |
| `Amount` | int64 | 正=入账，负=退款（分） |
| `RefType` | string | 关联类型（幂等键第 1 部分） |
| `RefID` | string | 关联 ID（幂等键第 2 部分） |
| `CreatedAt` | time.Time | |

### DayStat / ModelStat（json tags）
| 结构 | wire 字段 | 类型 | 说明 |
|---|---|---|---|
| DayStat | `day` | string | `YYYY-MM-DD` |
| DayStat | `tokens` | int64 | 当日消耗（**分**，源码注释如此） |
| ModelStat | `model` | string | 目录 model_id |
| ModelStat | `vendor` | string | 厂商（JOIN models，omitempty） |
| ModelStat | `input_tokens` | int64 | prompt token |
| ModelStat | `output_tokens` | int64 | completion token |
| ModelStat | `cost_cent` | int64 | 实际消耗金额（分） |
| ModelStat | `calls` | int64 | 调用次数 |

---

## Agent 聊天（POST /portal/chat）

- 透传目标：`POST {agent}/chat`，body + `Authorization` 头原样透传（agent 自己 `ValidateAPIKey` 绑定 tenant）。
- 鉴权：`Authorization: Bearer <租户 Key>`，或 `api-key` 头（agent `extractKey`）。
- 超时：gateway 专用 client **120s**（agent 内部走真实 DeepSeek 多轮 tool loop，5s adminHTTP 会误判不可达）。

### 请求 Body（≤64KB）
| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session_id` | string | 否 | 已有会话带回；缺省时服务端生成 `sid_<16hex>` |
| `message` | string | 是 | 用户消息（trim 后非空，否则 400） |

### 响应 200
```json
{
  "session_id": "sid_ab12cd34ef56gh78",   // string 会话 ID（新会话由服务端生成，前端留存）
  "reply": "您的余额为 1000.00 元。",       // string 客服回复（已 sanitize）
  "trace": [ {                              // []traceItem 本次工具执行审计可见结果
    "tool": "get_balance",                  // string 工具名
    "args": "{\"days\":7}",                 // string 参数（JSON 串）
    "confirmed": false,                     // bool 是否用户确认（写操作执行后才 true）
    "preview": "",                          // string 预览文案（写操作挂起时非空）
    "result": "余额 100000 分"               // string 执行结果/等待确认
  } ],
  "pending_confirm": false,                 // bool 会话是否有挂起的写操作待确认
  "pending_preview": "",                    // string 挂起操作的预览文案（无挂起为空串）
  "pending_tool": ""                        // string 挂起操作的工具名（无挂起为空串）
}
```

### 错误码
| 状态码 | body | 场景 |
|---|---|---|
| 401 | `{"error":"missing api key（请带租户 API key）"}` | 无 key |
| 401 | `{"error":"invalid api key"}` | key 无效 |
| 503 | `{"error":"billing 鉴权服务不可用"}` | billing 校验超时/不可达 |
| 400 | `{"error":"bad json body"}` | body 解析失败 |
| 400 | `{"error":"message 不能为空"}` | message 为空 |
| 500 | `{"error":"session 读取失败"}` / 其他错误文案 | 内部错误 |

### pending_action 确认状态机语义

- 会话持久化在 Redis `agent:session:<sid>`，**TTL 30 分钟**；`pending_action` 是会话上的挂起对象（内部结构：`{tool, args map, preview, idem_key, tenant_id}`），**响应层只暴露 `pending_confirm`/`pending_preview`/`pending_tool` 三个字段**。
- 写工具（recharge / refund / subscribe_plan / change_plan / cancel_subscription）**不会立即执行**：LLM 调用后先算预览（Preview），挂起 pending_action，回复「预览文案 + 请回复「确认」执行，或「取消」放弃。」，本轮回结束。
- 读工具（get_balance / get_consumption / get_recent_bills / list_models / list_plans / get_my_subscription）即时执行、实时回填。
- **确认/取消判定不依赖 LLM**（关键字精确匹配，整句去尾标点后比对）：
  - 确认关键字：`确认` `确定` `好的` `可以` `是的` `嗯` `ok` `yes` `yes.` `执行` `没问题` → 执行挂起操作（`tb.Run`，带 `idem_key` 幂等锚点防重复入账），trace 中该条 `confirmed=true`，`pending_confirm` 恢复 false。
  - 取消关键字：`取消` `算了` `不要` `不用` `放弃` `不了` `no` `stop` `退出` → 放弃操作，回复「好的，已取消本次操作，没有做任何改动。」，无 trace。
  - 其他回复：**保留挂起状态**继续正常对话（LLM 会被告知还有待确认操作，只引导用户回复确认/取消）。
- 写操作即使挂起也会立即 `saveSession`（挂起状态必须落盘，否则下一请求丢状态）；`idem_key` 形如 `agent:<tool>:<unixnano>:<hex>`。
- 单次对话最多 5 轮工具迭代；会话历史 LLM 侧截取最近 24 条。

### 可用的工具名（trace `tool` 字段取值）
读：`get_balance`、`get_consumption`、`get_recent_bills`、`list_models`、`list_plans`、`get_my_subscription`
写：`recharge`、`refund`、`subscribe_plan`、`change_plan`、`cancel_subscription`

---

## OpenAI 兼容 /v1/chat/completions（试一发用）

- 路径：`POST /v1/chat/completions`（gateway），鉴权同 Portal（Bearer 租户 Key），中间件链：APIKey → Idempotency（自动生成 `request_id`，支持 `Idempotency-Key` 头）→ RateLimit（600/min 默认）。
- 上游：router `/route` 选 provider → billing Reserve 预占 → 转发上游 `/v1/chat/completions`（真实渠道用渠道自己的上游 key 鉴权）→ 拿到 usage 后 ReportUsage+Settle 结算。

### 请求 Body
| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `model` | string | 是 | 目录 `model_id`（如 `deepseek-chat`） |
| `messages` | [{ role, content }] | 是 | 非空数组；`role`=system/user/assistant，`content`=string |
| `max_tokens` | int | 否 | **缺席不转发**（真实上游用模型默认值；0 会被 OpenAI 兼容接口拒掉），`omitempty` |
| `stream` | bool | 否 | true=SSE 流式，false(默认)=非流式 |

### 非流式响应（200）
- **原样透传上游响应**（OpenAI 兼容格式），网关只读 `usage` 字段结算：
```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1730000000,
  "model": "deepseek-chat",
  "choices": [ { "index": 0, "message": { "role": "assistant", "content": "..." },
                 "finish_reason": "stop" } ],
  "usage": { "prompt_tokens": 123, "completion_tokens": 45,
             "total_tokens": 168 }
}
```
> 网关只登记 `usage.prompt_tokens` / `usage.completion_tokens`（无 json tag 的本地结构），结算按实际用量而非预占。

### 流式响应（200，SSE）
- `Content-Type: text/event-stream`，`Cache-Control: no-cache`，逐行透传 `data: {...}` 帧、每帧 `Flush`；终帧 `data: [DONE]` 或带 usage 的终帧（choices 空 + usage 非空）结束。
- 结算规则：拿到 usage 终帧/`[DONE]` → 按实际结算；客户端断连/上游中断且未拿到 usage → **不结算**，账单保持 pending，交给 M4 对账冲正。

### 错误码（`abort` 统一 OpenAI 风格：`{"error":{"message":"..."}}`，注意无 `type` 字段——与中间件不同）
| 状态码 | message | 场景 |
|---|---|---|
| 400 | `invalid request body` | body 解析失败 |
| 400 | `model and messages required` | 缺 model 或 messages 空 |
| 400 | `model not listed or disabled: <model>` | 目录无此模型/已下架（billing InvalidArgument） |
| 401 | `missing api key` / `invalid api key` | APIKey 中间件（带 `type:"authentication_error"`） |
| 402 | `insufficient balance` | Reserve 余额不足（Payment Required） |
| 402 | `订阅额度不足，每 N 小时滚动刷新` | Reserve 订阅窗口额度超限 |
| 429 | （限流中间件） | RateLimit |
| 502 | `upstream error` / `all providers unavailable` | 上游失败/全渠道失败（已退款） |
| 503 | `routing unavailable` / `no available provider for model` | router 不可达/无可用 provider（不产生计费） |
| 503 | `billing unavailable` | billing gRPC 不可达 |

---

## 典型响应 JSON 示例

### 1. GET /portal/me（或 /admin/me）
```json
{
  "balance": 1000000,
  "balance_cent": 1000000,
  "ledger_balance": 990000,
  "consistent": false,
  "subscription": {
    "ID": 3, "TenantID": 1, "PlanID": 2, "PlanType": "recurring",
    "Status": "active",
    "CycleStart": "2026-01-01T00:00:00+08:00",
    "CycleEnd": "2026-02-01T00:00:00+08:00",
    "AutoRenew": true, "CycleNum": 1, "QuotaGranted": 0,
    "PendingPlanID": null, "CancelledAt": null,
    "CreatedAt": "2026-01-01T00:00:00+08:00", "UpdatedAt": "2026-01-01T00:00:00+08:00"
  },
  "allowance": [ { "tier": "旗舰", "quota": 2000000, "consumed": 350000, "remaining": 1650000 } ],
  "refresh_hours": 5,
  "plans": [ { "ID": 2, "Name": "Plus", "PlanType": "recurring", "PriceMoney": 99,
               "ValidityDays": 30, "RefreshHours": 5,
               "TierQuota": "{\"旗舰\":2000000,\"轻量\":8000000}",
               "Status": 0, "CreatedAt": "...", "UpdatedAt": "..." } ],
  "consumption": [ { "day": "2026-01-01", "tokens": 1200 } ],
  "consumption_by_model": [ { "model": "deepseek-chat", "vendor": "DeepSeek",
                              "input_tokens": 100, "output_tokens": 50,
                              "cost_cent": 45, "calls": 5 } ],
  "topups": [ { "ID": 9, "TenantID": 1, "Source": "recharge", "Amount": 1000000,
                "RefType": "recharge", "RefID": "ord_...", "CreatedAt": "..." } ]
}
```

### 2. GET /portal/models（买家目录）
```json
{ "items": [ { "id": 1, "vendor": "DeepSeek", "model_id": "deepseek-chat",
               "name": "DeepSeek Chat", "input_price_cent": 40,
               "output_price_cent": 160, "context_len": 65536,
               "tags": "[\"文本\",\"轻量\"]", "status": 0,
               "created_at": "2026-01-01T00:00:00+08:00",
               "updated_at": "2026-01-01T00:00:00+08:00" } ] }
```

### 3. GET /admin/overview
```json
{
  "tenants": [ { "id": 1, "name": "alice", "initial_quota": 1000000,
                 "balance": 950000, "redis_balance": 950000, "consistent": true,
                 "topups": 100000, "settled": 50000, "pending": 0,
                 "bill_count": 12, "today_bills": 3, "today_settled": 1000 } ],
  "totals": { "settled": 50000, "pending": 0, "bills": 12,
              "today_bills": 3, "today_settled": 1000 }
}
```

### 4. POST /portal/chat（确认流程示例：挂起 → 回复「确认」执行）
第一次（写操作挂起）：
```json
{
  "session_id": "sid_ab12cd34ef56gh78",
  "reply": "将为您充值 100 元（10000 分）。\n\n请回复「确认」执行，或「取消」放弃。",
  "trace": [ { "tool": "recharge", "args": "{\"amount_money\":100}",
               "confirmed": false, "preview": "将为您充值 100 元（10000 分）",
               "result": "等待确认" } ],
  "pending_confirm": true,
  "pending_preview": "将为您充值 100 元（10000 分）",
  "pending_tool": "recharge"
}
```
第二次（带同一 session_id，message=“确认”）：
```json
{
  "session_id": "sid_ab12cd34ef56gh78",
  "reply": "充值成功，订单 ord_xxx 已入账 10000 分。",
  "trace": [ { "tool": "recharge", "args": "{\"amount_money\":100}",
               "confirmed": true, "preview": "将为您充值 100 元（10000 分）",
               "result": "充值成功..." } ],
  "pending_confirm": false,
  "pending_preview": "",
  "pending_tool": ""
}
```

### 5. GET /admin/bills?tenant_id=0&limit=2（分页）
```json
{
  "total": 57,
  "items": [ { "ID": 58, "RequestID": "req_...", "Phase": "reserve", "Status": "settled",
               "TenantID": 1, "APIKeyID": 2, "ProviderID": 1, "Model": "deepseek-chat",
               "Funding": "balance", "PreQuota": 180, "ActualQuota": 150,
               "DeltaQuota": -30, "PromptTokens": 100, "CompletionTokens": 50,
               "InPriceCent": 40, "OutPriceCent": 160,
               "UsageReportedAt": "2026-01-01T00:00:00+08:00",
               "ErrorCode": "", "CreatedAt": "2026-01-01T00:00:00+08:00" } ]
}
```

---

## 附：billing admin :9103 直连端点全清单（网关 proxy 的上游，端口仅本机/内网可达，鉴权由 gateway/agent 把关）

| 端点 | 方法 | 已在上述代理路径 |
|---|---|---|
| `/admin/auth/initialized` `/admin/auth/setup` `/admin/auth/login` `/admin/auth/logout` `/admin/auth/me` | GET/POST | /admin/auth/* |
| `/admin/overview` | GET | /admin/overview |
| `/admin/bills` | GET | /admin/bills |
| `/admin/me` `/admin/plans` `/admin/subscription` `/admin/consumption` | GET | /portal/me 等、/admin/me 等 |
| `/admin/providers` | GET/POST | /admin/channels |
| `/admin/topups` `/admin/recharge-orders` | GET | /portal/topups 等、/admin/topups 等 |
| `/admin/recharge` `/admin/recharge/pay` `/admin/recharge/refund` | POST | /portal/recharge* |
| `/admin/subscribe` `/admin/change-plan` `/admin/cancel-subscription` | POST | /portal/* |
| `/admin/models` | GET/POST | /admin/models |
| `/portal/models` | GET | /portal/models |
| `/admin/models/status` `/admin/models/price` | POST | /admin/models/{status,price} |
| `/admin/sales` | GET | /admin/sales |
| `/admin/reconcile-check` | GET | /admin/reconcile-check |
| `/admin/audit` | GET | /admin/audit |
| `/healthz` | GET | /admin/health 聚合 |
| `/metrics` | GET | Prometheus（前端不用） |

router :9102：`/route?model=&exclude=`(GET)、`/report`(POST `{provider_id, ok}` → `{"ok":true}`)、`/breakers`(GET → `[State]`)、`/providers`(GET → `[ProviderState]`)、`/healthz`。
reconciler :9106：`/healthz`、`/status`(GET → `{"ok","svc","last_scanned","last_reversed","last_settled","last_rebuilt"}`)。
agent :9105：`/chat`(POST)、`/healthz`。