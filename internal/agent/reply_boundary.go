package agent

import (
	"regexp"
	"strings"
)

var confirmationInvitation = regexp.MustCompile(`(?:回复|请确认|确认即可|确认就|确认后我|确认的话我|确认的话.{0,6}办理)`)

// Finite current-application forms, not a semantic/history completion judge.
var rechargeApplication = regexp.MustCompile(`^(?:请\s*)?(?:(?:帮我|给我|替我|我要|我想|现在|立即)\s*)?(?:充值|充钱|充)\s*(?:[0-9]+(?:\.[0-9]+)?\s*(?:元|块|人民币|rmb|yuan)?)?\s*[。.!！]?$`)

// An explicit own-account application, with a bounded optional preview suffix.
// Whole-string matching excludes history, quotations, other owners and compounds.
var ownRechargeApplication = regexp.MustCompile(`^(?:明确申请|请)?\s*给我本人账户充值\s*[0-9]+(?:\.[0-9]+)?\s*(?:元人民币|元|人民币|rmb|yuan)\s*(?:[，,]\s*请先预览)?[。.!！]?$`)

// Finite explicit refund applications only; whole-string matching preserves
// negation/history/quotation/other-owner/consultation and compound exclusions.
var refundApplication = regexp.MustCompile(`^(?:(?:请\s*)?(?:(?:帮我|给我|替我|我要|我想|现在|立即)\s*)?(?:退款|退钱)\s*(?:[0-9]+(?:\.[0-9]+)?\s*(?:元人民币|元|块|人民币|rmb|yuan)?)?|(?:不是修改旧申请[；;]\s*新申请)?从我本人账户退回充值款\s*[0-9]+(?:\.[0-9]+)?\s*(?:元人民币|元|人民币|rmb|yuan)\s*(?:[，,]\s*请先预览)?)[。.!！]?$`)
var refundCompletion = regexp.MustCompile(`^\s*(?:已)?退款成功(?:[：:。！!，,]|$)`)
var rechargeCompletion = regexp.MustCompile(`^\s*(?:已)?充值成功(?:[：:。！!，,]|$)`)
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

// This is a bounded consistency repair, not a second intent router. Normal
// proposals remain model-selected; the separate exhausted-repair cash recovery
// is limited to an explicit own-cash read. Both arms share these bounds and
// count actual upstream calls/usage. Do not replace necessary questions.
func replyRepairReason(reply, user string, pending *pendingAction) string {
	application := strings.ToLower(strings.TrimSpace(user))
	if (rechargeApplication.MatchString(application) || ownRechargeApplication.MatchString(application)) && rechargeCompletion.MatchString(reply) {
		return "本轮尚未执行已确认的充值，不能声称充值成功或到账。明确申请应先调用充值工具生成真实预览，等待用户确认后才执行；不能生成预览就说明未执行。"
	}
	if refundApplication.MatchString(application) && refundCompletion.MatchString(reply) {
		return "本轮尚未执行已确认的退款，不能声称退款成功或已退回。明确申请应先调用退款工具生成真实预览，等待用户确认后才执行；不能生成预览就说明未执行。"
	}
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

// A bounded Chinese cash-amount consistency check, not a semantic intent
// classifier. Historical tool results never establish this turn's authority.
var cashBalanceAmount = regexp.MustCompile(`余额[^。！？\n；;]{0,16}(?:[¥￥]\s*[0-9]|[0-9][0-9,.]*\s*元)`)

var cashExampleScope = regexp.MustCompile(`例如|比如|假设|举例|您说的|你说的|引用`)
var cashHistoryScope = regexp.MustCompile(`上轮|上次|之前|此前|历史|当时|刚才|先前|曾经`)
var cashCurrentScope = regexp.MustCompile(`当前|现在|目前|最新`)

func cashBalanceClaimNeedsRead(reply string, trace []traceItem) bool {
	hasClaim := false
	for _, clause := range strings.FieldsFunc(reply, func(r rune) bool { return strings.ContainsRune("。！？\n；;，", r) }) {
		loc := cashBalanceAmount.FindStringIndex(clause)
		if loc == nil {
			continue
		}
		prefix := clause[:loc[0]]
		if cashExampleScope.MatchString(prefix) {
			continue
		}
		historical := cashHistoryScope.FindStringIndex(prefix)
		if historical != nil && !cashCurrentScope.MatchString(prefix[historical[1]:]) {
			continue
		}
		hasClaim = true
		break
	}
	if !hasClaim {
		return false
	}
	for _, tr := range trace {
		if tr.Tool == "get_balance" && tr.Phase == "read" && tr.Outcome == "success" {
			return false
		}
	}
	return true
}
