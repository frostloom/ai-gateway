# 知识库独立对照测评 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 沿用此前原生顺序实现方式；本计划尚未执行。

**Goal:** 建立真实Handler知识关闭/开启的独立受控录制入口，先完成零付费验证，再准备精确付费范围。

**Architecture:** internal/knowledgepilot独立负责范围、画像、顺序、recording和永久claim。cmd/knowledge-live默认只读check；execute在全部授权与计价绑定就绪后才调用同模型两臂，不创建JEV客户端；记录转换到既有knowledgeeval。

**Tech Stack:** Go、现有agent/billing/store依赖、本地HTTP替身、独立MySQL/Redis fixture、Docker知识镜像、现有Langfuse、SHA-256与JSONL。

**Spec:** docs/superpowers/specs/2026-10-24-knowledge-controlled-eval-design.md

## Global Constraints

- 仅codex/customer-knowledge-rag工作树；不合并main、不改JEV分支、不部署、不修改旧claim/历史档案。
- 两臂routing_mode=model；JEV调用上限0；知识72题/144任务，原题序交替AB/BA，每任务独立账户/会话。
- 候选5次供应商尝试/任务、总720次、512输出tokens；须本地验证并在真实批准前冻结，不是已批范围。
- 价格和费用不完整一律不可用，公开模板allow_paid=false、human_authorized=false、rates_verified=false、ready_for_paid_run=false。
- 已批¥100+$1累计上限不重复询问；新真实范围和精确执行版本另需明确批准，不沿用旧claim。
- 严格evaluation客户端，发送前持久预留；首次执行故障停止、不自动重试/续跑；未知usage保留预留。
- 完整请求包络、实际合同和知识精确片段摘要绑定；金标不入模型；main当前累计claim清单不可用旧分支快照替代。
- Go验证进程级PATH=/Users/a1/开发工具/bin:$PATH、SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk。

## Review Focus

1. 多工作树/并行campaign领取导致预算重复分配：同一互斥域核对最新永久claim后才能领取。
2. 调换源码、语料、scope、工具或输出目录绕过一次性授权：绑定全部摘要，固定身份永不重新领取。
3. 未知usage、缓存归一化差异、发送后崩溃：不写0、不释放预留、不重发。
4. 两臂账户、pending或金标串入模型：独立fixture、明确S011/S012初态，原始请求检查无评分字段。
5. 中断前缀、零引用、缺审查或Langfuse缺失被误报成功：明确partial和unavailable，禁止真实收益结论。

---

### Task 1: 冻结配置、画像和发送前门禁

**Files:** Create internal/knowledgepilot/{types,scope,scope_test}.go；Create eval/knowledge/control-v1/{scope.example,profiles,order}.json、README.md。

**Interfaces:**
- Scope承载schema/id、revision、suite/corpus/profile/arm配置/endpoint/fee摘要、Limits及授权状态；禁止API key字段。
- `CheckScope(scopeJSON, billingJSON, sourceJSON []byte) (CheckResult, error)`严格拒绝未知/缺失/额外JSON；CheckResult仅包含Ready、Blockers与已校验公开绑定，不能产生执行能力。
- `LoadInputs(root string, scope Scope) (Inputs, error)`验证现有72题manifest和精确Evidence、知识manifest及逐题FixtureSpec。
- `BuildOrder(cases []knowledgeeval.Case) []ScheduledTask`按原题顺序交替AB/BA；FixtureSpec禁止生产用户ID，包含初始业务状态和pending。

- [ ] **Step 1: RED** 写TestScopeRejectsUnboundAndPaidTemplates、TestInputsRejectChangedSuiteCorpusAndProfile、TestOrderHasIndependentAlternatingPairs，断言原72题/144任务且类别计数36/12/12/12、同题配置一致、两臂ID不共享；模板Ready=false，金额null不可伪装0。
- [ ] **Step 2:** `go test ./internal/knowledgepilot -run 'TestScope|TestInputs|TestOrder' -count=1`，确认因缺实现失败。
- [ ] **Step 3: GREEN** 实现上述接口及模板。arm运行身份用真实Handler元数据计算，Off无corpus/工具，On加载指定知识；固定512输出、thinking与endpoint摘要。冻结profiles和order摘要，保留金标仅给评分器。
- [ ] **Step 4:** 相同测试通过；`git diff --check`；仅提交该任务相关文件。

