# 计费修复上线前验收（2026-10-18）

产品修复版本：`02a226baedb53795bb82b4d540dacd2c7c853c63`。本轮只做本地验收与运行说明，不部署生产、不运行真实模型/JEV付费评测，也不代表 `full1220` 验收通过。

## 目标与顺序

1. 补齐真实 Kafka E2E，排除历史消息误通过，提交推送。
2. 对修复前 `5054b58` 和修复后 `02a226b` 用同一测试探针，比较单租户/多租户并发性能并核验资金不变量，提交推送。
3. 基于实测结果整理上线、暂停写入、核对和回滚步骤，独立审查后提交推送。

沿用当前工作区，保留 `plate_sim.py`。每个目标以实际命令、退出码与结果记录验收。

## 目标 1：Kafka E2E 已完成

保留现有 MySQL、Redis 和 Kafka 数据卷，执行：

```powershell
docker compose up -d --wait kafka
docker compose run --rm kafka-init
$env:AGENT_LIVE_JEV='0'
go test ./internal/billing/consumer -count=2 -v -timeout 90s
```

退出码 0，两轮 `TestConsumerKafkaE2E` 分别通过（10.78s、10.45s），包耗时 26.106s，没有 Skip。验证真实生产、消费、审计入库、重复投递不增加行数，以及 partition/offset/payload 字段。

原测试使用固定事件 ID 并读取共享 topic 历史；本次改为每轮唯一的临时 topic、消费组和事件 ID，测试结束后先取消并等待消费者退出，再关闭连接并删除本轮 topic。独立审查未发现确认的 P1/P2。

数据库仅使用 `ai_gateway_consumer_test`，只清该库的审计、outbox 测试表；业务库 `ai_gateway` 未清理。该固定测试库需要独占，不得与另一个 consumer 测试进程并行。新测试不向共享业务 topic 写事件。首次补跑旧测试已在本地共享 topic 写入其既有 `e2e` 事件；未删除共享 topic 或其历史。

原始结果：[kafka-e2e.txt](../eval/billing-readiness/2026-10-18/kafka-e2e.txt)。Kafka 本轮启动后保持运行，不自动停止或删除已有卷。

## 目标 2：本地现金链路对照压测已完成

结果是**资金断言通过、性能显著回退，暂不建议生产放量**。这不是生产容量认证，也不是旧版资金安全认证；旧版在本负载中没有并发重建或退款故障，不能以较快为由恢复其危险路径。

同一 Windows/amd64、Go 1.25.12、本地 Docker MySQL 8.0.40/Redis 7、`GOMAXPROCS=4`、SQL 池上限 40，串行运行修复版后再运行基线。每个场景预热每租户一次完整周期，预热不计时；计时窗口 5 秒，停止接收新周期后等待在途完成。每个版本六场景、两轮，共 12 条完整测量。修复版 112.859s、基线 95.395s，均退出码 0、无 Skip，含测试建库/迁移/预热的总耗时不是压测吞吐分母。

完整周期是直接调用 Store 的 `Reserve(100)` → `ReportUsage(50)` → `Settle(actual=50)`。不经过 HTTP、gRPC、网关、上游模型或 Kafka outbox 发布器；不可与旧 HTTP 压测的 req/s 直接比较。多租户场景按周期轮转到 8 个租户；1 worker 时始终只有一个在途周期。

表中吞吐是两轮成功完整周期/秒的算术平均；P99 是各轮 P99 的范围，不是把两轮合并后重新计算的分位数。

| 租户 | 并发 worker | 基线周期/秒 | 修复后周期/秒 | 变化 | 修复后 P99（ms） |
|---|---:|---:|---:|---:|---:|
| 1 | 1 | 12.56 | 5.62 | −55.2% | 178.6–231.2 |
| 1 | 8 | 84.91 | 7.05 | −91.7% | 1065.2–1337.9 |
| 1 | 32 | 203.93 | 6.61 | −96.8% | 5028.3–5036.1 |
| 8 | 1 | 13.59 | 5.69 | −58.1% | 179.0–225.1 |
| 8 | 8 | 92.95 | 38.56 | −58.5% | 204.2–404.3 |
| 8 | 32 | 200.55 | 44.82 | −77.7% | 758.8–1063.8 |

