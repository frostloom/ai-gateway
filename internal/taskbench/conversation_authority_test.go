package taskbench

import (
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"testing"
	"time"
)

func authorityResult(reply string) ConversationResult {
	f, s := conversationFixture()
	s.CapturedAt = time.Now()
	end := s.CapturedAt.Add(24 * time.Hour)
	f.Standard = store.Plan{ID: 47, Name: "月度·标准", PlanType: store.PlanTypeRecurring, PriceMoney: 80, RefreshHours: 5, TierQuota: `{"文本":12345}`}
	s.Own.Subscriptions = []store.Subscription{{TenantID: f.TenantID, PlanID: 47, Status: store.SubCancelled, AutoRenew: false, CycleEnd: &end}}
	s.Allowance = []store.TierRemain{{Tier: "文本", Quota: 12345, Consumed: 3000, Remaining: 9345}}
	in, out, cent := int64(1200), int64(300), int64(125)
	s.AnswerAuthority = &ConversationAnswerAuthority{TenantID: f.TenantID, CapturedAt: s.CapturedAt, CatalogComplete: true, Plans: []store.Plan{f.Standard}, BillsLimit: 10, BillsTotal: 1, BillsPhase: "settle", BillsStatus: "settled", RecentBills: []store.Bill{{ID: 9, TenantID: f.TenantID, Phase: "settle", Status: "settled", Model: "local-model", PromptTokens: &in, CompletionTokens: &out, ActualQuota: &cent, CreatedAt: s.CapturedAt}}, ConsumptionComplete: true, ConsumptionDays: 7, Consumption: []store.DayStat{{Day: s.CapturedAt.Format("2006-01-02"), Tokens: 125}}, RefreshHours: 5}
	return ConversationResult{Fixture: f, Initial: s, Observations: []ConversationObservation{{State: s, Turn: Turn{Response: Response{Reply: reply, Trace: []Trace{{Tool: "get_recent_bills", Phase: "read", Outcome: "success", ResolvedArgs: `{"limit":3}`}}}}}}}
}

func TestConversationAuthorityPlanPriceRefreshAndQuota(t *testing.T) {
	good := "可选套餐：\n套餐#47 月度·标准（定期订阅）· 月费 ¥80 · 每 5 小时刷新额度：文本1.2万"
	if factMismatch(CheckConversationFacts(authorityResult(good))) {
		t.Fatal("correct rounded plan facts rejected")
	}
	for _, bad := range []string{"套餐#47 月度·标准（定期订阅）· 月费 ¥800 · 每 5 小时刷新额度：文本1.2万", "套餐#47 月度·标准（定期订阅）· 月费 ¥80 · 每 24 小时刷新额度：文本1.2万", "套餐#47 月度·标准（定期订阅）· 月费 ¥80 · 每 5 小时刷新额度：文本2万"} {
		if !factMismatch(CheckConversationFacts(authorityResult(bad))) {
			t.Fatalf("wrong plan facts accepted: %s", bad)
		}
	}
	r := authorityResult("套餐#99 隐藏套餐（定期订阅）· 月费 ¥80 · 每 5 小时刷新额度：文本1.2万")
	r.Observations[0].State.AnswerAuthority.CatalogComplete = false
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("partial catalogue treated as nonexistent plan")
	}
	r.Observations[0].State.AnswerAuthority.CatalogComplete = true
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("complete catalogue ignored missing plan")
	}
}
func TestConversationAuthorityBillsAmountTokensAndScope(t *testing.T) {
	r := authorityResult("")
	day := r.Initial.CapturedAt.Format("2006-01-02 15:04:05")
	good := "最近 1 笔消费：\n" + day + " · local-model · 用量 1500 token · 扣费 ¥1.25\n合计扣费 ¥1.25。"
	r.Observations[0].Turn.Response.Reply = good
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("correct bills rejected")
	}
	for _, bad := range []string{"最近 1 笔消费：\n" + day + " · local-model · 用量 0 token · 扣费 ¥1.25\n合计扣费 ¥1.25。", "最近 1 笔消费：\n" + day + " · local-model · 用量 1500 token · 扣费 ¥125.00\n合计扣费 ¥1.25。", "最近 0 笔消费：\n合计扣费 ¥0.00。"} {
		r.Observations[0].Turn.Response.Reply = bad
		if !factMismatch(CheckConversationFacts(r)) {
			t.Fatalf("wrong bills accepted: %s", bad)
		}
	}
	r.Observations[0].Turn.Response.Reply = good
	r.Observations[0].State.AnswerAuthority.RecentBills[0].PromptTokens = nil
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("missing token evidence treated as explicit zero")
	}
	r.Observations[0].State.AnswerAuthority = nil
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("historical missing authority invented")
	}
}
func TestConversationAuthorityAllowanceCycleAndConsumption(t *testing.T) {
	r := authorityResult("")
	end := r.Initial.Own.Subscriptions[0].CycleEnd.Format("2006-01-02")
	r.Observations[0].Turn.Response.Reply = "当前订阅：套餐#47（定期订阅），本期到 " + end + "，已停止续费，本期到期前可继续使用，剩余额度：文本 9345/1.2万（每 5 小时滚动刷新，未用不累积）"
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("future cancelled allowance rejected")
	}
	r.Observations[0].Turn.Response.Reply = "当前订阅：套餐#47（定期订阅），本期到 1999-01-01，剩余额度：文本 0/1.2万（每 24 小时滚动刷新，未用不累积）"
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("wrong cycle/allowance accepted")
	}
	r = authorityResult("近 7 天每日消耗（元）：" + time.Now().Format("2006-01-02") + " 消耗 ¥1.25；合计 ¥1.25。")
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("correct consumption rejected")
	}
	r.Observations[0].Turn.Response.Reply = "近 7 天没有消耗记录。"
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("cash consumption erased")
	}
}
func TestConversationAuthorityRejectsForeignOrWrongQueryEvidence(t *testing.T) {
	for _, mutate := range []func(*ConversationAnswerAuthority){func(a *ConversationAnswerAuthority) { a.TenantID = 20 }, func(a *ConversationAnswerAuthority) { a.RecentBills[0].TenantID = 20 }, func(a *ConversationAnswerAuthority) { a.BillsPhase = "reserve" }, func(a *ConversationAnswerAuthority) { a.BillsLimit = 0 }} {
		r := authorityResult("最近没有消费记录。")
		mutate(r.Observations[0].State.AnswerAuthority)
		if !factMismatch(CheckConversationFacts(r)) {
			t.Fatal("foreign/mismatched independent evidence accepted")
		}
	}
}