### Task 2: 统一永久预算/claim及逐attempt记录

**Files:** Create internal/knowledgepilot/{claim,transport,journal,claim_test,transport_test}.go；若main已有统一预算互斥接口可直接复用，先做只读兼容性调查，不能同时维护两个互不协调的余额表。

**Interfaces:**
- `AcquireClaim(commonRoot string, scope Scope, deps []ClaimDependency) (*Claim, error)`：在共同预算互斥域重读/验SHA/合计永久预留并O_EXCL固定scope身份；返回前同步文件和父目录。当前历史路径不共锁，必须返回cross_entry_budget_uncoordinated并拒绝Ready。本任务只实现并验证这个阻断，不修改main；实际付费就绪依赖后续单独集成所有入口共锁/旧构建禁用，不能用无进程快照或人工串行替代。
- Claim不提供Release/Reset/Resume；ClaimDependency含原始完整SHA和币种预留。
- `NewTransport(upstream http.RoundTripper, claim *Claim, limits Limits, journal *Journal) http.RoundTripper`：只允许绑定目标，发送前核验完整payload包络、tokens、输出和调用次数并持久预留；返回后写原始usage与归一化证据；未知/错误后锁定停止。
- Journal是私密独占JSONL，保存任务/attempt关联、请求摘要、原始usage/响应/失败和时钟边界，不输出认证头。

- [ ] **Step 1: RED** 写TestClaimCrossWorktreeExclusive、TestClaimLatestCumulativeBudget、TestClaimRejectsMissingChangedDependency、TestClaimNotReissuedAfterFailureOrNewOutput、TestClaimRejectsUncoordinatedLegacyWriters；写TestTransportStopsAfterUnknownUsage、TestTransportCrashNeverResends、TestTransportRejectsOversizeBeforeSend、TestTransportCacheNormalizationAndFees。断言失败也永久预留、未知不归零、并发至多一次领取、全部发送均有先行持久记录。
- [ ] **Step 2:** 定向运行这些测试确认RED（全部本地临时目录/loopback，无真实凭据）。
- [ ] **Step 3: GREEN** 实现claim、Journal和严格Transport；缓存费率按已验证协议归一化，缺失字段停止、无静默fallback。收费门禁的上界来自实际payload证据和冻结token算法，不能只信scope里一个声明数字。
- [ ] **Step 4:** `go test ./internal/knowledgepilot -run 'TestClaim|TestTransport' -count=1`通过；检查所有测试仅loopback。提交。

### Task 3: 真实Handler对照collector与离线转换

**Files:** Create internal/knowledgepilot/{fixtures,runner,recording,runner_test,recording_test}.go；Modify internal/knowledgeeval/{report,report_test}.go仅增加runner_request时钟枚举；不改原RunAgentPair及其model/JEV合同。

**Interfaces:**
- `Run(ctx context.Context, o RunOptions) (Receipt, error)`：RunOptions显式依赖已领取Claim、Inputs、Provider/Observer和owned环境工厂，默认AllowExecution=false；Receipt.Status限checkpoint_pending/stopped，记录完整/部分任务和清理状态。
- `Comparison(record Recording, reviews []ScoredTask) (knowledgeeval.ComparisonInput, error)`：仅完整72题与全审查可转换；partial不可伪造完整Task；完整Langfuse逐条读回回执也是转换准入条件，缺失即拒绝正式报告。
- 实际工具执行调用真实Handler；知识B用Toolbox.SetKnowledge；强制model路由并注入若调用就报错的JEV测试哨兵。

