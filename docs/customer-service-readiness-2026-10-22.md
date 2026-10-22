# Pi DeepSeek 客服评测前的本机验证（2026-10-22）

客服配置已迁移到 Pi DeepSeek 后，本轮先完成相关回归和运行版本核对。没有发送真实模型或 JEV 推理请求，没有执行六案例配对评测。

## 发现并处理的环境问题

启动器执行 `bin/agent`，而非每次从 main 构建。原二进制的 VCS revision 为 `58073a8af5318e08fef01197d4d708fc2f35b760`，其构建状态为 modified；私密遥测配置仍将 release 标成 `58073a8`。因此只迁移模型配置并重启，不会更新客服代码。

从通过本轮测试的 main `af0ba14eb2d8df458052ecafaf6ea45e1ec4520d` 重建 `cmd/agent`，备份并替换本机被忽略的 `bin/agent`。同时更新主目录和启动器实际引用的旧工作树 `.env.agent` 中的 `AGENT_RELEASE`，保持 0600 权限。旧工作树仍由启动器使用，未清理。

新 release 为 `af0ba14eb2d8df458052ecafaf6ea45e1ec4520d-local`。现有前端及未跟踪文件仍保留，构建的 `vcs.modified=true` 如实记录；构建时没有已跟踪 Go 源码、go.mod、go.sum 差异。本次本机部署不满足原付费执行器要求的 pristine build 条件，不以新 release 冒充该条件已通过。二进制及配置备份保存在被忽略的 `.tmp/customer-readiness-20261022/backup/`。

通过 launchd SIGTERM 让原启动器清理子进程并自动重启。重启后，在内存中比较实际客服进程配置与 CC Switch Pi，地址、密钥、模型、协议均一致，仍使用 `deepseek-flash` / `openai`。

## 验证结果及边界

脱敏证据见 [部署验证回执](../deploy/local-services/customer-service-readiness-2026-10-22.json)。

- 客服 `/healthz`、网关 `/portal` 均返回 200。
- 向客服提交格式错误的 JSON，请求在解析阶段返回 400；未鉴权、未建立客服业务会话、未进入工具或推理流程。
- 从 Langfuse v2 observations API 按请求时间窗口定位并读回独立 trace；新 release 一致，`llm_calls=0`、`jev_calls=0`。这只证明请求遥测链路，不证明真实模型回复或工具调用质量。
- 六个 Go 包全部回归通过：`internal/agent`、`internal/taskbench`、`internal/observability`、`cmd/agent`、`cmd/paired-live`、`cmd/paired-eval`。
- Langfuse Python 脚本 25 项测试通过。首次受限沙箱不允许回环端口绑定；允许本机端口后原命令通过，没有改测试代码。

初次人工读回使用 `/api/public/traces` 得到 404；改用仓库已有脚本使用的 `/api/public/v2/observations` 成功。返回的 `http_status` 为数字，按数字及客户端时间窗口匹配。此次没有改动产品接口或通用查询脚本。

## 后续实施范围

原 `cmd/paired-live`、`internal/taskbench/authorized_entry*` 和 `scripts/langfuse-eval.py check_settings` 均绑定千问地址、`deepseek-v4.1-flash` 及 Anthropic 协议。它们不能直接验证当前 Pi 模型；旧授权文件和冻结计划继续作为历史资料。

下一小目标是适配当前 Pi 模型的评测准备入口：显式绑定地址、模型、协议；先覆盖配置匹配与不匹配的离线测试，再生成新的准备结果。保留次数上限、输出上限、独立租户、实际 trace 与评分读回、失败停止及禁止自动重试约束，不通过改写旧冻结文件或伪造价格已核实状态来运行新模型。本轮不扩大既有付费范围，也不重复余额核查。
