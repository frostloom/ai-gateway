# Customer Knowledge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付可在 Docker 中运行、由主模型调用、可通过 Langfuse 追溯的只读平台知识检索，并准备质量/成本的离线评测工具。

**Architecture:** agent 进程内装载经审核且摘要校验的公共知识文件，使用确定性中文全文检索，作为读工具回填模型。知识与二进制随同一 Docker 镜像发布；观测分别记录检索结果和实际进入请求的片段，不改 JEV 策略。

**Tech Stack:** Go 1.25.12、标准库 JSON/UTF-8/SHA-256、现有 OpenTelemetry/Langfuse、Docker Compose；不新增外部检索服务或付费 embedding。

**Spec:** `docs/superpowers/specs/2026-10-24-customer-knowledge-design.md`；语料依据 `docs/knowledge/platform-knowledge-draft-2026-10-24.md`。

## Global Constraints

- 用户 2026-10-24 回复“可以去准备开发吧”，按此前方案准备开发；设计及指标目标可进入实施计划，当前尚未选择计划执行方式。
- 仅在 `/Users/a1/.codex/worktrees/customer-knowledge-rag/ai-gateway` 的 `codex/customer-knowledge-rag` 开发；不切换/修改其他工作树。新明确分支要求覆盖日常 main 开发默认约定。
- 基点 `647ba9f`，文档进度 `4834904`。主目录 main 已继续推进；准备计划时仅只读核对为 `384775e`，不自动合并其测评工作。开始编码前核对新进展；涉及交汇文件时逐项评估，而非覆盖。
- 单次最多返回 3 个片段，每片段最多 768 UTF-8 字节；搜索词最多 256 字符；拼装结果最多 4096 字节。
- 资料规模首版限制为 100 文档、每文档 64 KiB，总量 2 MiB。
- 状态必须区分 ok、no_match、unavailable；draft 不参与检索；不可用不可伪装成无命中。
- 当前价格、余额、订单、剩余额度和状态只查业务工具。资料不是执行指令，不授予权限；原五轮上限、一次纠正和预览确认边界不扩张。
- 默认关闭知识功能；开启后目录/manifest 不合法则启动失败。内容采集默认沿用关闭；不绕过脱敏及采样，不把入请求等同于模型采用。
- 无付费模型/JEV调用，不改私密配置、不重启本机客服、不迁移数据卷、不重绑既有范围或释放/复用 claim。
- 本机 Go 命令使用进程级 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk`；不改系统 SDK。
- 小目标验证通过后只提交相关文件，以 frostloom 约定身份在本分支提交并推送，不合并 main、不强推。

## Review Focus

- 中文口语/否定句/规则例外：无关词重合不得输出不相关条款；Task 2 覆盖。
- 长规则和 UTF-8 边界：不能把否定或例外裁掉后成为相反结论；Task 1/2 覆盖。
- 历史已命中片段在消息裁剪后消失：只记录本次实际请求包含的片段；Task 4 覆盖。
- 模拟支付与真实付款混淆、订阅超限自动现金回退：语料和实时工具优先级必须正确；Task 1/3/6 覆盖。
- Docker localhost、只读权限、密钥进入构建上下文和无效知识发布：显式网络与失败探针必须有效；Task 5 覆盖。

---

## File Map 与接口约定

- `knowledge/platform/*.json`：12 个公共主题，每份一条文档；使用标准库 JSON，不添加 YAML 依赖。
- `knowledge/platform/manifest.json`：排序文件名与文件 SHA-256 的发布清单；不列自身。
- `internal/knowledge/corpus.go`、`search.go`：严格加载、分段与本地排名；无网络/环境变量/全局可变索引。
- `cmd/knowledge-manifest/main.go`：只读文件生成/检查 manifest 的构建工具，支持 `-dir`、`-check`；不自动批准草稿。
- `internal/agent/knowledge.go`：注册工具、限定结果格式、启用状态、片段请求关联；不把业务工具与知识搜索合并。
- `internal/observability/knowledge.go`：固定字段的安全知识元数据及有界片段观测。
- `deploy/agent/Dockerfile`、`compose.yml`、`env.example`、`README.md`：opt-in 的 agent 容器交付。
- `internal/knowledgeeval/`、`cmd/knowledge-eval/`：独立离线指标汇总，CLI 不加载密钥、不发模型请求。
- `eval/knowledge/dev-v1/`、`eval/knowledge/acceptance-v1/`：开发问法与首轮冻结验收集，分离维护。

核心类型在 Task 1/2 固定：
`Source{Path string; Symbol string}`；`Document{ID, Title, Version, Status, ReviewedAt, Scope string; Aliases []string; Sources []Source; Paragraphs []string}`。
`Hit{DocumentID, ChunkID, Title, Version, Scope string; Sources []Source; Text, SHA256 string; Paragraph int; Score float64}`。
`Result{Status, CorpusVersion string; Hits []Hit}`；`Corpus.Version() string`；`Load(fsys fs.FS) (*Corpus,error)`；`(*Corpus).Search(query string) (Result,error)`。
仅 approved 进入运行语料；ReviewedAt 是本轮源码核对日期，不伪称额外产品负责人背书。正式条目保留适用提交及模拟范围。

### Task 1: 可追溯语料与严格加载

**Files:** Create `knowledge/platform/*.json`, `manifest.json`, `internal/knowledge/corpus.go`, `corpus_test.go`, `cmd/knowledge-manifest/main.go`, `main_test.go`。

**Interfaces:** Produces 上述 Document/Source/Hit/Result/Corpus 类型、`Load(fs.FS)`、`Version()`；构建工具 `knowledge-manifest -dir knowledge/platform [-check]`。Corpus 装载后不可变。

- [ ] **Step 1: Write failing tests**：`TestLoadValidCorpus` 断言 12 个稳定文档 ID；`TestLoadRejectsTamper` 修改一字节后 Load 失败；表驱动覆盖重复 ID、draft 清单、未知字段、坏 JSON、缺清单、空正文、非法日期、路径穿越/符号链接、超过 100 文档/64 KiB/2 MiB，均拒绝。`TestParagraphLimits` 对超过 768 字节的不可分段条款拒绝装载，不盲目截断；完整段落分片后不丢否定和例外。
- [ ] **Step 2: Run RED**：`go test ./internal/knowledge ./cmd/knowledge-manifest -count=1`，确认失败原因是缺少接口/行为，不是无关配置。
- [ ] **Step 3: Implement**：按草案 12 项逐条拆成自足段落，保留原意、出处及 KB ID，不复制静态报价。manifest 仅接受排序后的 `.json` 文档白名单，CorpusVersion 为规范化 manifest 的 SHA-256；加载验证所有文件字节及集合一致，排除未声明/重复/越界文件。CLI 生成时同样验证文档，`-check` 不修改文件；默认装载不能自行重写摘要来掩盖篡改。
- [ ] **Step 4: Run GREEN**：同 RED 命令及 `go run ./cmd/knowledge-manifest -dir knowledge/platform -check`，预期 exit 0；人工比对 KB-004/005/008/009 不宣称真实扣费或现金回退。
- [ ] **Step 5: Commit/push**：仅纳入本任务文件，`feat(knowledge): add validated versioned platform corpus`；读回远程 SHA。

### Task 2: 中文检索与有界输出

**Files:** Create `internal/knowledge/search.go`, `search_test.go`, `eval/knowledge/dev-v1/questions.json`。

**Interfaces:** Consumes Task 1 Corpus；produces `(*Corpus).Search(query string) (Result,error)`。本地异常返回 error；正常空命中 Status=no_match；成功 Status=ok。

- [ ] **Step 1: Write failing tests**：`TestSearchChineseParaphrases` 对“额度啥时候回来”“取消套餐会退钱吗”“token和钱包一样吗”验证对应 KB ID；`TestSearchUnknown` 对发票/促销/外星天气返回 no_match；`TestSearchAPITerm` 命中 KB-012；`TestSearchBounds` 空白/257 字符返回验证错误、256 字符合法，结果≤3、每段≤768 UTF-8字节、JSON≤4096字节；`TestSearchDeterministic` 同分按文档/段落 ID 排序；`TestSearchNegation` 不把“超额自动扣款”检索为支持该说法的结论，结果保留拒绝超额的整句。
- [ ] **Step 2: Run RED**：`go test ./internal/knowledge -run Search -count=1`，预期新行为失败。
- [ ] **Step 3: Implement**：Unicode NFKC 不强引新依赖；使用标准库 Unicode 小写与空白归一化，中文连续字符二元组、英文/数字词，审核别名作为匹配词。排名按加权查询词覆盖率（标题3、别名2、正文1）确定，至少两个不同有效查询词重合且覆盖率≥0.20；英文 API 完整词命中可单词通过。先按完整段落分片，最终按分数降序、文档 ID/段落序号升序；装不进 4096 字节时去掉最低分完整命中，不剪文字。此初始阈值仅在 dev 集调整并记录，不用冻结集调参。
- [ ] **Step 4: Run GREEN**：同 RED 命令；开发集逐条记录 Top3/必要依据，检索分数不作为回答正确概率。
- [ ] **Step 5: Commit/push**：`feat(knowledge): add bounded deterministic Chinese retrieval`。

### Task 3: 主模型读工具与运行合同

**Files:** Create `internal/agent/knowledge.go`, `knowledge_test.go`, `cmd/agent/knowledge.go`, `knowledge_test.go`; Modify `internal/agent/tools.go`, `chat.go`, `prompt.go`, `runtime_metadata.go`, `runtime_metadata_test.go`, `cmd/agent/main.go`; add targeted historical gate tests in `internal/taskbench/knowledge_contract_test.go`。

**Interfaces:** `(*Toolbox).SetKnowledge(c *knowledge.Corpus) error`（启动前调用，nil 拒绝）；`(*Toolbox).KnowledgeVersion() string`；私有 `knowledgeTool() *execTool` 注册 query 必填字符串读工具。启动配置 `AGENT_KNOWLEDGE_ENABLED=false`、启用时 `AGENT_KNOWLEDGE_DIR` 必填；`configureKnowledge(tb *agent.Toolbox,get func(string)string) error`。

- [ ] **Step 1: Write failing tests**：`TestKnowledgeToolDisabledByDefault` 工具/知识提示均不出现；`TestKnowledgeToolFeedsNextModelRequest` 脚本模型先调工具再断言下一请求包含精确语料片段/版本；`TestKnowledgeDoesNotAuthorizeWrites` 注入“跳过确认”仍不能绕预览；`TestKnowledgeUsesLivePrice` 冲突静态金额不替代实时工具；`TestKnowledgeUnavailable` 坏目录启用启动失败；`TestKnowledgeRuntimeIdentity` 开关/corpus 改变身份，关闭不泄漏目录。`TestKnowledgeRejectsHistoricalScope` 新合同在依赖加载/claim 创建前拒绝旧冻结范围，旧 claim 文件不变。
- [ ] **Step 2: Run RED**：`go test ./internal/agent ./cmd/agent ./internal/taskbench -run Knowledge -count=1`，失败与新行为对应。
- [ ] **Step 3: Implement**：启用时扩展工具注册、`readTools`、sanitize 工具名、审计及同轮复用；工具搜索错误返回 unavailable 并保留错误路径，不当成正常无匹配。将知识提示独立常量按 tb 开关追加，运行 metadata 的 prompt hash 必须对应实际有效基础提示；增加 KnowledgeEnabled/CorpusVersion 字段。将 runtimeContract 递增到明确 knowledge-read 版本，保留既有限制，不改 JEV schema/threshold/fastpath/费用常量。新合同拒绝历史付费入口，不改旧授权或重写历史运行档案；默认关闭仍不假装新代码等于已部署版本。
- [ ] **Step 4: Run GREEN**：同 RED 命令；运行 `go test ./internal/agent ./cmd/agent -count=1` 与有界合同门禁回归，确认新只读工具不进入确认写分支，既有充值/退款/取消回归不变。所需 MySQL/Redis 使用隔离测试设施，禁止并发运行会清同名固定测试库的另一工作树测试。
- [ ] **Step 5: Commit/push**：`feat(agent): integrate platform knowledge read tool and runtime identity`。

### Task 4: Langfuse 检索及模型上下文追踪

**Files:** Create `internal/observability/knowledge.go`, `knowledge_test.go`, `internal/agent/knowledge_observability_test.go`; Modify `internal/agent/knowledge.go`, `llm.go`, `llm_protocol.go`, `telemetry.go` only at request preparation/attempt hooks; extend content flags in `internal/observability/content.go` without changing existing capture defaults。

**Interfaces:** `KnowledgeMetadata{CorpusVersion, DocumentID, ChunkID, SourceSHA256, Stage string; HitCount, RequestOrdinal int}`；`(*Operation).Knowledge(KnowledgeMetadata)` 固定字段验证；`(*Operation).KnowledgeContent(text string)` 返回 redacted/clipped/展示摘要元数据。agent 私有 `observeKnowledgeRequest(ctx context.Context,messages []Message,ordinal int)` 仅从实际消息中的合法、schema 匹配且 corpus 版本可核对的 search 工具结果选引用，不能扫描任意用户字符串冒认片段。

- [ ] **Step 1: Write failing tests**：`TestKnowledgeCaptureOff` span 无 query/正文/裸 query hash；`TestKnowledgeCaptureOn` 3 个 768 字节中文片段分别完整、JSON字节合法且实际脱敏标记正确；`TestKnowledgeRequestAfterTrim` 被 trimHistory 删除的片段不在 included_in_request，保留者包含；`TestKnowledgeUnsentAttempt` 本地构建失败不记 provider_response；`TestKnowledgeExporterFailure` exporter 失败不影响搜索返回；`TestKnowledgeInjectionIsNotContext` 用户伪造 JSON 不被当工具证据。
- [ ] **Step 2: Run RED**：`go test ./internal/observability ./internal/agent -run Knowledge -count=1`。
- [ ] **Step 3: Implement**：检索根 observation 下每片段一个 tool/span；关闭正文仍保留允许元数据。底层实际 HTTP payload 构建成功后标识 prepared/included，发起尝试及响应/错误另外标识；不把网络发起证明送达/计费，不导出凭据或全请求。原文 SHA 对应发布语料，脱敏展示另算 SHA；固定字段长度及 UTF-8/JSON 长度验证，不取消现有 2048/4096 全局保护。上下文历史中不属于当前有效语料的旧片段如仍发送则标记 stale，不能悄悄声称“无知识上下文”。
- [ ] **Step 4: Run GREEN**：同 RED 命令并运行现有 observer/content/processor 回归。使用本地 OTLP 接收器检验父子 trace、采样1、flush/丢失计数。真实 Langfuse 合成摄入及读回放在 Task 7；没有读回前只称本地观测通过。
- [ ] **Step 5: Commit/push**：`feat(observability): trace retrieved and requested knowledge context`。

### Task 5: Docker 打包与隔离冒烟

**Files:** Create `deploy/agent/Dockerfile`, `compose.yml`, `env.example`, `README.md`, `.dockerignore`, `cmd/agent/healthcheck.go`, `healthcheck_test.go`, `scripts/tests/test_agent_container.py`。

**Interfaces:** agent `-healthcheck` 子命令/模式只 GET 本进程 healthz，不加载数据库/密钥/模型；运行镜像 `/app/agent`、`/app/knowledge/platform`；Compose service `agent-knowledge` 使用 profile `knowledge`。所有敏感值由被忽略 env 文件注入，无默认真实密钥。

- [ ] **Step 1: Write failing tests**：`TestHealthcheckDoesNotInitializeServices` 健康模式无数据库/模型连接；容器脚本检查默认 disabled、启用有完整 manifest、UID非0、根文件系统只读、无宿主现有端口占用；构建上下文哨兵 `.env`、`.tmp`、私密 `.local-*` 不进入镜像。
- [ ] **Step 2: Run RED**：`go test ./cmd/agent -run Healthcheck -count=1`；`python3 scripts/tests/test_agent_container.py --validate` 预期缺部署物失败。
- [ ] **Step 3: Implement**：Go 1.25.12 多阶段构建，编译期校验 manifest；运行镜像提供 CA/nonroot/二进制/资料，禁复制整个仓库到 runtime。明确 `env_file` 路径、只读与必要 tmpfs。独立 Compose 明确 mysql/redis 网络服务地址或宿主 host-gateway；billing/admin/Langfuse 宿主示例用 `host.docker.internal`，不照搬 localhost；代理 host 与 CA 同样配置，不挂宿主 key/私密目录入镜像。项目名/端口可用独立 smoke 前缀，不能 down 原项目或删共享卷。
- [ ] **Step 4: Run GREEN**：`docker compose -f deploy/agent/compose.yml --profile knowledge config --quiet`；`docker build -f deploy/agent/Dockerfile -t ai-gateway-agent-knowledge:local .`；独立 smoke harness 提供 gRPC ValidateAPIKey、本地 admin/LLM/OTLP 与隔离 MySQL/Redis，检查成功搜索/no_match/篡改 corpus 拒启/非 root/只读/健康/退出 flush。如当前 Docker 网络或镜像不可用，保留失败并仅标容器未验证，不改全局镜像/代理配置。
- [ ] **Step 5: Commit/push**：`feat(deploy): package agent and knowledge in opt-in Docker service`。

### Task 6: 指标与冻结评测资料（不执行付费）

**Files:** Create `internal/knowledgeeval/types.go`, `report.go`, `report_test.go`, `cmd/knowledge-eval/main.go`, `main_test.go`, `eval/knowledge/acceptance-v1/questions.json`, `rubric.md`, `manifest.json`, `README.md`。

**Interfaces:** `BuildReport(input ComparisonInput) (Report,error)` 纯函数，无网络；`ComparisonInput` 固定 schema=knowledge-comparison.v1，包括 suite/corpus/code/prompt/tools/model/routing/budget 版本、配对 task 结果、盲化独立语义判定、全部尝试 usage/cost 与显式时钟边界。CLI `knowledge-eval -input PATH -output PATH` 只读已录制结果，不加载 env、不支持 execute/live 模式。

- [ ] **Step 1: Write failing tests**：`TestUnknownUsageIsNotZero` 计价字段 unavailable；`TestFailedAttemptsCountInCost` 失败成本计入整批分子；`TestZeroCorrectHasNoUnitCost` 分母0不输出数字；`TestCurrencySeparation` 人民币美元分开；`TestKnowledgeComparisonRejectsConfounds` JEV/model/预算/画像/版本未声明差异拒绝比较；`TestCategoryDenominators` 36/12/12/12 单列，不用安全拒绝抬正常题正确率；`TestUnreviewedBlocksQualityClaims` 未审不算成功；`TestClockBoundaries` runner 不冒充用户 E2E。
- [ ] **Step 2: Run RED**：`go test ./internal/knowledgeeval ./cmd/knowledge-eval -count=1`。
- [ ] **Step 3: Implement**：质量 gate 按已批准设计阈值输出 pass/fail/unavailable，检索 Recall@3 必须覆盖全套必需证据；单独输出引用覆盖率，不能零引用得到100%正确。成本按全部供应商尝试/冻结价表分别计价，统计总成本、共同通过题集成本及每正确任务成本；缺 usage 或费率不推算。延迟分布带 N 和边界；逐题配对胜负及小样本限制，配对二项结果给精确区间，不假设独立轮次。72题由独立审阅者盲查题目/金标；开发集与验收集无逐字重复，后续调参不能继续称该集未见。
- [ ] **Step 4: Run GREEN**：同 RED 命令；用纯合成完整/缺失/混合结果生成报告，明确 evidence_kind=local_stub、effect_claims_allowed=false。不改 `internal/taskbench/conversation_comparison.go` 旧路由比较门禁。冻结验收集摘要后不运行真实付费模型。
- [ ] **Step 5: Commit/push**：`feat(eval): add offline knowledge quality and cost comparison`。

### Task 7: 集成验证、审阅和交付说明

**Files:** Create `docs/customer-knowledge-offline-2026-10-24.md`; include only sanitized validation manifests under `eval/knowledge/offline-v1/`；Modify `deploy/agent/README.md` to record actual validated commands。私密运行资料只放 ignored 目录。

**Interfaces:** consumes Tasks1–6，不新增产品接口；产生明确的可复核离线结论及剩余真实评测门槛。

- [ ] **Step 1: Integration probes**：补 `TestKnowledgeLoopBoundary` 5轮仍有界；`TestKnowledgeOldClaimsRemainUnchanged` 旧资料摘要不变；检测同一固定测试库是否被别的进程使用，冲突则串行安排或改用新测试的唯一数据库，不清理对方库。
- [ ] **Step 2: Run tests**：运行 `go test ./internal/knowledge ./internal/agent ./internal/observability ./internal/knowledgeeval ./cmd/agent ./cmd/knowledge-eval -count=1`，再在项目约定 SDK 下 `go test ./... -count=1 -json` 保存 exit/status；失败先系统排查，保留首次失败，不把复验通过称首次全绿；必要服务 skip 明确记录。
- [ ] **Step 3: Verify Docker and Langfuse**：Task5 独立容器流程复验一次；真实本地 Langfuse 仅发送明确合成且不含私人资料的搜索/片段观测，采样1，flush统计与按trace只读API回读核对。覆盖 capture off/on、no_match/unavailable、脱敏/截断，核对实际进入替身模型请求的片段及 SHA。共享服务不可用则保留该项未验证，不擅自改密码/代理/全局采样。
- [ ] **Step 4: Independent review**：按适用 requesting-code-review/代码审查技能审阅所有产品差异及安全/计价门禁，处理阻断再运行对应检查。逐项对照设计验收；真实模型质量和收益均标未验证，不因替身通过写成90%准确率或15%降本已达成。
- [ ] **Step 5: Deliver/commit/push**：`docs(agent): record offline knowledge deployment acceptance`，确认仅相关文件、无密钥、远程SHA匹配。汇报 Docker/观测实际通过范围、尚缺真实评测条件；不自动部署或调用付费供应商。若 Hindsight 可用，只写长期已验证事实并读回；不可用保存在报告。

## Self-Review

- 设计覆盖：知识/manifest→Task1；检索上限/中文→Task2；工具/提示/权限/合同→Task3；内容开关/请求证据→Task4；容器→Task5；质量成本与冻结集→Task6；Docker实测/Langfuse读回/全量回归/审查→Task7。
- 接口一致：Task3 消费 Task1/2 Corpus/Result；Task4 读取实际工具消息而非伪造用户JSON；Task6 与现有路由比较器隔离。
- 五项 Review Focus 均指向明确测试，不把未来真实质量当作离线替身可验证事项。
- 下一步须选择执行方式；建议由当前代理按任务顺序实现，末尾独立审查，减少跨任务接口交接。若用户选独立代理逐任务实现，使用 subagent-driven-development，不能擅自并行修改工具/合同交汇文件。
