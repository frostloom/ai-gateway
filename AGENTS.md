# ai-gateway 项目记忆

2026-10-23 用户明确“批准本轮一次实际执行”，recharge-confirm-20261023已从83078437b01119abd6ad76530e5108f012affc80归档执行一次并stopped，覆盖上一条新范围未授权/零真实状态。实际1模型(1757输入39输出，配置计价¥0.003826非账单)+1JEV尝试；模型臂两轮六项独立复核通过，确认前未写、确认后100元仅一次入账，控制状态不变。JEV臂首轮transport failure3183.576584ms、无HTTP/usage，是否送达和收费未知；守卫阻止后续模型，确认轮未执行，任务failed，不能收益比较或把安全停止当完成。含历史累计25模型5JEV。独立固定claim永久消耗，10模型/¥0.337920+1JEV/$0.002688预留保留；新¥100+$1累计预算未分配额¥99.662080+$0.997312，不自动重试或重开旧claim。两trace三实际轮正文及14确定性评分已Langfuse读回，10观测无丢失；独立混合通过/失败审查单存，不上传全通过评分。原entry-outcome综合false保留，另查隔离库0；99旧证据不变。后续代理TCP/GET models200仅只读健康，不证明原失败根因或永久修复，无配置修改/重部署。记录docs/recharge-confirm-live-result-2026-10-23.md及同日live-result档案。继续目标仍是同等质量安全下比较JEV模型调用/总成本/完整任务耗时，不预设收益。

2026-10-23 已新增充值确认独立受控入口recharge-confirm-20261023，复用paired-live -retest但使用独立recharge-confirm-authorization.v1/计费/回执schema。固定recharge-002/confirm，两臂四轮共享10模型/¥0.337920声明预留+1JEV/$0.002688，绑定已部署056a760源码、35c117合同、修复离线manifest/acceptance及已批准¥100+$1预算SHA。固定永久claim记录预算摘要和预留，失败/换输出/换版本不重领；当前新预算只分配此一范围，未来范围须汇总永久预留再分配，不是通用多范围预算管理器。旧现金范围和计费不能混用，旧入口拒绝无身份新计费schema。本轮示例精确授权false、费率/token证据false、只读不就绪，无新真实推理/claim/客服重部署；这不撤销或重复询问总预算批准。新入口真实Handler隔离替身三路径、Go全项目18测试包及独立审查通过，隔离库0、99旧私密证据不变。档案eval/paired-pilots/2026-10-23-recharge-confirm-entry/，操作与剩余执行绑定见docs/recharge-confirm-entry-2026-10-23.md。

2026-10-23 充值确认边界修复056a760033186e4b768b320e50ec7b7186a32157已部署本机客服，覆盖文末该修复“尚未部署”的状态。归档构建（buildvcs=false，独立来源记录）安装SHA f4d6c6c0b4e7d7f819d00b1dc53ca3e1f5bb19168abe1b522b2e5052dbf553d5；两份.env.agent仅更新release，根.env不变，旧bin/配置备份保留。原launchd温和重启后新PID/release、四项模型配置、health/portal通过；解析阶段400实际返回合同35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1，trace994720ab0e63798ac48994dc90144ddf已Langfuse读回，模型/JEV均0。99份历史私密证据摘要不变，不等同真实模型质量通过。用户对人民币＋美元累计预算问题回复“100+1”，后续真实测评累计上限¥100＋$1已经批准，不再重复询问预算；不自动重试、不重置旧claim，不以余额授权额外支出。精确新案例/调用数/供应商payload/版本和累计预留仍须冻结绑定，预算批准不等于费率已核实。见docs/customer-recharge-boundary-deployment-2026-10-23.md和eval/paired-pilots/2026-10-23-expanded-budget/budget-ceiling.json。

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

Git 提交作者与提交者统一为 `frostloom <278895495+frostloom@users.noreply.github.com>`，在仓库本地配置维护。2026-10-22 用户明确授权一次性统一所有分支历史提交身份；该次操作须先备份、验证提交内容与拓扑不变，并对每个远程分支使用固定旧 SHA 的 force-with-lease。日常开发仍禁止强制推送。

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

