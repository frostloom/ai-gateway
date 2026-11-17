# auto 模型路由：JEV 前置选档设计（2026-11-16）

> **状态：已批准（2026-11-17）。** 用户按建议值整批批准：降级矩阵、熔断参数（3 次/30s）、L3 预算上限 ¥20、shadow 实现但不作上线门槛、portal 缓议。批准前后的全部决策见"降级策略（已批准）"与"开放问题的决议"两节。

## 背景与目的

网关要增加一个 `auto` 模型模式：请求地址与方式与普通模型完全一致，仅 `model` 字段可以填 `auto`；网关用 JEV（TypeSafe System One，类型化判定模型）按任务难度在便宜档与强档之间选择真实模型——简单任务走便宜模型，困难任务走强模型。

生态里已有同形态项目可参照（`mmornati/system-one-router`：Go 网关、`model:"auto"`、JEV 一次调用回答描述性问题、配置表选"质量底线之上最便宜的模型"，基准报告 topic 准确率 89%、与人工路由一致 72%、成本约 −43%）。本项目的差异是已经具备计费/额度/月卡、多 provider 加权选路与熔断、以及成套评测基建（1220 用例、Langfuse rubric 评分）。因此本设计重点不是"怎么路由"，而是"把选档接进现有网关链路，并按现有评测体系验证"。

## 已定的做法（用户决定）

- `auto` 是模型别名：URL、鉴权、幂等、限流、流式、失败语义都不变，只有 `model` 字段可以用 `auto`。
- 两档真实模型：`deepseek-v4-flash`（便宜档）与 `deepseek-v4-pro`（强档）。两档均已在 `models` 目录中上架并有价格（100/200 与 300/600 分/百万 token）。

## 目标

- 数据面所有"普通模型可用"的入口接受 `model:"auto"`：`/v1/chat/completions`、`/v1/messages`、`/v1/responses` 及根组的对应路径。每次请求一次 JEV 判定，按阈值选档。
- 一次请求（含 failover 重试）全程只用选中的档位，不会中途换档。
- 计费、账单、套餐语义按**实际选中模型**执行，与用户显式填写该模型完全一致。
- 判定失败不阻塞请求：JEV 运行期超时/错误按配置兜底档位放行，并显式标记。
- 决策可审计：响应头 + 结构化日志 + 指标。
- 用现有评测基建分层验证；付费部分单独授权。

## 非目标（v1 不做）

- check-and-escalate（答题后再判定、弱回答升级重试）——可作为二期。
- 上游运行期跨档重试：已 `Reserve` 锁定档位后的上游失败仍只做同模型 provider failover，不中途换档（`Route` 阶段的跨档降级见"降级策略（已批准）"）。
- 四问式判定（topic/risk/隐私分类）、本地小模型替代 JEV。
- 管理后台配置界面、按用户/密钥的路由偏好、决策缓存、多轮粘性（每请求独立判定）。
- portal 组（`/portal/completions`、`/portal/responses`）的 auto 支持——单独产品面，留待后续评估。
- 改动客服 agent 现有的 JEV 逻辑（那是意图判定，与本功能互不影响）。
- `/v1/models` 等模型列表端点不列出 `auto`（客户端显式设置即可；列出需改列表来源逻辑，留待后续）。
- 不把 `auto` 加入 billing `models` 目录（目录是计费权威，加入会破坏禁用态语义）。

## 反目标（这样做了就算失败）

- 非 `auto` 请求的行为、时延、计费有任何变化。
- `auto` 绕过模型上架、套餐范围、额度校验。
- JEV 故障导致请求失败，或静默换模型而不在响应/日志中标记。
- 判定结果不可审计（没有 header/日志，事后无法解释某请求为什么选了某档）。

## 约束

- 复用 `internal/jev` 客户端（已支持 noul/choice/score 类型化问题、失败诊断、`JEV_HTTPS_PROXY`）。
- 只能选出 `models` 目录中已上架、且 `providers` 中有可服务 provider 的模型。上架与否由 billing 在 `Reserve` 时强制（`InvalidArgument` → 400）；启动时只做配置语法校验，不做在线目录/路由探测（见"失败与兜底"）。
- 判定必须发生在 `Reserve`（预占）之前：billing 会拒绝目录中不存在的模型。
- `chatUpstreamBody` 在 `req.raw` 存在时**原样转发客户端字节**，因此改写必须落实到 raw JSON 的 `model` 字段，只改解析结构不够。
- 发给 JEV 的 state 是对话消息内容——与发给上游同信任级别，但 JEV 是额外的第三方数据流；本功能默认关闭，v1 不做脱敏，文档层面注明。
- 判定时延预算：默认 2s 超时；参照基准 p50 约 268ms。
- 不新增数据库表；配置走环境变量。
- 付费调用遵守项目授权规则：L2 判定基准（JEV 调用）与 L3 三臂评测都要单独批准。

