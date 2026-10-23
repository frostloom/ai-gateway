# 充值确认独立测评入口

本轮为已部署的充值确认修复准备独立真实测评入口，不执行真实模型/JEV。累计预算 ¥100＋$1 已获批准；本轮没有重新询问预算，也没有把旧失败批次或已消耗 claim 重新开放。

## 固定范围

`paired-live -retest` 新增固定 `recharge-confirm-20261023`，只接受 `recharge-002/confirm`：两臂各执行“充值 100 元”及后续确认，共四个用户轮次。使用隔离合成租户和本地业务夹具，模型为 Pi DeepSeek `deepseek-flash/openai`，JEV 为 `jev-1.13.0`。供应商接收合成对话、工具描述及评判状态，不是运行客服的真实用户数据。

共享上限为 **10 次模型尝试、¥0.337920 声明预留；1 次 JEV、$0.002688 声明预留**。模型每次最多 128 输出 token、16384 请求字节、16384 输入 token 包络；两臂和纠正路径共享次数，不按轮次重置。JEV 仍采用完整一次预留、失败即停止；供应商失败收费未核实，不标记为已核实。上述金额是配置费率下的预留，不能声称已核实账单或供应商扣费保证。

材料见 [独立档案](../eval/paired-pilots/2026-10-23-recharge-confirm-entry/plan.json)。新授权、计费、回执 schema 分别为 `recharge-confirm-authorization.v1`、`recharge-confirm-billing-attestation.v1`、`authorized-recharge-confirm.v1`。授权绑定计划原始字节 SHA、最终执行版本、已部署客服源码 `056a760033186e4b768b320e50ec7b7186a32157`、运行合同 `35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1`、修复离线验收 manifest/acceptance 及批准预算文件的 SHA。旧现金范围及计费材料不能混用。

## 永久领取与累计预算

执行先在仓库固定 `.tmp/paired-live-retests/recharge-confirm-20261023.json` 以独占创建方式同步持久领取，再构造业务夹具。该 claim 同时记录预算摘要及 10 模型/337920 microCNY、1 JEV/2688 microUSD 的永久预留；失败、换输出、换执行版本均不能重领，不能自动删除释放预算。

当前新总预算只分配这一个固定范围，其最大预留小于 ¥100＋$1。若实际领取，按永久预留计算的未分配额度为 ¥99.662080＋$0.997312；本轮尚未领取，实际新增消耗为 0。这不是可自由扩张的通用多范围预算管理器：未来增加范围时必须先汇总该 claim 及后续永久预留，并新增受审查的分配，不能把每个范围都当作新的 ¥100＋$1。旧范围沿用历史账本，历史累计 24 模型/4 JEV 单列。

## 只读检查与执行前条件

```sh
go run ./cmd/paired-live -retest \
  -authorization eval/paired-pilots/2026-10-23-recharge-confirm-entry/authorization.example.json \
  -plan eval/paired-pilots/2026-10-23-recharge-confirm-entry/plan.json \
  -billing-evidence eval/paired-pilots/2026-10-23-recharge-confirm-entry/billing.example.json
```

默认只读，不加载 env，不创建输出、claim 或夹具。[检查结果](../eval/paired-pilots/2026-10-23-recharge-confirm-entry/readiness.json) 为未就绪：`retest_not_authorized`、`account_tariff_unverified`、`token_envelope_unverified`。这里的授权 blocker 指新精确执行范围，不能解释为 ¥100＋$1 总预算未批准。示例保留 `human_authorized=false`、全零占位执行版本及计费未核实；离线 proposal 的 reviewed_revision 只是准备基线，不是最终可执行构建。

真实执行前应冻结最终提交、独立授权和同版本计费证据，并使用未修改且 VCS 绑定一致的执行器。仅把示例 false 改成 true 不是取得证据。运行只做这一范围，结束停在 checkpoint_pending，再核对两臂入账、确认前未执行、Langfuse 内容与评分，并完成语义审查；不能把本地替身通过称为真实质量通过。

本轮未重新部署客服，运行 release 仍为 `056a760…-local`。历史材料和运行配置保持原样。

## 验证

新增 scope 接受测试先观察到实现缺失导致的 RED，再通过；旧入口接受无身份充值计费 schema 的回归也先 RED、修复后 GREEN。真实 Handler＋隔离数据库＋外部替身覆盖正常、提供方失败、JEV 失败；正常两臂共 2 次替身模型/1 次 FakeJEV，两臂各一次成功充值，复用输出不触达上游。永久 claim 测试确认换版本及输出不能重领。

`go test ./... -count=1` 在进程级 MacOSX26.5.sdk 下 18 个测试包全部通过，独立审查无重要问题，隔离库残留 0。99 份既有私密证据和两份充值离线档案摘要不变。详见 [验证回执](../eval/paired-pilots/2026-10-23-recharge-confirm-entry/validation.json) 及同目录来源绑定与 manifest。本地测试不代表真实模型效果。
