# Langfuse本地Docker部署

服务位于独立分支 `codex/langfuse-observability` 的 E 盘工作树 `E:\github项目\ai-gateway\.worktrees\langfuse-observability`。网页地址 http://localhost:13000 ，本地管理员邮箱 `admin@ai-gateway.local`，随机密码见 `deploy/langfuse/.local-login.txt`（已忽略Git）。初始组织AI Gateway、项目Customer Service Evaluation；项目仅用于evaluation合成测试。

按用户最新要求，持久化改为Docker独立命名卷，与现有Docker容器一起管理；不再要求data存E盘，也没有迁移Docker全局存储。E盘保留Compose、忽略Git的部署凭据 `.env`、agent凭据 `.env.agent`、私有登录文件、代码与验收文档。`ai-gateway-langfuse`是独立Compose项目，没有复用现有MySQL/Redis/Postgres或重启现有客服。

六个服务：web、worker、Postgres17 Alpine、Redis7 Alpine、ClickHouse25.12、MinIO。使用实际下载镜像digest，具体值见compose.yml。Docker Hub首次EOF、镜像代理两次502；Langfuse/MinIO来自官方仓库，ClickHouse直连后成功，Postgres17 Alpine经现有DaoCloud镜像代理下载，Redis7 Alpine复用本机镜像。代理来源和digest保留，不能称为独立审计镜像供应链。

仅web13000和MinIO19090映射到127.0.0.1，其余依赖不暴露宿主端口。数据库密码、加密key、登录密码和项目API key独立随机生成，未复制模型key。AI助手及Langfuse自身遥测关闭。headless initialization变量为可选，空模板允许用户自行在界面注册；本机已用独立随机值初始化组织、项目及管理员。

## 运维命令

在工作树根目录执行：

```powershell
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml up -d --pull never --wait --wait-timeout 180
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml ps
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml logs --tail 50 langfuse-web langfuse-worker
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml down
```

正常down保留命名卷。镜像均已锁定，后续升级需明确更新digest及复验。

健康探针：web `/api/public/health?failIfDatabaseUnavailable=true`，另验 `/api/public/ready`；worker `/api/health`。Node进程使用容器HOSTNAME监听，容器内探针也使用该HOSTNAME，不能假定127.0.0.1可访问。依赖均有各自健康探针；web/worker首次迁移给120秒启动宽限。

## 真实摄入验收与生产边界

真实Go导出器发送两条明确标为synthetic的evaluation追踪（release=langfuse-deployment-smoke），无模型/JEV网络调用：JEV-only有AGENT/EVALUATOR/TOOL共3条observation，无GENERATION；模型路径有4条observation，其中合成GENERATION input=7、output=2、total=9。跨子span匿名身份、session、environment及schema version一致。

本次部署v4.58.0默认events_only，旧 `/api/public/traces` 返回404“endpoint unavailable”；这不是数据丢失。验收使用官方 `GET /api/public/v2/observations?traceId=...`。初次查询误用旧接口导致等待失败，随后新API确认已摄入。web/worker探针重建后已复查，合成数据仍保留；结果见deployment-validation.json。

agent导出器此前已在独立分支实现，真实项目key写 `.env.agent`。当前没有重启另一会话正在使用的客服，也没有自动把其env改成启用监控；生产流量接入需将该分支代码及环境变量部署到客服。页面中现有数据是合成验收，不是1220重跑、更不是生产成功率。

本机Docker总内存约7.36GiB，启动后六服务约3.1GiB（一次观测，非负载基准）；并发生产压力、保留策略、费用模型仍未认证。现有1220数据及金标未改，付费模型/JEV调用为0。

来源：[官方headless初始化](https://langfuse.com/self-hosting/administration/headless-initialization)、[官方健康接口](https://langfuse.com/self-hosting/configuration/health-readiness-endpoints)、[v4 API迁移](https://langfuse.com/faq/all/deprecated-api-migration)。
