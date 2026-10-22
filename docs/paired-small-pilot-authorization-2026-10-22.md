# 首轮小样本授权与执行状态（2026-10-22）

用户在本对话明确回复“好的我授权”，批准紧邻上一条提出的 6 案例、最多 24 次模型尝试和 6 次 JEV 调用，人民币预算 ¥1、美元预算 $0.02。授权已生效，不再将缺少用户授权列为阻塞，也不再重复询问同一范围。用户还要求停止余额核查；私人余额和账单内容不归档。

[机器可读授权](../eval/paired-pilots/2026-10-22-authorized-small-pilot/authorization.json)绑定原六批顺序、各批次数与冻结计划哈希。该授权不扩展到额外案例、重试、追加预算或全量评测。历史提案中的未授权状态保留为当时事实，以本记录更新当前状态。

## 实际执行状态

授权后的离线 preflight 绑定 main `dc4a8b5b143170f5494b7fd658937f92a720b89e`，使用既定模型 deepseek-v4.1-flash、Anthropic 协议、JEV jev-1.13.0。六案例、十二臂、二十六轮，声明预留 ¥0.811008 和 $0.016128，均在获批预算内；命令退出码 0，provider_calls=0。产物为 `.tmp/authorized-pilot-20261022/preflight.json`，哈希见授权记录。首次命令误写 `-model-protocol` 被参数解析拒绝，更正为现有 `-protocol` 后通过，未发送推理。

尚未启动真实测评。先前回复“目前仅差授权”不准确：`cmd/paired-eval/main.go` 当前只支持离线模式；真实底层 `RunAgentPair` 要求显式 observer、AllowPaid、RatesVerified、密钥和版本等条件。账户费率、收费 token 包络及 JEV 失败收费此前尚未核实，用户提供余额不能当作这些信息已被验证。不能因授权金额已批准就把 RatesVerified 设置为 true。

当前明确区分 human_authorized=true 与 ready_for_paid_run=false。未构造付费客户端、未创建真实执行账本、未发送模型或 JEV 请求；未修改现有执行守卫或产品代码。后续需完成符合现有守卫的真实入口及收费证据，继续使用本次授权，无需用户重复批准相同范围。

本轮只做授权记录、离线绑定和文档核验，没有重复全项目测试，也没有重复余额核查。
