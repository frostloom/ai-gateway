package store

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBalanceVoucherSingleUseAcrossTenants(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	first, second := seedTenant(t, s, 0), seedTenant(t, s, 0)
	issued, err := s.CreateBalanceVouchers(ctx, 50, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 1 || !strings.HasPrefix(issued[0].Code, "AGV-") {
		t.Fatal("invalid issued code")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	winner := uint64(0)
	for _, tenant := range []uint64{first, second} {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			result, err := s.RedeemBalanceVoucher(ctx, id, issued[0].Code)
			if err == nil && result.AmountCent == 5000 {
				mu.Lock()
				successes++
				winner = id
				mu.Unlock()
			}
		}(tenant)
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successful claims=%d", successes)
	}
	retried, err := s.RedeemBalanceVoucher(ctx, winner, issued[0].Code)
	if err != nil || !retried.AlreadyRedeemed || retried.BalanceCent != 5000 {
		t.Fatalf("retry %+v %v", retried, err)
	}
	var topups int64
	s.db.Model(&Topup{}).Where("ref_type = ?", "voucher").Count(&topups)
	if topups != 1 {
		t.Fatalf("topups=%d", topups)
	}
	rows, err := s.ListBalanceVouchers(ctx)
	if err != nil || len(rows) != 1 || rows[0].RedeemedTenantID == nil {
		t.Fatalf("listing %+v %v", rows, err)
	}
}

func TestBalanceVoucherValidationAndExpiry(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 0)
	for _, args := range [][3]int64{{0, 1, 30}, {1, 0, 30}, {1, 101, 30}, {1, 1, 0}, {1000001, 1, 30}} {
		if _, err := s.CreateBalanceVouchers(ctx, args[0], int(args[1]), int(args[2])); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	issued, err := s.CreateBalanceVouchers(ctx, 10, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&BalanceVoucher{}).Where("id = ?", issued[0].Item.ID).Update("expires_at", time.Now().Add(-time.Minute))
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err == nil {
		t.Fatal("expired code redeemed")
	}
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, "invalid"); err == nil {
		t.Fatal("invalid code redeemed")
	}
	if _, err := s.RedeemBalanceVoucher(ctx, 0, issued[0].Code); err == nil {
		t.Fatal("anonymous redeemed")
	}
}

func TestBalanceVoucherWaitsForPendingBatch(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	ctx := context.Background()
	issued, err := s.CreateBalanceVouchers(ctx, 10, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := s.Reserve(ctx, reserveReq("voucher-batch", tenant, 84))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err == nil {
		t.Fatal("redeemed against pending batch")
	}
	settleCtx := controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 1, Completion: 1})
	if _, err := s.Settle(settleCtx, &billingv1.SettleRequest{RequestId: "voucher-batch", BillId: reserved.BillId}); err != nil {
		t.Fatal(err)
	}

	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.getBalance(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	redeemed, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code)
	if err != nil || redeemed.BalanceCent != before+1000 {
		t.Fatalf("batch credit %+v %v before%d", redeemed, err, before)
	}
}

func TestBalanceVoucherRejectsDisabledAccount(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 0)
	issued, err := s.CreateBalanceVouchers(ctx, 10, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Tenant{}).Where("id = ?", tenant).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err == nil {
		t.Fatal("disabled account redeemed voucher")
	}
	var row BalanceVoucher
	s.db.First(&row, issued[0].Item.ID)
	if row.RedeemedTenantID != nil {
		t.Fatal("rejected voucher was consumed")
	}
}

func TestBalanceVoucherRejectsRestartedBatchProjectionMismatch(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	s.EnableLedgerBatch(time.Hour)
	issued, err := s.CreateBalanceVouchers(ctx, 10, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.rdb.Set(ctx, BalanceKey(tenant), 900, 0).Err(); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"AGV-" + strings.Repeat("0", 40), issued[0].Code} {
		if _, err := s.RedeemBalanceVoucher(ctx, tenant, code); err == nil {
			t.Fatal("redeemed against unresolved restart discrepancy")
		}
		if balance, err := s.getBalance(ctx, tenant); err != nil || balance != 900 {
			t.Fatalf("lagged debit overwritten: %d %v", balance, err)
		}
	}
	var row BalanceVoucher
	if err := s.db.First(&row, issued[0].Item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.RedeemedTenantID != nil {
		t.Fatal("rejected voucher consumed")
	}
}

func TestBalanceVoucherCommittedCreditRetryRejectsAmbiguousProjection(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	s.EnableLedgerBatch(time.Hour)
	issued, err := s.CreateBalanceVouchers(ctx, 10, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err != nil {
		t.Fatal(err)
	}
	// The durable credit could be awaiting Redis synchronization, but a
	// previously applied credit followed by an unflushed debit looks identical.
	if err := s.rdb.Set(ctx, BalanceKey(tenant), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err == nil {
		t.Fatal("ambiguous committed credit retry repaired projection")
	}
	if balance, err := s.getBalance(ctx, tenant); err != nil || balance != 1000 {
		t.Fatalf("ambiguous balance overwritten: %d %v", balance, err)
	}
	var count int64
	if err := s.db.Model(&Topup{}).Where("ref_type = ? AND ref_id = ?", "voucher", issued[0].Item.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate credit: %d %v", count, err)
	}
}

func TestBalanceVoucherOldCodeCannotRefundRestartedBatchDebit(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	issued, err := s.CreateBalanceVouchers(ctx, 10, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err != nil {
		t.Fatal(err)
	}
	// The voucher was fully synchronized before this separate spend. Simulate
	// its Redis debit surviving a process restart while its SQL batch is lost.
	if err := s.rdb.DecrBy(ctx, BalanceKey(tenant), 1000).Err(); err != nil {
		t.Fatal(err)
	}
	s.EnableLedgerBatch(time.Hour)
	if _, err := s.RedeemBalanceVoucher(ctx, tenant, issued[0].Code); err == nil {
		t.Fatal("old voucher restored an unrelated debit")
	}
	if balance, err := s.getBalance(ctx, tenant); err != nil || balance != 1000 {
		t.Fatalf("unflushed spend refunded: %d %v", balance, err)
	}
	ledger, err := LedgerBalance(ctx, s.db, tenant)
	if err != nil || ledger != 2000 {
		t.Fatalf("durable credit altered: %d %v", ledger, err)
	}
	var count int64
	if err := s.db.Model(&Topup{}).Where("ref_type = ? AND ref_id = ?", "voucher", issued[0].Item.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate credit: %d %v", count, err)
	}
}