## 方案

### 接入点

- 新增 `internal/autoroute` 包：解析 body → 若 `model == "auto"` 则判定 → 返回改写后的 body 与决策记录；非 auto 直接原样返回（零额外开销路径）。
- 在 `/v1` 组与根组（`cmd/gateway/main.go` 中 `APIKey → Idempotency → RateLimit` 之后、handler 之前）加一个中间件：读 body（沿用 8MiB 上限）、顶层解析 `model`；判定后把改写后的 JSON 替换回 `c.Request.Body`。chat / messages / responses 三类 handler 自动共用，handler 无需改动。
- 备选方案：在 `Chat` / `Native` handler 内部判定。位置更贴近 `Route()`，但需要复制逻辑；默认选中间件方案，实现中若发现 body 重放有坑再切换。
- 判定只发生一次；`forwardNonStream`/`relayStream` 的 failover 用改写后的 `req.Model` 重新 `Route`，档位天然保持不变。

### 判定

- 一次 `Evaluate` 调用：`state` = 最近若干条消息的 JSON（上限 8KiB，超出取最近消息）。
- state 提取器按 body 形状分三种：chat/messages 取 `messages[].content`；responses 取 `input`（字符串或 item 数组）；无法识别形状 → **不调 JEV 判定、直接改写为兜底档位并记录**（永不因此失败请求）。
- 问题：`complexity`，score 型 4 档 `[简单, 一般, 复杂, 极复杂]`（下标 0–3），取期望分数。
- 阈值（默认 1.5，可配）：`score ≥ 阈值` → pro；否则 flash。
- 概率与置信度写入日志备用；v1 不按置信度做特殊处理（统一阈值），避免多一条分支。

### 运行模式与兜底

- 模式 `AUTO_ROUTE_MODE=off|shadow|on`（默认 off）：
  - `off`：中间件遇到 `model=="auto"` 直接 400 `model not listed (auto routing disabled)`——确定性拒绝，不落到 Route/Reserve 的杂散错误。
  - `shadow`：只判定并记录，请求**以固定档位实际服务**（`AUTO_ROUTE_SHADOW_SERVE`，默认便宜档），响应头 `X-Auto-Applied: false` + `X-Auto-Would-Be`。用于上线前零质量风险地采集判定分布；只影响 auto 请求，普通流量零变化。
  - `on`：判定即改写，响应头 `X-Auto-Applied: true`。
- 运行期 JEV 超时/失败 → 兜底档位（默认 `cheap`=flash），`X-Auto-Fallback: jev_error`，日志记录失败种类（复用 `jev.Failure` 有界诊断字段）。
- **JEV 熔断器**：连续 3 次判定失败 → 熔断 30s，期间请求直接走兜底档、`X-Auto-Fallback: breaker_open`，不再等 2s 超时。半开探测：冷却期满后放行下一个请求真调 JEV，成功则闭合。v1 用进程内状态（多实例不同步，可接受）；参数可配。

### 降级策略（已批准）

按错误发生层分四级，默认值如下（均可配置）：

| 层 | 场景 | 行为 | 标记 |
| --- | --- | --- | --- |
| 1 | JEV 判定失败/超时 | 兜底档（默认 `cheap`）放行 | `X-Auto-Fallback: jev_error\|timeout\|decode` |
| 1 | JEV 连续 3 次失败 | 熔断 30s，期间直接兜底档 | `X-Auto-Fallback: breaker_open` |
| 2 | 强档无可用 provider（Route 时） | **自动降级便宜档**（默认开）：服务不中断、用户少付钱可能降质 | `X-Auto-Degraded: strong_unavailable` |
| 2 | 便宜档无可用 provider（Route 时） | **不自动升级强档**（默认关，避免意外账单）→ 现有 503 | — |
| 3 | 已 Reserve 后上游失败 | 仅同模型 provider failover（现有 3 次+exclude），耗尽 503；流式首字节后绝不重发 | 现有语义 |
| 4 | 配置错误 | 启动 fail-fast；`off` 态 auto → 400；body 形状认不出 → 不判定、改写兜底档 | `X-Auto-Fallback: unparseable` |

第 2 层安全性依据：`Route` 在 `Reserve` 之前（chat.go 现状），换档后按新模型 Reserve，计费与显式选择完全一致。第 3 层不跨档的原因：账单已按锁定档位预扣，中途换档破坏结算语义。
- **配置错误与运行期故障分开对待**：模式为 `shadow`/`on` 时，若 JEV 配置缺失/端点形状非法、档位配置非法（缺失、重复、非目录已上架名无法校验、阈值越界），启动直接失败（fail fast）——不做"没配好就全量兜底"，避免 100% 流量静默降档。
- 上架/可用性不做启动时在线探测（网关启动早于 billing/router 是常见顺序，且本地存在 `models:["*"]` 通配 provider 使 Route 式探测恒真、无意义）。运行期由 billing（目录/价格）与 Route（provider）按普通模型同样规则强制。