修复版测量期间（含 drain）完成 1232 个周期，基线 6208 个；另有每版本 54 个预热周期。两版全部场景均为 0 操作错误、0 资金断言错误、0 pending、0 重复 request/phase；settle 行数等于成功周期加预热数。各租户 Redis、SQL 账本和独立预期 `initial − 50 × 成功周期数（含预热）` 三者一致且非负，验证在最终状态完成，不覆盖任意时刻的瞬时余额。

延迟包括每次完整周期及其排队时间；吞吐分母包含停止发起请求后的 drain 时间，只计成功完成数。分位数使用排序后的索引 `floor((N−1)×p/100)`，错误尝试也进入延迟数组，但本次所有样本均无错误。5 秒窗口、两轮、本机短账本且未平衡版本运行顺序，只能用于发现本次明显回退，不能推算长期线上 SLO、热租户容量或大账本性能。

静态代码可见，每次现金预扣和终态变更都取得租户锁，并在 `requireCashMatch` 内无条件重建和再次核验账本；单次该函数两次调用四条查询组成的 `LedgerBalance`。重复聚合和租户串行排队是待剖析的候选成本，本轮没有用 profiling 定量拆分，不能把全部差值归因于某一项。后续应保留资金保护，针对这两部分做设计和测量，再用同一探针及故障回归验证。

### 复现与证据

探针：[cash_load_test.go](../internal/billing/store/cash_load_test.go)，默认跳过；显式运行：

```powershell
$env:AGENT_LIVE_JEV='0'
$env:REDIS_ADDR='127.0.0.1:6381'
$env:GOMAXPROCS='4'
$env:BILLING_CASH_LOAD='1'
$env:BILLING_CASH_LOAD_DURATION='5s'
$env:BILLING_CASH_LOAD_TAG='fixed-02a226b'
go test ./internal/billing/store -run '^TestCashLoad$' -count=2 -v -timeout 4m
```

必须独占 MySQL `ai_gateway_test`、Redis DB 1；禁止与任何另一个 Store 测试进程并行。每个子场景会清理固定测试库及 DB 1 的 `billing:*`、`usage:*`；不得改为业务库或 DB 0。运行之后当前进程如需执行普通回归，应移除三个 `BILLING_CASH_LOAD*` 环境变量，避免意外附带压测。

本轮基线通过 `git archive 5054b58 go.mod go.sum internal` 提取到忽略目录 `.tmp/cash-baseline-5054b58`，仅复制相同探针后运行相同命令（tag 改为 `baseline-5054b58`）。主工作区未切换旧版本。两份探针 SHA256 均为 `426872623996c7aa4a59a12bcb1be85cbdbff5ba4822ef4ef9f0fde28743fae2`。

原始每轮测量：[修复版 JSONL](../eval/billing-readiness/2026-10-18/cash-load-fixed-02a226b.jsonl)、[基线 JSONL](../eval/billing-readiness/2026-10-18/cash-load-baseline-5054b58.jsonl)。已检查每个文件恰为 12 条，六种 case 每种两条，completed > 0，errors/invariant_errors/pending/duplicate_phases 全为 0。Redis 不可用时底层测试夹具可能 Skip，因此不能仅凭 `go test` 退出码判断压测已实际运行。`go vet ./internal/billing/store ./internal/billing/consumer` 已通过。独立审查重新核算全部表格及原始数据，未发现确认的 P1/P2 阻断。

## 目标 3：待完成

上线与回滚运行手册待独立审查；当前性能结果不足以支持生产放量。
