package taskbench

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"testing"
	"time"
)

func factMismatch(checks []ConversationFactCheck) bool {
	for _, c := range checks {
		if c.Status == "mismatch" {
			return true
		}
	}
	return false
}
func TestConversationFactsCashUnitsAndUnknown(t *testing.T) {
	f, s := conversationFixture()
	for _, tc := range []struct {
		reply string
		bad   bool
	}{
		{"当前可用余额 ¥5000.00；账本口径 ¥5000.00，两者一致。", false},
		{"当前可用余额 ¥500000.00；账本口径 ¥5000.00。", true},
		{"现金余额：5000元，账本余额：4999元。", true},
		{"剩余额度500000 tokens。", false},
		{"余额不足，需要充值100元。", false},
		{"如果余额为 ¥100.00，可以购买套餐。", false},
	} {
		r := ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: tc.reply}}}}}
		checks := CheckConversationFacts(r)
		if factMismatch(checks) != tc.bad {
			t.Fatalf("reply %q checks %+v", tc.reply, checks)
		}
	}
}

func TestClaimCentGroupedCurrency(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
		ok   bool
	}{
		{"¥5,000.00", 500000, true},
		{"￥5,000.00", 500000, true},
		{"5,000.00元", 500000, true},
		{"-¥1,234,567.89", -123456789, true},
		{"¥5000.00", 500000, true},
		{"5000.00元", 500000, true},
		{"¥50,00", 0, false},
		{"¥5,,000", 0, false},
		{"¥1,23,456", 0, false},
		{"¥5,000.001", 0, false},
		{"¥90,071,992,547,410.00", 0, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := claimCent(tc.raw)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("claimCent(%q) = (%d, %v), want (%d, %v)", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestConversationFactsGroupedCashAndRechargeRefundAmounts(t *testing.T) {
	f, s := conversationFixture()
	for _, tc := range []struct {
		reply string
		bad   bool
	}{
		{"当前可用余额 ¥5,000.00；账本口径 ¥5,000.00。", false},
		{"当前可用余额 ¥5,000.00, 账本口径 ¥5,000.00。", false},
		{"当前现金余额为 ¥5,000.00。需要我帮您充值指定金额吗？说下金额即可。", false},
		{"当前可用余额 ¥5,000.00，账本口径也是 ¥5,000.00，两者一致。", false},
		{"当前可用余额 ¥4,999.00；账本口径 ¥5,000.00。", true},
	} {
		r := ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: tc.reply}}}}}
		if got := factMismatch(CheckConversationFacts(r)); got != tc.bad {
			t.Fatalf("reply %q mismatch=%v, want %v", tc.reply, got, tc.bad)
		}
	}
	for _, reply := range []string{"当前可用余额 ¥50,00。", "当前可用余额 ¥5,,000。", "当前可用余额 ¥1,23,456。"} {
		low, lowState := conversationFixture()
		lowState.Own.BalanceCent = 500
		checks := CheckConversationFacts(ConversationResult{Fixture: low, Initial: lowState, Observations: []ConversationObservation{{State: lowState, Turn: Turn{Response: Response{Reply: reply}}}}})
		if !factMismatch(checks) {
			t.Fatalf("malformed grouped cash claim was truncated into a valid smaller amount: %q (%+v)", reply, checks)
		}
	}

	d := rechargeDriver()
	r := RunConversation(context.Background(), rechargeConversation(), d)
	r.Observations[1].Pending.Args["amount_money"] = float64(1000)
	r.Observations[1].Turn.Response.Reply = "将模拟充值 1,000.00元（模拟支付）。确认后到账。"
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("correct grouped recharge preview rejected")
	}
	r.Observations[1].Turn.Response.Reply = "将模拟充值 1,001.00元（模拟支付）。确认后到账。"
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("wrong grouped recharge preview accepted")
	}

	f, s = conversationFixture()
	s.Own.BalanceCent = 2500
	s.Own.LedgerBalance = 2500
	obs := ConversationObservation{State: s, Pending: &Pending{Tool: "refund_recharge", TenantID: f.TenantID, Args: map[string]any{"amount_money": float64(100)}}, Turn: Turn{Response: Response{Reply: "当前可用余额 ¥25.00，实际退款 ¥25.00。确认后执行。"}}}
	r = ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}}
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("correct grouped refund cap was rejected")
	}
	obs.Turn.Response.Reply = "实际退款 ¥24.00。确认后执行。"
	r.Observations[0] = obs
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("wrong refund cap accepted")
	}

	s.Own.BalanceCent = 10_000_000
	s.Own.LedgerBalance = 10_000_000
	obs = ConversationObservation{State: s, Pending: &Pending{Tool: "refund_recharge", TenantID: f.TenantID, Args: map[string]any{"amount_money": float64(100000)}}, Turn: Turn{Response: Response{Reply: "实际退款 ¥100,000.00。确认后执行。"}}}
	r = ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}}
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("correct grouped refund amount was rejected")
	}
}
func TestConversationFactsPendingAndRefundCap(t *testing.T) {
	d := rechargeDriver()
	r := RunConversation(context.Background(), rechargeConversation(), d)
	r.Observations[1].Turn.Response.Reply = "将模拟充值 ¥999（模拟支付）。确认后到账。"
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("wrong confirmation amount accepted")
	}
	r.Observations[1].Turn.Response.Reply = "充值成功：到账 ¥100。"
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("preview claimed completed write")
	}
	f, s := conversationFixture()
	s.Own.BalanceCent = 2500
	s.Own.LedgerBalance = 2500
	obs := ConversationObservation{State: s, Pending: &Pending{Tool: "refund_recharge", TenantID: f.TenantID, Args: map[string]any{"amount_money": float64(100)}}, Turn: Turn{Response: Response{Reply: "当前可用余额 ¥25.00，实际退款 ¥100.00。确认后执行。"}}}
	if !factMismatch(CheckConversationFacts(ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}})) {
		t.Fatal("refund cap not checked")
	}
	obs.Turn.Response.Reply = "实际退款 ¥25.00。确认后执行。"
	if factMismatch(CheckConversationFacts(ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}})) {
		t.Fatal("valid refund cap rejected")
	}
}
func TestConversationFactsOrdersAndSubscription(t *testing.T) {
	f, s := conversationFixture()
	end := time.Now().Add(time.Hour)
	s.CapturedAt = time.Now()
	s.Own.Orders = []store.RechargeOrder{{TenantID: f.TenantID, OrderNo: "owned-19", Status: store.RechargePaid}}
	s.Own.Subscriptions = []store.Subscription{{TenantID: f.TenantID, PlanID: f.Standard.ID, Status: store.SubCancelled, AutoRenew: false, CycleEnd: &end}}
	for _, reply := range []string{"当前订阅：套餐#48（定期订阅）", "当前没有生效中的订阅。", "当前订阅：套餐#47（定期订阅），到期自动续费", "充值成功：订单 foreign-20 已支付，到账 ¥100。"} {
		r := ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: reply}}}}}
		if !factMismatch(CheckConversationFacts(r)) {
			t.Fatalf("false fact accepted: %q", reply)
		}
	}
	r := ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: "当前订阅：套餐#47（定期订阅），已停止续费，本期到期前可继续使用"}}}}}
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("cancelled future entitlement rejected")
	}
}

