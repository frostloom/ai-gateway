# Langfuse分阶段接入

本次授权：独立worktree下评估可行性和配置，暂不改另一会话的会话runner/生产路由。

已完成：调用链/telemetry/Go依赖审计；固定官方配置来源；独立六服务Compose与空密钥模板；临时假密钥渲染、缺密钥拒绝、隔离检查；可行性与证据边界文档。

下一实施单元：在internal/agent新增可关闭的OTel导出器，在cmd/agent/main.go初始化和限时关闭。先写本地HTTP接收替身的失败测试，核验默认关闭、格式授权、JEV-only、不完整usage、脱敏、超时/队列满及HTTP失败不影响业务，再实施。chat.go/telemetry.go/真实JEV与LLM调用边界最小埋点，不改评分器或1220归档。

部署验收单元：生成独立真实凭据、锁定镜像digest、起服务、合成span界面验证、实测开销和停机flush。不能把config检查当成此验收已完成。

暂不做：模型裁判、模型重跑、生产raw内容采集、共享数据库、合并main。所有模型/JEV付费调用为0。完整方案见docs/langfuse-feasibility-2026-10-13.md。
