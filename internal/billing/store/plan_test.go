package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// M9 话费式计费边界单测：退款上限不超余额、double-pay 不双入账、one_time 拒绝订购、
// recurring 订阅/续费/改套餐/退订均不入账钱（GPT Plus 式：月费换额度资格）。
// 金额单位 = 分（0.01 元）；复用 reserve_test.go 的 newTestStore/seedTenant/reserveReq
// （真实 MySQL/Redis 测试库）。reserve 相关测试须先 seedMockModel（mock-model 单价
// 1_000_000 分/M，pre = estPrompt×1.2 整值）。

// payOrder 建单并支付，返回订单。money 单位为元，内部 ×100 转分入账。
func payOrder(t *testing.T, s *Store, ctx context.Context, tid uint64, money int64, idemKey string) *RechargeOrder {
	t.Helper()
	o, err := s.CreateRechargeOrder(ctx, tid, money, idemKey)
	if err != nil {
		t.Fatalf("create recharge: %v", err)
	}
	if _, err := s.PayRecharge(ctx, o.OrderNo); err != nil {
		t.Fatalf("pay recharge: %v", err)
	}
	return o
}

func topupSum(t *testing.T, s *Store, ctx context.Context, tid uint64) int64 {
	t.Helper()
	var n int64
	if err := s.db.WithContext(ctx).Model(&Topup{}).
		Where("tenant_id = ?", tid).Select("COALESCE(SUM(amount),0)").Scan(&n).Error; err != nil {
		t.Fatalf("sum topups: %v", err)
	}
	return n
}

// 充值订单退款量 = min(订单额度, 当前余额)：消费到余额 < 订单额度时，只退余额，绝不退负。
func TestRefundCappedAtBalance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tid := seedTenant(t, s, 1_000) // ¥10

	o := payOrder(t, s, ctx, tid, 100, "ord-cap-1") // 订单额度 10,000 分
	// 真实预占消耗：estPrompt=7500 → pre=9000，余额 11,000 → 2,000 < 订单 10,000
	if _, err := s.Reserve(ctx, reserveReq("cap-reserve-1", tid, 7_500)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 2_000 {
		t.Fatalf("balance after reserve = %d, want 2000", bal)
	}

	r, err := s.RefundRecharge(ctx, tid, o.OrderNo)
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if r.AmountCent != 10_000 || r.RefundCent != 2_000 || r.BalanceAfter != 0 {
		t.Fatalf("refund = %+v, want 订单额度 10000 / 退 2000 / 退后余额 0", r)
	}
	// 负入账行落库（source=refund, ref=order_no）
	var neg int64
	if err := s.db.WithContext(ctx).Model(&Topup{}).
		Where("tenant_id = ? AND source = ? AND amount < 0", tid, TopupRefund).Count(&neg).Error; err != nil {
		t.Fatalf("count neg topup: %v", err)
	}
	if neg != 1 {
		t.Fatalf("refund 负入账行 = %d, want 1", neg)
	}
	// 账本 == Redis == 0（initial 1000 + 充值 10000 − 退款 2000 − 预占 9000 = 0）
	ledger, err := LedgerBalance(ctx, s.db, tid)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if bal, _ := s.getBalance(ctx, tid); ledger != 0 || bal != 0 {
		t.Fatalf("ledger=%d redis=%d, want 0/0", ledger, bal)
	}
	// 订单已退款 → 重复退款幂等（no-op）
	o2, _ := s.rechargeByOrderNo(ctx, o.OrderNo)
	if o2.Status != RechargeRefunded {
		t.Fatalf("order status = %s, want refunded", o2.Status)
	}
	if r2, err := s.RefundRecharge(ctx, tid, o.OrderNo); err != nil || r2.RefundCent != 0 {
		t.Fatalf("re-refund = %+v err=%v, want 退 0 幂等", r2, err)
	}
}

