# 项目迁移与最新进展交接（2026-10-20）

> 2026-10-21 补充：本机已恢复主目录 Langfuse 私密配置并验证本地摄入、评分读回；历史分支内容已核对纳入 main，后续仅在 main 开发。最新执行入口修复、回归和真实小样本准备边界见[测评准备说明](paired-run-readiness-2026-10-21.md)，开发交付规则见[项目约定](../AGENTS.md)。下文保留迁移核对当时的历史状态；旧工作树仍被本机启动脚本引用，继续保留。

这份文档是换电脑后的第一阅读入口。核对的代码基线为 **main `64ddd20`**；本交接文档随后单独提交。它记录仓库与证据现状，不代表新电脑已经安装、恢复数据库或通过验收。本轮只读核对文件与 Git 状态，没有读取密钥内容，没有运行模型或 JEV。

## 1. 先看结论

- 项目包含 Go 网关/路由/计费/客服、Vue 控制台、MySQL/Redis/Kafka，以及独立 Docker 部署的 Langfuse。管理员和普通用户分开登录，普通用户可直接注册。
- 客服、Langfuse 集成代码、中文界面构建层、评测工具和公开归档均已在 main。旧 Langfuse 工作树不是继续开发的必需入口，但其中仍有必须单独保存的私有配置。
- 1220 条原始问题、三组共 3660 条历史真实首轮输出已经归档；1220 条任务定义已补齐。**没有执行新版 1220 条完整真实 V1/V2 配对评测。**
- 最新完成的是免费本地链路验证及历史证据对账，不能当作新版真实模型准确率、总体提速或总费用下降证明。
- 换电脑最低需要：GitHub 仓库 + 私密配置。若要保留原账户、账单及 Langfuse 页面内容，还需要相应持久化数据；这些不在 GitHub。
- 后续真实模型和真实 JEV 推理均需新的明确费用授权。不能因为迁移成功、用户说“继续”，就自动启动付费实验。

## 2. 最新开发进展

| main 提交 | 完成内容 | 证据边界 |
|---|---|---|
| `04a0938` | main 集成验收归档 | 对当时版本完成全量 Go/Python/Web 验收，不代表每次后续提交都重跑全量 |
| `75af0a5` | JEV 分流阈值、安全及回退边界测试 | 证明执行门控遵守规则，不证明置信度校准 |
| `58f5a4e` | 从已有真实小样本核验 usage、预算、费用与日志一致性 | 不重新请求供应商；发现旧费用摘要差异并单列，原件不改 |
| `6f51070` | 离线分解 JEV 直读及模型回退开销 | 历史观测，不是新优化后的实测收益 |
| `f585547` | selective 路由在多轮上下文提前跳过不适合直读的 JEV 判定 | 运行时版本 `selective-context-prefilter-v1`；不更改提示词或工具契约 |
| `64ddd20` | 必要追问与退款余额上限纳入完整本地配对/观测链路 | 6 场景、12 臂；结构通过仍保留独立语义审核，不产生真实准确率 |

最近一次相关回归为 108 个顶层测试通过、零失败/跳过，两个包 `internal/taskbench` 和 `cmd/paired-eval` 的 vet 通过；本地 OTLP 捕获 12 task、26 turn、20 generation，零丢弃/导出失败。所有模型/JEV响应均由本地替身提供，**不是向实际 Langfuse 项目写入这次测试**。

此前全项目验收为 Go 361 个顶层通过、3 个按开关跳过，Python 137 项通过，Web verify 通过。后续独立 Python 回归曾达到 158 项，客服预过滤相关 Go 回归为 agent/taskbench 共 215 个顶层通过、1 个显式跳过。不同版本、不同范围的数字不能相加当“总案例数”。原始日志及版本见下表各文档。

## 3. 当前 V1/V2 设计与评分规矩

**V1 对照臂 `model`**：模型判断工具/答复，不调用 JEV。**V2 实验臂 `jev_selective`**：首次、单一、无参数的高置信只读请求，满足执行层约束才直读并绕过模型；其余交回模型。支持直读的工具为 `get_balance`、`get_consumption`、`list_plans`、`get_my_subscription`。多轮上下文提前跳过 JEV；正常确认/取消走服务端状态机，不靠模型自称成功。

