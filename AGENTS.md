# ai-gateway 项目记忆

只在处理 ai-gateway 时使用。Hindsight 在 `http://127.0.0.1:8888`，记忆库 `ai-gateway`。服务没开就跳过，不要为此停下来问用户。不要读取或写入其他项目的记忆库。

开始具体任务前先读取。`POST /v1/default/banks/ai-gateway/memories/recall`，正文只需要 `{"query":"与当前任务相关的一句话"}`。用查到的内容，不要把整段记忆复述给用户。

出现跨对话仍有用的决定、配置、账号角色或已经完成的环境变更时，自己写入这个库，不必等用户说「写入」。`POST /v1/default/banks/ai-gateway/memories`，正文为 `{"items":[{"content":"事实","tags":["ai-gateway"]}],"async":false}`。

不写密钥、密码、token。不写闲聊、一次性问题和没有定下来的猜测。

2026-10-21 已验证同步记忆写入可能耗时数分钟（一次成功请求约 240 秒）。保持客户端连接并设置足够超时；客户端断开会取消同步任务。同步请求没有后台 operation 记录，队列为空不能证明成功。仅在成功响应且实际读回后确认入库；超时记为未确认，先检查日志和存储再重试。排查证据见 `docs/hindsight-write-diagnosis-2026-10-21.md`。

2026-10-22 另见服务端事实提取四次超时后 HTTP 500（约 487 秒）；当时单次 LLM 总时限为 120 秒、重试 3 次。延长客户端超时不解决该类失败。先核对日志与存储；不要无限重试或擅改共享服务级配置。诊断记录见同一文档的后续章节。

## 开发流程与小目标交付

开发时遵循适用的 Superpowers 技能流程：开始前读取相关技能；新功能或行为变更先明确设计和实施计划；实现功能或修复缺陷按适用的测试驱动流程进行；遇到失败先系统排查；完成前运行与改动相关的验证，根据范围进行代码审查。用户明确的项目约定优先，继续只在 `main` 开发。

主动读取、写入和更新有长期价值的 Hindsight 项目记忆，仅使用 `ai-gateway` 库。更新时纠正过时事实，保留必要上下文；服务不可用时继续工作，在本地约定或报告保留重要结论，不把未确认写入宣称为成功。

每完成一个边界清楚、验证通过的小目标，就在 `main` 提交并推送到 GitHub。提交前检查差异和暂存范围，仅纳入该小目标相关文件；保留其他已有改动，不提交密钥、私密配置或临时运行数据。推送前核对远程变更，禁止强制推送；推送后确认远程提交，并报告提交号和验证结果。该授权不包含付费模型/JEV调用，付费执行仍需明确范围和预算授权。

## 开发分支约定

后续开发统一使用 `main`。除非用户明确要求，不新建或切换到功能分支，不创建新的开发工作树。历史分支仅用于追溯。

核对旧分支是否已纳入 `main` 时，同时检查正常合并关系和压缩合并后的代码内容，不能仅凭提交号不同判定漏合。

旧 Langfuse 工作树仍被本机启动脚本和私密配置引用；处理分支时保留该目录及其配置，迁移并验证依赖后再清理。

## 本机验证与部署

2026-10-22 Hindsight 默认模型已按用户要求迁移至 CC Switch 中 Pi 的 DeepSeek 配置，使用 `provider=deepseek`、`model=deepseek-flash`；地址和密钥只在本机私密配置维护。控制台 9999 端口启用原生访问密钥登录（无邮箱账号体系），记忆 API 仍为 8888。新配置下已完成同步写入与实际读回。Langfuse 已更新原本地账户登录资料，用户 ID 和项目权限保留；登录资料只见被忽略的 `deploy/langfuse/.local-login.txt`。两项变更均保留原数据卷。记录见 `docs/local-services-settings-2026-10-22.md`。

2026-10-20 已将匹配现有 Langfuse 数据的 `.env`、`.env.agent`、`.local-login.txt` 复制到主目录 `deploy/langfuse/`，权限为 0600，均被 Git 忽略。被替换的原 `.env` 备份在被忽略的 `deploy/langfuse/data/config-backups/`。本机启动脚本仍引用旧工作树配置，清理旧目录前需迁移该脚本依赖。

当前 Mac 默认 MacOSX27.0.sdk 会触发链接器 unknown architecture 错误；本机 Go 回归使用进程级 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk`，不修改系统默认值。

全项目回归和 Langfuse 实际摄入、评分读回记录见 `.tmp/full-acceptance-20261020-bz5w3zde/`。小样本 preflight 保持 `allow_paid=false`；本地替身验证通过不代表真实模型评测已完成或已获付费授权。

## 当前首轮真实测评授权

2026-10-22 用户已在本对话明确批准既定六案例、最多 24 次模型尝试 / 6 次 JEV，人民币 ¥1 / 美元 $0.02 的首轮测评；不再重复询问这项授权，不再重复余额核查。范围、每批次数及计划绑定见 `eval/paired-pilots/2026-10-22-authorized-small-pilot/authorization.json`，状态说明见 `docs/paired-small-pilot-authorization-2026-10-22.md`。授权不等于执行完成或收费证据已核实，当前仍未启动真实调用；不得将未经核实的 RatesVerified 标为 true。早期文档“尚未授权”为历史状态，以本条为准。

2026-10-22 新增 `cmd/paired-live` 真实首批入口：默认只读检查，不加载密钥；仅显式 execute 且收费证据就绪时执行 batch-001，完成后停在 checkpoint_pending。同范围授权在仓库 `.tmp/paired-live-authorizations/` 持久领取，换输出路径不能重置；失败也不自动删除或重试。执行要求与 HEAD 一致的未修改 VCS 构建及冻结输入校验。当前真实调用仍为零，账户费率、token 包络、JEV 失败收费证据尚未核实；授权保留，不重复询问或核查余额。操作说明见 `docs/paired-live-entry-2026-10-22.md`。
