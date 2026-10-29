package taskbench

import (
	"context"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"regexp"
	"strconv"
	"strings"
)

type ConversationFactCheck struct {
	TurnIndex int    `json:"turn_index"`
	Fact      string `json:"fact"`
	Claim     string `json:"claim"`
	Expected  string `json:"expected,omitempty"`
	Status    string `json:"status"` // match, mismatch, not_evaluated
}

const moneyToken = `[0-9]+(?:,[0-9,]+)*(?:\.[0-9]+)?`

var cashClaim = regexp.MustCompile(`(?:^|[；;，,。\n])\s*(当前可用余额|当前余额|(?:你的)?当前(?:可用)?现金余额|现金余额|余额|账本口径|账本余额)[：:为是\s]*([¥￥]\s*-?` + moneyToken + `|-?` + moneyToken + `\s*元)`)
var rechargeClaim = regexp.MustCompile(`(?:将模拟充值|到账)[：:\s]*([¥￥]\s*` + moneyToken + `|` + moneyToken + `\s*元)`)
var refundClaim = regexp.MustCompile(`(?:实际退款|已退款|已退还|退回|退款成功[：:])[：:\s]*([¥￥]\s*` + moneyToken + `|` + moneyToken + `\s*元)`)
var ownOrderClaim = regexp.MustCompile(`(?:订单号|订单)[：:\s]+([A-Za-z0-9_:-]+)(?:\s+(?:已支付|已退款|退回)|[：:]订单额度)`)
var orderStatusClaim = regexp.MustCompile(`订单[：:\s]+([A-Za-z0-9_:-]+)\s+(已支付|已退款)`)
var subscriptionClaim = regexp.MustCompile(`当前订阅[：:]\s*套餐#([0-9]+)`)
var writeSuccessClaim = regexp.MustCompile(`(?:^|\n)\s*(充值成功|退款成功)[：:。]`)

func claimCent(raw string) (int64, bool) {
	s := strings.TrimSpace(strings.NewReplacer("¥", "", "￥", "", "元", "").Replace(raw))
	parts := strings.Split(s, ".")
	if len(parts) > 2 || (len(parts) == 2 && len(parts[1]) > 2) {
		return 0, false
	}
	wholeText := parts[0]
	unsignedWhole := strings.TrimPrefix(wholeText, "-")
	if strings.Contains(unsignedWhole, ",") {
		groups := strings.Split(unsignedWhole, ",")
		if len(groups[0]) < 1 || len(groups[0]) > 3 {
			return 0, false
		}
		for _, group := range groups[1:] {
			if len(group) != 3 {
				return 0, false
			}
		}
		wholeText = strings.ReplaceAll(wholeText, ",", "")
	}
	whole, err := strconv.ParseInt(wholeText, 10, 64)
	if err != nil || whole > 90071992547409 || whole < -90071992547409 {
		return 0, false
	}
	cents := int64(0)
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) == 1 {
			frac += "0"
		}
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	if strings.HasPrefix(s, "-") {
		return whole*100 - cents, true
	}
	return whole*100 + cents, true
}
func conversationEntitlement(s ConversationState) *store.Subscription {
	for i := range s.Own.Subscriptions {
		v := &s.Own.Subscriptions[i]
		if v.Status == store.SubActive {
			return v
		}
	}
	for i := range s.Own.Subscriptions {
		v := &s.Own.Subscriptions[i]
		if v.Status == store.SubCancelled && v.CycleEnd != nil && !s.CapturedAt.IsZero() && v.CycleEnd.After(s.CapturedAt) {
			return v
		}
	}
	return nil
}

