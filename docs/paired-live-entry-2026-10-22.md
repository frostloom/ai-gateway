# 首批真实评测入口（2026-10-22）

`cmd/paired-live` 已提供显式的 batch-001 执行入口。六案例、最多 24 次模型尝试和 6 次 JEV、¥1 / $0.02 的既有授权继续有效。此版本执行首批后强制停在 `checkpoint_pending`，其余五批保持 `not_started`；首批仍须人工检查 trace、score 和语义质量。`cmd/paired-eval` 保持离线用途。

## 同日后续：Pi 首批的保守预留策略与源码归档构建

用户已多次明确批准原预算内执行首批，并授权直接采用 DeepSeek 官方价格、自行判断 Token 预算及调查 JEV。当前 Pi 授权文件为 `eval/paired-pilots/2026-10-22-authorized-pi-small-pilot/authorization.json`，计划为 `eval/paired-pilots/2026-10-22-pi-customer-preflight/plan.json`。官方价格和离线 Token 判断见 `docs/pi-customer-evaluation-entry-2026-10-22.md` 后续调查节。

此前将“必须有供应商失败收费保证”作为唯一入口，混淆了供应商证明和工程预算策略。新增 `paired-billing-attestation.v2` 只供绑定 Pi v2 的试点使用：仍要求独立来源 SHA、官方费率和 Token 包络审查，但允许显式选择 `jev_failure_policy=reserve_full_attempt_stop_on_failure`，同时保持 `jev_failure_charges_verified=false`。它必须携带精确的 `jev_max_input_tokens=64000`、`jev_input_micro_usd_per_million=42000`、`jev_full_attempt_micro_usd=2688`；不接受更小预留或旧千问授权。该选择是受信操作员的保守风险分配，不是供应商对失败收费上限或退款的保证。

该策略不依赖失败免费：每次按完整 64k 输入预留 $0.002688。JEV 任意失败会持久化模型预算 halt，阻止剩余模型回退；重开预算不能抹去 halt，不自动重试或执行后续批次。旧 v1 证明继续要求原三个 verified 条件，不能用新字段偷偷升级。检查输出和回执分别记录失败策略及供应商 verified=false；check 的 ready 只涵盖授权和预算策略，不替代构建、配置和实际执行检查。

工作区已有前端产物和未追踪探针，不能清理或隐藏它们来伪造干净构建。新增 `scripts/build-paired-live.py --output /绝对路径/paired-live`：仅从已提交 `main` 的 `git archive` 在独占临时目录编译，不新建 Git 分支或开发工作树，不改动工作区。使用 `-mod=readonly -trimpath -buildvcs=false`，通过明确命名的 `archivedCommit` / `archivedTree` 记录真实来源；不伪造 Go 的 `vcs.revision` 或 `vcs.modified`。同名 `.provenance.json` 记录提交、tree、源码归档与二进制 SHA、Go 和 SDK 版本。它信任本机受信构建者，与原 VCS stamp 一样不是密码学供应链证明。

归档构建执行时，提交与 tree 必须匹配当前 HEAD 和 `-revision`；磁盘上实际读取的冻结 definitions/profiles 不得有修改或额外文件，包括被忽略的文件。工作区 Go 源码没有参与归档编译，因此其无关探针不再构成误报。原干净 VCS 构建路径继续保留。实际执行仍需全部其他守卫及唯一领取记录，预算和首批 checkpoint 均未扩大。

以下各节的千问 v1 未就绪记录与命令保留为历史，不代表当前 Pi v2 的状态。

本轮没有发送真实模型或 JEV 请求。现有收费依据仍缺三项：账户适用费率、收费 token 包络、JEV 失败请求收费规则。余额和支出授权不能替代它们；不再核查余额，不生成虚假的已核实证明。

## 收费依据补齐结果

同日后续重新读取六份官方公开资料，确认公开忙时价格与既定预留单价一致，且该模型的 `max_tokens` 包含回答与思考输出；JEV 的公开输入单价、输出免费及默认上下文限制也与原声明一致。原始材料、来源哈希及未就绪声明保存在被 Git 忽略的 `.tmp/paired-live-private/`，没有读取账户余额或发送推理。

