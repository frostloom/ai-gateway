# Langfuse 采集链路恢复（2026-10-23）

本机 Langfuse 健康、认证及新客服 release 的零推理 trace 已恢复读回。历史部署回执的未确认状态保留，本次另存恢复证据，没有回填或覆盖旧记录；没有真实模型或 JEV 调用，没有推进或重跑付费评测。

## 调查与处置

最初 Web 重启计数已到 7，而 Worker、ClickHouse 和 Kafka 健康。随后 Docker API 的 `~/.docker/run/docker.sock` 短暂消失，`docker logs`、`docker inspect` 都返回无法连接引擎；因此故障期间的日志读取失败不能直接归为 Langfuse HTTP 故障。检查发现 Docker Desktop 后端进程正在运行，打开既有 Docker 应用并等待引擎就绪，API 恢复至 Docker 29.8.2。

引擎恢复后，Langfuse 六项既有服务全部达到 healthy。没有调整 Docker 全局资源、重置引擎数据、修改 Langfuse 镜像/配置/访问密钥或清理数据卷，也没有停止其他项目服务。

Web 日志反复停留在初始化片段，提取的日志没有明确的应用崩溃堆栈。旧 Docker VM/后端日志存在 `/analytics/track/oom-kills` 上报计数增长，但没有在已取得证据中确定被终止进程或对应容器；不能仅凭该端点名称、上报计数、空的容器 OOM 事件和 `OOMKilled=false` 就认定或排除具体 OOM。历史 Web 重启及此次引擎中断的完整根因尚未确认，本次证明的是当前链路恢复，不能宣称资源问题永久解决。

## 验证

1. Langfuse `/api/public/health?failIfDatabaseUnavailable=true` 和项目 scores API 认证通过。
2. 原部署时的错误 JSON 请求未找到匹配记录，继续保留“未确认”；本轮在服务健康后发送一次新的错误 JSON，请求在解析阶段返回 400，不进入身份解析、业务会话或推理。
3. 新 trace `ab923259cbb20718bea4a965fc758f4c` 已从 observations API 实际读回：`agent.turn`、release `80051a64d9abbd2c9b5bc0d16c0622753ebc3ae5-local`、`stage=request`、HTTP 400、`llm_calls=0`、`jev_calls=0`。
4. 仅读回原第二批四轮内容及 14 项确定性评分，匹配验证通过；未重复写评分，独立 failed 结论保持不变。

本次只有运维恢复和脱敏文档，不修改产品源码；验证采用实际健康/认证、解析拒绝请求、trace 与既有评分读回，不重复无关 Go 测试。原始诊断及独立恢复回执在被忽略的 `.tmp/langfuse-recovery-20261023/`，公开脱敏证据见 `deploy/local-services/langfuse-recovery-2026-10-23.json`。

上述检查不证明真实模型已遵守新的本轮现金读取守卫。后续真实复测仍需新计划与明确预算范围，不复用旧批次 claim。

六项服务在 90 秒观察窗口内全部保持 running/healthy，容器身份、启动时间和重启计数未变；这仅是短时稳定性证据，不是长期可靠性保证。
