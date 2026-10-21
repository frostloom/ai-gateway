# 真实小样本测评准备（2026-10-21）

同日后续：[价格、预算与本地链路的新核验](paired-prepaid-readiness-2026-10-21.md)已完成；[Hindsight排查](hindsight-write-diagnosis-2026-10-21.md)已确认写入成功，本文末尾的超时描述保留为当时状态。

本轮在 `main` 修复并验证了执行前的 Langfuse 记录器检查，代码提交为 `d35d98c`。仍未开始新的真实模型/JEV测评；`allow_paid=false`、`ready_for_paid_run=false`。继续开发、提交和推送的授权不包含付费推理。

## 已完成的入口修复

`RunAgentPair` 之前在配对执行结束后才检查 recorded_run 是否传入 observer。回归测试用随机隔离的本机 MySQL 环境和 loopback 替身复现了 2 次模型请求、1 次 JEV 工厂构造及运行日志创建。这些是本地测试调用，不是外部推理。

现在缺少 observer 时在执行之前拒绝：模型/JEV调用、预算尝试数均为零，run/JEV日志和运行锁均不创建。默认禁用行为、local_stub 可选 observer、提示词、分流规则及写操作确认规则保持原有语义。显式 observer 的完整 handler、隔离账户、OTLP捕获和 campaign 流程仍通过集成测试。

当前 CLI `cmd/paired-eval` 继续只支持离线 prepare/preflight/report；没有增加 live 模式。历史 `execution-driver.go.txt` 写死旧版本、旧输出路径和旧案例选择，且未传当前所需 observer，不能直接重跑。后续授权运行应通过显式调用 `RunCampaignBatch`/`RunAgentPair`，传入已核验的当前配置、独立新日志和 observer；不改旧归档来伪装成新运行。

## 冻结的小样本范围

V1 为 `model`，V2 为 `jev_selective`；同一渠道、模型、协议、提示词和工具契约。当前离线配置为 `deepseek-v4.1-flash`、Anthropic 协议、输出上限128、thinking disabled、JEV固定 `jev-1.13.0`。这些是配置绑定，不证明外部渠道或模型版本当前可用。

|案例|场景|每臂用户轮次|
|---|---|---:|
|balance-001|answer_complete|1|
|plans-001|answer_complete|1|
|bills-001|answer_complete|1|
|consumption-001|answer_complete|1|
|subscription-001|answer_complete|1|
|balance-020|explicit_scope_followup|2|
|recharge-001|supplement_then_confirm|3|
|recharge-002|confirm|2|
|recharge-002|cancel|2|
|recharge-002|duplicate_confirm|3|
|recharge-002|cancel_then_confirm|3|
|refund-001|balance_cap|3|

合计12案例、24对照臂、46个用户轮次。24次模型 HTTP 尝试和12次 JEV 调用只能作为有限试跑上限，不能保证24臂全部完成。工具回填和答复修正可能多次请求模型；确认/取消及直读路径也可能少请求。预算耗尽时保留未完成状态，不能将已记录臂等同于成功任务。

## 现有预算机制及提案边界

- `PairBudgetTransport` 限制模型请求次数、字节数、输出token和按声明输入上限计算的人民币预留；usage缺失、超限或收费不明后阻止后续模型尝试。
- `boundedPairJEV` 持久化限制JEV调用数、固定返回版本；错误或中断后阻止后续JEV请求。它本身没有美元预算控制，也不保证同时停止所有模型回退请求。
- `CampaignBudget` 已支持模型人民币、JEV美元的分开预留，整批预留不退款。应复用该层，不能说项目完全没有JEV金额预算机制。费率和每次收费范围仍是操作者声明，不是账户账单保证。
- `RunCampaignBatch` 会在批次结束检查结构、完成范围及持久化JEV错误，失败后阻止后续批次扩张；这不等于首次JEV错误发生时立刻终止本批全部模型回退。

此前提案按输入每百万token 2元、输出8元、每次输入预留16384/输出128估算：24次模型尝试预留0.811008元，提议模型预算1元。JEV按每百万输入token 0.042美元、每次64000输入token估算，12次为0.032256美元；可在campaign中分别声明美元预留总额和每次2688微美元的收费范围。以上沿用本会话此前公开价格证据，**本轮没有重新核验公开报价、账户专属费率、失败收费或输入分词上限，不是可直接执行的付费授权**。

## 验证回执

|检查|结果|
|---|---|
|测试先失败再修复|已复现旧执行副作用；修复后同一回归用例通过|
|针对性入口/默认禁用/完整配对测试|通过|
|全量 `go test ./... -p 1 -count=1 -json -timeout 8m`|369个顶层通过，零失败，3个显式跳过；17个测试包通过|
|`go vet ./...`|退出0|
|独立只读代码审查|未发现实质问题|
|修复提交后的离线 preflight|12案例/24臂/46轮，禁止付费，provider_calls=0|

三个显式跳过项为 `TestLiveJevSemanticsDiagnostic`、`TestCashLoad`、`TestCashProfileRowTiming`。另有27个包无测试文件，不能计为测试用例跳过。Go命令使用进程级 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk`，未修改系统默认SDK。

[结构化核对结果](../eval/paired-pilots/2026-10-21-recorded-observer-readiness/verification.json)记录代码版本、冻结定义摘要和验证计数。完整新测试日志保存在本机 `.superpowers/sdd/2026-10-20-paired-observer-precondition/`，新preflight保存在本机 `.tmp/paired-readiness-20261021/`；这两处不是GitHub共享归档。

## 继续执行的条件

后续先核验账户费率和失败收费、模型/JEV可用版本及请求输入上限，再冻结具体案例和允许的调用次数、分开币种的预算。驱动必须显式构造可用的 evaluation observer，传入当前代码版本和配置绑定，使用新的campaign/预算/运行日志；不得复用历史执行回执。

明确费用范围并获得实际调用授权后才允许 `allow_paid=true`。未获得授权时继续只做离线准备。实际运行后需读回Langfuse trace、调用和评分，并独立审核回答；结构通过、导出成功、必要追问存在，都不能直接当成新版模型准确率。

本会话此前对 `c939684` 的本机恢复记录：Langfuse六容器健康，1220定义读回，本地替身的12 trace、26 turn、20 generation和84评分读回通过；匹配已有持久化数据的私密配置已恢复到主目录并被Git忽略。这是此前的验证记录，不是本次入口修复后又进行了一次真实模型测评。旧启动脚本仍引用旧工作树配置，保留旧目录，迁移并验证依赖后再清理。

## 项目记忆与交付

开发约定已保存在根目录 `AGENTS.md` 并推送；Hindsight 的 `ai-gateway` 记忆读取成功。本轮同步写入长期规则和完成记录的请求再次超时，尚未确认入库成功；不把超时当成保存成功。项目约定、代码提交和本文保留可交接结论，不影响后续本地工作。