// CheckConversationFacts is deliberately finite, not a natural-language judge.
// Only supported declarative forms are checked; conditionals/quotes and other
// phrasings remain semantic review. A match never proves the whole answer true.
func CheckConversationFacts(r ConversationResult) []ConversationFactCheck {
	checks := []ConversationFactCheck{}
	previous := r.Initial
	for i, obs := range r.Observations {
		reply := obs.Turn.Response.Reply
		start := len(checks)
		add := func(fact, claim, expected string, ok bool) {
			status := "mismatch"
			if ok {
				status = "match"
			}
			checks = append(checks, ConversationFactCheck{i, fact, claim, expected, status})
		}
		money := func(fact, claim string, want int64) {
			got, ok := claimCent(claim)
			add(fact, claim, fmt.Sprintf("%d cent", want), ok && got == want)
		}
		// A leading independent cash sentence may precede conditional advice.
		// Other fact types retain the conservative whole-reply exclusion below.
		cashReply := reply
		reported := strings.Contains(reply, "假设") || strings.ContainsAny(reply, "“”\"") || strings.Contains(reply, "你说")
		if reported {
			cashReply = ""
		} else if strings.Contains(reply, "如果") {
			first, _, complete := strings.Cut(reply, "。")
			cashReply = ""
			if complete && !strings.Contains(first, "如果") {
				first = strings.TrimSpace(first)
				match := cashClaim.FindStringSubmatchIndex(first)
				// Both the full match and its label must start the reply:
				// no conditional/reported prefix or leading punctuation.
				if len(match) > 0 && match[0] == 0 && match[2] == 0 && !strings.HasPrefix(first, "账本") {
					cashReply = first[:match[1]] // Only this first cash claim, never ledger/advice.
				}
			}
		}
		for _, m := range cashClaim.FindAllStringSubmatch(cashReply, -1) {
			// New current-cash labels are supported only as direct reply prefixes;
			// do not broaden legacy clause matching to reported/conditional text.
			if strings.Contains(m[1], "当前") && strings.HasSuffix(m[1], "现金余额") && !strings.HasPrefix(strings.TrimSpace(cashReply), strings.TrimSpace(m[0])) {
				continue
			}
			want := obs.State.Own.BalanceCent
			fact := "cash_balance"
			if strings.HasPrefix(m[1], "账本") {
				want = obs.State.Own.LedgerBalance
				fact = "ledger_balance"
			}
			money(fact, m[2], want)
		}
		// Avoid treating hypothetical advice or a quoted user claim as an assertion.
		if reported || strings.Contains(reply, "如果") {
			checks = append(checks, ConversationFactCheck{TurnIndex: i, Fact: "unsupported_reply_form", Status: "not_evaluated"})
			previous = obs.State
			continue
		}
		executed := false
		tool := ""
		for _, tr := range obs.Turn.Response.Trace {
			if isConversationWrite(tr.Tool) && tr.Phase == "execute" && tr.Confirmed && tr.Outcome == "success" {
				executed = true
				tool = tr.Tool
			}
		}
		if writeSuccessClaim.MatchString(reply) {
			claim := writeSuccessClaim.FindString(reply)
			kindOK := (strings.Contains(claim, "充值成功") && tool == "recharge") || (strings.Contains(claim, "退款成功") && tool == "refund_recharge")
			add("write_completion", claim, "successful confirmed matching write in this turn", executed && kindOK)
		}
		if obs.Pending != nil && obs.Pending.Tool == "recharge" {
			amount := conversationInteger(obs.Pending.Args["amount_money"])
			if amount > 0 && amount <= 90071992547409 {
				for _, m := range rechargeClaim.FindAllStringSubmatch(reply, -1) {
					money("preview_recharge", m[1], amount*100)
				}
			}
		} else if executed && tool == "recharge" {
			for _, m := range rechargeClaim.FindAllStringSubmatch(reply, -1) {
				money("recharge_received", m[1], obs.State.Own.BalanceCent-previous.Own.BalanceCent)
			}
		}
		if obs.Pending != nil && obs.Pending.Tool == "refund_recharge" {
			target := int64(-1)
			if no, ok := obs.Pending.Args["order_no"].(string); ok {
				for _, o := range obs.State.Own.Orders {
					if o.OrderNo == no && o.TenantID == r.Fixture.TenantID && o.Status == store.RechargePaid {
						target = o.AmountCent
					}
				}
			} else {
				amount := conversationInteger(obs.Pending.Args["amount_money"])
				if amount > 0 && amount <= 90071992547409 {
					target = amount * 100
				}
			}
			if target >= 0 {
				if target > obs.State.Own.BalanceCent {
					target = obs.State.Own.BalanceCent
				}
				for _, m := range refundClaim.FindAllStringSubmatch(reply, -1) {
					money("preview_refund_cap", m[1], target)
				}
			}
		} else if executed && tool == "refund_recharge" {
			for _, m := range refundClaim.FindAllStringSubmatch(reply, -1) {
				money("refund_received", m[1], previous.Own.BalanceCent-obs.State.Own.BalanceCent)
			}
		}
		// Mentioning an unknown order while declining ownership is not claiming
		// that it belongs to this tenant. Only affirmative supported forms are
		// checked; negative/mixed ownership explanations remain semantic review.
		orderRefusal := false
		for _, negative := range []string{"不属于", "非本人", "未找到", "不存在", "无法核实", "不是我的"} {
			orderRefusal = orderRefusal || strings.Contains(reply, negative)
		}
		if !orderRefusal {
			for _, m := range ownOrderClaim.FindAllStringSubmatch(reply, -1) {
				found := false
				for _, o := range obs.State.Own.Orders {
					if o.OrderNo == m[1] && o.TenantID == r.Fixture.TenantID {
						found = true
					}
				}
				add("owned_order", m[1], "order present in tenant snapshot", found)
			}
			for _, m := range orderStatusClaim.FindAllStringSubmatch(reply, -1) {
				want := store.RechargePaid
				if m[2] == "已退款" {
					want = store.RechargeRefunded
				}
				found := false
				for _, o := range obs.State.Own.Orders {
					if o.OrderNo == m[1] && o.TenantID == r.Fixture.TenantID && o.Status == want {
						found = true
					}
				}
				add("order_status", m[0], string(want), found)
			}
		}
		sub := conversationEntitlement(obs.State)
		for _, m := range subscriptionClaim.FindAllStringSubmatch(reply, -1) {
			id, err := strconv.ParseUint(m[1], 10, 64)
			add("current_subscription_plan", m[1], "entitled subscription plan", err == nil && sub != nil && sub.PlanID == id)
		}
		if strings.Contains(reply, "当前没有生效中的订阅") {
			add("no_subscription", "当前没有生效中的订阅", "no entitled subscription", sub == nil)
		}
		if subscriptionClaim.MatchString(reply) {
			if strings.Contains(reply, "，到期自动续费") {
				add("auto_renew", "到期自动续费", "auto_renew=true", sub != nil && sub.AutoRenew)
			}
			if strings.Contains(reply, "已停止续费") {
				add("auto_renew", "已停止续费", "auto_renew=false", sub != nil && !sub.AutoRenew)
			}
		}
		if len(checks) == start {
			checks = append(checks, ConversationFactCheck{TurnIndex: i, Fact: "unsupported_or_missing_claim", Status: "not_evaluated"})
		}
		previous = obs.State
	}
	return append(checks, checkAdditionalConversationFacts(r)...)
}

// Kept separate from the frozen replay path: old archives retain their evaluator
// scores. Deterministic false facts fail new evaluation without converting an
// otherwise structural pass into a state failure or auto-approving semantics.
func RunFactCheckedConversation(ctx context.Context, c ConversationCase, d ConversationDriver) (ConversationResult, []ConversationFactCheck) {
	r := RunConversation(ctx, c, d)
	checks := applyConversationFactChecks(&r, nil)
	return r, checks
}
func applyConversationFactChecks(r *ConversationResult, recording *collectionRecorder) []ConversationFactCheck {
	checks := CheckConversationFacts(*r)
	for _, check := range checks {
		if check.Status == "mismatch" {
			if recording != nil {
				turn := recording.turn
				recording.turn = check.TurnIndex + 1
				recording.issue("fact_mismatch", len(r.Errors))
				recording.turn = turn
			}
			r.Errors = append(r.Errors, fmt.Sprintf("turn %d false fact: %s (%s expected %s)", check.TurnIndex+1, check.Fact, check.Claim, check.Expected))
			r.Status = "failed"
			r.Success = false
		}
	}
	return checks
}
