package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// M9 订阅额度（GPT Plus 式）单测：档次解析、Reserve 额度内/超额/免费绕过、窗口滚动恢复、
// settle 不碰钱、MeView 剩余额度推导。复用 reserve_test.go 的 newTestStore/seedTenant。

// seedTierModel 种带档次标签的模型（计费用，单价 1_000_000 分/M 让 pre 成整值）。
func seedTierModel(t *testing.T, s *Store, modelID, tags string) {
	t.Helper()
	m := Model{
		Vendor: "测试", ModelID: modelID, Name: modelID,
		InputPriceCent: 1_000_000, OutputPriceCent: 1_000_000, Status: 0,
		Tags: tags,
	}
	if err := s.db.Create(&m).Error; err != nil {
		t.Fatalf("seed tier model %s: %v", modelID, err)
	}
}

// subscribeTestPlan 建一个定期订阅套餐并开通，返回套餐（额度参数由调用方定）。
func subscribeTestPlan(t *testing.T, s *Store, ctx context.Context, tid uint64, tierQuota string, refreshHours int) *Plan {
	t.Helper()
	p := &Plan{
		Name: fmt.Sprintf("订阅·测试-%d", time.Now().UnixNano()), PlanType: PlanTypeRecurring,
		PriceMoney: 80, ValidityDays: 30, RefreshHours: refreshHours, TierQuota: tierQuota,
	}
	if err := s.db.Create(p).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := s.SubscribePlan(ctx, tid, p.ID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return p
}

func reserveTierReq(rid string, tenantID uint64, model string, estPrompt int64) *billingv1.ReserveRequest {
	return &billingv1.ReserveRequest{
		RequestId: rid, TenantId: int64(tenantID), ApiKeyId: 1,
		Model: model, EstPromptTokens: estPrompt,
	}
}

// TierFromTags 档次优先级：取最贵档；免费档/未知标签/无标签 → 计为免费（不限量）。
func TestTierFromTags(t *testing.T) {
	cases := []struct {
		tags []string
		want string
	}{
		{[]string{"文本", "轻量"}, "文本"}, // 取最贵档
		{[]string{"旗舰", "推理"}, "旗舰"},
		{[]string{"推理", "视觉", "长文本"}, "推理"},
		{[]string{"免费"}, "免费"},
		{nil, ""},              // 无标签 → 免费不限量
		{[]string{"未知标签"}, ""}, // 未知标签同样计为免费
	}
	for _, c := range cases {
		if got := TierFromTags(c.tags); got != c.want {
			t.Fatalf("TierFromTags(%v) = %q, want %q", c.tags, got, c.want)
		}
	}
	// 展示排序稳定性：旗舰 在最前、免费 在最后
	order := TierNames()
	if order[0] != "旗舰" || order[len(order)-1] != "免费" {
		t.Fatalf("tierOrder = %v, want 旗舰 最先 / 免费 最后", order)
	}
}

// 订阅额度 Reserve 全路径：额度内走 funding=subscription/pre=0（不碰钱）、settle 零钱、
// 超额 RESERVE_SUB_QUOTA、窗口滑过恢复、免费档绕过额度走余额。
func TestReserveSubscriptionAllowance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedTierModel(t, s, "sub-text", `["文本"]`)
	seedTierModel(t, s, "sub-free", `["免费"]`)
	tid := seedTenant(t, s, 1_000) // ¥10
	subscribeTestPlan(t, s, ctx, tid, `{"文本":300}`, 5)

	// ① 额度内（est 100 + 已用 0 ≤ 300）：reserve 走订阅额度（funding=subscription、pre=0），余额不动
	resp, err := s.Reserve(ctx, reserveTierReq("sub-1", tid, "sub-text", 100))
	if err != nil || resp.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve in-window: code=%v err=%v", resp.Code, err)
	}
	var row Bill
	if err := s.db.Where("request_id = ? AND phase = ?", "sub-1", PhaseReserve).First(&row).Error; err != nil {
		t.Fatalf("load reserve row: %v", err)
	}
	if row.Funding != FundingSubscription || row.PreQuota != 0 {
		t.Fatalf("reserve row funding=%s pre=%d, want subscription/0", row.Funding, row.PreQuota)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 1_000 {
		t.Fatalf("balance = %d, want 1000（额度内不扣钱）", bal)
	}

	// ② settle：actual=0/delta=0，账本不变（订阅使用对元口径完全隐形）
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: "sub-1", PromptTokens: 100}); err != nil {
		t.Fatalf("report usage: %v", err)
	}
	settle, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: "sub-1"})
	if err != nil || !settle.Settled || settle.DeltaQuota != 0 {
		t.Fatalf("settle: %+v err=%v, want settled/delta=0", settle, err)
	}
	if ledger, _ := LedgerBalance(ctx, s.db, tid); ledger != 1_000 {
		t.Fatalf("ledger = %d, want 1000（订阅使用不入账本）", ledger)
	}

	// ③ 超额（已用 100 + est 300 > 300）：RESERVE_SUB_QUOTA，余额不动、不落行
	resp2, err := s.Reserve(ctx, reserveTierReq("sub-2", tid, "sub-text", 300))
	if err != nil || resp2.Code != billingv1.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatalf("over-quota: code=%v err=%v, want SUB_QUOTA", resp2.Code, err)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 1_000 {
		t.Fatalf("balance = %d, want 1000（超额拒绝不动钱）", bal)
	}
	if n := countReserveRows(s); n != 1 {
		t.Fatalf("reserve rows = %d, want 1（超额不落行）", n)
	}

	// ④ 窗口滚动：把 sub-1 消费回拨滑出 5h 窗口 → 额度恢复，同量请求重新 OK
	if err := s.db.Model(&Bill{}).Where("request_id = ? AND phase = ?", "sub-1", PhaseReserve).
		Update("created_at", time.Now().Add(-6*time.Hour)).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
	resp3, err := s.Reserve(ctx, reserveTierReq("sub-3", tid, "sub-text", 300))
	if err != nil || resp3.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("post-refresh: code=%v err=%v, want OK（窗口滑过额度恢复）", resp3.Code, err)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 1_000 {
		t.Fatalf("balance = %d, want 1000（窗口刷新不碰钱）", bal)
	}

	// ⑤ 免费档绕过额度：走余额路径，真实预扣（est 84 → pre 100）
	respF, err := s.Reserve(ctx, reserveTierReq("sub-free-1", tid, "sub-free", 84))
	if err != nil || respF.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("free model: code=%v err=%v", respF.Code, err)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 900 {
		t.Fatalf("balance = %d, want 900（免费档走余额扣 100）", bal)
	}
	var freeRow Bill
	if err := s.db.Where("request_id = ? AND phase = ?", "sub-free-1", PhaseReserve).First(&freeRow).Error; err != nil {
		t.Fatalf("load free row: %v", err)
	}
	if freeRow.Funding != FundingBalance || freeRow.PreQuota != 100 {
		t.Fatalf("free row funding=%s pre=%d, want balance/100", freeRow.Funding, freeRow.PreQuota)
	}
}