校正一个容易混淆的事实：JEV Python SDK 的类型允许 usage 为空，但官方 HTTP OpenAPI 的成功响应要求 `usage`，其中 `input_tokens` 和 `output_tokens` 都是必需字段；`input_tokens` 被定义为收费输入 token。现有执行守卫要求实际响应提供用量，与 HTTP 契约一致。成功响应的用量仍是调用后信息，不能证明请求失败、超时或过长时的最大扣费。该 HTTP 规范的哈希与此前保存的一致，这是对证据解读的校正，不是供应商刚刚修改了接口。

| 未满足项 | 足以补齐的依据 | 现有材料的限度 |
| --- | --- | --- |
| 账户费率 | 本项目凭据适用的单价上界及附加费用说明；无需依赖优惠或免费额度 | 公开标价、历史抵扣与余额不能单独证明账户适用上界 |
| 收费输入 token 包络 | 对完整 system、messages、tools 及模板的无推理计数方法，或供应商确认输入收费不超过每请求 16384 token | 16384 字节限制不是 token 计数；所查精确渠道 OpenAPI 没有列出计数接口 |
| JEV 失败收费 | 覆盖 HTTP 错误、断连、超时和超上下文的最大扣费规则；每次不超过既定 $0.002688 即可，并不要求失败免费 | 默认 64k 是上下文限制；错误文档没有给出上述最大扣费承诺 |

这三项目前属于供应商或账户资料依赖。本地替身测试、重复 preflight 或重复余额查询无法补齐，保留 `ready_for_paid_run=false`。未消费实际授权领取记录；首批执行尚未启动。

供后续供应商确认的具体问题已明确：千问需确认本项目按量 Anthropic 渠道、`deepseek-v4.1-flash` 的账户最高输入/输出费率，以及完整请求在发送前如何证明收费输入不超过 16384、输出总量不超过 128；TypeSafe 需确认固定 `jev-1.13.0` 在关闭长上下文时，包括失败尝试在内的最高输入计费量与单次最大扣费。这里只准备确认项，没有向外发送消息或提交工单。

## 默认检查

在仓库根目录运行：

```sh
SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go run ./cmd/paired-live
```

默认检查只读授权、计划和可选的收费证明，不读取 env 文件、不创建运行目录、不连接数据库、不构造付费客户端。当前输出 `human_authorized=true`、`ready_for_paid_run=false`，以及三个稳定的 blocker。检查退出码 0 仅表示检查正常完成。

## 收费证明格式

保存在被忽略的 `.tmp/paired-live-private/` 本机私密目录，不能提交真实账单或密钥。下面是未就绪模板；只有实际依据充分时才可逐项更改：

```json
{
  "schema": "paired-billing-attestation.v1",
  "scope_plan_sha256": "80c1686d56b0d29dda8a2a10af05ecd62a793573d896a9f751d021af94a9b557",
  "model_base": "https://maas.qianwenaiapi.com/apps/anthropic",
  "model": "deepseek-v4.1-flash",
  "protocol": "anthropic",
  "jev_model": "jev-1.13.0",
  "account_tariff_verified": false,
  "token_envelope_verified": false,
  "jev_failure_charges_verified": false,
  "evidence_sha256": ""
}
```

`-billing-evidence` 指向此 JSON，`-billing-source` 指向收费依据文件；三项为 true 时必须验证依据文件的实际 SHA256。输入各不超过 1 MiB。这是可信操作员的证据声明及字节绑定，不是自动向供应商查价，也不保证最终账单与声明相符。模型计划按输入/输出每百万 token ¥2 / ¥8、每请求输入上界 16384 和输出上限 128 声明预留；JEV 每次声明预留 $0.002688。实际供应商计费不能由本机守卫强制决定。

## 显式执行配置

只读取重复 `-env-file` 指定的私密文件，不从进程环境发现密钥。格式为每行 `KEY=value`，支持空行、整行注释和外层引号，不执行 shell 展开；同名键由后文件覆盖。