2026-10-22 后续用户停止上述收费核查，要求运行中的客服改用 CC Switch 的 Pi DeepSeek 配置。现行客服配置已更新为 `https://api.deepseek.com/v1`、`deepseek-flash`、`openai`，密钥与 Pi 文件及 CC Switch 相同，仅存本机被忽略的根 `.env`（0600）；私密备份在 `.tmp/paired-live-private/customer-model-20261022/`。经原 launchd 服务组温和重启后，运行进程四项配置比对、模型列表认证、客服健康和用户入口检查均通过。以本条覆盖旧千问“当前客服配置”描述；历史千问评测材料不代表现行客服配置。详情见 `docs/customer-service-pi-model-2026-10-22.md`。

2026-10-22 评测前核对发现 launchd 使用的 `bin/agent` 仍为 `58073a8` 的旧构建。已从 main `af0ba14` 重建并部署，主目录及启动器引用的旧工作树 `.env.agent` 的 release 更新为完整 SHA 加 `-local`；运行进程 Pi 配置及新 release 的 Langfuse trace 已读回确认。构建保留 `vcs.modified=true`，不代表付费执行器的 pristine build 条件通过。备份在被忽略的 `.tmp/customer-readiness-20261022/backup/`。六个相关 Go 包与 25 项 Langfuse Python 测试通过，无真实模型/JEV 推理。后续评测先适配仍锁定千问的执行/检查入口，详见 `docs/customer-service-readiness-2026-10-22.md`。

2026-10-22 AI 客服评测入口已新增 Pi v2 目标计划支持（代码 `2e98b1e`）：`paired-eval` 离线准备、`paired-live` 及首批库会绑定新地址/模型/协议；v2 授权须独立匹配新计划 SHA 与 model_base，旧千问授权和收费证据不能混用。六案例准备档案见 `eval/paired-pilots/2026-10-22-pi-customer-preflight/`，保持 allow_paid=false、ready_for_paid_run=false、零真实推理。Go 全项目18个测试包通过，Python162项含15项跳过，独立审查通过。新 `langfuse-eval.py check --campaign-plan` 只读显式env文件，输出仅为settings检查，不声称runtime绑定通过。本机运行客服未设 JEV_MODEL；新增无密钥 `evaluation.env.example` 只为评测pin到jev-1.13.0，未应用于运行服务。覆盖上条“后续还需适配入口”的待办状态；详情 `docs/pi-customer-evaluation-entry-2026-10-22.md`。

2026-10-23 已补齐 Pi 第二批检查点续跑入口：`paired-live -continue-from <首批原目录> -review <独立复核>` 只选择 batch-002，复用原 campaign 永久预留与原 claim，单批结束再次停止。执行还要求单独绑定 plan/首批 receipt/review SHA 的 continuation-authorization；本次用户只批准离线开发，未授权或执行第二批真实付费调用。首批实际已完成 2 次模型 / 1 次 JEV、语义复核及独立评分同步；第二批当前仍未启动。原计费账本、预留标记和清理结果均须完整，失败或中断不能换目录重试。Go 全项目18个测试包及独立审查通过，操作说明见 `docs/pi-batch002-continuation-2026-10-23.md`。

2026-10-23 用户随后明确批准并已执行第二批真实评测（balance-020/explicit_scope_followup）：实际6次模型/1次JEV，模型配置计价¥0.023126（非供应商账单），完成后停在checkpoint_pending，第三至第六批未启动。Langfuse四轮内容与14项确定性评分已读回。独立复核两臂均failed：第二轮明确查询现金余额时仅沿用首轮结果，无新get_balance读取，不满足冻结expected_actions=[read]；金额事实和无写状态仍正确。不得把结构通过或正确金额算作全任务通过，不改案例或自动重跑。原评分保持needs_review/unknown，现有独立评分入口只接受passed，本次未发布独立通过评分。累计实际8次模型/2次JEV，原campaign永久累计预留模型10次/¥0.337920、JEV2次/$0.005376。证据见 docs/pi-second-live-checkpoint-2026-10-23.md。覆盖此前“第二批仍未启动、未获执行授权”的历史状态；新的重测范围仍需明确。

