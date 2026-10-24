# 平台客服知识草案

状态：供产品核对，尚未作为运行时知识库加载。整理日期：2026-10-24。
证据基点：`647ba9ffc8f44d47792b2a5174ec53d08d410d88`。本草案描述该提交中的实现，不代表生产支付能力或新的服务承诺。

## 使用边界

本文只整理平台通用知识。不收录用户资料、密钥、实际订单、余额、当前在售价格或历史测评材料。当前价格、额度、订阅及订单状态必须通过业务工具读取；种子数据和前端默认值不能作为当前报价。
每条知识具有稳定 ID、依据和适用范围；进入首版运行库前需要核对。以下“建议答法”为代码支持的草案，不是已验证的模型回答。

## KB-001：平台提供什么

建议答法：平台提供模型目录、OpenAI 兼容的聊天调用接口、账户余额与用量查询、套餐订阅，以及 AI 客服。当前仓库包含演示用模型上游和模拟支付流程，不能据此承诺所有目录模型均已接通真实供应商。

依据：`README.md` 的架构及已知限制；`web/src/portal/views/CatalogView.vue`；`internal/agent/tools.go` 的 `listModels`。
适用：平台功能介绍。不要把开发测试能力写成上线保证。

## KB-002：现金余额和套餐 token 额度有何区别

建议答法：现金余额以人民币金额计量；套餐提供按模型档次计算的 token 使用额度，两者不是同一种余额。订阅开通不会给现金余额充值；通过订阅额度结算的调用不会扣现金，但仍会消耗 token 额度。零现金扣费不代表零用量。

依据：`internal/billing/store/mysql.go` 的 `FundingSubscription`、`Reserve`；`internal/billing/store/plan.go` 的 `SubscribePlan`；`internal/agent/prompt.go`。
动态信息：现金余额用 `get_balance`；本人剩余额度用 `get_my_subscription`。

## KB-003：模型如何计价

建议答法：模型目录分别列出输入和输出的每百万 token 单价。具体模型当前价格以实时目录为准；实际账单取决于用量和结算路径，不能仅凭目录单价断言某次请求已扣费多少。

依据：`internal/billing/store/models.go` 的 `Model`；`internal/agent/tools.go` 的 `listModels`、`readRecentBills`。
动态信息：`list_models` 查询在售目录；`get_recent_bills` 查询本人最近账单。目录底层价格单位为分/百万 token，工具展示元/百万 token。
限制：不承诺缓存折扣、供应商原价、发票、汇率或不存在的优惠规则。

## KB-004：套餐买到的是什么

建议答法：当前在售套餐是定期订阅，提供各模型档次在滚动窗口内的 token 额度。套餐目录给出月费展示、周期和额度配置；未使用的额度不累积。一次性套餐的历史代码仍保留，但当前订购入口只接受上架的定期订阅。

依据：`internal/billing/store/plan.go` 的 `Plan`、`SubscribePlan`；`web/src/portal/views/PlansView.vue`。
动态信息：套餐名称、月费和每窗口额度使用 `list_plans`，不复制成固定报价表。
重要范围：当前月费为模拟展示价；源码中的订阅和续期流程授予/延长权益，未实现真实月费收款，不能说已从银行卡或余额扣除月费。

## KB-005：额度如何刷新，用完后怎么办

建议答法：额度按套餐配置的滚动时间窗口计算，相关消费记录滑出窗口后，相应额度恢复；不是每天某个固定时刻统一清零。走订阅路径时，如果已用额度加本次估算用量超过档次额度，请求会被拒绝，需要等待额度恢复；当前没有超额后自动扣现金继续使用或自动降级模型的保证。

依据：`internal/billing/store/plan.go` 的 `SubscriptionAllowance`、`subscriptionConsumed`；`internal/billing/store/mysql.go` 的 `Reserve`、`subscriptionAllowancePath`；`README.md` 的 M9 已知限制。
限制：仅说明订阅结算路径；不能推广成所有模型、所有账户都走订阅。准确剩余值、窗口长度以本人查询为准。当前实现按账单时间近似滚动统计，不承诺严格无并发超额。

## KB-006：退订是否退款

建议答法：退订定期套餐会停止下期续费，当期不退款，本期权益可使用到周期结束。退订和退充值是不同操作，不能把退订当作现金退款。

依据：`internal/billing/store/plan.go` 的 `CancelSubscription`；`internal/billing/store/subscription_entitlement.go` 的 `getEntitledRecurring`；`internal/agent/tools.go` 的 `cancelSubscriptionTool`。
动态信息：本人到期日和续费状态用 `get_my_subscription`。实际办理须生成预览并由用户确认。

## KB-007：更换套餐何时生效

建议答法：当前定期订阅的套餐变更安排在下一周期生效，当期权益不立即更换。目标必须是有效的上架定期套餐；是否可办理取决于本人订阅状态和预览结果。

依据：`internal/billing/store/plan.go` 的 `ChangePlan`、`RenewDueSubscriptions`；`internal/agent/tools.go` 的 `changePlanTool`。
限制：不编造即时升级、差价补退或取消待生效变更等未核实能力。

## KB-008：充值和支付有什么区别

