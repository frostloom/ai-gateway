package agent

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Full-message, deliberately narrow recovery permission. This does not route
// arbitrary messages: the model must already have exhausted its one repair and
// still be proposing an unsupported current cash amount.
var ownCashQuery = regexp.MustCompile(`^(?:(?:请|麻烦)(?:帮我)?|帮我|我想|我要|我只|只)?(?:查询|查看|查一下|查|看看|看一下)(?:一下)?(?:本人(?:的)?(?:账户)?|我的?(?:账户)?|本账户)?(?:当前|现在|最新)?(?:人民币(?:现金)?余额|现金余额)(?:是多少|有多少|还有多少)?$`)
var excludedTokenQuery = regexp.MustCompile(`^(?:也)?(?:不是|不查|不查询|不用查|无需查询)(?:套餐)?(?:tokens?(?:额度|配额)?|额度|配额)$`)
var excludedCashWrites = regexp.MustCompile(`^(?:也)?不(?:办理|需要|要)(?:充值|退款|订阅)(?:(?:、|或|和)(?:充值|退款|订阅))*$`)

func isExplicitOwnCashQuery(user string) bool {
	text := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.ToLower(user))
	parts := strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("，,。！？!?；;", r) })
	if len(parts) == 0 || !ownCashQuery.MatchString(parts[0]) {
		return false
	}
	for _, part := range parts[1:] {
		if !excludedTokenQuery.MatchString(part) && !excludedCashWrites.MatchString(part) {
			return false
		}
	}
	return true
}

func (h *Handler) recoverCashRead(ctx context.Context, sid string, tenant uint64, user string, pending *pendingAction, trace []traceItem) (string, traceItem, bool) {
	if pending != nil || !isExplicitOwnCashQuery(user) || ctx.Err() != nil || h.tb == nil {
		return "", traceItem{}, false
	}
	for _, tr := range trace {
		if tr.Tool == "get_balance" && tr.Phase == "read" {
			return "", traceItem{}, false
		}
	}
	tool := h.tb.tools["get_balance"]
	if tool == nil || tool.confirm || tool.run == nil {
		return "", traceItem{}, false
	}
	started := time.Now()
	args := map[string]any{}
	result, err := h.runTimedTool(ctx, "get_balance", args, tenant, "")
	outcome := "success"
	reply := sanitizeReply(result)
	if err != nil {
		outcome = "failure"
		result = "错误：" + err.Error()
		reply = "本轮未能核实当前现金余额，请稍后重新查询。"
	}
	h.audit(sid, tenant, "get_balance", args, false, "", result, guardJSON("execution", "cash_read_recovery", "unsupported_cash_after_repair", nil))
	tr := traceItem{Tool: "get_balance", Args: "{}", ResolvedArgs: "{}", Result: result, Phase: "read", Outcome: outcome, DurationMS: float64(time.Since(started)) / float64(time.Millisecond)}
	return reply, tr, true
}