2026-10-23 第二批失败的离线修复已完成：问题是模型直接沿用历史现金余额正文，非跨轮readResults缓存泄漏；执行层增加有限中文现金金额的本轮成功get_balance证据校验，缺失时最多纠正一次，再缺失则返回无法核实且不显示金额。历史/举例/用户引语分句保留，预览附带的无依据金额被移除但确认绑定保留。评测器对expected_actions=[read]强制成功读取trace，澄清/read备选仍需语义复核；离线重放实际第二批日志现能抓住两臂缺读。Go全项目18包与独立复审通过。本轮零新增真实模型/JEV请求，未部署运行客服，未重置原claim/预算或重跑旧批次。守卫不是通用事实验证，纠正可能增加模型调用；新真实复测须明确计划和预算范围。说明见docs/current-balance-read-fix-2026-10-23.md。

2026-10-23 本机运行客服已部署现金余额读取修复：从已测试提交80051a64d9abbd2c9b5bc0d16c0622753ebc3ae5的源码归档构建（buildvcs=false，明确来源清单），安装bin/agent，并将主目录及原启动器引用的旧工作树.env.agent release更新为完整SHA加-local，保留旧二进制/配置备份和旧目录。经原launchd温和重启后，新PID、release、四项模型配置一致性、客服健康和18080/portal均确认；一个解析阶段400请求未推理。启动时发现Kafka探针JVM只读查询需18.84/24.02秒，原5秒超时误判；Compose探针改为interval15秒/timeout30秒，保留30次重试、镜像与数据卷，Kafka实际healthy。Langfuse Web反复重启，API读回暂不可用，新release trace未确认；不要把健康检查当成遥测或真实模型质量通过。当前没有新增付费重测，旧批次claim保持消耗，说明见docs/customer-current-cash-deployment-2026-10-23.md。

2026-10-23 Langfuse采集链路恢复：排查时发现Docker API socket短暂消失，打开原Docker Desktop并等待引擎恢复；本轮未改Langfuse/全局资源配置、镜像、密钥或数据卷。恢复后六项服务healthy且90秒无重启，项目健康认证通过；新错误JSON400请求的80051a6-local trace ab923259cbb20718bea4a965fc758f4c已读回，llm_calls/jev_calls均0。原第二批四轮内容及14项评分仍可读，failed复核不变。旧部署请求未读回的历史回执保留，新恢复回执单独归档；以本条覆盖当前采集仍未确认状态。Web历史重启和引擎中断完整根因未确认，VM/后端oom-kills上报增长不能单独证明具体容器OOM；不能声称永久解决。详情docs/langfuse-recovery-2026-10-23.md。本轮未付费重测。

2026-10-23 已生成修复后独立单案例复测离线提案cash-read-retest-20261023（档案eval/paired-pilots/2026-10-23-cash-read-retest-offline/，文档docs/pi-cash-read-retest-proposal-2026-10-23.md）。原balance-020/explicit_scope_followup两臂各两轮不变；提议共享12次模型/1次JEV，声明预留¥0.405504/$0.002688，含每轮最多一次纠正的规划包络，超限停止。当前human_authorized=false、allow_paid=false、ready_for_paid_run=false，零真实调用/未领claim。完整原六批加此复测需36次模型/¥1.216512、7次JEV/$0.018816，不能说仍在原24/¥1/6/$0.02授权内。现有paired-live不支持该独立范围，旧授权配新plan已验证在凭据前拒绝；下一步先实现新受控入口和独立claim/账本、绑定最终构建与计费风险证据，再明确请求付费执行批准。独立档案复核通过不等于真实模型复测通过，旧第二批failed和claim保留。

2026-10-23 独立现金复测受控入口已实现：`paired-live -retest` 接受独立 `cash-read-retest-authorization.v1` 与 `paired-retest-billing-attestation.v1`，绑定固定 cash-read-retest-20261023、单案例plan SHA、最终40位execution_revision和旧失败日志/复核摘要。共享12次模型/¥0.405504与1次JEV/$0.002688；默认false示例只读且未就绪，不加载env、不领取；显式执行仍需新付费授权及计费证据，原授权不能混用。固定 `.tmp/paired-live-retests/cash-read-retest-20261023.json` 独占同步领取，失败或换目录/版本不释放；独立账本，完成只停checkpoint_pending。Go全项目18包及本地四轮逐轮读取、提供方/JEV失败停止测试通过，独立代码审查无重要问题；本轮未真实复测，原第二批failed/claim保留。以此覆盖上条“尚不支持该独立范围”的待办，操作见docs/standalone-cash-retest-entry-2026-10-23.md；新的付费复测仍须明确批准。