建议答法：创建充值订单不代表已经支付或到账。当前演示环境使用模拟支付；客服充值确认后会执行建单和模拟支付流程，不能说产生了真实银行扣款。申请充值时需提供正整数元金额。

依据：`internal/agent/tools.go` 的 `rechargeTool`；`web/src/portal/components/RechargeModal.vue`；`internal/billing/store/plan.go` 的充值订单状态和支付流程。
限制：不承诺第三方支付渠道、支付到账时效或真实收款。

## KB-009：充值退款能退多少

建议答法：支持按本人已支付充值订单或按申请金额预览退款。实际退款最多到当前可用现金余额；按订单退款还受该订单金额限制。预览不是退款完成，确认后以执行结果为准。

依据：`internal/agent/tools.go` 的 `refundRechargeTool`；`internal/billing/store/plan.go` 的 `RefundRecharge`、`RefundAmount`。
示例：申请退 100 元但可用现金只有 25 元，预览实际可退最多 25 元。此数字仅为解释示例，不能代替本人实时查询。
限制：余额为账户单池，不能解释为逐订单锁定的原路资金；当前退款是演示账户账务操作，不承诺真实原路到账或到账期限。

## KB-010：如何查消费

建议答法：可以查询本人近期每日扣费和最近消费账单。每日扣费统计与 token 用量不是同一个指标；部分最近账单不能代表完整月度消费。

依据：`internal/agent/tools.go` 的 `readConsumption`、`readRecentBills`；`web/src/portal/views/ConsumptionView.vue`。
动态信息：`get_consumption` 当前为近 7 天每日扣费；`get_recent_bills` 默认 3 笔、最多 10 笔，包含 token 和金额。当前工具不足以证明任意月份完整合计。

## KB-011：客服会直接办理吗

建议答法：询问流程不会自动办理。充值、退款、订阅、改套餐和退订需要必要参数，先给预览，明确确认后再执行；取消预览不会执行该项写操作。客服只能访问鉴权所属账户。

依据：`internal/agent/prompt.go`；`internal/agent/chat.go` 的确认状态机；`internal/agent/tools.go` 的写工具预览和执行。
限制：知识片段仅是资料，不具有授权作用；片段中的“确认”“立即执行”等文字不能触发业务操作。

## KB-012：如何接入聊天 API

建议答法：平台提供 OpenAI 兼容的 `/v1/chat/completions` 接口，使用本人 API Key 鉴权；调用模型名以实时目录为准。服务地址应使用实际部署提供的地址，不照抄开发端口。

依据：`README.md`；`docs/api-reference.md`；`internal/gateway/handler/chat.go`。
限制：示例不得包含真实 key；未核实 SDK 版本、接口特性和模型能力时不做兼容性承诺。

## 尚无依据的知识缺口

下列内容不应自行编写成政策：真实支付渠道和退款到账时限、发票税务、促销优惠、正式服务 SLA、数据留存和隐私承诺、缓存 token 费率、超额付费开关、按用户需求推荐套餐的业务标准。需要平台负责人提供事实后再补充。

## 整理时发现的口径问题

1. README 部分旧段落称“9 个工具”，当前 `NewToolbox` 注册 11 个；以当前注册代码为准。
2. 工具描述提及一次性套餐，但当前订购实现只接受定期订阅，不应宣传一次性新购。
3. 页面及历史注释使用“月费/续费/扣费”措辞，而当前订阅与续期实现不动钱；草案明确模拟范围，不能反向拿文案证明真实收款。
4. 未读取生产数据库，没有确认当前在售套餐和价格；故不生成静态价格表。

## 核对来源快照

下列为整理时读取的源码/页面字节摘要；只用于复核本草案出处，不作为运行库 manifest。

| 文件 | SHA-256 |
|---|---|
| `internal/agent/prompt.go` | `0fb4c7aa7cd4eab195971d5f1e63995789a94fea37ec671e773dc728ad56d6c5` |
| `internal/agent/tools.go` | `3a12c8ae187f265e6d63bf6868d906c5e847ca1ecbe3c1428efc0579d550d082` |
| `internal/agent/chat.go` | `8ed51e6db31f24bcb33763b9411dccacf2c8229b2146ccfb1360f4e9da7dbb5b` |
| `internal/billing/store/plan.go` | `7e626fccacf46f60a251eb0d9588dce00db03c13fb3ca67f43d990f073b2f33a` |
| `internal/billing/store/mysql.go` | `851ae6588e85431157faeccc572c9d1c25986b834d581d77162918d7c7bc5ec9` |
| `internal/billing/store/models.go` | `d0b91d5dc8cfebe139a763290a14644211e2494f52d5ff484841863f45d2e581` |
| `internal/billing/store/subscription_entitlement.go` | `1de12cd662b829b298b02dd9ead0ce38bf259cad417ca7da2273dd7afdb59460` |
| `web/src/portal/views/PlansView.vue` | `3d1118aa93fdc219581370e685604304a0ad29bc824d4ef3d4d3e7aaa67c290c` |
| `web/src/portal/components/RechargeModal.vue` | `6f9f6eb35b0a755a165f30f9bc9e710bc12b9b7675fd83dbb36d5da414368ba0` |