// 余额已耗尽（0）时退款被明确拒绝，且不写任何负入账。
func TestRefundZeroWhenBalanceSpent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tid := seedTenant(t, s, 0)
	o := payOrder(t, s, ctx, tid, 100, "ord-zero-1") // 充值后余额 10,000 分
	// 精确预占到 0：estPrompt=8334 → pre=8334×1.2=10000.8→10000
	if _, err := s.Reserve(ctx, reserveReq("zero-reserve-1", tid, 8_334)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0", bal)
	}
	if _, err := s.RefundRecharge(ctx, tid, o.OrderNo); err == nil {
		t.Fatal("want refund refused（余额 0）")
	} else if !strings.Contains(err.Error(), "无可退额度") {
		t.Fatalf("err = %v, want 含『无可退额度』", err)
	}
	if got := topupSum(t, s, ctx, tid); got != 10_000 {
		t.Fatalf("topups = %d, want 10000（拒绝退款不应写负行）", got)
	}
}

// 按金额退款（不绑定订单）：退款量 = min(金额, 当前余额)；同幂等锚点重试不重复退。
func TestRefundAmountCappedAtBalance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 1_000)
	payOrder(t, s, ctx, tid, 100, "ord-amt-1") // 充值后余额 11,000 分

	// 退 ¥60 → 6,000 分 < 余额 11,000，全退
	r, err := s.RefundAmount(ctx, tid, 60, "idem-amt-1")
	if err != nil {
		t.Fatalf("refund amount: %v", err)
	}
	if r.RefundCent != 6_000 || r.BalanceAfter != 5_000 || r.OrderNo != "" {
		t.Fatalf("refund amount = %+v, want 退 6000 / 余 5000 / 无订单", r)
	}
	// 同锚点重试 → 不再退（幂等）
	r2, err := s.RefundAmount(ctx, tid, 60, "idem-amt-1")
	if err != nil || r2.RefundCent != 0 {
		t.Fatalf("re-refund amount = %+v err=%v, want 退 0 幂等", r2, err)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 5_000 {
		t.Fatalf("balance after retry = %d, want 5000", bal)
	}

	// 超额退：退 ¥200 → 只退到当前余额 5,000 分
	r3, err := s.RefundAmount(ctx, tid, 200, "idem-amt-2")
	if err != nil {
		t.Fatalf("refund over cap: %v", err)
	}
	if r3.RefundCent != 5_000 || r3.BalanceAfter != 0 {
		t.Fatalf("refund over cap = %+v, want 只退余额 5000 / 退后 0", r3)
	}
	// 余额 0 时退款被拒
	if _, err := s.RefundAmount(ctx, tid, 10, "idem-amt-3"); err == nil {
		t.Fatal("want refund refused（余额 0）")
	}
	// 账本口径一致：1000 + 10000 − 6000 − 5000 = 0
	ledger, err := LedgerBalance(ctx, s.db, tid)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if ledger != 0 {
		t.Fatalf("ledger = %d, want 0", ledger)
	}
}

// 同一订单重复支付：状态幂等，不重复入账；同 idemKey 再建单返回同一订单。
func TestPayRechargeIdempotent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 0)
	o := payOrder(t, s, ctx, tid, 100, "ord-pay-1")

	o2, err := s.PayRecharge(ctx, o.OrderNo)
	if err != nil {
		t.Fatalf("re-pay: %v", err)
	}
	if o2.Status != RechargePaid {
		t.Fatalf("status = %s, want paid", o2.Status)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 10_000 {
		t.Fatalf("balance = %d, want 10000（重复支付不双入账）", bal)
	}
	if got := topupSum(t, s, ctx, tid); got != 10_000 {
		t.Fatalf("topups = %d, want 10000", got)
	}
	// 同 idemKey 再建单 → 幂等返回已有订单
	o3, err := s.CreateRechargeOrder(ctx, tid, 100, "ord-pay-1")
	if err != nil {
		t.Fatalf("re-create: %v", err)
	}
	if o3.OrderNo != o.OrderNo {
		t.Fatalf("idem create = %s, want %s", o3.OrderNo, o.OrderNo)
	}
}

