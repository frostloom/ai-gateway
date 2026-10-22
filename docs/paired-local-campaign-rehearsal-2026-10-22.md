# 六批顺序本地演练（2026-10-22）

`RunLocalCampaign` 将既有六批计划串成一次本地演练：逐批预留预算、执行真实 handler/工具、重放评分、核对本机 OTLP，再开始下一批。公共参数仅接受冻结定义、执行版本、计划和已拥有的隔离 fixture；没有模型地址、密钥、JEV 工厂或外部 observer 参数，也不读取这些凭据的环境变量。

模型由进程内 Anthropic transport 返回合成响应，JEV 为固定 Fake，observer 只向自身创建的 loopback 接收器导出。业务使用项目现有独占 MySQL schema、私有 Redis 和每臂独立账户；调用者仍负责关闭 fixture。没有外部模型或 JEV 推理，也不增加付费 CLI。

## 重复运行

本机 MySQL fixture 服务需在 `127.0.0.1:3307` 可用，沿用既有测试账号；测试不会附着应用数据库。正常演练使用临时输出并自动清理：

```sh
SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk \
  go test ./internal/taskbench -run '^TestLocalCampaignSixBatches$' -count=1 -v
```

如需保留账本和读回报告，先在仓库根目录新建一个空的本地证据目录，将它的绝对路径通过 `TASKBENCH_LOCAL_CAMPAIGN_RECEIPTS` 传入同一命令。测试在其下新建 `six-batches/`；已存在时拒绝运行，失败后也不能复用。该参数只指定本地文件位置。测试版本标记为 `test-local-rehearsal`，不是经过审查的真实模型执行版本；归档时须同时记录源码哈希。

## 检查与停止

六案例顺序沿用[计划](../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json)。每批完成后读取 `run.jsonl`、模型预算及 JEV 账本，再生成 `report.json`；补查事实错误、HTTP/结构失败、模型 usage 缺失、预算停止及 task/turn/generation 的 trace/案例/臂关联和数量。语义审核保持 `needs_review`，不会因为演练通过发布质量或收益结论。

`receipt.json` 保留全部计划批次，区分 `checked_local`、`failed`、`not_started`；出错时保存失败阶段和部分报告，不保存依赖错误正文。任何失败、中断、导出/读回/写盘问题都停止后续批次，不自动重试、不重新分配预留。最终回执自身写入失败会返回错误和 stopped 状态，但不能保证磁盘存在一份有效回执，调用者必须检查错误。

原有 `campaign.jsonl` 的 finish 表示单批执行结果；外层报告、OTLP检查或文件写入失败可能发生在此之后。**判断整轮演练必须结合最终回执和返回错误，不能仅凭单批 finish。**运行器始终拒绝已有输出目录，不会据此续跑后面的批次。

金额只用于模拟沿用计划中的分币种声明预留；模型 usage 是合成值。FakeJEV 没有计费 usage，因此报告中的全系统 token 合计仍可能为 unknown，不能补零或当作真实计费证据。OTLP 是本地替身读回，不是新一轮实际 Langfuse 摄入。

## 验证记录

功能测试已覆盖六批、12臂、26轮、20次合成模型尝试、6次FakeJEV，OTLP任务/轮次/generation为12/26/20，外部推理为零。中途预算耗尽、模型HTTP失败、usage缺失、JEV失败、OTLP导出/接收失败、报告和最终回执写入失败，以及事实检查均有定向测试。

全项目 Go 回归通过379个顶层测试、17个测试包，零失败；3项显式跳过为真实JEV诊断、现金负载探针和行锁剖析。`go vet ./internal/taskbench ./cmd/paired-eval` 通过；独立只读审查未发现 Critical/Important/Minor 问题。保留的六批演练证据核对了13条有序campaign事件、六份报告/日志SHA、0600文件/0700目录及锁文件清理。命令、源码和回执哈希见[核验记录](../eval/paired-pilots/2026-10-22-local-campaign-rehearsal/verification.json)。

`allow_paid=false`、`ready_for_paid_run=false`。真实小样本仍需[账户计费核查](paired-account-readonly-2026-10-22.md)所列的收费包络和明确预算授权；本次演练不解除这些条件。
