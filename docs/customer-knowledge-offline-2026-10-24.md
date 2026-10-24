# 客服知识库离线交付与验收（2026-10-24）

分支 `codex/customer-knowledge-rag`，独立工作树 `/Users/a1/.codex/worktrees/customer-knowledge-rag/ai-gateway`，基点647ba9f；不合并正在推进的main或JEV路由工作树。本轮完成知识检索、客服接入、同镜像Docker交付、Langfuse内容追溯和离线指标报告。未替换运行中的客服，未发起客服真实模型/JEV调用、未新建付费范围或claim。

## 交付内容

- `knowledge/platform/`：12个公共主题，含稳定ID、日期、适用源码、来源与完整段落；不存当前报价、用户余额或私密配置。manifest逐文件SHA校验，集合摘要绑定发布，启动不重写清单。草稿、损坏、越界目录、符号链接及超限资料拒绝加载。
- `search_platform_knowledge`：中文二元组和英文词的本地确定性检索，默认关闭，开启时作为主模型只读工具。最多3段、每段768字节、整体4096字节；无命中与不可用分开，保留完整限制，不裁去否定词。
- 价格、订阅、余额、订单仍查现有业务工具。知识不是执行授权，充值退款等仍预览后确认；五轮上限、一次纠正、JEV策略不扩大。
- 新模式有独立提示词/工具/合同摘要与corpus版本。默认关闭仍保持原v5有效提示/工具/合同身份，不能据此声称新二进制已部署或与旧部署源码逐字节相同。现有paired-live不支持开启本知识模式；未来真实入口须单独绑定新身份和预算，不沿用旧claim。
- `deploy/agent/`：生产runtime为scratch、CA证书、非root65532与知识文件；opt-in Compose只读运行、不挂可变知识卷。smoke target才含验收替身，不能作为正式交付镜像。配置样例明确宿主与容器地址，部署操作留给后续独立步骤。
- `cmd/knowledge-eval`：只读录制数据，不加载密钥或调用模型；72题为36知识/12未知/12动态/12安全。质量、精确片段召回、引用、全部失败成本、独立币种、缓存及延迟分别统计，数据缺失不可用，local_stub不允许真实收益结论。

## 验证结果与限制

最终 `PATH=/Users/a1/开发工具/bin:$PATH SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./... -count=1 -json` 退出0：22个有测试包通过、27个包无测试。4项opt-in跳过：TestCashLoad、TestCashProfileRowTiming、TestLiveJevSemanticsDiagnostic、TestKnowledgeLangfuseReadback。最后一项另行显式执行通过，其他3项未在本轮开启。

首次完整回归为21通过/1失败，失败名为TestArchiveBuildScriptBuildsCommittedSourceAndRecordsHashes。系统PATH选中Python3.9，既有归档脚本的tarfile.extractall(filter=...)不受支持；现有Python3.12.15的进程级PATH下该测试独立通过，随后完整回归通过。首次失败保留，不修改脚本、测试门槛或系统Python，不称首次全绿。

Docker实际构建成功，运行镜像检查非root、知识清单存在、无私密环境/源码目录。两次独立随机Compose项目通过成功检索、no_match、只读非root、坏知识在数据库连接前拒启和本地OTLP。全部临时项目已清理，不动原有数据卷。

首次两次容器冒烟因mysqladmin ping在临时引导阶段过早通过而连接TCP被拒；仅将测试MySQL探针改为TCP认证查询指定数据库，并加入fixture readiness。后续一次gRPC鉴权3秒超时，根因未确认；加诊断能力后的独立复验及最终复验通过，无生产超时/代理/重试修改，不声称永久解决该次网络暂态。

## Langfuse 实际摄入与读回

使用现有本机项目配置，仅发送合成查询和公共知识；未调用评分写入API或客服真实供应商。最终四条trace按v2 observations接口显式读取core/basic/metadata/usage/io，共28项逐条读回，exported=accepted、dropped=failed=0：

| 场景 | Trace ID | 观测数 |
|---|---|---:|
| 正文关闭、正常检索 | 8b587cac465731543e41de3e3d70a50e | 9 |
| 正文开启、正常检索 | 0ac8f29869823e5a85ae223f1529f8d0 | 10 |
| 正文开启、no_match | 0037731465dd20c56cfc9d61ba065ade | 5 |
| 正文开启、unavailable | 4efec956aac7f4d6ad4e5a7498730bf6 | 4 |

关闭时没有input/output正文；开启时原文解码后逐片段相同，版本/ID/原文SHA可关联。检索、实际请求上下文、HTTP响应/传输错误分别记录；脱敏与截断有标记。HTTP响应不证明任务成功或模型语义采用。

初始新探针错误使用旧observations接口404，之后未请求v2的metadata/io字段；另有把Langfuse规范化布尔当字符串、把JSON字符串编码当明文的断言假设失败。通过只读查询同一trace核对后修正验收，不改生产采集逻辑，不重复写既有trace。包括失败探针在内，本次合成release累计10条trace/84项观测，完整列表另存，不能只当作最终28项或真实问答样本。

## 独立审查与修复

一次独立只读审查发现2项Important：必需评分字段缺失/null被当零、Recall只按文档前缀接受不存在的片段。另有知识答案引用覆盖为零仍可通过降本门禁，按“每个知识回答保留来源”要求提升为Important。

三项均由独立反例先RED，再修复GREEN：必需质量计数字段显式拒绝缺失/null；金标和命中使用corpus_version、chunk_id、原文SHA精确相等；知识答案引用覆盖要求100%，引用精度另至少95%。问题与事实金标未改，尚未真实执行前补齐精确证据字段并重新冻结摘要，旧版本仍在Git可追溯。最终全项目回归退出0，无第二次审查，无待处理Critical/Important或递延Minor。

## 实施决定

1. 默认关闭保留原v5身份，开启使用新增合同后缀与语料版本，避免重绑历史范围；未来付费入口仍须精确绑定，判断失误的代价为后续接入门禁返工。
2. 报价测试只证明实时工具和权威输入接入，不声称一般模型必然服从；真实语义仍需独立测评。
3. 使用Docker内置前端避免额外Docker Hub前端下载；旧Docker兼容性需另行处理。
4. 知识引用覆盖100%遵从每题保留来源要求；代价为更严格门禁。精确片段schema在真实执行前补齐，后续collector需遵循新结构。

## 证据与下一步

公开脱敏摘要在 `eval/knowledge/offline-v1/validation.json`，含成功读回回执、全部合成探针、合成算术报告和私密日志SHA。原始日志保留于本工作树被忽略的 `.tmp/knowledge-validation/2026-10-24/`，首次失败不删除。对照记录信任录制者与独立审查出处，不是密码学执行证明；合成报告中的满分等只是算术fixture。

快照的2个既有永久claim文件SHA未变；本分支没有修改旧tracked paired-pilots档案。没有部署、模型质量验证、真实收益结论或新增付费批准。首版完成的是工程与证据准备；90%正确率、95%召回、15%降本均为待真实受控对照验证的目标。

项目记忆：开发范围与边界已在ai-gateway库同步写入并实际召回确认；最终完成事实另作同步记录，具体响应保留在私密验收目录。
