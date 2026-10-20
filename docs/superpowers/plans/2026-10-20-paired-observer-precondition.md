# 配对测评记录器前置检查 Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan inline. 用户要求自主继续、不提问，只在 main 开发，每完成验证通过的小目标提交推送。

**Goal:** recorded_run 未提供 Langfuse observer 时，在任何模型/JEV请求和配对运行日志创建之前失败。

**Architecture:** 复用 RunAgentPair 现有入口，将已有执行后检查移到执行前。保留 local_stub 的可选 observer、默认禁用行为、现有预算和 campaign 约束；不增加付费 CLI。

**Tech Stack:** Go、httptest、已有隔离 MySQL fixture、taskbench/observability。

**Spec:** 本轮运行前核对发现 RunAgentPair 到 runPreparedPair 返回后才检查 nil observer；历史脚本未传 observer，必须在真实调用前拒绝。用户已授权执行入口适配和本地验证，禁止真实付费调用。

## Global Constraints

- 只在 main；保留其他未提交改动，不新建分支或工作树。
- 所有测试模型/JEV均为本地替身；不读生产密钥，不执行真实推理。
- cmd/paired-eval 继续只有离线 prepare/preflight/report。
- 独立语义审核、账户价格核验和付费授权继续待完成。

## Review Focus

- nil observer 的 recorded_run 必须拒绝，即使其他执行参数齐备。
- 拒绝时模型请求、JEV工厂构造和预算尝试数都为零。
- 拒绝时不得创建 run journal、JEV journal 或运行锁。
- local_stub 缺少 observer 继续可执行，默认禁用入口不变。
- 显式 observer 的本地实际 handler 配对、Langfuse采集与 campaign 恢复继续通过已有集成测试。

## Task 1: 将记录器检查移到执行之前

**Files:** Modify `internal/taskbench/paired_adapter.go`; Create `internal/taskbench/paired_adapter_authorization_test.go`.

**Interfaces:** 保持 AgentPairOptions 和 RunAgentPair 签名不变；recorded_run 的必选 observer 由事后报错改为事前拒绝。

- [x] 写 TestRecordedAgentPairRequiresObserverBeforeExecution，使用随机隔离环境、本地 provider 和计数 JEV 工厂，缺少 observer 时断言明确错误、零请求/尝试、无日志和锁。
- [x] 运行该测试，确认旧实现确实调用替身或生成日志而失败。
- [x] 在 AllowExecution 检查之后、其他执行逻辑之前拒绝 recorded_run 的 nil observer；删除晚到的重复检查，local_stub 保留可选 observer。
- [x] 同一测试通过；运行全部 Go 回归与 go vet（进程级 SDKROOT=MacOSX26.5.sdk），记录具体跳过项。
- [x] 按 requesting-code-review 进行独立只读审查，处理实质问题。
- [x] 仅提交计划、入口和测试，普通推送 main，核对远程 SHA。

## 执行记录

开始前 HEAD：073cf833acb7b9563839fead868f33e4c15b2e0b。用户明确禁止提问，因此沿用既有 Go 架构与 main，原地实施。

2026-10-21 验证回执：RED 已复现旧实现的 2 次替身模型请求、1 次JEV构造和运行日志创建；修复后针对性测试通过。完整 `go test ./... -p 1 -count=1 -json -timeout 8m`：369 个顶层测试通过，3 个显式跳过，零失败；`go vet ./...` 通过。跳过项为 TestLiveJevSemanticsDiagnostic、TestCashLoad、TestCashProfileRowTiming。所有命令使用进程级 MacOSX26.5.sdk。独立只读审查未发现实质问题。

目标1提交 `d35d98c` 已普通推送；GitHub main SHA 已与本地完整核对一致。后续仅追加测评准备说明和结构化验证摘要，保留所有历史数据及原有无关改动。
