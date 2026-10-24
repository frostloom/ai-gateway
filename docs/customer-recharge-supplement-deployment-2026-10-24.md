# 补充金额充值边界修复部署验收

已将提交 `262a47f25572ad77ef38f68d00fb8703d0f45e53` 的归档构建部署至本机客服，覆盖[离线交付](recharge-supplement-fix-offline-2026-10-24.md)中“尚未部署”的当前状态。本轮没有新增真实模型/JEV调用或付费范围，实际累计仍29模型7JEV；原failed、授权及永久claim不改写。

## 构建和安装

使用已验证提交的git archive构建cmd/agent，进程级MacOSX26.5.sdk，参数 `-mod=readonly -trimpath -buildvcs=false`。安装二进制SHA256为 `1dbe8d2e92b60404250e3a90a2e013e62eaa78307f47393338d51ab795da8091`；源码归档/树/安装摘要见[部署回执](../deploy/local-services/customer-recharge-supplement-deployment-2026-10-24.json)。来源单独记录，不宣称满足付费执行器的pristine/VCS stamp门禁。

旧二进制及主目录、旧Langfuse工作树两份.env.agent已备份至被忽略的 `.tmp/paired-live-private/customer-recharge-supplement-deploy-20261024/backup/`，目录0700、备份0600。原子替换bin/agent，两份配置仅更新AGENT_RELEASE；根.env摘要未变。原启动器、旧工作树、数据卷保留，经既有local.ai-gateway服务组SIGTERM温和重启。

## 实际验收

- 新PID及release `262a47f25572ad77ef38f68d00fb8703d0f45e53-local` 已确认；进程内四项Pi DeepSeek模型配置与原配置一致，比对不输出密钥。
- 客服9105/healthz、网关18080/portal均HTTP200。
- 错误JSON请求HTTP400，在request解析阶段停止，LLM/JEV均0；返回新合同v4 SHA `624a53c69b2fece1f705332ca208ee854be0588decad986d4dee42355ad46477`。
- 精确trace `2a1d870c890a92fca67ac09fcf3f6b49` 已从Langfuse实际读回，匹配新release、request阶段、HTTP400及零推理。
- 2950项历史私密文件/claim摘要逐项不变。

这些证据证明新构建正在运行且遥测可读，不代表真实模型充值任务质量通过。原三个充值范围仍绑定旧35c117合同，不能用于新源码；尚无修复后新付费执行入口或精确批准。下一步冻结补充金额三轮任务的新独立范围、最终执行版本及请求/计费包络，再请求该具体一次付费执行批准，不重复询问已批¥100＋$1累计总预算。
