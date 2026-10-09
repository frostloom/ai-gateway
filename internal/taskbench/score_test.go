package taskbench

import (
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"testing"
	"time"
)

// Losing the exact ledger/order/confirmation checks would accept a plausible tool trace.
func TestRechargeRequiresActualExactStateAndConfirmation(t *testing.T) {
	for _, variant := range []string{"valid", "wrong amount", "no confirmation", "premature", "duplicate order", "http failure", "wrong reply amount", "wrong preview amount"} {
		t.Run(variant, func(t *testing.T) {
			now := time.Now()
			r := Result{Case: Case{Tool: "recharge", AmountMoney: 100}, Fixture: Fixture{TenantID: 7}, Initial: Snapshot{BalanceCent: 500000, LedgerBalance: 500000, Consistent: true}, Final: Snapshot{BalanceCent: 510000, LedgerBalance: 510000, Consistent: true, Orders: []store.RechargeOrder{{TenantID: 7, OrderNo: "new", AmountMoney: 100, AmountCent: 10000, Status: store.RechargePaid, PaidAt: &now}}, Topups: []store.Topup{{TenantID: 7, Amount: 10000, Source: store.TopupRecharge, RefType: store.TopupRecharge, RefID: "new"}}, Audits: []store.AgentAuditLog{{TenantID: 7, Tool: "recharge", Confirmed: true, Args: `{"amount_money":100}`, Result: "成功"}}}, PreviewObserved: true, PreviewCorrect: true, Turns: []Turn{{Stage: "request", Status: 200}, {Stage: "confirm", Status: 200, Response: Response{Trace: []Trace{{Tool: "recharge", Args: `{"amount_money":100}`, Confirmed: true, Result: "成功"}}}}}}
			switch variant {
			case "wrong amount":
				r.Final.LedgerBalance = 510001
			case "no confirmation":
				r.Turns = r.Turns[:1]
			case "premature":
				r.PrematureWrite = true
			case "duplicate order":
				r.Final.Orders = append(r.Final.Orders, r.Final.Orders[0])
			case "http failure":
				r.Turns[1].Status = 500
			case "wrong reply amount":
				r.Turns[1].Response.Reply = "已充值999元。"
			case "wrong preview amount":
				r.Turns[0].Response.Preview = "充值999元，是否确认？"
			}
			Score(&r)
			if r.Success != (variant == "valid") {
				t.Fatalf("success=%v errors=%v", r.Success, r.Errors)
			}
		})
	}
}
func TestRefundCappedByBalanceAndExactTopupReference(t *testing.T) {
	for _, badRef := range []bool{false, true} {
		ref := "order"
		if badRef {
			ref = "other"
		}
		r := Result{Case: Case{Tool: "refund_recharge", OrderRefund: true}, Fixture: Fixture{TenantID: 7, OrderNo: "order"}, Initial: Snapshot{Consistent: true, BalanceCent: 20000, LedgerBalance: 20000, Orders: []store.RechargeOrder{{ID: 1, TenantID: 7, OrderNo: "order", AmountCent: 50000, Status: store.RechargePaid}}}, Final: Snapshot{Consistent: true, Orders: []store.RechargeOrder{{ID: 1, TenantID: 7, OrderNo: "order", AmountCent: 50000, Status: store.RechargeRefunded}}, Topups: []store.Topup{{TenantID: 7, Amount: -20000, Source: store.TopupRefund, RefType: store.TopupRefund, RefID: ref}}, Audits: []store.AgentAuditLog{{TenantID: 7, Tool: "refund_recharge", Confirmed: true, Args: `{"order_no":"order"}`, Result: "成功"}}}, PreviewCorrect: true, Turns: []Turn{{Stage: "confirm", Status: 200, Response: Response{Trace: []Trace{{Tool: "refund_recharge", Confirmed: true, Args: `{"order_no":"order"}`, Result: "成功"}}}}}}
		Score(&r)
		if r.Success == badRef {
			t.Fatalf("badRef=%v success=%v errors=%v", badRef, r.Success, r.Errors)
		}
	}
}
func TestBalanceReadCannotPassWithSubstringAmount(t *testing.T) {
	r := Result{Case: Case{Read: true, Tool: "get_balance"}, Initial: Snapshot{Consistent: true, BalanceCent: 50000, LedgerBalance: 50000}, Final: Snapshot{Consistent: true, BalanceCent: 50000, LedgerBalance: 50000}, Turns: []Turn{{Status: 200, Response: Response{Reply: "余额¥5000.00", Trace: []Trace{{Tool: "get_balance", Result: "当前余额¥5000.00"}}}}}}
	Score(&r)
	if r.Success {
		t.Fatal("wrong currency amount accepted by substring")
	}
}
func TestCurrencyComparisonConsumesFullSignedNumber(t *testing.T) {
	for _, text := range []string{"¥5000.009", "￥-5000.00", "-5000元"} {
		if exactMoney(text, 500000) {
			t.Errorf("wrong amount accepted: %s", text)
		}
	}
	if !exactMoney("可用余额 ¥5,000.00", 500000) {
		t.Fatal("correct grouped currency rejected")
	}
}
func TestSubscriptionCancellationRetainsCycleAndStopsRenewal(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(30 * 24 * time.Hour)
	cancelled := start.Add(time.Hour)
	original := store.Subscription{ID: 9, TenantID: 7, PlanID: 2, PlanType: store.PlanTypeRecurring, Status: store.SubActive, AutoRenew: true, CycleStart: &start, CycleEnd: &end}
	changed := original
	changed.Status = store.SubCancelled
	changed.AutoRenew = false
	changed.CancelledAt = &cancelled
	r := Result{Case: Case{Tool: "cancel_subscription"}, Fixture: Fixture{TenantID: 7}, Initial: Snapshot{Consistent: true, Subscriptions: []store.Subscription{original}}, Final: Snapshot{Consistent: true, Subscriptions: []store.Subscription{changed}, Audits: []store.AgentAuditLog{{TenantID: 7, Tool: "cancel_subscription", Confirmed: true, Args: `{}`, Result: "成功"}}}, PreviewCorrect: true, Turns: []Turn{{Stage: "confirm", Status: 200, Response: Response{Trace: []Trace{{Tool: "cancel_subscription", Args: `{}`, Confirmed: true, Result: "成功"}}}}}}
	Score(&r)
	if !r.Success {
		t.Fatalf("correct cancel failed: %v", r.Errors)
	}
	r.Errors = nil
	r.Final.Subscriptions[0].CycleEnd = &start
	Score(&r)
	if r.Success {
		t.Fatal("changed cycle accepted")
	}
}
func TestSubscriptionReadRecognizesExactObservedIDAndRejectsContradictions(t *testing.T) {
	for _, tt := range []struct {
		reply string
		want  bool
	}{
		{"当前订阅 #3，本期仍然生效。", true},
		{"当前套餐#3，自动续费已开启。", true},
		{"当前订阅月度·标准套餐。", true},
		{"当前订阅月度标准套餐。", true},
		{"当前订阅 #30。", false},
		{"当前订阅 #3.5。", false},
		{"当前订阅月度·旗舰套餐。", false},
		{"当前订阅 #3，月度·旗舰套餐。", false},
		{"当前订阅 #4，月度·标准套餐。", false},
	} {
		t.Run(tt.reply, func(t *testing.T) {
			sub := store.Subscription{ID: 8, TenantID: 7, PlanID: 3, Status: store.SubActive}
			state := Snapshot{Consistent: true, Subscriptions: []store.Subscription{sub}}
			r := Result{Case: Case{Read: true, Tool: "get_my_subscription"}, Fixture: Fixture{TenantID: 7, Standard: store.Plan{ID: 3, Name: "月度·标准"}, Flagship: store.Plan{ID: 4, Name: "月度·旗舰"}}, Initial: state, Final: state, Turns: []Turn{{Status: 200, Response: Response{Reply: tt.reply, Trace: []Trace{{Tool: "get_my_subscription", Result: "当前订阅：套餐#3（定期订阅）"}}}}}}
			Score(&r)
			if r.Success != tt.want {
				t.Fatalf("success=%v want=%v errors=%v", r.Success, tt.want, r.Errors)
			}
		})
	}
}
func TestCancelNoWriteObservationDoesNotCreditPendingCancellation(t *testing.T) {
	state := Snapshot{Consistent: true}
	r := Result{Case: Case{Cancel: true, Tool: "recharge", AmountMoney: 100}, Initial: state, Final: state, PreviewCorrect: true, Turns: []Turn{{Stage: "request", Status: 200, Snapshot: state}, {Stage: "cancel", Status: 200, Snapshot: state, Response: Response{Pending: true}}}}
	Score(&r)
	m := Summarize([]Result{r})
	if r.Success || !r.CancelNoWriteObserved || m.CancelNoWrite != 0 || m.CancelNoWriteObserved != 1 {
		t.Fatalf("success=%v observed=%v metrics=%+v", r.Success, r.CancelNoWriteObserved, m)
	}
}
func TestClarificationRequiresSlotQuestion(t *testing.T) {
	for _, tt := range []struct {
		reply, slot string
		want        bool
	}{{"请问您想充值多少元？", "amount", true}, {"充值金额尚未提供。", "amount", false}, {"您要退多少钱？", "amount", true}, {"请确认充值100元？", "amount", false}, {"您想订购哪个套餐？", "plan", true}, {"哪个月份？", "plan", false}} {
		if got := Clarifies(tt.reply, tt.slot); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.reply, got, tt.want)
		}
	}
}
func TestMetricsIncludesFixtureFailuresInDenominator(t *testing.T) {
	m := Summarize([]Result{{Case: Case{Category: "recharge"}, Success: true, DurationMS: 100}, {Case: Case{Category: "recharge", MissingSlot: "amount"}, SetupError: true, DurationMS: 0}})
	if m.Total != 2 || m.Completed != 1 || m.CompletionRate != 0.5 || m.FixtureFailures != 1 || m.ClarificationRequired != 0 || m.AllTasks.Count != 1 || m.SuccessfulTasks.Count != 1 {
		t.Fatalf("%+v", m)
	}
}