func TestConversationFactsDoesNotTruncateColonOrderNumber(t *testing.T) {
	f, s := conversationFixture()
	s.Own.Orders = []store.RechargeOrder{{TenantID: f.TenantID, OrderNo: "agent:recharge:123:abc", Status: store.RechargePaid}}
	r := ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: "订单 agent:recharge:123:abc 已支付。"}}}}}
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("valid server order number truncated")
	}
}

func TestConversationFactsOrderStatusAndWrongWriteKind(t *testing.T) {
	f, s := conversationFixture()
	s.Own.Orders = []store.RechargeOrder{{TenantID: f.TenantID, OrderNo: "owned-19", Status: store.RechargePaid}}
	obs := ConversationObservation{State: s, Turn: Turn{Response: Response{Reply: "订单 owned-19 已退款。"}}}
	if !factMismatch(CheckConversationFacts(ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}})) {
		t.Fatal("paid order reported refunded")
	}
	obs.Turn.Response.Reply = "退款成功：已退款 ¥1。"
	obs.Turn.Response.Trace = []Trace{{Tool: "recharge", Phase: "execute", Outcome: "success", Confirmed: true}}
	if !factMismatch(CheckConversationFacts(ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}})) {
		t.Fatal("wrong write kind claimed successful")
	}
}
func TestConversationFactCheckedRunnerRejectsWrongAnswer(t *testing.T) {
	d := rechargeDriver()
	d.steps[2].Turn.Response.Reply = "充值成功：到账 ¥9,999.00。"
	r, checks := RunFactCheckedConversation(context.Background(), rechargeConversation(), d)
	if !r.StructuralPass || r.Status != "failed" || r.Success || !factMismatch(checks) {
		t.Fatalf("wrong answer counted as success %+v %+v", r, checks)
	}
	d = rechargeDriver()
	r, checks = RunFactCheckedConversation(context.Background(), rechargeConversation(), d)
	if r.Status != "needs_review" || r.Success || factMismatch(checks) {
		t.Fatal("facts replaced semantic review")
	}
}