退款等写操作必须预览、绑定参数并由用户确认，执行层复核身份、参数和状态；JEV 不获得直接写权限。缺信息时必要追问是合理行为。无工具自然语言默认 `unreviewed`，必要追问语义为 `pending_independent_review`，结构通过的任务仍可为 `needs_review`，Langfuse 的 `task_success` 仍为 `unknown`。

历史 `nojev/hint/fast` 不等于当前 V1/V2；其中历史首轮合理动作 90.16% / 88.77% / 89.26% 是既有 AI 审核标签比例，**不是独立事实准确率或完整任务成功率**。不得用新模拟账户补造历史事实真值，也不得把 4291 个场景定义当 4291 次真实执行。

完整任务应看终态、账本/订单/订阅不变量、确认前零写入、取消不写及重复确认不重写；回答事实和追问是否恰当需要单独审核。效率分别记录模型/JEV调用、普通/缓存 token、服务端轮次及整任务耗时；费用区分估算和实际账单，未知不能填零。

## 4. 文档与产物导航

以下路径均相对仓库根目录；按用途查找，不必按文件日期从头阅读。

| 用途 | 文档 / 目录 | 怎么使用 |
|---|---|---|
| 项目架构、接口、账户 | [README](../README.md)、[接口](api-reference.md)、[登录](account-login.md) | 理解服务；README 部分旧路由、旧测试数字和旧启动示例不作为最新评测口径 |
| 评分规则 | [评测契约](customer-service-evaluation-contract-2026-10-09.md)、[V1/V2对照](customer-service-v1-v2-comparison-2026-10-13.md) | 冻结分母、首轮/整任务边界、两臂公平配置 |
| 1220 定义 | [定义说明](customer-service-task-definitions-2026-10-12.md)、`eval/task-definitions/` | 1220 来源展开 1220 首轮诊断 + 3071 完整分支；含首轮路由、回答要求、后续输入、终态 |
| 原始数据与历史审核 | `eval/intent-suite/`、`eval/experiments/`、`eval/baselines/`、`eval/annotations/`、`eval/behavior-review/` | 追溯语料、原始真实输出及旧评分；不得覆盖、混算新版成绩 |
| 历史回答证据资格 | [复核说明](customer-service-historical-answer-evidence-2026-10-13.md)、`eval/reviews/2026-10-13-answer-evidence-v1/` | 对 3660 输出追溯 Reply/HTTP/工具/审核标签；历史数据库真值缺失保持不可核验 |
| 独立事实依据 | [回答依据](customer-service-answer-authority-2026-10-13.md) | 价格、余额、token、周期等必须取独立账户/工具真值，不能以回答证明回答 |
| 夹具与执行器 | [账户初始化](customer-service-profile-initialization-2026-10-13.md)、[会话执行器](customer-service-conversation-runner-2026-10-13.md)、[客服集成](customer-service-agent-integration-2026-10-13.md) | 六种合成 profile、独立 DB 状态 oracle、确认/取消等结构断言 |
| 配对运行准备 | [运行适配器](customer-service-paired-runtime-adapter-2026-10-14.md)、[小样本准备](customer-service-small-paired-preparation-2026-10-14.md)、`eval/runtime-profiles/2026-10-13-six-profiles-v1/` | 同一模型/提示词/tools/运行时，双方独立账户，日志及预算守卫 |
| 已有真实小样本 | `eval/paired-pilots/2026-10-17-jev-acceptance-v1/` | 四任务八臂历史证据；不是 1220 新成绩，也不是本轮六项替身测试 |
| 全项目验收 | [main 验收](main-integration-acceptance-2026-10-20.md)、`eval/integration-acceptance/2026-10-20-main/` | 对应版本、环境、命令、测试结果和压缩原日志 |
| JEV门控/费用对账 | [离线核查](jev-offline-readiness-2026-10-20.md)、`eval/jev-offline-readiness/2026-10-20/` | SHA 与 usage/预算核对；旧摘要 46612 微元与明细 46592 微元差异单列，原件保留 |
| JEV回退开销 | [开销分解](jev-fallback-overhead-2026-10-20.md)、上述目录 `overhead/` | 定位历史回退开销；不能只挑直读样本宣称整体更快 |
| 最新产品优化 | [上下文预过滤](jev-context-prefilter-2026-10-20.md)、上述目录 `context-prefilter/` | 设计、版本兼容性、测试与审查记录；旧 journal 不可续跑成新版本 |
| 最新链路验证 | [必要追问准备](jev-clarification-readiness-2026-10-20.md)、上述目录 `clarification-readiness/` | 6 项本地覆盖；`preflight.json` 是 12 项离线候选，`allow_paid=false`，不是执行回执 |
| Langfuse安装 | [原部署记录](langfuse-deployment-2026-10-13.md)、`deploy/langfuse/` | Compose、模板和原验收；原文“仅独立分支”的描述是历史状态，代码现在在 main |
| Langfuse工作台 | [工作台说明](langfuse-workbench-2026-10-16.md)、`eval/langfuse-workbench/2026-10-15-v1/`、[内容评分](langfuse-content-scores-2026-10-15.md) | 数据集、分数、审核队列、提示词、看板及发布/读回回执；导入不等于执行 |
| Langfuse中文界面 | [中文构建说明](../deploy/langfuse/zh/README.md)、`deploy/langfuse/compose.zh.yml` | 固定版本的局部汉化；主 compose + overlay 同时使用才保留中文 |
| 面试与工程过程 | `docs/interview/`（尤其 `CHANGELOG.md`）、`docs/superpowers/plans/` | 项目叙事、修改原因、缺陷/验证过程；最新三个 JEV 文档也包含面试边界 |
| 计费性能/上线限制 | `docs/billing-*.md` | 并发一致性、性能与发布门禁；通过功能回归不代表生产容量已经认证 |

