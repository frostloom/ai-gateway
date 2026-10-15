# Langfuse 正文、评分与启动检查

本轮仅使用本机假模型和 Fake JEV，向真实本机 Langfuse evaluation 项目验证采集与评分。没有请求外部模型/JEV，没有启动 1220 条付费评测，生产服务没有重启。原始 1220 案例、3660 首轮输出、历史失败和旧准备包保持原样。

## 验证范围与口径

四类任务覆盖余额查询、歧义后澄清查询、预览确认后充值、取消操作，分别运行 model / jev_selective 两臂。最终验证批次为 8 个任务追踪、14 个有问答正文的轮次、49 个节点；导出失败和丢弃为零。12 个 generation 来自本机替身，不能当真实模型调用、JEV 质量或省钱证明。

每臂回写七项确定性评分：evaluation_status、structural_pass、task_success、answer_review_status、fact_check_status、necessary_clarification_status、http_failures，共 56 项。全部八臂结构通过但语义仍 needs_review，task_success=unknown；这不是成功率 100% 或失败率 100%。部分事实匹配不代表整段回答正确，必要澄清保留独立审核状态。Langfuse 展示本地可信报告的评定，没有启用额外付费 LLM 裁判。

证据目录：`eval/paired-pilots/2026-10-15-langfuse-content-scores-v1/`。smoke.json 为实际运行回执，reports 为运行中生成的报告，score-plan.json 绑定 trace ID、案例、实验臂、证据类型和 journal SHA；score-verification.json 为独立只读读回。content-verification.json 只保留正文长度与摘要，密钥和私有配置不归档。manifest.json 固定文件摘要。报告仍需来自可信本地运行；SHA 防止归档变化，不是第三方真实性认证。

首次开发验证曾出现所有写入已接受、首次读回尚未全部可见。之后仅查询评分确认存在，没有重跑客服。评分同步工具使用固定 trace+指标 ID，无自动重试，失败保留部分回执；后续可先执行 verify，只读确认；需要补写时发布同一 ID，不重新推理。最终批次与早期开发批次的追踪不混合计数。

## 使用与边界

`scripts/langfuse-eval.py plan` 离线生成计划，`publish --execute` 显式同步，`verify` 只读核验，`check --preflight <目录>` 检查启动条件。每项命令必须指定新的 `--out`，不覆盖证据；评分最多每批 256 项。publish/verify 显式传入多个 `--report`；旧报告缺 observed_turns 时应从原 journal 离线重新生成新版报告，不猜轮数。

网络工具只允许 localhost/127.0.0.1 的 Langfuse 地址，禁止重定向转发项目密钥。评分前校验全部根节点关联、实际轮数、连续轮次与问答正文，异常停止写入。支持整数及规范数字字符串轮次。failed/setup_failed 不要求虚构不存在的回答。身份采用至少 32 字节 HMAC 密钥，正文经现有脱敏及截断规则处理；脱敏不能当作所有自由文本隐私的绝对保证。

显式配置顺序为 `.env`、独立项目私有 `.env.agent`、`deploy/langfuse/evaluation.env.example`，后者固定用户选择的千问 Anthropic 网关、deepseek-v4.1-flash、jev-1.13.0 和 evaluation 正文采集。私有文件不进 Git；配置存在不代表模型密钥已验证，check 不访问推理网关。

新准备包 `eval/paired-pilots/2026-10-15-observed-preflight-v2/` 绑定当前代码、冻结任务定义、六种运行 profile 和旧准备包 SHA；启动时重新推导核验。仍为 1220 主任务 / 2440 臂、另列 1851 边界场景，保留原预算包络及运行限制。启动检查通过不构成付费授权，ready_for_paid_run 始终 false。

旧 full-preflight-v1 的运行代码校验会因本轮代码变更拒绝当前版本，这是旧绑定的预期保护；原 manifest/proposal 字节保留，以 Git 历史及其 SHA 核验，新准备包另行重现。不能为了让旧校验通过而更新历史基线。

全量正式运行前仍须小规模真实配对验收、独立语义审核、最新渠道价格与明确币种总预算、用户付费授权，并验证每个实际批次的 Observer 注入、采集覆盖和评分读回。完整 1220 执行适配也须逐批确认；准备包不等于已执行全量。缺 usage 或渠道价格保留 unknown，不用 Langfuse 默认价格代替渠道账单。

## 工程验证

Go 全量测试与 go vet 通过；Python 全量 125 项通过。新工具覆盖未知任务结果、必要追问、部分事实、稳定评分 ID、失败不重试、错误追踪零写入、正文缺失、冻结 profile 篡改、轮次字符串及异常格式。冻结准备包可离线重现。独立审查发现的 HMAC 长度、冻结输入重新核验、正文轮次覆盖和索引类型问题均加入回归并修复。
