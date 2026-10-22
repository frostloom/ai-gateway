# 本机 Hindsight 与 Langfuse 配置更新（2026-10-22）

按用户要求迁移 Hindsight 模型配置并修改两个服务的本机登录资料。密钥、密码、私人邮箱、会话 cookie、数据库账户备份均不归档到 Git 或项目记忆。

## Hindsight

- 从 CC Switch 数据库的 Pi DeepSeek 提供商读取配置，与 Pi 当前模型配置文件核对一致；选择 Flash 模型，实际配置为 `provider=deepseek`、`model=deepseek-flash`，使用该提供商的请求地址和密钥。
- 更新本机 `/Users/a1/开发工具/hindsight/.env`，并在原 Compose 文件传入 `HINDSIGHT_CP_ACCESS_KEY`。修改前的文件备份保存在该部署目录的私密备份目录，环境文件权限为 0600。
- 本机版控制台没有邮箱账号体系；采用其原生访问密钥登录方式，访问密钥按用户指定密码设置。控制台仍使用本机 9999 端口，记忆 API 仍使用 8888 端口。
- 重建原服务容器使配置生效，继续使用 `hindsight_hindsight-data` 数据卷。没有迁移、删除或读取其他项目的记忆库。

## Langfuse

- 修改唯一现有本地账户的邮箱和密码，使用现有 bcrypt 实现生成密码哈希；保留用户 ID、管理员标志和现有项目关系，使旧会话过期。没有新建替代账户或重新初始化项目。
- 同步更新主目录 `deploy/langfuse/.env` 的初始化登录设置，以及被 Git 忽略的 `deploy/langfuse/.local-login.txt`，文件权限均为 0600，原文件已私密备份。没有修改项目 API key 或 `.env.agent`。
- 仅重建 Web 容器，运行来源改为主目录的 Compose 和现有中文界面覆盖文件，继续使用原数据库和存储。Web 仍使用本机 13000 端口。

## 验证

[脱敏验证结果](../deploy/local-services/validation-2026-10-22.json)记录以下实际检查：

- Pi DeepSeek 地址与密钥的非推理 `GET /models` 返回 HTTP 200，目标模型在列表中；运行容器中的地址、密钥、提供商和模型与选定配置一致。
- Hindsight 健康检查通过，未登录控制台跳转登录页；错误访问密钥被拒绝，正确密钥取得会话并进入控制台。
- Langfuse 健康检查通过；通过真实 CSRF、credentials callback、session 流程验证指定登录资料，登录后用户 ID 与修改前一致；错误密码未取得用户会话。
- 通过新配置向 `ai-gateway` 同步写入一条不含凭据的环境变更记忆，耗时 10.729 秒，返回 HTTP 200、`success:true`；随后直接读回新增模型迁移事实。该时长仅描述本次写入，不代表长期性能保证。
- 核对两个服务原数据卷仍在使用，私密配置权限正确，脱敏记录不含凭据。本轮是本机部署配置与登录变更，没有运行 Go 回归，也没有启动真实评测 campaign。

Hindsight 原先的服务端提取超时记录保留在[诊断文档](hindsight-write-diagnosis-2026-10-21.md)，属于迁移前的历史证据。
