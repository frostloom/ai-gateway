# 计费审查修复与离线验证计划

> **For agentic workers:** 使用 superpowers:systematic-debugging、test-driven-development、dispatching-parallel-agents、requesting-code-review 和 verification-before-completion。独立文件并行，共享计费接口顺序集成。

**Goal:** 修复7c80493审查发现的五个问题，补足零API费用回归与CI，保留面试可用证据。

**Architecture:** Reserve同请求跨进程序列化，覆盖Redis扣减/SQL落账/失败补偿；结算标记版本化并兼容旧标记。网关只记录成功上游，ReportUsage追加可选provider_id，不改目录售价。启动脚本部分重启需要稳定控制令牌。评测仍仅离线契约和模拟服务。

**审查追加:** SQL连接丢失后需要健康连接重锁查账本；恢复连接再次丢失由Redis调用owner接管与补偿校验防护。SQL使用独立15秒上限，订阅路径不复用余额路径的固定连接。

**Tech Stack:** Go、MySQL、Redis Lua、protobuf、PowerShell、Python、GitHub Actions。

**Spec:** 用户授权修复五项审查问题、继续完善原有测试并记录变更；原有客服评测契约与预算约束继续有效。

## Global Constraints

- 不调用真实模型/JEV，不重跑1220在线评测；不读取或输出密钥。
- 使用隔离测试数据库和Redis DB，保留原档案、历史分数与原始证据。
- 不修改无关plate_sim.py；不把通过单测称为生产绝对正确。
- 来源审查与实际复现分开记录；记录修复机制、取舍和验证命令。

## Tasks

### 1. 预扣与结算

- [x] 失败测试复现同request并发成功/失败、取消context后补偿、旧标记重试。
- [x] mysql.go及Lua修复；避免简单查行或仅判断首次扣减的竞态修补。
- [x] 隔离测试核验Redis余额、账本、guard及幂等重试。

### 2. SSE及成功上游归属

- [x] 假503上游保持body打开，证明切换及退款不能等待EOF。
- [x] 追加ReportUsage可选provider_id，更新成功上游且保持售价快照。
- [x] SSE/非流式切换回归，旧客户端provider_id=0兼容。

### 3. 控制面启动

- [x] 部分重启缺稳定令牌时在启动任何服务前失败；配置令牌正常复用。
- [x] M5脚本认证新增控制面接口，本地替身行为验证。

### 4. 评测回归与面试记录

- [x] CI明确禁用在线诊断，增加Python契约/归档及Windows脚本离线回归。
- [x] 记录既有三次提交与本轮修复的触发、机制、测试、局限和后续测评。
- [x] 全Go/Python回归、静态检查、旧档案SHA和独立审查。
- [x] 提交并推送（最终是否成功以远程main及交付消息的提交SHA为准）。

验证证据见 `docs/interview/CHANGELOG.md`：Go全项目通过，最后一次所有权修复后独立store全量通过，Python67项、vet、三项归档校验通过。独立审查追加的恢复失联窗口已经覆盖；旧标记歧义及崩溃孤儿guard作为局限保留。
