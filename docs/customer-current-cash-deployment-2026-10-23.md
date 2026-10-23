# 本轮现金余额修复部署（2026-10-23）

按用户继续推进的要求，本机客服已从旧 `af0ba14` 构建更新至修复提交 `80051a64d9abbd2c9b5bc0d16c0622753ebc3ae5`。本轮没有真实模型或 JEV 推理，没有重跑已消耗的评测批次。

## 构建与部署

仅从已通过全项目 18 包回归与独立复审的提交源码归档构建 `cmd/agent`，使用 `-mod=readonly -trimpath -buildvcs=false` 和 MacOSX26.5.sdk，不包含工作区已有前端资产或临时探针。归档 SHA、tree、二进制 SHA 与实际安装哈希一致，见 `deploy/local-services/customer-current-cash-deployment-2026-10-23.json`。该构建无 Go VCS stamp，使用明确的本机来源清单，不冒充 pristine paid executor 构建。

备份旧 `bin/agent` 与主目录、旧工作树的两份 `.env.agent` 到被忽略的 `.tmp/customer-deploy-20261023/backup/`，私密配置权限 0600。原子替换客服二进制，只更新两份配置的 `AGENT_RELEASE=80051a64d9abbd2c9b5bc0d16c0622753ebc3ae5-local`；模型地址、协议、模型名和密钥未变。旧工作树和启动脚本保留。经原 `local.ai-gateway` launchd 启动器 SIGTERM 温和重启，PID 已变化，新运行进程四项模型配置与根 `.env` 内存比较全部一致，release 匹配。

## 启动依赖修复

原 Kafka 已停止，启动器重启时等待其健康。原健康探针启动 JVM 执行 topic 只读查询，但超时只有 5 秒；单独执行同一查询分别耗时 18.84 和 24.02 秒，均退出 0，返回 3 个 topic。将 Compose Kafka 探针间隔从 5 秒调为 15 秒、超时从 5 秒调为 30 秒，保留原 30 次重试、镜像、业务配置、topic 和数据卷；Compose 配置校验通过，实际 Kafka 达到 healthy，kafka-init 启动成功。

应用 Compose 更新时原等待中的启动器记录旧 Kafka 容器退出（137），发生在重新创建旧容器期间；不能把它直接归为 OOM。新的 Kafka 通过健康检查。近期 Docker OOM 事件查询为空。

## 运行验证及限制

- 客服 `127.0.0.1:9105/healthz`、网关 `127.0.0.1:18080/portal` 均返回 200。
- 一个格式错误的 `/chat` JSON 请求返回 400，在解析阶段结束，不进入鉴权、业务会话或推理流程。
- 验证期间 Langfuse Web 多次重启、API 读回间歇不可用；如回执中 `trace_readback_confirmed=false`，则新 release 遥测尚未实际读回，不以本轮健康检查代替采集链路通过。具体重启根因未确认，不凭 CPU 占用或同时变慢就认定内存耗尽。

部署确认修复代码已加载，不能证明真实模型已按预期重新读余额。下一轮真实复测仍需绑定新代码、新执行计划及明确预算授权，不能清理旧 claim 或用原第二批输出目录重跑。回滚所需旧二进制与私密配置均保留，不能提交备份内容或密钥。
