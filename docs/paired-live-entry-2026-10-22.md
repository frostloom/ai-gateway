# 首批真实评测入口（2026-10-22）

`cmd/paired-live` 已提供显式的 batch-001 执行入口。六案例、最多 24 次模型尝试和 6 次 JEV、¥1 / $0.02 的既有授权继续有效。此版本执行首批后强制停在 `checkpoint_pending`，其余五批保持 `not_started`；首批仍须人工检查 trace、score 和语义质量。`cmd/paired-eval` 保持离线用途。

本轮没有发送真实模型或 JEV 请求。现有收费依据仍缺三项：账户适用费率、收费 token 包络、JEV 失败请求收费规则。余额和支出授权不能替代它们；不再核查余额，不生成虚假的已核实证明。

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
