# 客服切换至 Pi DeepSeek 配置（2026-10-22）

按用户最新要求，停止继续核查原千问评测的计费问题，将本机客服请求地址、密钥、模型切换至 CC Switch 中 Pi 的 DeepSeek 配置。此次变更已在运行服务生效。

## 实际配置

| 项目 | 当前值 |
| --- | --- |
| 配置来源 | CC Switch 数据库 Pi 的 `cc-switch-deep-seek`，并与 `~/.pi/agent/models.json` 核对 |
| 请求地址 | `https://api.deepseek.com/v1` |
| 模型 | `deepseek-flash` |
| 协议 | `openai`，对应 Pi 的 `openai-completions` |
| 密钥 | 与上述 Pi 提供商一致，仅在内存中比较，未归档密钥或掩码 |
| 客服端口 | `127.0.0.1:9105` |

Pi 当前文件只配置了 DeepSeek 提供商，含 Pro 和 Flash 两个模型，没有设置默认模型。此次保留客服原有 Flash 档位，选择该提供商中的 `deepseek-flash`；没有使用旧 Pi 会话中的千问记录作当前配置。

更新根目录被 Git 忽略的 `.env` 四项 `AGENT_LLM_*`，文件权限为 0600。原文件私密备份在被忽略的 `.tmp/paired-live-private/customer-model-20261022/`。未修改 CC Switch、Pi 自身配置或共享的 Hindsight 服务配置。

原启动器为本机 `local.ai-gateway` launchd 服务组，读取根 `.env` 后载入旧 Langfuse 工作树的遥测配置；已核对后者不覆盖这四项模型设置。通过 SIGTERM 让原启动脚本清理子进程，再由已有 KeepAlive 机制重新启动并加载新配置。该操作未修改启动脚本或清理旧工作树。此前客服端口未监听，重启后已恢复。

## 验证

[脱敏回执](../deploy/local-services/customer-service-model-validation-2026-10-22.json)记录以下已完成检查：

- CC Switch 与 Pi 文件中的地址、协议及密钥一致；目标模型列在 Pi 配置中。
- 只对选定 DeepSeek 地址执行非推理 `GET /models`，HTTP 200，返回列表包含 `deepseek-flash`；禁止跟随重定向。
- 在内存中读取实际客服进程环境，确认地址、密钥、模型、协议四项均与新配置一致；不输出进程完整环境。
- 客服 `/healthz` 和网关 `/portal` 均返回 HTTP 200。
- 现有协议检测、OpenAI 输出预算、OpenAI 遥测和显式评测客户端测试通过；`cmd/agent` 编译检查通过。本次没有修改 Go 源码，未重复全项目回归。
- `.env` 及备份被 Git 忽略，未将任何凭据、私密配置或旧前端改动纳入提交。

上述验证没有发送真实模型或 JEV 推理请求，也没有执行原千问配对评测。旧千问冻结计划及价格记录保留为历史材料，不是当前客服模型配置。后续运行客服继续从根 `.env` 读取这次配置。