func TestConversationAuthorityRejectsOutOfWindowConsumption(t *testing.T) {
	r := authorityResult("近 7 天每日消耗（元）：合计 ¥1.25。")
	r.Observations[0].State.AnswerAuthority.Consumption[0].Day = r.Initial.CapturedAt.AddDate(0, 0, -8).Format("2006-01-02")
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("out-of-window authority accepted")
	}
}

func TestConversationAuthorityUnknownModelDisplay(t *testing.T) {
	r := authorityResult("")
	r.Observations[0].State.AnswerAuthority.RecentBills[0].Model = ""
	r.Observations[0].Turn.Response.Reply = "最近 1 笔消费：\n" + r.Initial.CapturedAt.Format("2006-01-02 15:04:05") + " · ? · 用量 1500 token · 扣费 ¥1.25\n合计扣费 ¥1.25。"
	if factMismatch(CheckConversationFacts(r)) {
		t.Fatal("real unknown-model placeholder rejected")
	}
}

func TestConversationAuthorityOneTimeIsNotRecurring(t *testing.T) {
	r := authorityResult("套餐#47 月度·标准（定期订阅）· 月费 ¥80 · 每 5 小时刷新额度：文本1.2万")
	r.Observations[0].State.AnswerAuthority.Plans[0].PlanType = store.PlanTypeOneTime
	if !factMismatch(CheckConversationFacts(r)) {
		t.Fatal("one-time plan accepted as recurring")
	}
}

func TestConversationAuthorityPaddedZeroDaysAreNotRecords(t *testing.T) {
	r := authorityResult("近 7 天没有消耗记录。")
	r.Observations[0].State.AnswerAuthority.Consumption[0].Tokens = 0
	checks := CheckConversationFacts(r)
	if factMismatch(checks) {
		t.Fatal("zero padded day treated as consumption record")
	}
	found := false
	for _, c := range checks {
		if c.Fact == "cash_consumption_records_unknown" && c.Status == "not_evaluated" {
			found = true
		}
	}
	if !found {
		t.Fatal("zero cash falsely proves no records")
	}
}

func TestConversationAuthorityMultipleBillQueriesNeedAttribution(t *testing.T) {
	r := authorityResult("最近 1 笔消费：合计扣费 ¥1.25。")
	r.Observations[0].Turn.Response.Trace = append(r.Observations[0].Turn.Response.Trace, Trace{Tool: "get_recent_bills", Phase: "read", Outcome: "success", Args: `{"limit":10}`})
	found := false
	for _, c := range CheckConversationFacts(r) {
		if c.Fact == "bill_query_scope_missing" && c.Status == "not_evaluated" {
			found = true
		}
	}
	if !found {
		t.Fatal("multiple bills reads were assigned an invented reply scope")
	}
}
