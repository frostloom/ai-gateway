package store

import (
	"context"
	"sync"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// mock-model 单价 1_000_000 分/M → estPrompt=84 时 pre = 84×1.2 = 100（见 seedMockModel）。

// 结算全流程：预占 100 → 报用量(50,100) → 实际 150、delta +50 → 余额 850。
func TestSettleFullFlow(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-settle", tenantID, 84)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{
		RequestId: req.RequestId, BillId: 0, PromptTokens: 50, CompletionTokens: 100,
	}); err != nil {
		t.Fatalf("report usage: %v", err)
	}
	resp, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId, BillId: 0})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if resp.DeltaQuota != 50 {
		t.Fatalf("delta = %d, want 50（actual 150 - pre 100）", resp.DeltaQuota)
	}
	if resp.BalanceAfter != 850 {
		t.Fatalf("balance = %d, want 850", resp.BalanceAfter)
	}
	assertReserveStatus(t, s, req.RequestId, StatusSettled)

	var settleRows int64
	s.db.Model(&Bill{}).Where("request_id = ? AND phase = ?", req.RequestId, PhaseSettle).Count(&settleRows)
	if settleRows != 1 {
		t.Fatalf("settle rows = %d, want 1", settleRows)
	}
}

// 结算幂等：同一 request_id 重复 settle 只应用一次 delta、只落一行 settle。
func TestSettleIdempotent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-settle-dup", tenantID, 84)
	s.Reserve(ctx, req)
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{
		RequestId: req.RequestId, PromptTokens: 50, CompletionTokens: 100,
	}); err != nil {
		t.Fatalf("report usage: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
		}()
	}
	wg.Wait()

	if bal, _ := s.getBalance(ctx, tenantID); bal != 850 {
		t.Fatalf("balance = %d, want 850（重复结算只应用一次 delta）", bal)
	}
	var settleRows int64
	s.db.Model(&Bill{}).Where("request_id = ? AND phase = ?", req.RequestId, PhaseSettle).Count(&settleRows)
	if settleRows != 1 {
		t.Fatalf("settle rows = %d, want 1", settleRows)
	}
	assertReserveStatus(t, s, req.RequestId, StatusSettled)
}

// 冲正退款：预占 100 → 退 100 → 余额还原；重复冲正不二次退。
func TestReverseIdempotent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-rev", tenantID, 84)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	r1, err := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId, Reason: "test"})
	if err != nil || !r1.Reversed || r1.RefundQuota != 100 {
		t.Fatalf("reverse#1: reversed=%v refund=%d err=%v", r1.Reversed, r1.RefundQuota, err)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 1000 {
		t.Fatalf("balance = %d, want 1000（已退回）", bal)
	}
	r2, err := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId, Reason: "retry"})
	if err != nil || !r2.Reversed {
		t.Fatalf("reverse#2（幂等重放）: reversed=%v err=%v", r2.Reversed, err)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 1000 {
		t.Fatalf("balance = %d, want 1000（重复冲正不能二次退款）", bal)
	}
}

// 已结算再冲正：钱按实际结清了，Reverse 必须 no-op（不能把 actual 一起退了）。
func TestReverseAfterSettled(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-rev-settled", tenantID, 84)
	s.Reserve(ctx, req)
	s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50, CompletionTokens: 100})
	s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})

	r, err := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId, Reason: "late"})
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if r.Reversed || r.RefundQuota != 0 {
		t.Fatalf("已结算后冲正: reversed=%v refund=%d, want no-op", r.Reversed, r.RefundQuota)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 850 {
		t.Fatalf("balance = %d, want 850（不应退款）", bal)
	}
}

// 重建投影：Redis 被污染后，RebuildBalance 以 MySQL 账本重算。
// 场景：A 结算(actual 150) + B 冲正(全退) + C 在途预扣(100) → 权威余额 = 1000-150-100 = 750。
func TestRebuildBalance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)

	// A：完整结算
	s.Reserve(ctx, reserveReq("rb-a", tenantID, 84))
	s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: "rb-a", PromptTokens: 50, CompletionTokens: 100})
	s.Settle(ctx, &billingv1.SettleRequest{RequestId: "rb-a"})

	// B：预占后冲正（全退）
	s.Reserve(ctx, reserveReq("rb-b", tenantID, 84))
	s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: "rb-b", Reason: "test"})

	// C：在途（预扣未结算，模拟网关崩溃）
	s.Reserve(ctx, reserveReq("rb-c", tenantID, 84))

	// 当前 Redis 投影：1000 -100(A预扣) -50(A delta) -100(B预扣) +100(B退) -100(C预扣) = 750
	if bal, _ := s.getBalance(ctx, tenantID); bal != 750 {
		t.Fatalf("pre-corrupt balance = %d, want 750", bal)
	}

	// 污染 Redis：模拟「Lua 已扣但落库失败」等脏投影
	if err := s.rdb.Set(ctx, BalanceKey(tenantID), 999, 0).Err(); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	bal, err := s.RebuildBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if bal != 750 {
		t.Fatalf("rebuild balance = %d, want 750（initial - 在途预扣 - 已结算actual）", bal)
	}
	if got, _ := s.getBalance(ctx, tenantID); got != 750 {
		t.Fatalf("redis after rebuild = %d, want 750", got)
	}
}

// 重建投影不重复扣：连续跑两次重建结果一致（幂等）。
func TestRebuildBalanceIdempotent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	s.Reserve(ctx, reserveReq("rb2-a", tenantID, 84))
	s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: "rb2-a", PromptTokens: 50, CompletionTokens: 100})
	s.Settle(ctx, &billingv1.SettleRequest{RequestId: "rb2-a"})

	b1, err := s.RebuildBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("rebuild#1: %v", err)
	}
	b2, err := s.RebuildBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("rebuild#2: %v", err)
	}
	if b1 != b2 {
		t.Fatalf("rebuild not idempotent: %d vs %d", b1, b2)
	}
}

func assertReserveStatus(t *testing.T, s *Store, requestID, want string) {
	t.Helper()
	var row Bill
	if err := s.db.Where("request_id = ? AND phase = ?", requestID, PhaseReserve).First(&row).Error; err != nil {
		t.Fatalf("load reserve row: %v", err)
	}
	if row.Status != want {
		t.Fatalf("reserve status = %s, want %s", row.Status, want)
	}
}
