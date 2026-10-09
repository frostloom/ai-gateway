package agent

import (
	"regexp"
	"strings"
)

var confirmationInvitation = regexp.MustCompile(`(?:回复|请确认|确认即可|确认就|确认后我|确认的话我|确认的话.{0,6}办理)`)
var explicitMoney = regexp.MustCompile(`(?:\d+\s*(?:元|块|rmb|yuan)|(?:充值|退款|退|充)\s*\d+)`)
var asksMoney = regexp.MustCompile(`^(?:请问|请告诉|请提供|麻烦|您|你).{0,16}(?:充|退).{0,12}(?:多少|金额)`)
var consultationPatterns = map[string]*regexp.Regexp{
	"recharge":            regexp.MustCompile(`(?:怎么|如何).{0,6}(?:充值|充钱)|充值.{0,12}(?:怎么|如何|流程|手续费)`),
	"refund_recharge":     regexp.MustCompile(`(?:怎么|如何).{0,6}(?:退款|退钱|退$)|(?:退款|退钱).{0,12}(?:怎么|如何|流程|手续费)`),
	"subscribe_plan":      regexp.MustCompile(`(?:怎么|如何).{0,6}(?:订阅|订购|开通)|(?:订阅|订购).{0,12}(?:怎么|如何|流程)`),
	"change_plan":         regexp.MustCompile(`(?:怎么|如何).{0,6}(?:改套餐|换套餐|升级)|(?:改套餐|换套餐).{0,12}(?:怎么|如何|流程)`),
	"cancel_subscription": regexp.MustCompile(`(?:怎么|如何).{0,6}(?:退订|停止订阅|取消订阅)|退订.{0,12}(?:怎么|如何|流程)`),
}

var explicitApplicationPatterns = map[string]*regexp.Regexp{
	"recharge":            regexp.MustCompile(`(?:帮我|给我|替我|直接|现在|立即|请|我要|我想)\s*(?:充值|充钱|充)`),
	"refund_recharge":     regexp.MustCompile(`(?:帮我|给我|替我|直接|现在|立即|请|我要|我想)\s*(?:退款|退钱|退)`),
	"subscribe_plan":      regexp.MustCompile(`(?:帮我|给我|替我|直接|现在|立即|请|我要|我想)\s*(?:订阅|订购|开通)`),
	"change_plan":         regexp.MustCompile(`(?:帮我|给我|替我|直接|现在|立即|请|我要|我想)\s*(?:改套餐|换套餐|升级|改成|换成)`),
	"cancel_subscription": regexp.MustCompile(`(?:帮我|给我|替我|直接|现在|立即|请|我要|我想)\s*(?:退订|停止续费|取消订阅|取消续费)`),
}

// This is a bounded consistency repair, not a second intent router. The model
// still chooses the tool. Both primary arms use the same repair and count its
// actual upstream calls/usage. Do not replace necessary questions.
func replyRepairReason(reply, user string, pending *pendingAction) string {
	if pending == nil && strings.Contains(reply, "确认") && confirmationInvitation.MatchString(reply) {
		return "没有待确认操作，不能要求确认执行。明确申请且参数齐全则调用对应写工具生成真实预览；仅咨询则回答方法。"
	}
	if explicitMoney.MatchString(strings.ToLower(user)) && asksMoney.MatchString(reply) &&
		(strings.Contains(user, "充") || strings.Contains(user, "退")) {
		return "用户已给金额，不要重复询问金额。核对业务和单位；明确申请用写工具生成预览，方向不清只澄清方向。"
	}
	return ""
}

// The execution layer may reject a contradictory proposal but never chooses a
// replacement write. Interpretive uncertainty goes back to the user.
func writeProposalClarification(name, user string) string {
	text := strings.ToLower(user)
	if (name == "recharge" || name == "refund_recharge") && strings.Contains(text, "credit") && !strings.Contains(text, "元") && !strings.Contains(text, "yuan") && !strings.Contains(text, "rmb") {
		return "请确认credit的含义与金额单位：是人民币现金余额，还是套餐token配额？不能直接按人民币金额办理。"
	}
	if name == "refund_recharge" && (strings.Contains(text, "到余额") || strings.Contains(text, "到 balance") || strings.Contains(text, "to balance")) {
		return "需要先澄清资金方向：您是要退回充值款，还是将款项退入账户余额？退充值会减少可用余额。"
	}
	if name == "refund_recharge" && (strings.Contains(text, "withdraw") || strings.Contains(text, "提现")) {
		return "您是申请退回充值款，还是提现？请说明资金方向及金额单位。"
	}
	if pattern := consultationPatterns[name]; pattern != nil && pattern.MatchString(text) && !explicitApplicationPatterns[name].MatchString(text) {
		explanation := "明确申请且信息齐全后，系统会生成预览；确认后才执行。"
		switch name {
		case "refund_recharge":
			explanation = "退款可按本人已支付充值订单号或元金额申请，实际最多退到当前余额；先预览，再确认执行。"
		case "cancel_subscription":
			explanation = "退订仅停止下期续费，当期不退款；具体生效日期以本人订阅为准。"
		}
		return explanation + "您当前是在咨询，尚未生成操作；若要办理请明确申请。"
	}
	return ""
}
