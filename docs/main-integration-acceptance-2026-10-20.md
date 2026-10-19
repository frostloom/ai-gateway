# main 集成回归验收（2026-10-20）

本轮验证 main `04ee794350ff349e7ac1e8d6cd8dc21cfef21a54`。Go 回归于北京时间 2026-10-19 23:52 开始，2026-10-20 00:01 结束。目标是补齐此前因本机测试依赖未启动而受阻的集成回归；没有修改产品代码，也没有运行真实模型或 JEV 推理。

## 环境与隔离

启动现有 Compose 的 MySQL、Redis、Kafka，三者健康；没有执行 down、删卷或重建业务数据库。保留现有卷，依赖保持运行。

使用 Go 1.25.12、Windows/amd64 和仓库已有本地依赖。测试包以 `-p 1` 串行运行；Store 使用 `ai_gateway_test` / Redis DB1，Agent 使用 `ai_gateway_agent_test` / DB3，鉴权使用 DB2，Consumer 使用 `ai_gateway_consumer_test`。Taskbench 使用已有隔离夹具。夹具会清理自己的测试数据，不能与其他使用相同夹具的测试进程并行。

`AGENT_LIVE_JEV=0`、`BILLING_CASH_LOAD=0`、`BILLING_CASH_PROFILE=0`、`TASKBENCH_LANGFUSE_SMOKE=0`。不加载项目 `.env`，不启动客服服务或付费评测命令；推理接口由测试本地替身实现。

## 实际结果

| 检查 | 结果 |
|---|---|
| `go test ./... -p 1 -count=1 -json -timeout 5m` | 退出 0；17 个测试包通过；361 个顶层测试通过、0 失败、3 个显式跳过 |
| Go 子测试统计 | 包含父测试和子测试共 774 条通过事件，不能当作 774 个独立案例 |
| 真实 Kafka E2E | `TestConsumerKafkaE2E` 实际通过，10.74 秒，没有 Skip |
| `go vet ./...` | 退出 0 |
| `python -m unittest discover -s scripts/tests` | 退出 0，137 项通过 |
| `cd web; npm run verify` | 退出 0；类型检查、生产构建、前端运行时冒烟通过 |

Go 的另外 27 个包没有测试文件；JSON 输出将其记为 package skip，不能与测试用例跳过混淆。三个跳过的测试仅为 `TestLiveJevSemanticsDiagnostic`、`TestCashLoad`、`TestCashProfileRowTiming`，均按显式开关禁用。没有因 MySQL、Redis 或 Kafka 不可用而跳过的测试。

覆盖客服查询、必要追问、预览后确认、取消、重复确认、权限边界、JEV 分流及回退、资金一致性和恢复、网关流式结算、Kafka 消费、隔离账户与评测适配器。前端冒烟还验证挂起任务确认条、确认后的状态及审计风险标记。

## 证据与可复现性

[结构化结果](../eval/integration-acceptance/2026-10-20-main/summary.json) 与 [归档清单](../eval/integration-acceptance/2026-10-20-main/manifest.json) 保存本轮摘要和压缩前后 SHA256。目录中的 gzip 保留完整 Go JSONL、Python/前端日志、空的 vet 成功日志、依赖容器状态和运行范围。压缩数据已逐字节解压校验。

复现前确认没有另一个测试进程使用这些固定测试库及分区。启动依赖后设置上述四个开关及 `REDIS_ADDR=127.0.0.1:6381`，运行表中命令。`-timeout 5m` 是每个 Go 测试二进制的限制，不是整轮耗时；本轮串行执行约八分钟。

## 结论与后续

此前缺依赖导致的集成回归阻塞已解除，本轮 main 回归通过。这不代表新版客服完整任务成功率、外部网关可用性、线上 Langfuse 入库或生产容量验收。观测测试使用本地捕获；没有启用真实 Langfuse smoke。本轮也没有重新运行压测，既有资金锁排队与容量限制仍按此前性能报告处理。

1220 条真实付费评测继续暂缓。已有 1220 条定义、3660 条历史首轮输出和其他冻结证据没有改写；本轮不产生新版 V1/V2 准确率。下一阶段优先离线核对 JEV 分流覆盖、置信度阈值边界、安全回退和成本/延迟统计完整性，再独立申请真实调用预算。