// MeView 活跃订阅的各档次剩余额度（token）实时推导；无订阅/退订后为 nil。
func TestMeViewSubscriptionAllowance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedTierModel(t, s, "sub-text", `["文本"]`)
	tid := seedTenant(t, s, 0)

	// 无订阅 → allowance nil、refresh_hours 0
	mv0, err := s.MeOverview(ctx, tid)
	if err != nil {
		t.Fatalf("meoverview: %v", err)
	}
	if mv0.Allowance != nil || mv0.RefreshHours != 0 {
		t.Fatalf("no-sub allowance = %+v refresh=%d, want nil/0", mv0.Allowance, mv0.RefreshHours)
	}

	subscribeTestPlan(t, s, ctx, tid, `{"文本":300,"旗舰":100}`, 5)
	if _, err := s.Reserve(ctx, reserveTierReq("mv-1", tid, "sub-text", 100)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// 额度消费按 token 统计：网关跑完流后 ReportUsage 写入实际用量，reserve→report 间隙不占额度。
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: "mv-1", PromptTokens: 100}); err != nil {
		t.Fatalf("report usage: %v", err)
	}

	mv, err := s.MeOverview(ctx, tid)
	if err != nil {
		t.Fatalf("meoverview: %v", err)
	}
	if mv.RefreshHours != 5 {
		t.Fatalf("refresh_hours = %d, want 5", mv.RefreshHours)
	}
	if mv.Allowance == nil || len(mv.Allowance) != 2 {
		t.Fatalf("allowance = %+v, want 2 档", mv.Allowance)
	}
	if mv.Allowance[0].Tier != "旗舰" {
		t.Fatalf("allowance[0].tier = %s, want 旗舰（贵档在前）", mv.Allowance[0].Tier)
	}
	byTier := map[string]TierRemain{}
	for _, a := range mv.Allowance {
		byTier[a.Tier] = a
	}
	if a := byTier["文本"]; a.Quota != 300 || a.Consumed != 100 || a.Remaining != 200 {
		t.Fatalf("文本 allowance = %+v, want quota300/consumed100/remaining200", a)
	}
	if a := byTier["旗舰"]; a.Quota != 100 || a.Consumed != 0 || a.Remaining != 100 {
		t.Fatalf("旗舰 allowance = %+v, want quota100/consumed0/remaining100", a)
	}

	// 退订 → 无 active 订阅 → allowance 回到 nil
	if _, err := s.CancelSubscription(ctx, tid); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	mv2, _ := s.MeOverview(ctx, tid)
	if mv2.Allowance != nil {
		t.Fatalf("退订后 allowance = %+v, want nil", mv2.Allowance)
	}
}
