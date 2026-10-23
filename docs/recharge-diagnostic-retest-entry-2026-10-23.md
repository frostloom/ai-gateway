# 带诊断的充值确认独立重测准备

本轮只准备新受控入口和离线验证，没有发起真实模型/JEV，没有领取新的实际 claim，也没有重部署客服。原[充值确认失败](recharge-confirm-live-result-2026-10-23.md)及其 claim 保持，原[传输排查](jev-transport-diagnosis-2026-10-23.md)结论仍是根因未知、未复现，不能把增加诊断说成修复了供应商故障。

## 范围与身份

新固定 ID 为 `recharge-confirm-diagnostic-retest-20261023`，使用 `recharge-confirm-authorization.v2`、`recharge-confirm-billing-attestation.v2`、`authorized-recharge-confirm.v2`。仅重测原 `recharge-002/confirm`：两臂各“充值 100 元”后明确确认，共四轮；共享最多 **10 次模型尝试/¥0.337920 声明预留＋1 次 JEV/$0.002688 声明预留**。供应商仍为 Pi DeepSeek `deepseek-flash/openai` 与 TypeSafe `jev-1.13.0`，仅使用合成租户、对话、工具说明/历史和隔离业务结果。

运行合同 `35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1`、已部署客服源码 `056a760033186e4b768b320e50ec7b7186a32157` 及原离线验收保持绑定。新增授权必须同时绑定诊断源码 `5c2f669413c0787ba1bd3a429c9552d628e1a44c`、诊断 manifest、原失败 journal 和独立复核摘要；最终执行器仍须绑定其已审查提交和源码树。旧 v1 批准或计费材料不能改个输出路径混用，任意新 ID 均不接受。

`transport_cause` 可区分有限类型错误，未知送达与收费仍保持 unknown。它不保证再次推理成功，也不是完整网络阶段追踪。JEV 失败仍停止后续模型，不自动重试。

## 独立永久领取与累计预算

新 claim 固定为 `.tmp/paired-live-retests/recharge-confirm-diagnostic-retest-20261023.json`。创建前必须读取原 `.tmp/paired-live-retests/recharge-confirm-20261023.json`，原始字节 SHA 必须为 `2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc`；缺失或变化时不能创建新 claim。归档中的 `prior-claim.json` 仅保存这份已消耗记录的无密钥历史副本，不代表新批准，也不用于覆盖本机原记录。

两个固定范围各永久预留 337920 microCNY、2688 microUSD。原 claim 已消耗，本轮新 claim 尚未领取；若后续明确批准并领取，累计预留将为 **¥0.675840＋$0.005376**，新批准 ¥100＋$1 预算下未分配额度为 **¥99.324160＋$0.994624**。失败也不释放预留；换版本/输出不能重领。这里只实现两个固定分配，后续增加范围仍须重新汇总所有永久预留，不能把每个范围视为新的总预算。

## 默认检查与真实执行边界

```sh
go run ./cmd/paired-live -retest \
  -authorization eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry/authorization.example.json \
  -plan eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry/plan.json \
  -billing-evidence eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry/billing.example.json
```

新例子 `human_authorized=false`、计费证据 false、执行版本为占位；只读检查不读取私密 env、不连接业务数据库、不创建输出或 claim。准备基线的 proposal 不是最终可执行构建。总预算已经批准，不重复询问；原一次真实批准已消耗，本轮精确实际重测仍需单独明确批准，不能自行把例子的 false 置为 true 后付费。

独立批准后还需将本轮完整请求/纠正包络、费率和失败全额预留证据绑定最终执行版本，并从该提交归档构建。不能直接使用上一轮 scope 的计费文件。客服实际 runtime 未重新部署，运行服务与受控评测执行器的版本区别必须保留。

## 本地验收

新增范围首先因不被支持而 RED，实现后门禁通过。回归覆盖新旧材料不能混用、诊断/原失败证据不能更换、缺失或篡改父 claim 不创建子 claim、换输出/版本不重领、两轮累计预留及父记录字节不变。

真实 Handler＋隔离夹具的正常、模型提供方失败及 JEV 本地 EOF 三条路径已验证；正常两臂完成同一 100 元预览/确认。JEV EOF 路径确实将 `transport_cause=eof` 写入实际测评日志并停止，原失效响应不被当作任务成功。以上仅本地替身/故障注入，不代表真实模型或 JEV 质量。

档案位于 `eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry/`，最终验证见其中 `validation.json`；私密回归日志位于被忽略的 `.tmp/paired-live-private/recharge-diagnostic-retest-entry-20261023/`。

完整 `go test ./... -count=1` 在进程级 MacOSX26.5.sdk 下 18 包通过，独立审查无阻断，隔离库残留 0。143 份历史私密证据及原 claim 摘要不变，本轮真实模型/JEV 均为 0，历史累计仍为 25/5。
