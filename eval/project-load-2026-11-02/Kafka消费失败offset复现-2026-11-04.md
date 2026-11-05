# Kafka 审计消费失败后的 offset 复现

## 结论和风险

修复前的消费循环不能保证注释所说的“落库失败后重投”。同分区的第一条消息审计写入失败后，循环继续读取并提交第二条；该分区的消费组 offset 因而越过第一条，重启消费组后第一条不会自动重投。这可能使 `audit_events` 缺少事件；账单和 outbox 的持久化不因此自动丢失，但需要按事件核对并修复审计缺口。Kafka 中消息是否仍可人工重放取决于保留时间和具体操作。当前工作树修复及验证见下文。

修复前代码见 `red.json` 中记录的源码哈希及 Git 差异：失败后继续 Fetch，并在后续成功时提交；`internal/pkg/kafka/consumer.go` 使用 kafka-go 消费组、1 秒异步提交。对于同一分区，提交后续 offset 会覆盖更早的位置。审计表的 `event_id` 唯一约束可以吸收重投，但不能补救被跳过的消息。

## 实测及边界

运行 `python3 eval/project-load-2026-11-02/kafka-offset-repro/run.py`，脚本创建随机名字、仅本机绑定的独立 Kafka 3.7.0 容器和单分区 topic，运行结束删除自己创建的容器。HEAD、镜像 ID、程序 SHA、输出与返回码见同目录 `kafka-offset-repro/result.json`。没有连接既有 Kafka、数据库或模型。

实际使用项目 `internal/pkg/kafka` 的 producer 和 consumer：发送同 key 的两条消息，按现有循环模拟第一条审计失败（不提交）、第二条成功（提交），关闭消费端后用同一消费组重新打开。第一条 offset 0，第二条 offset 1；重启后 8 秒内没有重投第一条，程序退出 0。本实验注入的是**处理结果**，没有运行真实 `EventConsumer.handle` 或 MySQL，因此它验证了 Kafka offset 机制和现有循环的组合后果，并非全链路数据库故障注入。应进一步用真实 consumer、隔离 MySQL 注入第一次写失败复测，并覆盖 rebalance、异步提交失败、进程崩溃。

另一个已记录的独立问题：`eval/project-load-2026-11-02/kafka-partition-output.txt` 显示同一请求的 reserve、usage、settle 使用不同 event key 时可落不同分区。不能假设这几个阶段跨分区按顺序消费。

## 当前工作树修复与验证

`internal/billing/consumer/consumer.go` 对当前消息的审计写入失败进行每秒一次、可取消的重试，成功后才继续读取后续消息。原消费端本身串行处理所有分区，因此失败期间整个消费者暂停，可能积压；这一方案优先防止越过失败记录，尚未测定恢复速度和吞吐影响。

真实 consumer、独立单分区 Kafka 3.7.0 和独立 MySQL 8.0.40 的测试见 `kafka-offset-repro/run_consumer.py`。通过 GORM callback 在写 audit 前注入错误，数据库和 Kafka 均真实运行，但不是实际数据库断网或服务宕机。运行：

```sh
python3 eval/project-load-2026-11-02/kafka-offset-repro/run_consumer.py trial
```

原代码的 `red.json`：首次写入失败后只存在 `second`，测试失败。相同测试的 `green.json`：重试后 `first` 和 `second` 均存在，测试通过。扩展测试 `recovery.json`：对第三条持续注入失败，取消消费者并关闭，再移除故障、用同一消费组重启，第三条恢复入库；包含取消时退出断言。测试代码、源文件哈希及原始日志与这些 JSON 同目录。各次运行使用全新容器和消费组，未触及已有服务。

扩展测试以 `--race` 重跑通过，见 `recovery-race.json`；本机链接器输出一条 `LC_DYSYMTAB` 警告。outbox 包的 race 测试通过，Kafka 客户端包没有测试；consumer 普通编译检查通过。未运行全项目测试：既有 consumer 集成测试硬编码已有 MySQL 和 Kafka 地址，不能直接在本机执行。

尚未覆盖真实 MySQL 故障、broker 故障、rebalance 并发、进程被强杀、提交确认丢失、跨分区长期阻塞及恢复后性能。消息内容损坏目前直接提交跳过，属于另一项明确的数据保留策略风险；还需要以 outbox 和 audit 的事件 ID 做持续缺口核对。此修复并不证明整条账务链路或生产部署已安全。