2026-10-23 用户随后在明确供应商、合成payload与预算后回复“批准”，独立cash-read-retest-20261023已使用5bf242a归档构建真实执行一次并stopped，两臂failed；覆盖此前未获批准/零新增复测状态。实际8模型/1JEV（与旧两批累计16/3），模型输入14742输出259，配置计价¥0.031556非账单；独立永久预留12/¥0.405504、1/$0.002688，旧账本及20项证据未变，新claim消耗不释放。两臂首轮get_balance成功、现金¥5000.00事实正确且区分token；自动fact not_evaluated是表达解析未覆盖。第二轮各纠正一次后仍无新读取，均仅返回“本轮未能核实当前现金余额，请稍后重新查询。”，读取与答案要求失败，诚实限制不等于任务成功。独立12项复核、四轮Langfuse内容SHA及14项失败评分读回已完成；17项观测导出无丢失。原entry-outcome综合false保留，另查MySQL残留隔离库0，不虚报全部清理通过。无重试/后续付费授权，无客服部署；下一步离线改善读取执行，不能把有限纠正说成已保证fresh read。详情docs/cash-read-retest-live-result-2026-10-23.md，脱敏档案eval/paired-pilots/2026-10-23-cash-read-retest-live-result/。

2026-10-23 真实复测后的离线读取执行修复已完成：保留一次回复纠正，之后模型仍报无本轮成功读取依据的现金金额时，仅完整明确的本人现金/RMB查询可恢复一次get_balance({})；走鉴权tenant、原Toolbox与真实trace/audit/耗时，直接返回权威工具结果，不加模型调用或伪造tool历史。含糊/历史/引语/否定余额查询/他人/复合写/未知分句、待确认操作、同轮已尝试过余额读取（包括失败）、上下文取消均不额外恢复；失败保持限制、不重试。不是通用NLU，也不保证模型不报金额或所有问法都触发。两路由共享策略，runtimeContract摘要已改变；覆盖旧“执行层不会自动选择读取”的当前实现描述。旧handler整例回归先失败，新handler四轮读取通过，替身模型持续遗漏第二轮工具仍为8模型/1JEV，完整Go18包及独立审查通过。原真实失败日志重报仍failed，claim与账本源摘要未变；本轮0新增真实评测推理、未部署客服、无新付费授权。详见docs/cash-read-recovery-offline-2026-10-23.md及对应离线validation.json。

2026-10-23 一次现金读取恢复4c715556d893a5bf8dadcd6a164310692ec2570a已部署本机客服，覆盖此前“该修复未部署”的当前状态。从已测试提交归档构建cmd/agent，MacOSX26.5.sdk，buildvcs=false明确来源；安装SHA17a15bc898dedaf74f2108bc99cda81678c35e8804de9a81f86ba9c2b49f8541。旧bin和两份.env.agent以0600备份在被忽略.tmp/paired-live-private/customer-cash-recovery-deploy-20261023/backup/；只更新主目录与旧工作树AGENT_RELEASE为完整SHA-local，根目录.env及模型配置不变。原launchd服务组SIGTERM温和重启，新PID/release、四项模型配置、客服健康和18080/portal通过；解析阶段400请求返回新执行合同SHA b92458d1bafd3187a2daa6387b0575e9517da84f66df6602b1945a8d47ba9d8e，精确trace b9e7960bed1f5f0661eeca578d3e5c8e已Langfuse读回，模型/JEV均0。旧启动器/工作树/数据卷保留，原付费证据摘要未变；未新付费复测，旧failed/消耗claim不能重用。详情docs/customer-cash-read-recovery-deployment-2026-10-23.md及deploy/local-services同名JSON。

2026-10-23 现金 fresh-read 恢复已部署后，新增独立固定复测入口 `cash-read-recovery-retest-20261023`（v2 授权/计费/回执）。绑定客服源码 `4c715556d893a5bf8dadcd6a164310692ec2570a`、合同 `b92458d1bafd3187a2daa6387b0575e9517da84f66df6602b1945a8d47ba9d8e` 及上一轮失败证据；新旧 claim 永久独立，换版本/输出不能重领。离线材料见 `eval/paired-pilots/2026-10-23-cash-read-recovery-retest-offline/`，默认授权 false，尚未获本轮付费批准、未领取新 claim、零新增真实推理。此前批准仅用于已经消耗的旧复测，不可沿用。18 个 Go 测试包及独立审查通过；详见 `docs/cash-read-recovery-retest-preparation-2026-10-23.md`。

