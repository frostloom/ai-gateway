# 客服真实链路接入与回答事实核验（2026-10-13）

本轮完成免费评测准备：真实客服HTTP、会话状态机、工具、billing admin和临时数据库贯通，外部LLM和JEV替换成本地脚本。未读取.env，未向外部模型或JEV请求，未重跑1220条真实模型案例。旧1220输出、标注、分数与原30条归档保持不变。

## 执行与证据边界

`NewHandlerConversationDriver(handler, oracle)`直接调用指定HTTP handler，不读取服务器URL或密钥配置。`ConversationOracle`负责初始化账户、读取业务/对照账户状态和Redis pending，不能使用助手响应自报的状态。保留实际HTTP状态、原始响应、trace、telemetry、服务端pending和独立数据库快照。

本地测试接入真实`agent.Handler`和真实`billing/admin.Handler`，在随机临时MySQL库和私有miniredis上运行，结束删除测试库。LLM使用只监听本地的httptest服务器，JEV使用`jev.Fake`。身份走真实的内部管理员令牌和租户作用域路径，**不覆盖租户API key的gRPC鉴权或浏览器注册登录链路**。测试包内的oracle复用账户初始化设施，生产服务没有新增测试模式。

共15个代表分支：V1余额查询、V2高置信余额查询、V2低置信余额回退，以及V1/V2各自的充值确认、取消、重复确认、取消后再确认、追问后确认、退款余额上限。读场景用明确的本地smoke定义；写场景来自冻结定义。它们不代表已执行全部3071完整任务。

| 本地脚本场景 | 本地LLM HTTP请求数 | 能证明什么 |
| --- | ---: | --- |
| model余额读取 | 2 | 真实工具循环、回复与遥测贯通 |
| jev_selective高置信读取 | 0 | 脚本判定满足阈值时真实分流绕过模型 |
| jev_selective低置信读取 | 2 | 真实分流回退模型工具循环 |

15分支首次全部通过，本体15.24秒、包级19.441秒。各分支仍`needs_review`，完整任务正确率不可用。上表是预设依赖下的机制回归，不是JEV真实准确率、加速比或成本节省；本地模型请求不产生供应商费用。脚本不返回usage，token保持未知，不用估计值充当真实用量。

没有工具轨迹的回复标记`unreviewed`，缺标注不算已证明错误或已证明正确。数据库和pending仍照常校验；必要追问、取消、解释的语义需独立审核，不能从案例期望反填实际标签。必要追问计数只统计已有clarify标签的观测，未审核动作的计数不能解读成追问覆盖率。

## 事实评分

`CheckConversationFacts(result)`给出逐轮有限事实结果`match`、`mismatch`、`not_evaluated`。`RunFactCheckedConversation`在完整对话结构评分后追加事实错误：操作正确但已识别回答事实错误，则任务失败；`StructuralPass`只表示工具/数据库结构是否通过。符合事实不能自动通过所有回答语义审核。

当前支持明确的声明形式：现金余额/账本口径（元换分，用整数计算）、充值预览金额和实际到账差额、退款预览上限和实际扣减差额、本人订单号/已支付或已退款状态、当前订阅套餐ID、明确自动续费或停止续费状态、当前无生效订阅，以及没有本轮成功确认执行却宣称充值/退款成功。未来周期内已取消订阅仍有权益，不能误报不存在。

存在条件、引述或超出支持语法时保持`not_evaluated`，金额核验不提取任意token数字，不把“余额不足，需要充值100元”当余额100元。不支持的描述、遗漏关键事实、目录/模型价格、详细账单及完整额度自然语言仍需要权威数据与语义审核。即使人工审核通过，新的V1/V2比较也会再次核验可识别事实。

事实评分是新增入口，旧离线ReplayConversations评分路径保持原口径；既有30条报告不覆盖、不改分。需要重新解读的旧数据应生成新版本报告并标明评分版本。

## 本轮发现的真实缺陷

冻结取消表达“取消操作，不要执行”的首句目标“操作”没有被`isPendingCancellation`的完整句规则接受。系统会转模型且保留pending，后续确认可能执行已撤销意图的操作。本轮先用新增分类测试和真实数据库对话重现，再加入完整目标“操作”；否定取消、取消订阅、混合修改请求和疑问句继续保留原边界。扩展跨请求回归覆盖五种写工具。

另外修复评测接入问题：本地脚本最初漏了冻结案例首轮追问；订单号允许冒号，事实正则不得截成`agent`。前者是脚本配置错误，后者是新核验器错误，不冒充模型质量缺陷。

## 对照与复现

V1/V2可比条件见[对照契约](customer-service-v1-v2-comparison-2026-10-13.md)。入口固定model/jev_selective，校验模型、协议、提示词/工具hash、输出预算、profile定义、执行版本、案例顺序与服务端runtime；缺条件或不同条件不能声称效率改进。成功率必须双方完整任务审核结束；本地替身结果不发布模型质量成功率。

```powershell
$env:AGENT_LIVE_JEV='0'
go test ./internal/taskbench -run TestConversationRealAgentLocalDependencies -v -count=1
go test ./internal/taskbench -run 'TestConversationHandler|TestConversationFact|TestConversationComparison|TestConversationUnreviewedAction' -count=1
go test ./internal/taskbench -run TestConversationArchivedStoreEvidenceReplay -count=1
```

第一条需本地3307测试MySQL，Redis私有实例自动启动；基础设施不可用时明确skip，不能称执行成功。后两条纯离线。真实模型评测仍未启动，后续授权的小规模调用应另保存原始记录和价格依据。

修复独立审查发现的真实退款“退回”漏检及订单拒绝误判后，全项目Go与vet通过，Python96项通过（首次沙箱权限失败，授权环境复跑通过），六项历史verify通过。新的[15分支事件归档](../eval/conversation-regressions/2026-10-13-agent-local-v1/README.md)保留原始事件和30次本地模型HTTP请求计数，外部请求0；SHA完整性与旧30条重放回归通过。变更全过程见[面试记录](interview/CHANGELOG.md)，远程CI尚未核验。
