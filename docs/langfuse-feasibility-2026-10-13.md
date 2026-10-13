# Langfuse 接入可行性与配置（2026-10-13）

## 结论与当前状态

可行。工作分支codex/langfuse-observability基于7ae4ad9，独立worktree。当前仅交付部署配置与接入设计：Docker Compose渲染通过，没有拉镜像、启动容器、创建账户、配置真实密钥、修改客服生产代码或导入1220条历史记录。当前agent没有OpenTelemetry导出器，仅设置OTEL变量不会自动产生Langfuse数据。

现有internal/agent/telemetry.go已有每轮模型/JEV调用与错误、token及缺失标记、缓存用量、JEV置信度/fast path/原因、总耗时及阶段耗时；chat.go已有read/preview/execute工具阶段和session。go.mod暂无直接OTel依赖。Prometheus继续承接基础运行指标，Langfuse承接可追溯会话观测和审核记录。当前阶段无需LangChain、Python代理层或改客服决策逻辑。

## 版本与隔离

官方文档当前为v4，OTLP HTTP支持JSON/protobuf，不支持OTLP gRPC。Go使用官方OpenTelemetry SDK和HTTP exporter。部署参考官方Compose提交e3d264c38ec79e0c76746e92a3718be639002ce1，来源SHA记录在upstream.json。镜像使用该配置的官方版本标签（web/worker :4等），标签可变化；这是本地试用配置，生产前必须锁定验证过的镜像digest并复测，不能宣称版本完全固定。

六服务：langfuse-web、langfuse-worker、PostgreSQL17、ClickHouse25.12、Redis7、MinIO。使用独立Compose项目ai-gateway-langfuse和项目管理的数据卷、网络，无固定container_name，不复用ai-gateway MySQL/Redis。仅127.0.0.1:13000界面与127.0.0.1:19090对象存储对外映射，数据库/worker不映射宿主端口。查看Docker端口未发现这两个默认端口占用，实际启动前再次检查。

已有gateway docker-compose.yml不改；不要把本配置与它通过多个-f合并。Docker引擎资源仍共享，启动前检查CPU/内存/磁盘，避免影响另一会话。官方VM建议4核16GiB内存、100GiB存储，是整套VM部署建议，不是本机实测最低要求。Compose单机不具备高可用和备份，不能直接认定生产就绪。

## 配置文件与部署步骤

文件：deploy/langfuse/compose.yml、deploy/langfuse/.env.example。真实密钥文件deploy/langfuse/.env已加入.gitignore，不读取/复用项目根目录.env。

在本worktree根目录执行以下Python生成独立密钥（仅在你准备启动时运行；本轮未执行）。以独占模式创建，已有文件会报错，避免覆盖：

```python
from pathlib import Path
import secrets
values = {'LF_WEB_PORT': '13000', 'LF_MINIO_PORT': '19090'}
for name in ['POSTGRES_PASSWORD', 'CLICKHOUSE_PASSWORD', 'REDIS_PASSWORD',
             'MINIO_PASSWORD', 'SALT', 'NEXTAUTH_SECRET', 'ENCRYPTION_KEY']:
    values['LF_' + name] = secrets.token_hex(32)
with Path('deploy/langfuse/.env').open('x', encoding='utf-8') as f:
    f.write(''.join(k + '=' + v + '\n' for k, v in values.items()))
```

ENCRYPTION_KEY为64位hex；数据库密码用hex避免URL转义错误。更改端口时NEXTAUTH_URL及web的媒体URL随LF_WEB_PORT/LF_MINIO_PORT同步生成。默认localhost仅适用于本机浏览器；远程访问需配置TLS域名、对应NEXTAUTH_URL与浏览器可达S3 URL，不能只改端口绑定。

```powershell
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml config --quiet
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml up -d
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml ps
```

访问http://localhost:13000，注册Langfuse管理员、创建组织和项目。分别建production与evaluation项目，不混写。拿到Langfuse项目公钥/私钥后配置agent的导出器，不是模型key，也不是JEV key。保持现有agent模型密钥仅在原服务中使用。停服务使用同一配置的down，不加-v，以免清除观测数据。

TELEMETRY_ENABLED=false关闭Langfuse自身使用统计；LANGFUSE_IN_APP_AGENT_ENABLED=false且未配置任何AI provider密钥。不创建LLM-as-a-judge任务，单纯链路上报不产生模型调用费。仍有基础设施存储成本；日志设置10MiB×3轮转。OSS缺少企业自动保留策略能力，自部署默认长期保存，需要另行设计支持的清理/备份流程，不能把日志轮转误认为trace数据清理。

## Go导出器配置（设计，尚未实现）

使用OTLP HTTP exporter；建议明确配置trace端点，避免base路径重复追加：

| 配置 | 本地agent建议 | 含义 |
|---|---|---|
| OTEL_EXPORTER_OTLP_TRACES_ENDPOINT | http://localhost:13000/api/public/otel/v1/traces | 完整HTTP trace端点 |
| OTEL_EXPORTER_OTLP_TRACES_PROTOCOL | http/protobuf | 不用grpc |
| OTEL_EXPORTER_OTLP_TRACES_HEADERS | Authorization=Basic <base64(public_key:secret_key)>,x-langfuse-ingestion-version=4 | Langfuse项目授权和v4实时摄入 |
| OTEL_SERVICE_NAME | ai-gateway-agent | 服务名 |
| AGENT_LANGFUSE_ENABLED | false（拟新增） | 未实现；未来应用显式开关 |