新证据通常含 `summary.json`（摘要）、`manifest.json`（摘要锚点）、`.jsonl.gz`（原日志）、`preflight.json`（配置）。gzip 是已压缩原件，不能为“好阅读”随意改写后重新算 SHA。仓库 `.gitattributes` 对冻结证据禁用换行转换；Windows 若校验失败，先检查 Git 字节与属性，不能直接更新 manifest 迎合失败文件。

## 5. 需要带走什么

### 5.1 GitHub 可恢复的内容

代码、`web/dist`、公开评测数据/压缩输出、冻结定义、manifest、测试、部署模板、中文构建脚本与文档均通过 Git 获取。无需复制 `bin/`、`.gocache/`、`web/node_modules/`、Docker 镜像、一般日志；它们可重建。本地 `eval/reports/` 被忽略，若里面有未归档的独有实验则需另外筛选保存，不能假设 GitHub 包含全部临时报告。

### 5.2 必须私下迁移的配置（本机仅核对存在性）

| 旧电脑实际位置 | 新电脑推荐位置 | 用途 |
|---|---|---|
| 仓库根 `.env` | 仓库根 `.env` | 模型/JEV渠道密钥、数据库/服务配置及共享鉴权等，以实际文件为准 |
| `.worktrees/langfuse-observability/deploy/langfuse/.env` | `deploy/langfuse/.env` | Langfuse 服务密码、加密材料和初始化配置 |
| 同目录 `.env.agent` | `deploy/langfuse/.env.agent` | 客服连接 Langfuse 的项目凭据及采集配置 |
| 同目录 `.local-login.txt` | `deploy/langfuse/.local-login.txt` | 本机 Langfuse 初始登录资料；实际登录仍以恢复后的账户状态为准 |

当前 main 的 `deploy/langfuse/` 下上述三个私有文件均不存在，真实文件在旧工作树中。把文件复制到新目录即可，不需要迁移整个工作树。密钥通过加密文件/私密渠道传输，不写进本交接文档或 Git。恢复旧 Langfuse 数据时保留配套的加密材料与密码，不能拿新生成配置直接覆盖。

模型渠道历史提案为 `https://maas.qianwenaiapi.com/apps/anthropic`、`deepseek-v4.1-flash`、Anthropic 协议；这次没有读取 `.env` 确认实际值，也没有验证可用性/报价。新电脑按实际渠道密钥核对，不能把密钥试发到其他供应商。

### 5.3 Docker 数据与界面状态

**1220 的公开归档在 Git 仓库，不依赖 Docker 卷。** Langfuse 网页中的导入数据集、trace、评分、审核队列、提示词和看板存在其持久化系统中；只带源码和 env 不会自动还原这些页面。

