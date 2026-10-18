# Cash Performance Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. Review with superpowers:requesting-code-review before completion.

**Goal:** 定量定位计费修复后的性能成本，减少重复账本读取，并用故障回归和不同账本规模验证。

**Architecture:** 延续用户批准的租户 MySQL 会话锁与 Redis owner fencing。只在测试层计时；先记录原版证据，再复用同锁内已计算的权威余额，继续读回 Redis 核对。

**Tech Stack:** Go、GORM、MySQL 8、Redis 7、Docker Desktop。

**Spec:** 用户已批准的三步范围：耗时剖析、最小优化、完整回归及更长窗口和不同账本规模复测；前序背景见 [readiness](../../billing-release-readiness-2026-10-18.md)。

## Global Constraints

- 沿用当前工作区与 main；每个完成的小目标分别提交并推送 GitHub。
- 保留 plate_sim.py；不部署生产，不运行付费模型或 JEV。
- 所有数据库测试独占 ai_gateway_test 和 Redis DB 1；不得并行运行 Store 测试进程。
- 保留租户锁、owner fence、孤儿 guard 恢复、持久幂等与已提交流水。

## Review Focus

- 失联旧 owner 不得覆盖新持有者余额：复跑 cash_concurrency_test.go。
- 已提交账目遇到 Redis 故障不得删除：复跑充值、退款和恢复测试。
- Redis 读回错误或不一致必须拒绝：增加 requireCashMatch 故障用例。
- 取消语义、prepared fallback 与 rows 生命周期不得被测试计时改变：保留 DriverContext，实际 SQL 校准。
- 累计并发耗时不是 wall 占比；失败样本、drain 和预热必须明确记录。

## Task 1: 测试层耗时剖析

**Files:** cash_load_test.go、cash_profile_test.go、docs/billing-cash-performance-2026-10-18.md、eval/billing-readiness/2026-10-18/。

- [x] 包装 SQL driver 和 Redis hook，统计执行、prepare、commit、锁往返、池等待和三个阶段。
- [x] 独立审查并修复 DriverContext 转发；校准 SELECT ? 单次计数与 rows 持有时间。
- [x] 六场景采样及复测；保留失败样本，解释瓶颈和限制；提交推送。

## Task 2: 同锁内复用权威余额

**Files:** internal/billing/store/plan.go、cash_concurrency_test.go、cash_profile_test.go。

- [x] 新增回归，验证每次核验仅一次账本聚合，并继续拒绝 Redis 读回故障和不一致；先运行，观察重复查询断言失败。
- [x] requireCashMatch 使用 RebuildBalance 返回的 ledger；保留 getBalance 与比较。
- [x] 聚焦回归、完整 Go 回归、vet、独立审查；提交推送。

## Task 3: 扩展性能验收

**Files:** cash_load_test.go、性能报告、原始 JSONL。

- [x] 支持显式选择历史账本规模；预置费用纳入独立余额预期和最终 settle 行数核验。
- [x] 相同环境下对比优化前后，使用更长窗口和两种账本规模；记录所有错误和限制。
- [x] 更新上线判断；审核、提交推送。

## Execution ledger

- 基线 HEAD 39fd16d。Task 1 期间 Docker 无法启动；日志确定为残留 dockerInference/Secrets Engine socket。仅备份运行端点目录后重启，引擎恢复，三个测试依赖均 healthy，原容器和卷保留。
- 第一轮 3s 采样：六场景完成，单租户32 worker 锁超时、15个 pending，SQL 与 Redis 相等但不满足‘全部生命周期完成’验收；原样保留。事务提交最长约5.8s，因此不得把全部回退归因于聚合查询。
- 校准测试已通过。计时按并发累计，stage 包含其内部 SQL/Redis，禁止跨层相加。
- Task 1 提交 1b3102d；HTTPS TLS EOF 后，以官方公钥严格验证的 SSH443 成功推送，不改全局配置。Task 2 新回归先红后绿；完整 Go 回归17包通过、0失败，vet通过，独立只读审查无新增 P1/P2。
- Task 2 提交并推送 2f16bd0。Task 3 同探针10s、history0/1000四轮24组全部退出0，共3987个成功周期；资金/操作/profile错误均0，query预算12/4/39→6/2/31逐行验证。历史探针smoke与vet通过；最终独立审查无新增P1/P2，数据和表格均重新核算通过，提交推送。仍暂缓生产放量。