Basic值在进程配置中构造，不在日志中输出，不混用模型Authorization。agent若在另一个Compose网络内，localhost指向自己，不能直接用此端点；Docker Desktop可使用host.docker.internal:13000，或为agent显式连入Langfuse网络。部署的UI外部地址与进程OTLP地址分别配置。

默认仅上传允许字段：路由模式、commit、评分版本、匿名session/tenant标签、stage、工具名、outcome、token及missing、耗时、JEV置信度和fast-path原因。身份用独立HMAC密钥生成别名，不写API key、Authorization、密码、真实账号或完整钱包/订单数据。默认不上传用户原文、回答原文及工具参数/结果；只靠元数据不能做语义复核，后续开启脱敏内容采样须有明确允许字段与截断规则。不给外部模型请求传播带身份的baggage。

## 观测边界与指标规则

每条用户消息一个root trace，由匿名session关联多轮会话；任务operation ID另行关联补参、预览、确认/取消及业务执行，不假定一个session只处理一个任务。JEV、每次模型尝试、每次工具预览/执行分别建立真实时间span。现有telemetry是轮次聚合值，不能用其总耗时伪造逐次调用时间线；完整子span必须在实际调用边界埋点。

入口及错误出口都要记录；JEV绕过模型时仍记录root/JEV/tool，不虚构generation；确认执行可能不调用JEV或模型，也保留业务span。pending不算失败；用户取消单列终态；必要追问是否合理仍需规则/人工审定。HTTP200、工具success、用户点赞均不能单独证明任务成功。

JEV fast-path覆盖率=实际走fast path轮次/适用轮次，另报全部轮次比例及JEV调用覆盖率（实际调用轮次/全部轮次），不能混称；高置信度正确率仅在有审核标签的样本算，报告样本数及抽样方法。usage缺失是unknown，不是0；费用依据实际网关的价格版本和真实usage，保留cache桶及缺失。JEV是否免费需另核验其价格，不能由这次观测接入推断。完整任务耗时区分后端处理时间、包含用户等待的墙钟时间；报告P50/P95和失败分母。

生产V1/V2流量通常不等价，在线看板的耗时差不能直接宣称JEV因果收益；需受控分流或冻结同集对照。固定1220条及旧输出、金标、代码绑定仍是Git归档；Langfuse只保存版本关联和新实验/监控。新生产样本经去重、脱敏、审核后进入新版本，不能回填旧历史运行真值。

## 后续验收与故障隔离

1. 开关关闭：无网络导出、保持原回复/状态/路由。
2. 本地HTTP接收替身：验证OTLP格式、授权、缺失usage、失败轮次、JEV无模型、确认绑定及跨租户标签隔离，不调用模型。
3. 有界异步队列，固定导出超时/重试上限，队列满统计drop而非阻塞客服；禁用全文后仍限制span大小。抽样标记和drop数量纳入监控，不宣称全量完整。
4. 不使用请求已取消context执行同步导出；关机限时flush。遥测失败只记录故障，不能回滚业务、不增加模型请求。
5. 实际起独立Docker栈后用合成span核验界面/session/usage，确认down不删卷。该步骤尚未执行，静态config检查不是服务健康或摄入成功证据。
6. 在相同本地负载下比较开关前后延迟/错误/丢弃率，再决定采样率。未实测前不给出开销百分比承诺。

## 本轮验证与来源

Docker Compose config --format json使用临时假密钥成功渲染：六服务、项目隔离、loopback端口、内部依赖无端口、无固定容器名、无模型密钥、AI/自身遥测关闭。清空部署密钥后渲染按预期拒绝。真实key未创建；没有容器启动或生产服务重启。Go生产代码未改，前一轮agent基线43.503秒通过。

- [官方Docker配置（固定来源提交）](https://github.com/langfuse/langfuse/blob/e3d264c38ec79e0c76746e92a3718be639002ce1/docker-compose.yml)
- [官方Docker部署](https://langfuse.com/self-hosting/deployment/docker-compose)
- [OTel接入及v4摄入](https://langfuse.com/integrations/native/opentelemetry)
- [费用跟踪](https://langfuse.com/docs/observability/features/token-and-cost-tracking)
- [自部署价格](https://langfuse.com/pricing-self-host)
- [数据保留](https://langfuse.com/docs/administration/data-retention)


### 审查补充：Langfuse字段传播

在每个root/JEV/generation/tool span写入langfuse.session.id（匿名会话）、langfuse.user.id（HMAC租户别名）、langfuse.version（埋点schema版本）、langfuse.release（Git提交）、langfuse.trace.tags（production或evaluation）、langfuse.trace.metadata.routing_mode及langfuse.trace.metadata.operation_id。不只放root或使用随意命名的字段，否则v4按observation过滤与聚合可能不完整。采样与缺失usage的标签也应在对应span保留。

在进程内部由受控span processor复制允许字段，跨会话context不共享，不自动把身份baggage发往供应商。合成span验收新增：所有子span均可按同一session/版本/模式检索，production与evaluation互不混入，跨租户/并发请求标签不串写。最终字段仍需按锁定部署版本实际摄入验证，不把文档映射当作已接通。

独立审查无配置阻塞；以上字段传播与fast-path命名建议已纳入设计。Compose中S3内部地址/桶名固定，部署密码和本机端口仅使用LF_变量，避免项目其他环境变量改变隔离栈。