- 要继续开发且不要求保留旧 UI：可以全新部署，保留 Git 中的原始证据；新项目重新生成凭据。旧发布回执中的资源 ID 不代表新实例中存在。
- 要完整保留旧 Langfuse：迁移 Postgres、ClickHouse、MinIO 及相关持久化/队列状态，保持同一备份时点与原私密配置。先停止写入并完成一致性备份，验证恢复后再继续采集；不要只拷一个数据库目录。
- 要保留普通用户/管理员账户、模拟账单等业务状态：另备份业务 MySQL；需要保留会话/待确认态时另处理 Redis。根 compose 的 Redis 没有声明持久卷，不能假定 Docker 卷包带有会话。
- Kafka 数据与消费位点若要求延续，也要保存；若只要开发环境可重新初始化，但不能称为恢复历史事件状态。

本轮没有执行备份、导出或恢复。此前若已有迁移包，应先检查日期和清单，不要假定包含当前配置及全部最新状态。不要在源机执行 `docker compose down -v` 或清卷。

## 6. Git 分支与工作树

核对时 main 为 `64ddd20`。本地仅有 main 与 `codex/langfuse-observability`；后者工作树干净，位于 `.worktrees/langfuse-observability`，停在 `6153452`，已是 main 的祖先。它的**代码**已纳入 main，私有配置仍需按前节迁移。

刷新远程后仍有 `origin/cursor/fix-windows-evidence-hash-1045`、`origin/cursor/improvements-backlog-878d`；它们在提交祖先关系上不是 main 已合并分支。里面有证据字节、时区、计费/控制面等提交。这里没有重新判定其补丁是否已通过其他提交纳入，不能据分支名直接合并或删除。新机拉取后先比较 diff/cherry，再决定。本次 fetch 后 main 与 origin/main 一致；新机仍以届时 fetch 后状态为准。

最简单的新机获取方式：

```powershell
git clone https://github.com/frostloom/ai-gateway.git
Set-Location ai-gateway
git fetch --all --prune
git switch main
git pull --ff-only origin main
git log -6 --oneline
git branch -a
```

clone 会获取远程分支引用，但不自动为每个远程分支建本地工作树。旧工作树的 `.git` 是指向旧目录的连接文件，不能单独搬过去当完整仓库。

如需额外保留旧电脑所有本地分支引用，可在旧仓库执行 `git bundle create ../ai-gateway-all-refs.bundle --all`，新电脑用 `git bundle verify <文件>` 校验。bundle 包含提交与引用，**不含未提交文件、env、Docker 数据**；不是数据库备份。当前另有未跟踪 `plate_sim.py`，未归入项目改动，本轮未修改/提交；如仍有用途请单独带走。

## 7. 新电脑安装与启动（Windows PowerShell）

安装 Git、Docker Desktop（Linux containers）、**Go 1.25.12**、与锁文件兼容的 Node/npm、Python 3。旧机使用特殊绝对路径的 Go/Python 只是本机安装位置，新机不要照抄盘符。以 `go.mod`、`go.sum`、`web/package-lock.json` 为依赖依据。

先进入新仓库根目录，放置上述私有配置，再执行：

```powershell
go version
node --version
python --version
docker version
go mod download
Push-Location web
npm ci
npm run verify
Pop-Location
docker compose up -d mysql redis kafka kafka-init
docker compose ps
```

新机首次下载依赖和镜像需要网络；不要沿用旧机离线检查时的 `GOPROXY=off`。根 compose 主要启动依赖，不会自动把所有 Go 应用打包启动。

启动全部应用前核对 `.env` 的端口、数据库、共享 `CONTROL_PLANE_TOKEN` 及渠道配置。启动脚本会导入 `.env`，并停止/重启目标端口上的进程，确认是这套项目的端口再执行：

```powershell
.\scripts\start-all.ps1 -Rebuild
```

它不会自动重新构建前端，因此上面的 npm 步骤应先完成。浏览器入口：管理员 `http://localhost:18080/`，普通用户 `http://localhost:18080/portal`。客服 `9105`，billing gRPC/admin `9101/9103`，router `9102`；MySQL `3307`、Redis `6381`、Kafka `29092`。注册/登录与账户数据依赖数据库是否恢复，不能假定旧密码自动存在。启动服务不等于授权发送真实客服请求；费用验证前不要点击聊天快捷问题。

