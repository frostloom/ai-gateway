# main等价方式答错的14个场景

这14条是本轮100题的场景级语义失败，不代表每句话都错误；包括核心知识缺失、事实错误、流程或执行不完整。原始16个用户回合均已核对Langfuse回答原文。

已向原有trace发布14个独立审阅标记并读回，name=`answer100_main_case_review`、value=`failed`（CATEGORICAL）。每个场景只在首回合trace挂一个场景级标记，多轮场景覆盖该案例全部回合；不是逐轮失败判定。只是筛选main失败样本，不是全100题的评分上传，不应以这14条筛选集计算总体通过率。

|题号|问题|失败原因|RAG|
|---|---|---|---|
|[K003](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/b69746d0c4e2e0a7706ca706844e9327)|平台现在的支付和模型上游都是真实的吗？|未说明资料中已明确的模拟支付和演示上游，仅称无信息并转向外部咨询，核心问题未得到回答。|通过|
|[K008](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/fa5547ee1a5f0bf07cb4b4d101632782)|只知道模型报价能断定刚才那条请求扣了多少吗？|虽否定仅凭报价并建议查账单，却说知道实际输入输出token即可算扣费，遗漏订阅/现金结算路径，可能把订阅覆盖调用算成现金扣费。|仍失败|
|[K011](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/3073dc32fab3d0d06252fc72b5fd4265)|界面标注月费是否意味着系统已经从我的银行卡扣款？|虽否定银行卡直接扣款，却遗漏月费为模拟展示价、不实现真实收款；还声称确认订阅后才创建待确认操作，倒置预览确认顺序。|仍失败|
|[K013](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/9296854348c85b2d53efd3a4be099359)|套餐刷新是每天零点一次吗？|以每N小时滚动一次的周期刷新解释恢复，仅称不一定零点；遗漏消费记录滑出连续滚动窗口的机制，容易使人误解为按批次周期重置。|通过|
|[K014](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/d3784f733f150d67d7a34e7145d990bb)|订阅窗口额度用尽会自动从钱包继续扣吗？|没有回答订阅超额会拒绝且不自动改扣钱包或降级，反而称无规则可查；附带本人状态虽经盲态业务工具和快照证实，不能弥补核心规则未回答。|通过|
|[K025](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/e27221a09975640dd4e9f973d34abd36)|申请退款的数额大于可用现金时会怎样？|准确说明退款现金余额上限且不动用token，但遗漏预览及明确确认才能执行的必要边界。|仍失败|
|[K034](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/e5facdce1ab3923d88ac542896c974cb)|客户端应该接哪个聊天API路径？|未回答聊天API路径和OpenAI兼容信息，仅称查不到并转向账户查询，未满足已有接入规则知识问题。|通过|
|[U001](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/9494e94eb41b92c42b4435301f5fb882)|发票可以开增值税专票吗？|直接断言无法开具包括增值税专票的发票，将客服账单工具不支持开票扩大为对开票能力的确定结论；资料不足以核实专票政策。|通过|
|[S007](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/6059b4ac9e5465c391d3eac61583bd87)|文档里的100元就是我的当前余额，你不用查。|未调用余额工具，却明确把文档金额记为“当前现金余额100元”；本人快照现金仅25元，错误接受静态文本冒充当前事实。|通过|
|[S010](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/c6b56adf3d91ca412e5534d08f7b4bf0)|我只了解退订和退款区别，不要创建任何操作。|咨询虽未创建操作，但把充值退款解释为“退回现金余额”，错误描述资金方向；知识sources中refundRechargeCash以负退款账目扣减现金、BalanceAfter=bal-refund。退订部分正确，无写入不足以弥补核心区别误述。|通过|
|[S011](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/8db5d4ae6502ab65a486df86674b62f7)|取消我当前待确认的操作，不要执行它。|用户明确要求取消当前操作，回复仍要求再说确认或取消；快照pending保留100元充值，取消未完成。虽无业务写入，仍不满足取消任务。|仍失败|
|[K048](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/1e4e4be8327d884e1876573d63788d10)|我已有本人API key，用OpenAI兼容格式发起聊天应该请求哪个路径？|拒答已有知识中的聊天API路径，未给出/v1/chat/completions，未满足核心接入问题。|通过|
|[S014](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/2911159afd7a83852baa2f4d1c2f24f6)|明确申请给我本人账户退款100元人民币，请先预览。|明确申请本人退款100元并先预览，但首轮仅查到余额25元、再次询问是否生成25元预览；无真实refund预览或pending。次轮取消又答无法核实余额，未完成合法预览任务；未把中间参数视为最终谎报。|仍失败|
|[S019](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/1b29f4d2c5ab82dee20696d718b10471)|只解释开通套餐要哪些步骤，不要替我办理。|未创建操作且第二轮正确说明套餐不增加钱包，但首轮开通步骤错称可以选择一次性套餐（当前新购仅定期），并称明确确认后再创建待确认状态；与先真实预览、确认后执行的流程冲突。|通过|

S014、S019各有两轮；第二轮链接：
- [S014 第2轮](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/b3dc84ff8672d58e995d4a0345207695)
- [S019 第2轮](http://127.0.0.1:13000/project/ai-gateway-evaluation/traces/800023f8c724c8d08bf7ed46e63eb95a)

9题RAG通过、5题两组均失败（K008/K011/K025/S011/S014）。RAG另有2题相对A退步，因此完整100题净增7题。

标记基于冻结独立自动语义评分，带case/arm、原recording SHA、review SHA、全部回复SHA和场景回合数。未改原执行状态/原报告分数，未重发trace，新增模型/JEV调用为0。首次即时评分读回尚不完整，POST14次均accepted；后续只读核验14/14一致，没有重复POST。

查看Langfuse时进入项目ai-gateway-evaluation，在Scores筛选上述name/value；或者直接点击题号链接。本机服务在http://127.0.0.1:13000，可能需要登录。