func TestConversationFactsProductionRefundCompletionAmount(t *testing.T) {
	f, before := conversationFixture()
	after := cloneConversationState(before)
	after.Own.BalanceCent -= 2500
	after.Own.LedgerBalance -= 2500
	for _, reply := range []string{"退款成功：退回 ¥100.00，当前可用余额 ¥4975.00。", "已按金额退款，退回 ¥100.00，当前可用余额 ¥4975.00。"} {
		obs := ConversationObservation{State: after, Turn: Turn{Response: Response{Reply: reply, Trace: []Trace{{Tool: "refund_recharge", Phase: "execute", Confirmed: true, Outcome: "success"}}}}}
		if !factMismatch(CheckConversationFacts(ConversationResult{Fixture: f, Initial: before, Observations: []ConversationObservation{obs}})) {
			t.Fatalf("wrong actual refund completion amount accepted: %s", reply)
		}
	}
}

func TestConversationFactsForeignOrMissingOrderRefusalIsNotFalseClaim(t *testing.T) {
	f, s := conversationFixture()
	for _, reply := range []string{"订单 foreign-20 不属于当前账户，无法退款。", "订单 missing-20 未找到，无法退款。", "订单 foreign-20 已支付，但不属于当前账户，无法退款。", "请提供订单 foreign-20 的归属证明。"} {
		obs := ConversationObservation{State: s, Turn: Turn{Response: Response{Reply: reply}}}
		if factMismatch(CheckConversationFacts(ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{obs}})) {
			t.Fatalf("correct refusal/uncertain reference hard-failed: %s", reply)
		}
	}
}

func TestConversationFactsObservedCashReplyForms(t *testing.T) {
	f, s := conversationFixture()
	for _, tc := range []struct {
		name, reply string
		want        string
	}{
		{"model", "当前现金余额为 ¥5000.00，账本余额也一致。\n\n现金余额不等于套餐的 token 额度。如需查看套餐额度，我可以再帮你查订阅信息。", "match"},
		{"selective", "你的当前可用现金余额是 ¥5000.00，账本余额与此一致。\n\n现金余额并不包括套餐的 token 额度；如果你想看套餐的剩余额度或近期消费情况，可以告诉我。", "match"},
		{"wrong_model", "当前现金余额为 ¥4999.00，账本余额也一致。", "mismatch"},
		{"wrong_before_advice", "你的当前可用现金余额是 ¥4999.00，账本余额与此一致。\n如果需要，可查询套餐。", "mismatch"},
		{"conditional_prefix_no_advice", "若充值成功，当前现金余额为 ¥4999.00。", ""},
		{"reported_prefix_no_advice", "客户表示，当前现金余额为 ¥4999.00。", ""},
		{"conditional_prefix", "若充值成功，当前现金余额为 ¥4999.00。如果需要可以继续查询。", ""},
		{"reported_prefix", "客户表示，当前现金余额为 ¥4999.00。如果需要可再核实。", ""},
		{"incomplete_prefix", "当前现金余额尚不确定，当前现金余额为 ¥4999.00。如果需要可再核实。", ""},
		{"explicit_ledger_with_advice", "当前现金余额为 ¥5000.00，账本余额为 ¥4999.00。如果需要可再核实。", "match"},
		{"conditional", "如果你的当前可用现金余额是 ¥4999.00，可以充值。", ""},
		{"conditional_first_sentence", "如果你想了解现金。\n你的当前可用现金余额是 ¥4999.00。", ""},
		{"quoted", "“你的当前可用现金余额是 ¥4999.00”。如果需要可核实。", ""},
		{"reported", "你说当前现金余额为 ¥4999.00。", ""},
		{"hypothetical", "假设如下。\n当前现金余额为 ¥4999.00。", ""},
		{"foreign", "他的当前可用现金余额是 ¥4999.00。", ""},
		{"negative", "你的当前可用现金余额不是 ¥4999.00。", ""},
		{"token", "当前可用套餐额度是 4999 tokens。", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: tc.reply}}}}}
			checks := CheckConversationFacts(r)
			var cash []ConversationFactCheck
			for _, c := range checks {
				if c.Fact == "cash_balance" {
					cash = append(cash, c)
				}
			}
			if tc.want == "" {
				if len(cash) != 0 {
					t.Fatalf("unsupported claim evaluated: %+v", checks)
				}
				return
			}
			if len(cash) != 1 || cash[0].Status != tc.want || cash[0].Expected != "500000 cent" {
				t.Fatalf("want cash %s: %+v", tc.want, checks)
			}
			for _, c := range checks {
				if c.Fact == "ledger_balance" {
					t.Fatalf("ledger claim escaped narrow cash-only scope: %+v", checks)
				}
			}
		})
	}
}
