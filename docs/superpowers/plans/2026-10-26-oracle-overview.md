# 配对Oracle单次采集复用总览

> For agentic workers: use executing-plans inline; one final reviewer via requesting-code-review.

**Goal:** 在保持独立语义校验及每次新读的前提下减少重复总览查询。
**Architecture:** LiveBackend私有snapshotWithOverview返回同次读取；私有authority辅助只接收同租户这次总览。
公开入口各自保留新读，不新增跨调用缓存。仅配对Oracle合并本人重复读取。
**Tech Stack:** 现有Go/Gorm/MySQL/miniredis，无新依赖。
**Spec:** docs/repeated-diagnostic-health-2026-10-26.md的回归阻塞及下述验收要求。

目标：减少同一次配对状态采集的重复数据库查询，保持每次调用新读及原语义断言。
仅修改taskbench；客服、账务实现、case/预算/运行合同、测试调度和各调用时限保持原样。
本轮默认600秒累计回归失败保留后，用户明确选择Go整套测试累计窗口20分钟；
最终命令为 `go test ./... -count=1 -timeout=20m -json`，真实付费测评仍5分钟。

依据：私密阶段计时的正常完整场景runner22944.889ms，200次State累计12013.965ms，
120次journal写入555.088ms；当前State对本人读取三次MeOverview、控制账户一次。
采样计时随宿主负载变化，不用跨轮耗时差证明因果或历史根因。

- [x] RED：真实隔离库Gorm查询计数，期望每次State只有两次完整套餐目录读取，
   当前本人三次+控制一次应实际得到4；保留错误与原日志。
   同时验证公开Snapshot/Authority结果一致、后续State读取目录/账单更新、身份和错误传播。
- [x] GREEN：私有Snapshot辅助返回本次新取的MeView；同次State用于余额、Allowance及Authority。
   公开Snapshot和CaptureConversationAnswerAuthority各自仍独立获取新总览；不跨State缓存。
   目录/消费来自同租户MeView，账单仍独立查询，非事务原子快照。
- [x] 完整新矩阵/旧矩阵与原六案例、race定向、用户批准20分钟的完整Go回归；单次独立复审。
   原私密/公开SHA与七claim、最终隔离库、客服源码逐项核对；提交并直推main。
   真实执行仍须回归成功后另行按最终HEAD归档冻结、明确新付费范围批准。

用户已授权自主迭代和直接main推送，不逐项请求方法确认。失败不得改判成功或降低门禁。
