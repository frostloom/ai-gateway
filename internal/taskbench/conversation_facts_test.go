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
	d.steps[2].Turn.Response.Reply = "充值成功：到账 ¥999。"
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
