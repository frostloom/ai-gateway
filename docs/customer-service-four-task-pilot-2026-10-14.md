# 四任务真实配对试跑与离线修复（2026-10-14）

本轮是四个开发任务、V1=model/V2=jev_selective 共八臂的真实试跑。八臂都有结束记录：余额两臂结构通过并经独立工程AI审核回答正确；其余六臂在首轮发生服务集成错误，未完成任务。不能把这轮当作1220条总体正确率，也不能称为已验证的新V2收益。

## 授权、渠道与冻结证据

用户选择千问网关，并明确确认现有 `AGENT_LLM_API_KEY` 属于该渠道。本轮通过显式驱动固定 `https://maas.qianwenaiapi.com/apps/anthropic`、`deepseek-v4.1-flash`、Anthropic协议，不向旧DeepSeek端点发送密钥。准备文件生成于确认之前，其中“等待确认”的表述保留作为时间顺序证据；后续确认见[价格及授权记录](../eval/paired-pilots/2026-10-14-four-task-v1/public-price-evidence.json)。账户余额/账单没有已知可查接口，未声称完成余额查询。

本次执行代码版本为 `225e5cead8799d544757701f6daa4138475584ef`，每臂使用隔离MySQL账户与私有Redis。任务、定义、profile、提示词、工具、端点和预算均绑定到归档；隔离环境关闭后清理。不覆盖旧1220/3660记录及12任务归档。

- [冻结配置](../eval/paired-pilots/2026-10-14-four-task-v1/attempt-01/configuration.json)
- [原始运行](../eval/paired-pilots/2026-10-14-four-task-v1/attempt-01/run.jsonl)、[原始报告](../eval/paired-pilots/2026-10-14-four-task-v1/attempt-01/report.json)
- [独立回答审核侧文件](../eval/paired-pilots/2026-10-14-four-task-v1/independent-review.json)、[费用回执](../eval/paired-pilots/2026-10-14-four-task-v1/attempt-01/execution-receipt.json)
- [实际执行驱动源码](../eval/paired-pilots/2026-10-14-four-task-v1/execution-driver.go.txt)、[摘要清单](../eval/paired-pilots/2026-10-14-four-task-v1/manifest.json)

## 任务边界和实际结果

| 任务 | 正确行为 | V1 | V2 |
|---|---|---|---|
| balance-001/answer_complete | 读取本人现金余额并准确回答 | 余额5000元，结构及独立回答审核通过 | 同样通过；JEV失败后交给模型 |
| balance-020/explicit_scope_followup | 确认现金还是token；明确现金后读取 | 首轮HTTP500，未到追问/第二轮 | 同左 |
| recharge-002/confirm | 100元预览→明确确认→仅一次模拟写入 | 首轮HTTP500，未到确认和执行 | 同左 |
| recharge-002/cancel | 100元预览→取消→零写入且清除pending | 首轮HTTP500，未到取消 | 同左 |

必要追问是正确行为，写操作需要先预览再确认，取消应零执行。本轮失败由客户端预算集成造成，不能据此判断模型不理解意图。充值确认终态的若干断言失败是首轮未完成的后果，不代表实际发生非法充值。六臂均未执行业务工具，本人及对照账户业务状态未改变。

原报告余额两臂仍为 `needs_review`，没有篡改成自动成功。侧文件逐项绑定回复/要求/日志SHA，由独立工程AI核对工具trace、权威余额500000分、现金单位和对照账户；不是人工盲审或独立金标。审核通过两条、集成失败六条属于本轮开发证据分布，不发布25%模型准确率。

## 调用、费用与时间

模型上游共10次HTTP请求，全部HTTP200；客服端另六臂HTTP500。两条余额各2次模型请求，其余各1次。失败臂遥测的第二次模型调用是客户端准备重试但被本地预算拒绝，不能计成已扣费上游请求。

全局限制14次、每次最多128输出token、请求16384字节、输入保守上限16384 token、人民币保护上限¥0.50；预留¥0.337920，实际usage按声明保守费率估算¥0.043220，输入合计17918 token（包括缓存读取）、输出923 token。预留不等于消费，估算不等于渠道账单。

采用千问公开忙时输入¥2/百万、输出¥8/百万，未使用缓存折扣或闲时折扣。价格来源：[官方模型页](https://www.qianwenai.com/models/deepseek-v4.1-flash)及其[引用配置](https://q.alyasset.com/lowcode/entry/prod/10e6274c39186c9d53b07631bdf0987e.js)。

JEV固定 `jev-1.13.0`，最多8次。实际上游仅1次失败后停止；四次Evaluator调用包含后三次本地停止拒绝，不是四次真实推理。没有保存具体上游错误/usage，失败原因与费用未知；不能归因为密钥失效、地区限制或置信度不足。只读 `/v1/models` 返回200仅证明该接口可认证，不证明推理成功或免费。官方输入USD0.042/百万、输出免费；8×64000输入的USD0.021504只是公开context价格包络，不是已强制的美元账单上限，人民币与美元不合并。[JEV官方模型说明](https://docs.typesafe.ai/models)

唯一余额配对的服务端总响应耗时为V1 3682.2017ms、V2 3014.297ms。它不是用户端完整耗时，V2实际回退模型、含JEV错误与不同缓存命中；一个开发样本不能证明JEV提速。其他失败臂比较报告耗时/usage未知，不填零、不排除失败后做收益统计。

## 缺陷与离线修复

Anthropic的max_tokens同时限制思考和回答；小输出限制下仅thinking耗尽会触发旧客户端自动扩容重试，128→256→512。本轮256请求被固定128预算拒绝。使用本机HTTP复现了三次扩容，再修复为：显式evaluation客户端每次Chat固定预算且无扩容重试，Anthropic发送 `thinking.type=disabled`，实际模式写入运行metadata并校验两臂配置一致。业务工具循环仍可能多次Chat，每次均受总次数/费用保护。普通服务默认行为未改。协议依据：[官方Anthropic接口](https://platform.qianwenai.com/docs/api-reference/chat/anthropic)。

新增测试覆盖仅thinking/max_tokens不扩容、显式disabled和声明模式缺失拒绝；旧归档不补写新字段。仅做离线修复和本地验证，**没有追加付费复测**，不能声称六臂已经真实跑通。

后续真实复测应使用新的归档目录与修复后版本，先恢复JEV可诊断的上游错误证据，再经预算授权复测小样本。本次保留所有失败与费用记录作为面试中“预算保护拦住隐式重试、如何发现和修复”的证据。
