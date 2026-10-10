package store

import (
	"context"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// 有标价但估算金额取整为 0 时，余额路径至少预扣 1 分；余额不够则拒绝，不落行。
// 两边价格都是 0 的模型仍然预扣 0。
func TestReserveMinimumCentWhenPriceRoundsToZero(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	cheap := Model{
		Vendor: "测试", ModelID: "cheap-model", Name: "Cheap",
		InputPriceCent: 100, OutputPriceCent: 200, Status: 0, Tags: "[]",
	}
	if err := s.db.Create(&cheap).Error; err != nil {
		t.Fatal(err)
	}
	free := Model{
		Vendor: "测试", ModelID: "free-model", Name: "Free",
		InputPriceCent: 0, OutputPriceCent: 0, Status: 0, Tags: "[]",
	}
	if err := s.db.Create(&free).Error; err != nil {
		t.Fatal(err)
	}

	broke := seedTenant(t, s, 0)
	resp, err := s.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId: "rid-min-broke", TenantId: int64(broke), ApiKeyId: 1,
		Model: "cheap-model", EstPromptTokens: 10,
	})
	if err != nil || resp.Code != billingv1.ReserveCode_RESERVE_INSUFFICIENT {
		t.Fatalf("zero balance: code=%v err=%v", resp.GetCode(), err)
	}
	if n := countReserveRows(s); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}

	okTenant := seedTenant(t, s, 5)
	resp, err = s.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId: "rid-min-ok", TenantId: int64(okTenant), ApiKeyId: 1,
		Model: "cheap-model", EstPromptTokens: 10,
	})
	if err != nil || resp.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("min pre: code=%v err=%v", resp.GetCode(), err)
	}
	var row Bill
	if err := s.db.Where("request_id = ?", "rid-min-ok").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.PreQuota != 1 {
		t.Fatalf("pre = %d, want 1", row.PreQuota)
	}
	if bal, _ := s.getBalance(ctx, okTenant); bal != 4 {
		t.Fatalf("balance = %d, want 4", bal)
	}

	freeTenant := seedTenant(t, s, 0)
	resp, err = s.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId: "rid-free", TenantId: int64(freeTenant), ApiKeyId: 1,
		Model: "free-model", EstPromptTokens: 10,
	})
	if err != nil || resp.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("free model: code=%v err=%v", resp.GetCode(), err)
	}
	var freeRow Bill
	if err := s.db.Where("request_id = ?", "rid-free").First(&freeRow).Error; err != nil {
		t.Fatal(err)
	}
	if freeRow.PreQuota != 0 {
		t.Fatalf("free pre = %d, want 0", freeRow.PreQuota)
	}
}

// 补扣会超过剩余余额时，扣到 0 并结清，不把账单留在 pending。
// 账本 actual 等于实际收走的金额，Redis 与 LedgerBalance 一致，重复结算不二次扣。
func TestSettleShortfallDoesNotStickPending(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 100)
	req := reserveReq("rid-short", tenantID, 84) // pre=100，扣完余额为 0
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{
		RequestId: req.RequestId, PromptTokens: 50, CompletionTokens: 100, // actual=150
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Settled || resp.DeltaQuota != 0 || resp.BalanceAfter != 0 {
		t.Fatalf("settle = settled:%v delta:%d bal:%d", resp.Settled, resp.DeltaQuota, resp.BalanceAfter)
	}
	assertReserveStatus(t, s, req.RequestId, StatusSettled)

	var settle Bill
	if err := s.db.Where("request_id = ? AND phase = ?", req.RequestId, PhaseSettle).First(&settle).Error; err != nil {
		t.Fatal(err)
	}
	if settle.ActualQuota == nil || *settle.ActualQuota != 100 || settle.ErrorCode != "shortfall:50" {
		t.Fatalf("settle row actual=%v code=%q, want actual 100 shortfall:50", settle.ActualQuota, settle.ErrorCode)
	}

	again, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
	if err != nil || !again.Settled {
		t.Fatalf("replay settle: %+v err=%v", again, err)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 0 {
		t.Fatalf("balance = %d, want 0", bal)
	}
	ledger, err := LedgerBalance(ctx, s.db, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if ledger != 0 {
		t.Fatalf("ledger = %d, want 0 (redis 0)", ledger)
	}
}