### 计费与套餐

- 改写发生在前：`Route`、`Reserve`（费率查找）、bills、套餐范围判断全部使用实际模型，语义与用户显式选择无差别。
- 套餐 `AllowedModels` 不特殊处理：若选中的模型不在套餐范围，按现有规则走现金余额/拒绝——与显式选择同一行为。
- bills 只记实际模型（不加列、不改 schema）；auto→实际模型的映射在日志与响应头里。

### 可观测

- 响应头：`X-Auto-Model`（实际服务模型）、`X-Auto-Would-Be`（判定档位；shadow 时与 Model 不同）、`X-Auto-Applied`、`X-Auto-Score`、`X-Auto-Decision-Ms`、`X-Auto-Fallback`（仅兜底时：`jev_error|timeout|decode|breaker_open|unparseable`）、`X-Auto-Degraded`（仅跨档降级时：`strong_unavailable`）。
- 结构化日志 `auto_route`：request_id、模式、实际模型、判定档位、score、概率、阈值、判定耗时、兜底/降级原因、熔断器状态。
- Prometheus：`gateway_auto_decisions_total{mode,model}`、`gateway_auto_decision_latency_seconds`、`gateway_auto_fallback_total{reason}`、`gateway_auto_degraded_total{reason}`、`gateway_auto_breaker_state`（gauge：0 closed / 1 open）。
- 不接 Langfuse（网关数据面当前未接 observer；评测在评测层做，见测试方案）。

### 配置（环境变量）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `AUTO_ROUTE_MODE` | `off` | `off` / `shadow` / `on` |
| `AUTO_ROUTE_TIERS` | `deepseek-v4-flash,deepseek-v4-pro` | 顺序 = 便宜,强 |
| `AUTO_ROUTE_THRESHOLD` | `1.5` | score ≥ 阈值选强档 |
| `AUTO_ROUTE_FAIL_MODE` | `cheap` | JEV 失败兜底档位（`cheap`/`strong`） |
| `AUTO_ROUTE_SHADOW_SERVE` | 便宜档 | shadow 模式实际服务档位 |
| `AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE` | `1` | 强档无 provider 时自动降便宜档 |
| `AUTO_ROUTE_UPGRADE_CHEAP_UNAVAILABLE` | `0` | 便宜档无 provider 时自动升强档 |
| `AUTO_ROUTE_BREAKER_FAILURES` | `3` | JEV 连续失败 N 次进熔断 |
| `AUTO_ROUTE_BREAKER_COOLDOWN_S` | `30` | 熔断冷却秒数（后半开探测） |
| `AUTO_ROUTE_TIMEOUT_MS` | `2000` | 单次判定预算 |
| `AUTO_ROUTE_MAX_INPUT_BYTES` | `8192` | 发给 JEV 的 state 上限 |
| `JEV_ENDPOINT` / `JEV_API_KEY` / `JEV_MODEL` | 官方端点 / 必填 / `jev-latest` | 复用既有 `JEV_HTTPS_PROXY` |

### 安全

- JEV key 只从环境变量读取；日志与响应头不回显 key、判定正文。
- 启动校验端点形状（http 仅允许 loopback，与 `jev.NewEvaluationClient` 规则一致）。

## 测试方案

### L0 单元测试（无外部依赖）

- selector：阈值边界（恰好等于阈值）、配置校验、兜底行为、三种 state 提取器、输入裁剪、shadow 不改写为判定档、off 拒绝。
- 熔断器：3 次失败→open、冷却后半开放行、成功→closed、失败计数复位。
- 降级决策：强档 Route 失败→便宜档+标记（开关开/关）、便宜档失败不升级（开关默认关）、开关开时升级。

### L1 中间件/handler 集成（httptest + fake JEV/provider/billing）

- `auto` 请求：上游收到的 **raw 字节**里 model 已改写为实际模型，解析结构的 `Model` 一致。
- 单请求只判定一次；failover 后 `Route` 仍用同档模型。
- 非 auto 请求：body 字节不变（回归断言）。
- 三类入口（chat/messages/responses）各自走通；无法识别的 body 形状走兜底并标记。
- SSE 流式透传、4xx/5xx 透传语义、`Reserve` 收到实际模型、bills 记实际模型。
- `off` 态 400、shadow 态服务固定档 + headers、JEV 失败/超时兜底。
- 熔断打开期间请求不再调 JEV、直接兜底；半开探测成功后恢复。
- Route 阶段强档无 provider：默认降级便宜档 + `X-Auto-Degraded`；便宜档无 provider：默认 503；两个开关的四种组合。