- [ ] **Step 1: RED** 写TestRunnerRealHandlerOffOn、TestRunnerIndependentStateAndPending、TestRunnerNoJEVOrGoldLeak、TestRunnerStopsOnProviderFailureAndWrites、TestRecordingRejectsPartialMissingReviewOrEvidence、TestRecordingRequiresObserverReadback、TestRecordingRecallUsesFirstRetrieval、TestRunnerRequestClockNotEndToEnd。捕获完整请求证明A无知识工具，B的检索片段进入后续请求，S011取消无写/S012确认no-op；所有尝试含失败均入journal。
- [ ] **Step 2:** 定向执行确认RED；fixture使用本轮独立数据库/Redis命名空间，失败也查残留并只清理本轮资源。
- [ ] **Step 3: GREEN** 实现runner及collector，复用既有知识观测并记录精确Evidence。Task.Retrieved仅取首次检索返回的前三段；后续检索另存journal，不能覆盖/聚合提高Recall。报告器接受runner_request且EndToEnd=false，既有枚举保持不变。冷/热缓存按真实usage记录，不人为把两臂缓存视为相同。请求时钟与整个runner时钟分开。
- [ ] **Step 4:** 定向测试通过，最大5调用/任务、一次纠正共享循环的loopback边界通过。生成独立审阅包和映射，评分冻结后才合并A/B；缺失字段和零引用沿用现有拒绝/门禁。提交。

### Task 4: CLI、防误调用验收和付费前材料

**Files:** Create cmd/knowledge-live/{main,main_test}.go；Create docs/knowledge-controlled-eval-preparation-2026-10-24.md；Create eval/knowledge/control-v1/validation.json；必要时增加scripts/tests中的离线CLI测试。

**Interfaces:**
- CLI参数`-scope -billing-evidence -billing-source`默认check；仅显式`-execute -revision -out -release -env-file`才可能执行；check不能读env、建claim或连接任何服务。
- `run(args []string, out io.Writer) int`：成功check返回0但输出Ready可为false；非法输入2；执行停止1；只给脱敏信息。拒绝旧paired-live授权/计费schema及JEV配置透传。

- [ ] **Step 1: RED** 写TestCheckNeverLoadsCredentialsOrOpensNetwork、TestExecuteRequiresNewExactAuthorizationAndProvenance、TestOldAuthorizationCannotEnableKnowledge、TestChangedPayloadAndRevisionRejected，断言未通过前无密钥读取/fixture/claim/provider调用。
- [ ] **Step 2:** CLI测试RED后实现最小入口；原子输出私密回执，check也明确model_calls=0/jev_calls=0，不能把策略检查当完整环境就绪。
- [ ] **Step 3:** `go test ./cmd/knowledge-live ./internal/knowledgepilot ./internal/knowledgeeval ./internal/agent ./internal/knowledge -count=1`及全`go test ./... -count=1 -json`；保留首次失败，问题按systematic-debugging排查。Docker只核对同镜像corpus摘要和当前smoke；不改运行中的客服。
- [ ] **Step 4:** 本地替身Langfuse采集与精确读回；独立最终代码审查；修复发现后必要回归，保存失败与完整脱敏摘要，不上传自动全通过质量评分。
- [ ] **Step 5:** 冻结最终归档来源、payload/token上界、费率与累计claim证据。费用无法核实时模板保持false/null，不请求含糊执行批准；共享预算互斥未集成时继续标记未就绪、不请求实际执行批准；全部技术准备完成后给用户精确版本、72题/144任务、最高调用/金额与失败策略，只请求本范围一次付费批准。
- [ ] **Step 6:** 提交并推送本知识分支，核对远程SHA；无付费推理、部署或main合并。长期决定写ai-gateway记忆；不可连接则留本地记录且不称入库。

## 设计审阅与执行边界

当前仅完成准备设计和计划，所有实现复选框均未完成。依writing-plans要求，具体计划交用户审阅后进入原生实现；不重新选择执行方式。真实付费另按项目精确范围约定请求一次批准，¥100+$1总预算不重复问。
