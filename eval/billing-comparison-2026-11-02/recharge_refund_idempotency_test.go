package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestRechargeRefundIdempotencyConflicts(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("order-idem-%d", time.Now().UnixNano())
	a := store.Tenant{Name: id + "-a", InitialQuota: 1000}
	b := store.Tenant{Name: id + "-b", InitialQuota: 1000}
	for _, tenant := range []*store.Tenant{&a, &b} {
		if err := db.Create(tenant).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	order, err := s.CreateRechargeOrder(ctx, a.ID, 1, id+"-order")
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.CreateRechargeOrder(ctx, a.ID, 1, id+"-order")
	if err != nil || same.ID != order.ID {
		t.Fatal(same, err)
	}
	if o, err := s.CreateRechargeOrder(ctx, b.ID, 1, id+"-order"); err == nil {
		t.Errorf("cross-tenant order unexpectedly returned %+v", o)
	}
	if o, err := s.CreateRechargeOrder(ctx, a.ID, 2, id+"-order"); err == nil {
		t.Errorf("different order amount unexpectedly returned %+v", o)
	}
	first, err := s.RefundAmount(ctx, a.ID, 1, id+"-refund")
	if err != nil || first.RefundCent != 100 {
		t.Fatal(first, err)
	}
	sameRefund, err := s.RefundAmount(ctx, a.ID, 1, id+"-refund")
	if err != nil || sameRefund.RefundCent != 0 {
		t.Fatal(sameRefund, err)
	}
	if result, err := s.RefundAmount(ctx, a.ID, 2, id+"-refund"); err == nil {
		t.Errorf("different refund amount unexpectedly returned %+v", result)
	}
	if result, err := s.RefundAmount(ctx, b.ID, 1, id+"-refund"); err == nil {
		t.Errorf("cross-tenant refund unexpectedly returned %+v", result)
	}
	for _, tenant := range []store.Tenant{a, b} {
		balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || balance != ledger {
			t.Fatal(balance, ledger, err)
		}
	}
}

func TestRefundIdempotencyPartialAndLegacy(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("refund-partial-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 150}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 150, 0).Err(); err != nil {
		t.Fatal(err)
	}
	first, err := s.RefundAmount(ctx, tenant.ID, 2, id+"-partial")
	if err != nil || first.RefundCent != 150 || first.AmountCent != 200 {
		t.Fatal(first, err)
	}
	repeat, err := s.RefundAmount(ctx, tenant.ID, 2, id+"-partial")
	if err != nil || repeat.RefundCent != 0 || repeat.AmountCent != 200 {
		t.Fatal(repeat, err)
	}
	if response, err := s.RefundAmount(ctx, tenant.ID, 1, id+"-partial"); err == nil {
		t.Fatal("different requested amount accepted", response)
	}
	var topup store.Topup
	if err := db.Where("ref_type=? AND ref_id=?", store.TopupRefund, id+"-partial").First(&topup).Error; err != nil || topup.RequestedCent == nil || *topup.RequestedCent != 200 || topup.Amount != -150 {
		t.Fatal(topup, err)
	}
	if err := db.Model(&store.Topup{}).Where("id=?", topup.ID).Update("amount", -201).Error; err != nil {
		t.Fatal(err)
	}
	if response, err := s.RefundAmount(ctx, tenant.ID, 2, id+"-partial"); err == nil {
		t.Fatal("invalid historical amount accepted", response)
	}
	if err := db.Model(&store.Topup{}).Where("id=?", topup.ID).Update("amount", -150).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Topup{}).Where("id=?", topup.ID).Update("requested_cent", nil).Error; err != nil {
		t.Fatal(err)
	}
	if response, err := s.RefundAmount(ctx, tenant.ID, 2, id+"-partial"); err == nil {
		t.Fatal("unverifiable historical refund accepted", response)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 0 || ledger != 0 {
		t.Fatal(live, ledger, err)
	}
	t.Log("requested=200 refunded=150 matching replay no-op; conflict and legacy unknown rejected")
}

func TestConcurrentCrossTenantRefundKey(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("refund-cross-%d", time.Now().UnixNano())
	tenants := []store.Tenant{{Name: id + "-a", InitialQuota: 1000}, {Name: id + "-b", InitialQuota: 1000}}
	for i := range tenants {
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range tenants {
		go func(i int) {
			<-start
			response, err := s.RefundAmount(ctx, tenants[i].ID, 1, id)
			if err == nil && (response == nil || response.RefundCent != 100) {
				results <- fmt.Errorf("bad response %v", response)
				return
			}
			results <- err
		}(i)
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("success=%d", success)
	}
	var rows []store.Topup
	if err := db.Where("ref_type=? AND ref_id=?", store.TopupRefund, id).Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	for _, tenant := range tenants {
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != ledger {
			t.Fatal(live, ledger, err)
		}
		want := int64(1000)
		if rows[0].TenantID == tenant.ID {
			want = 900
		}
		if live != want {
			t.Fatal(live, want)
		}
	}
	t.Log("cross-tenant concurrent same key: one refund, one conflict, both balances reconciled")
}