// one_time 买断套餐 M9 起不再售出：订阅被明确拒绝、不落订阅行、不动账。
func TestSubscribeOneTimeRefused(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 1_000) // ¥10
	ot := &Plan{Name: "买断·测试", PlanType: PlanTypeOneTime, PriceMoney: 10, TierQuota: `{}`}
	if err := s.db.WithContext(ctx).Create(ot).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := s.SubscribePlan(ctx, tid, ot.ID); err == nil || !strings.Contains(err.Error(), "已不支持订购") {
		t.Fatalf("want 一次性套餐拒绝订购，got %v", err)
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Subscription{}).Where("tenant_id = ?", tid).Count(&n).Error; err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if n != 0 {
		t.Fatalf("subscriptions = %d, want 0（拒绝不应落订阅行）", n)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 1_000 {
		t.Fatalf("balance = %d, want 1000（拒绝不应动账）", bal)
	}
}

// recurring 每租户至多一条 active：重复订阅被拒、不双开；订阅/退订均不入账钱（GPT Plus 式额度资格）。
func TestSubscribeRecurringOnceAndCancelNoRefund(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 0)
	rec := &Plan{Name: "月度·测试", PlanType: PlanTypeRecurring, PriceMoney: 80, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":100000}`}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}

	sub, err := s.SubscribePlan(ctx, tid, rec.ID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if sub.CycleEnd == nil || !sub.AutoRenew {
		t.Fatalf("recurring sub 缺 cycle/auto_renew: %+v", sub)
	}
	if sub.QuotaGranted != 0 {
		t.Fatalf("quota_granted = %d, want 0（月费换额度资格，无钱入账）", sub.QuotaGranted)
	}
	// 订阅不产生任何入账/余额变化
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（订阅不入账钱）", bal)
	}
	if got := topupSum(t, s, ctx, tid); got != 0 {
		t.Fatalf("topups = %d, want 0", got)
	}
	// 重复订阅被拒
	if _, err := s.SubscribePlan(ctx, tid, rec.ID); err == nil {
		t.Fatal("want 重复订阅被拒")
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（重复订阅不动账）", bal)
	}
	// 退订：当期不退（退款 0）、auto_renew=false、本期用到 cycle_end
	cr, err := s.CancelSubscription(ctx, tid)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cr.Refund != 0 {
		t.Fatalf("cancel refund = %d, want 0（当期不退）", cr.Refund)
	}
	sub2, _ := s.GetActiveRecurring(ctx, tid)
	if sub2 != nil {
		t.Fatalf("退订后仍有 active recurring: %+v", sub2)
	}
	var one int64
	if err := s.db.WithContext(ctx).Model(&Subscription{}).
		Where("tenant_id = ? AND plan_type = ? AND auto_renew = ?", tid, PlanTypeRecurring, false).Count(&one).Error; err != nil {
		t.Fatalf("count cancelled: %v", err)
	}
	if one != 1 {
		t.Fatalf("cancelled rows = %d, want 1", one)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（退订无退款、不动账）", bal)
	}
}

