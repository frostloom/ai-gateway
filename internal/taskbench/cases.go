package taskbench

// DefaultCases is a separate frozen task dataset. It does not alter the
// historical single-turn intent suite. Every case receives its own fresh tenant.
func DefaultCases() []Case {
	return []Case{
		{ID: "recharge-ready", Category: "recharge", Message: "给我的账户充值100元", Tool: "recharge", AmountMoney: 100},
		{ID: "recharge-missing", Category: "recharge", Message: "我要充值", Followup: "充值100元", MissingSlot: "amount", Tool: "recharge", AmountMoney: 100},
		{ID: "recharge-cancel", Category: "recharge", Message: "充值100元", Tool: "recharge", AmountMoney: 100, Cancel: true},
		{ID: "recharge-repeat", Category: "recharge", Message: "帮我充200元", Tool: "recharge", AmountMoney: 200, DuplicateConfirm: true},
		{ID: "refund-ready", Category: "refund", Message: "请给我退款100元", Tool: "refund_recharge", AmountMoney: 100},
		{ID: "refund-missing", Category: "refund", Message: "我要退款", Followup: "按金额退款100元", MissingSlot: "amount", Tool: "refund_recharge", AmountMoney: 100},
		{ID: "refund-cancel", Category: "refund", Message: "退款100元", Tool: "refund_recharge", AmountMoney: 100, Cancel: true},
		{ID: "refund-composite", Category: "refund", Message: "先查一下余额，然后帮我退款100元", Tool: "refund_recharge", AmountMoney: 100, RequireBalanceAnswer: true},
		{ID: "refund-order", Category: "refund", Message: "退掉已支付的充值订单{order_no}", Tool: "refund_recharge", OrderRefund: true},
		{ID: "subscribe-ready", Category: "subscribe", Message: "给我订购月度·标准套餐", Tool: "subscribe_plan", Plan: "standard"},
		{ID: "subscribe-missing", Category: "subscribe", Message: "我要订购一个套餐", Followup: "订购月度·标准套餐", MissingSlot: "plan", Tool: "subscribe_plan", Plan: "standard"},
		{ID: "subscribe-cancel", Category: "subscribe", Message: "订购月度·标准套餐", Tool: "subscribe_plan", Plan: "standard", Cancel: true},
		{ID: "subscribe-flagship", Category: "subscribe", Message: "开通月度·旗舰订阅", Tool: "subscribe_plan", Plan: "flagship"},
		{ID: "change-ready", Category: "change", Message: "把我当前的套餐改成月度·旗舰", Tool: "change_plan", Plan: "flagship", InitialSubscription: true},
		{ID: "change-missing", Category: "change", Message: "我要改套餐", Followup: "改成月度·旗舰套餐", MissingSlot: "plan", Tool: "change_plan", Plan: "flagship", InitialSubscription: true},
		{ID: "change-cancel", Category: "change", Message: "把套餐改成月度·旗舰", Tool: "change_plan", Plan: "flagship", InitialSubscription: true, Cancel: true},
		{ID: "cancel-subscription", Category: "cancel_subscription", Message: "退订我的定期订阅，停止下期自动续费", Tool: "cancel_subscription", InitialSubscription: true},
		{ID: "cancel-abort", Category: "cancel_subscription", Message: "退订当前套餐", Tool: "cancel_subscription", InitialSubscription: true, Cancel: true},
		{ID: "balance-read", Category: "read", Message: "我的账户现在还有多少余额？", Tool: "get_balance", Read: true},
		{ID: "subscription-read", Category: "read", Message: "查看我当前的订阅套餐", Tool: "get_my_subscription", Read: true, InitialSubscription: true},
		{ID: "satisfied-balance", Category: "already_satisfied", Message: "只查询余额，不充值也不退款", Tool: "get_balance", Read: true},
		{ID: "satisfied-subscription", Category: "already_satisfied", Message: "我已经有月度·标准套餐，请查看当前订阅，不要重复订购", Tool: "get_my_subscription", Read: true, InitialSubscription: true},
		{ID: "refund-capped", Category: "refund", Message: "按金额退款6000元，最多退到当前余额", Tool: "refund_recharge", AmountMoney: 6000},
		{ID: "cancelled-read", Category: "already_satisfied", Message: "只查询我现在还有没有生效中的定期订阅，不要重新订购", Tool: "get_my_subscription", Read: true, InitialSubscription: true, InitiallyCancelled: true},
	}
}