### L2 离线判定基准（付费：仅 JEV，需单独授权）

- 输入复用 `eval/campaigns/2026-11-10-langfuse-r2` 的 packets（1220 条，**不调用任何真实模型**）。
- 难度代理标签：使用本机 Langfuse（ClickHouse `scores` 表）已有的 rubric 评分——2026-11-14 核验：2042 个 EVAL 评分，其中 2022 个挂 r2 实验条目（model 臂 1010 条已评：660 过 / 350 挂；jev 臂 1012 条已评：678 过 / 334 挂；两臂配对 1008 条：双过 597、仅 model 过 62、仅 jev 过 80、双挂 269）。代理定义：两臂都挂 = 难，两臂都过 = 易；末尾 414 条未评分，不进入代理标签。仓库里的 `grades/*.json` 只是 `{}` 发布标记，**不是**分数，权威分数在 Langfuse。
- 指标：档位分布（多少进 pro）、判定 p50/p95、同输入重复抽样的一致性（JEV 稳定性）、与代理标签的一致率（难题的 pro 命中率、易题的 flash 命中率）。
- 预算量级：1220 次 JEV 判定约 $0.05–0.1。

### L3 三臂付费评测（需单独授权）

- 分层抽样（建议约 200 题：按代理标签难/易分层，具体样本量与预算见开放问题）。
- 三臂：always-flash、always-pro、auto。**auto 臂走真实网关**：本机部署一套带两档真实 provider 的网关实例，评测 runner 经 HTTP 调 `model:"auto"`，端到端覆盖中间件/改写/判定；JEV 调用计入 L3 预算。L2 与 L3 共用同一 selector 实现。
- 每题按 rubric 评分，复用现有 Langfuse 管道与 grader 配置（判分材料对臂别盲评）。
- 指标：rubric 通过率（总体/难题子集）、总成本、每通过任务成本、auto 与 pro-only 的质量差、判定时延开销。

## 验收标准（草案，供审）

- 质量：auto 总体通过率 ≥ pro-only − 3pp；难题子集 ≥ pro-only − 5pp。
- 成本：auto 总成本 ≤ pro-only 的 60%（L3 实测，或 L2 分布推算）。
- 时延：判定 p50 ≤ 300ms、p95 ≤ 1s（不含上游转发）。
- 回归：非 auto 路径现有测试全绿；L1 断言全部覆盖。

## 设计评审记录（builder-check 处理）

独立审查提出的问题与处理：

1. **shadow 与 auto 组合不可服务**（auto 不在目录，不改写会被 Reserve 拒/把字面 auto 发上游）→ 已定：shadow 仅作用于 auto 请求，且请求以固定档实际服务、判定结果只记录（`X-Auto-Applied:false` + `X-Auto-Would-Be`）。
2. **L2 难度标签是否存在** → 存在但不在仓库：在 Langfuse `scores` 表（本会话已核验，见 L2 节数字与覆盖缺口）；仓库内 grade 文件只是发布标记。
3. **/responses 与 portal 入口** → v1 覆盖三类数据面入口（chat/messages/responses + 根组），state 提取器按形状分支、无法识别即兜底；portal 明确非目标。
4. **启动校验判据** → 只做配置语法 fail-fast；目录/可用性由 billing/Route 运行期强制；不做在线探测（启动顺序与通配 provider 使探测无意义）。
5. **JEV 未配置时 fail-open 还是 fail-fast** → 配置错误（启用但缺 key/形状非法）fail-fast；运行期 JEV 故障才兜底。
6. **/v1/models 与 billing 目录** → v1 不列出 auto、不加入目录；off 态由中间件确定性 400。
7. **L3 auto 臂驱动方式** → 真实网关端到端（本机部署两档真实 provider），JEV 调用计入 L3 预算；L2/L3 共用 selector。

## 开放问题的决议（2026-11-17 用户批准）

- **L3 预算**：上限 **¥20**（估算实际约 ¥5–10；执行前仍按项目规则报具体计划，花完照例回报）。
- **shadow**：实现（off/shadow/on 三态），但**不作为上线门槛**；可用自己的工具 dogfood 采集分布。
- **阈值回调**：L2 落盘每条用例的原始分数，换阈值只是本地重算，**不需要**新的 JEV 授权；只有改问题措辞/档位定义才需新授权。
- **portal 支持 auto**：缓议，L3 之后再评估（接入本身只是把中间件挂到 portal 组）。
- **降级矩阵与熔断参数**：按"降级策略（已批准）"节的默认值整批批准。

## 工作量分级

中等：一个新包 + 一个中间件 + 分层测试为主；不改计费核心、不建新表、不动客服 agent。实现计划见后续 writing-plans 产物。
