# 充值确认边界修复部署验收

2026-10-23 已将 `056a760033186e4b768b320e50ec7b7186a32157` 部署到本机客服，覆盖此前“该修复尚未部署”的状态。修复及离线验证见 [源码交付](recharge-confirm-fix-offline-2026-10-23.md)。本次只做部署及零推理验收，没有新增真实模型或 JEV 请求。

## 来源和安装

从该提交的 `git archive` 构建 `cmd/agent`，使用进程级 MacOSX26.5.sdk，参数为 `-mod=readonly -trimpath -buildvcs=false`。二进制 SHA256 为 `f4d6c6c0b4e7d7f819d00b1dc53ca3e1f5bb19168abe1b522b2e5052dbf553d5`。归档、源码树和安装摘要见 [部署回执](../deploy/local-services/customer-recharge-boundary-deployment-2026-10-23.json)。来源单独记录，不宣称此构建满足付费执行器的 pristine/VCS stamp 要求。

旧二进制及主目录、旧 Langfuse 工作树的两份 `.env.agent` 备份在被忽略的 `.tmp/paired-live-private/customer-recharge-boundary-deploy-20261023/backup/`，目录 0700、备份 0600。原子替换 `bin/agent`，两份配置只更新 `AGENT_RELEASE` 为完整提交 SHA 加 `-local`；根 `.env` 摘要不变。原启动器、旧工作树和数据卷保留，通过现有 `local.ai-gateway` launchd 服务组 SIGTERM 温和重启。

## 实际验收

- 新 PID、运行 release，以及内存中四项 Pi DeepSeek 模型配置与原配置一致性全部确认；比对不输出密钥。
- `9105/healthz` 和 `18080/portal` 均返回 200。
- 错误 JSON 请求在 request 解析阶段返回 400，未进入身份、会话、工具或推理；模型/JEV 均为 0。
- 实际响应合同为 `35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1`。
- 精确 trace `994720ab0e63798ac48994dc90144ddf` 已从 Langfuse 读回，匹配新 release、request 阶段、HTTP 400 及 0 模型/0 JEV。
- 99 份历史私密证据逐项核对 SHA256，均未变；旧 failed、claim 和账本保持原状态。

这证明新构建已运行且遥测可读，真实模型对充值确认的效果仍需新测评验证。

## 后续预算

用户在询问后续真实测评累计总预算（人民币＋美元）时回答“100+1”，现记录为 **¥100＋$1 的累计总上限**，不按批次重置，不自动重试失败调用，也不把账户余额当作额外授权。机器可读记录见 [预算上限](../eval/paired-pilots/2026-10-23-expanded-budget/budget-ceiling.json)。

该预算已经获批，不应重复询问。它不重开任何已消耗的旧范围，不能替代新案例、供应商、payload、调用数、token 包络、版本和累计预留的冻结绑定，也不证明供应商费率已核实。目前新精确执行范围尚未绑定，本轮新增实际调用为 0/0。历史累计 24 模型/4 JEV 单独保留，不虚报供应商账单成本。
