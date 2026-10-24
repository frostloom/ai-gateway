# 客服与平台知识库容器

公共知识在 `knowledge/platform`，随 agent 同镜像发布。默认关闭检索；开启后校验 manifest，失败直接退出。修改正式知识应重新生成清单、测试并重建镜像；不支持运行时挂载目录覆盖。当前规则描述演示平台，不能当作真实支付承诺。

## 构建和启动（手动部署步骤，尚未自动执行）

```sh
docker build -f deploy/agent/Dockerfile --target runtime -t ai-gateway-agent-knowledge:local .
cp deploy/agent/env.example deploy/agent/.env.agent
chmod 600 deploy/agent/.env.agent
# 在本地填写匹配现有服务的配置和密钥，需要知识功能时设 AGENT_KNOWLEDGE_ENABLED=true。
docker compose --env-file deploy/agent/.env.agent -f deploy/agent/compose.yml --profile knowledge config --quiet
docker compose --env-file deploy/agent/.env.agent -f deploy/agent/compose.yml --profile knowledge up -d
```

必须同时使用 `--env-file`，使 Compose 变量插值和容器 env_file 一致。私密文件被 Git 和 Docker 上下文忽略，不能将实际 key 写进 Dockerfile。默认独立宿主端口19105；不得直接替换正在使用的9105客服。

本配置只部署 agent。容器不能通过 localhost 访问宿主机服务：示例使用 host.docker.internal，Linux 通过 host-gateway 映射；若连接同一 Compose 网络中的 MySQL/Redis，应改用 mysql:3306、redis:6379。宿主服务若只监听 loopback，须先验证容器可达，不能把 healthz 存活当作所有依赖可用。代理地址同理，企业 CA 须经审核加入镜像，不关闭 TLS 验证。

运行镜像为 scratch，CA证书来自构建镜像，UID/GID 65532；Compose 设置只读根文件系统、丢弃 capabilities，无知识可写卷。多阶段 build 使用 Go1.25.12。`smoke` target 额外包含验收替身，只用于测试；交付使用 runtime target。

## Langfuse

延续 AGENT_LANGFUSE_ENABLED、AGENT_LANGFUSE_CAPTURE_CONTENT 开关。开启正文后，可见 tool.search_platform_knowledge 参数、knowledge.chunk 原文片段、knowledge.request_context 实际包含的片段和 knowledge.request_outcome HTTP 响应/传输错误。默认关闭正文，仅导出固定的版本/ID/摘要/阶段；脱敏展示摘要与原文摘要分开，clipped/redacted 标记不能忽略。

`provider_response` 只说明收到HTTP响应，不证明业务成功、送达收费状态或模型采用片段。需要按 trace 读回验证摄入；队列丢失和采样会影响完整性。Langfuse 不是答案正确性的金标。

## 隔离验收

```sh
python3 scripts/tests/test_agent_container.py --validate
python3 scripts/tests/test_agent_container.py --smoke
```

smoke 创建随机 Compose 项目，独立临时 MySQL/Redis、本地 gRPC/HTTP 模型及 OTLP 替身，无宿主端口、无真实API key。只删除该随机项目自己的资源。替身 usage 为合成值，不作token/账单/模型质量证据。