| 配置键 | 要求 |
| --- | --- |
| `AGENT_LLM_BASE_URL` | 上述 Qwen Anthropic 地址 |
| `AGENT_LLM_MODEL` / `AGENT_LLM_PROTOCOL` | `deepseek-v4.1-flash` / `anthropic` |
| `AGENT_LLM_API_KEY` | 本机私密模型密钥 |
| `JEV_MODEL` / `JEV_API_KEY` | `jev-1.13.0` / 本机私密 JEV 密钥 |
| `JEV_ENDPOINT` | 可省略；填写时必须为 `https://api.typesafe.ai/v1/systemone` |
| `AGENT_LANGFUSE_ENABLED` / `AGENT_LANGFUSE_CAPTURE_CONTENT` | 均为 `true` |
| `AGENT_LANGFUSE_ENVIRONMENT` / `AGENT_LANGFUSE_SAMPLE_RATE` | `evaluation` / `1` |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | 本机回环地址，路径 `/api/public/otel/v1/traces` |
| `LANGFUSE_PUBLIC_KEY` / `LANGFUSE_SECRET_KEY` | 原项目 API keys，非网页登录密码 |
| `AGENT_LANGFUSE_HASH_KEY` | 至少 32 字节的私密身份哈希键 |
| `PAIRED_FIXTURE_ADMIN_DSN` | 允许创建及清理隔离测试数据库的本机配置 |
| `AGENT_LLM_HTTPS_PROXY` / `JEV_HTTPS_PROXY` | 可选的显式代理，不自动发现系统代理 |

## 单次执行与证据

必须先提交并审查运行源码，再从未修改的版本构建可核实的二进制。入口验证 `-revision`、实际二进制的 `vcs.revision` 和当前仓库 HEAD 一致，并要求 `vcs.modified=false`；构建后修改 Go 源码或冻结输入同样会拒绝。`go run` 缺少可核实 stamp 时不能执行。已有无关改动保留，不能为了通过检查擅自清理；当前脏工作区构建只能检查，不能付费执行。

以下仅示范就绪后的命令，不代表本轮已执行：

```sh
SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk \
  go build -buildvcs=true -o /tmp/ai-gateway-paired-live ./cmd/paired-live
/tmp/ai-gateway-paired-live -execute \
  -revision REVIEWED_FULL_HEAD_SHA -release authorized-pilot-batch-001 \
  -billing-evidence .tmp/paired-live-private/billing-attestation.json \
  -billing-source .tmp/paired-live-private/billing-source.txt \
  -env-file .tmp/paired-live-private/paired-live.env \
  -out .tmp/paired-live-private/authorized-first-batch-unique
```

执行前校验冻结定义及 profiles，再验证构建与显式配置。在创建 fixture 或发送请求前，使用规范化计划范围在仓库 `.tmp/paired-live-authorizations/` 独占写入授权领取记录，同步文件和目录。换输出路径、JSON 空白或无关 metadata 都不能重新领取同一范围。领取后即便初始化失败，也不自动删除记录、重试或恢复；先核对本机证据，禁止通过清理记录绕过额度。该约束用于本机受信操作流程，不是跨机器的中心化计费控制。

输出目录必须全新，权限 0700；账本和回执 0600。首批的模型尝试上限为 3、JEV 上限为 1，模型预留 ¥0.101376、JEV 预留 $0.002688。使用原预算守卫、失败停机和部分证据记录。JEV 请求序列化字节数不超过 16384；这不等于精确 token 数。缺失用量或输入超过 64000 会失败，既有 handler 在同批预算内可能转模型 fallback，但不会启动下一批或自动重试 JEV。

`receipt.json` 记录授权/计划/收费证明哈希、执行版本、预算、部分报告和六批状态。资源关闭后写 `entry-outcome.json`，该文件是判断 cleanup/回执是否成功的最终依据；它缺失或 CLI 非零退出均须停止核对，不可只根据较早的 receipt 推断完成。即使退出 0，`checkpoint_pending` 也不代表质量评测通过或整轮完成。

本机验证记录见 `eval/paired-pilots/2026-10-22-authorized-live-entry/validation.json`。
