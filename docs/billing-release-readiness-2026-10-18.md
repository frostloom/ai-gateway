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

## 目标 2、3：待完成

压测与上线运行手册尚待验收，Kafka E2E 通过不代表生产吞吐或端到端模型评测已通过。