### Langfuse 中文部署

先恢复配套数据或决定全新初始化；在仓库根目录使用新的配置位置：

```powershell
docker build -t ai-gateway-langfuse-zh:4.58.0-local-v2 deploy/langfuse/zh
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml -f deploy/langfuse/compose.zh.yml up -d --wait --wait-timeout 180
docker compose --env-file deploy/langfuse/.env -f deploy/langfuse/compose.yml -f deploy/langfuse/compose.zh.yml ps
```

首次需要下载固定基础镜像；不要照抄旧机文档中的 `--pull never` 要求。以后管理中文部署继续同时传两个 compose 文件。打开 `http://localhost:13000/`。汉化是局部界面层，原始问答、评分值与动态业务内容不翻译。默认看板时间窗口可能只查最近一天，查看旧证据应调整到对应日期。

`start-all.ps1` 只加载根 `.env`，**不会自动加载 `.env.agent`**。启用采集时需把 `.env.agent` 所需变量通过受控方式注入 agent 进程或根私有 `.env`，并核对 `AGENT_LANGFUSE_ENABLED`、evaluation 环境、项目 keys、OTLP 地址和内容采集开关。不要把真实模型密钥填入 Langfuse 调试台或开启自动 LLM 裁判。旧项目 ID、数据集/看板 URL 在全新实例中可能不同。

## 8. 换机后的免费核验与接续顺序

先核对 Git 版本、私有配置位置、数据库是否恢复，以及 `git status --short`。不要先跑在线评测脚本。可先做不需要供应商的证据检查：

```powershell
$env:PYTHONUTF8='1'
python scripts/verify-task-definition-archive.py verify
python scripts/audit-historical-answer-evidence.py verify
python scripts/audit-jev-efficiency.py eval/paired-pilots/2026-10-17-jev-acceptance-v1
python scripts/analyze-jev-overhead.py eval/paired-pilots/2026-10-17-jev-acceptance-v1
```

随后在**单独的新 PowerShell 窗口，不载入 `.env`**执行本地回归。启动依赖后确保无别的测试进程操作相同测试库；夹具会清理专用数据，不能改成业务库：

```powershell
$env:REDIS_ADDR='127.0.0.1:6381'
$env:AGENT_LIVE_JEV='0'
$env:TASKBENCH_LANGFUSE_SMOKE='0'
$env:BILLING_CASH_LOAD='0'
$env:BILLING_CASH_PROFILE='0'
go test ./internal/agent ./internal/taskbench ./cmd/paired-eval -p 1 -count=1 -timeout 5m
go vet ./internal/agent ./internal/taskbench ./cmd/paired-eval
```

测试需允许本地 TCP、临时文件和专用数据库；缺依赖要修环境，不能把跳过当通过。`TASKBENCH_LANGFUSE_SMOKE=0` 验证的是本地接收器，不是实际项目入库。

接下来按顺序进行：

1. 核验 Langfuse 数据恢复/项目关联；纯观测探针可验证采集，不需要调用模型。旧工作台发布工具见说明，不能拿旧资源 ID 对全新项目直接盲目续传。
2. 可继续免费增强：逐 trace/case/arm 核对 OTLP 归属、人工独立审核已有答案、完善迁移验证。当前观测测试主要核对汇总数量，逐臂归属是已记录的可选增强。
3. 真调用前固定**新的**源码/路由版本、任务子集、模型/协议/短提示词与 128 输出上限、同配置两臂、独立账户和新 journal；旧版本日志不可混入。
4. 单独核验渠道及模型/JEV费率、制定两种费用的硬上限、输入输出/调用次数上限，再请求明确的小样本费用授权。12 项 preflight 不是已批准执行计划，旧建议 24 次调用也不保证完成 24 臂。
5. 小样本真实链路、回答审核、失败分母与实际 Langfuse 摄入可靠后，再申请 1220 条评测预算。采集失败优先重传观测，不能为补页面重复付费推理。

给新电脑上的助手可直接说明：**“先读 docs/MIGRATION-HANDOFF-2026-10-20.md 和三份最新 JEV 文档，核对当前 Git 与迁移状态。保留所有冻结证据，先做免费检查；未经新费用授权禁止真实模型/JEV调用，每个小目标验证并记录后推送。”**