2026-10-23 用户在明确合成payload/DeepSeek/TypeSafe及12模型/¥0.405504、1JEV/$0.002688后批准新的 `cash-read-recovery-retest-20261023`。已从 main `c6e32305663ab8af5e747de683390b78dbfe844b` 归档执行一次：实际8模型/1JEV，两臂12项独立复核全部通过。第二轮各纠正一次后由执行层cash_read_recovery新读余额，不能声称模型主动fresh-read或JEV收益。配置模型计价¥0.031572非账单；含历史累计24模型/4JEV。19项观测、4轮内容、14项确定性及4项独立评分读回，清理回执true且隔离库0。新claim已永久消耗，不自动重试，不沿用该批准；旧失败及32份既有证据哈希不变。本条覆盖上一条本轮“尚未付费批准/零新增推理”状态。结果见 `docs/cash-read-recovery-retest-live-result-2026-10-23.md` 和同日recovery-retest-live-result档案；未部署客服。

2026-10-23 现金事实解析已离线补齐：仅支持回复直接开头的“当前现金余额为…”/“你的当前可用现金余额是…”等新增明确金额；含后续“如果”建议时只检查开头独立首句的首条现金断言，条件/转述前缀、引述/假设和账本范围保持保守。18类边界回归经RED→GREEN，全项目18个Go测试包通过；独立审查两项P2已用反例修正。同一恢复复测journal离线重评仅fact_checks变化，两臂仍needs_review，不替代fresh-read和语义复核。衍生档案 `eval/paired-pilots/2026-10-23-cash-fact-parser-offline/` 绑定新解析源码与原journal/report哈希，原报告/评分不改写，47份旧私密证据哈希不变；零新增真实模型/JEV、未部署。后续门禁见 `docs/cash-fact-parser-offline-2026-10-23.md`，已消耗付费授权不可沿用。

2026-10-23 第三案例充值确认离线验收已完成但总体blocked：真实Handler+隔离fixture+OpenAI替身/FakeJEV下，正常两臂2模型，支持纠正路径4模型；旧3次上限导致第二臂HTTP500/failed，底层RunAgentPair nil不能当任务成功。发现两项未修复阻断：预览前模型宣称充值成功不被纠正；重复确认可再生成充值preview/pending（测试未发生第二次入账）。诊断探针不锁定缺陷必须存在，正常行仍needs_review，不宣称真实模型质量。单confirm候选10模型/¥0.337920+1JEV/$0.002688只是声明，未授权/未准备付费入口，duplicate仅辅助离线场景。18个Go测试包通过，独立审查认可“发现阻断的验收交付”，47份旧私密证据不变、隔离库0，真实模型/JEV均0，未改运行逻辑或部署。下一步先修复这两项并更新运行合同/历史范围兼容，再考虑真实测评。文档docs/recharge-confirm-offline-2026-10-23.md，档案eval/paired-pilots/2026-10-23-recharge-confirm-offline/。

2026-10-23 充值确认两项边界已修复源码并离线复验（尚未部署）：空pending的完整确认词句在LLM/JEV前no-op，复合新申请仍路由；有限当前充值申请的leading充值成功断言最多纠正一次，耗尽诚实未执行；recharge预览附带错误成功文案替换，真确认成功不误删。运行合同升级v3/SHA35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1。旧cash恢复v2材料保留b92458d1原绑定，可只读但runtime_contract_mismatch，Ready/冻结执行/runner/claim均拒绝，收费证据仍验证；历史claim/档案不改写。六组本地捕获：正常2模型、纠正4、重复确认2且第三轮0LLM/0JEV、持续虚报4次后诚实taskfailed零写、接受纠正4次完成预览确认；旧3预算仍在第二臂耗尽。18个Go测试包通过，独立审查无阻断，原私密证据全不变/隔离库0。单confirm候选上限10模型/¥0.337920+1JEV/$0.002688为声明，用户允许提高预算不等于无限或新的精确付费授权，仍false、零真实模型/JEV。本机运行客服仍原部署4c71555/旧合同，下一步独立部署验收后再准备新执行范围。文档docs/recharge-confirm-fix-offline-2026-10-23.md，档案eval/paired-pilots/2026-10-23-recharge-confirm-fix-offline/。覆盖上条“两项尚未修复”的源码状态，原失败记录保留。