// 续费器：到期 + auto_renew → 推进下一计费周期（额度资格延续，不再入账钱）；重跑不双续。
func TestRenewDueSubscription(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 0)
	rec := &Plan{Name: "月度·续费测试", PlanType: PlanTypeRecurring, PriceMoney: 80, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":100000}`}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub, err := s.SubscribePlan(ctx, tid, rec.ID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// 把周期拨到已到期
	past := time.Now().Add(-time.Hour)
	if err := s.db.WithContext(ctx).Model(&Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]any{"cycle_start": past.Add(-30 * 24 * time.Hour), "cycle_end": past}).Error; err != nil {
		t.Fatalf("rewind cycle: %v", err)
	}
	n, err := s.RenewDueSubscriptions(ctx)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if n != 1 {
		t.Fatalf("renewed = %d, want 1", n)
	}
	// M9：续费只延续「额度资格」，不产生任何入账/余额变化
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（续费不入账钱）", bal)
	}
	// 重跑：周期已推进到未来，不应再续
	if n2, _ := s.RenewDueSubscriptions(ctx); n2 != 0 {
		t.Fatalf("renewed2 = %d, want 0", n2)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（幂等不双续）", bal)
	}
	if ledger, err := LedgerBalance(ctx, s.db, tid); err != nil || ledger != 0 {
		t.Fatalf("ledger = %d err=%v, want 0", ledger, err)
	}
	// cycle_num 推进；quota_granted 保持 0（无钱可授）
	var s2 Subscription
	if err := s.db.WithContext(ctx).First(&s2, sub.ID).Error; err != nil {
		t.Fatalf("load sub: %v", err)
	}
	if s2.CycleNum != 2 || s2.QuotaGranted != 0 {
		t.Fatalf("cycle_num=%d quota_granted=%d, want 2 / 0", s2.CycleNum, s2.QuotaGranted)
	}
}

// 改套餐：记 pending_plan_id 下期生效（当期额度/到期不变、余额不动）；目标非定期被拒。
func TestChangePlanPendingNextCycle(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 0)
	rec1 := &Plan{Name: "月度·改A", PlanType: PlanTypeRecurring, PriceMoney: 80, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":100000}`}
	rec2 := &Plan{Name: "月度·改B", PlanType: PlanTypeRecurring, PriceMoney: 300, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":400000}`}
	ot := &Plan{Name: "买断·改C", PlanType: PlanTypeOneTime, PriceMoney: 10, TierQuota: `{}`}
	if err := s.db.WithContext(ctx).Create(rec1).Error; err != nil {
		t.Fatalf("create rec1: %v", err)
	}
	if err := s.db.WithContext(ctx).Create(rec2).Error; err != nil {
		t.Fatalf("create rec2: %v", err)
	}
	if err := s.db.WithContext(ctx).Create(ot).Error; err != nil {
		t.Fatalf("create ot: %v", err)
	}
	if _, err := s.SubscribePlan(ctx, tid, rec1.ID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := s.ChangePlan(ctx, tid, rec2.ID); err != nil {
		t.Fatalf("change plan: %v", err)
	}
	sub, _ := s.GetActiveRecurring(ctx, tid)
	if sub.PendingPlanID == nil || *sub.PendingPlanID != rec2.ID {
		t.Fatalf("pending_plan_id = %v, want %d", sub.PendingPlanID, rec2.ID)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（改套餐当期不生效、不动账）", bal)
	}
	// 改到一次性 → 拒绝
	if err := s.ChangePlan(ctx, tid, ot.ID); err == nil || !strings.Contains(err.Error(), "定期") {
		t.Fatalf("want 目标非定期被拒，got %v", err)
	}
	// 到期续费时应用 pending_plan_id → plan_id 切换为新套餐（额度按新套餐生效；仍不入账）
	past := time.Now().Add(-time.Hour)
	if err := s.db.WithContext(ctx).Model(&Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]any{"cycle_start": past.Add(-30 * 24 * time.Hour), "cycle_end": past}).Error; err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if n, _ := s.RenewDueSubscriptions(ctx); n != 1 {
		t.Fatalf("renewed = %d, want 1", n)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 0 {
		t.Fatalf("balance = %d, want 0（下期按新套餐仍只延续额度资格，不入账）", bal)
	}
	var s2 Subscription
	if err := s.db.WithContext(ctx).First(&s2, sub.ID).Error; err != nil {
		t.Fatalf("load sub: %v", err)
	}
	if s2.PlanID != rec2.ID || s2.PendingPlanID != nil {
		t.Fatalf("plan_id=%d pending=%v, want 切换 rec2 且清空 pending", s2.PlanID, s2.PendingPlanID)
	}
}
