package comparison

import (
	"context"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestRechargeAndRefundAmountBounds(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("amount-bounds-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 99999999999949}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	t.Run("recharge multiplication", func(t *testing.T) {
		order, err := s.CreateRechargeOrder(ctx, tenant.ID, math.MaxInt64, id+"-huge")
		if err == nil {
			t.Errorf("overflowing order created: %+v", order)
		}
	})
	t.Run("recharge pushes balance over safe maximum", func(t *testing.T) {
		order, err := s.CreateRechargeOrder(ctx, tenant.ID, 1, id+"-normal")
		if err != nil {
			t.Fatal(err)
		}
		paid, err := s.PayRecharge(ctx, order.OrderNo)
		if err == nil {
			t.Errorf("unsafe payment accepted: %+v", paid)
		}
		var row store.RechargeOrder
		if err := db.First(&row, order.ID).Error; err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := db.Model(&store.Topup{}).Where("ref_type=? AND ref_id=?", store.TopupRecharge, order.OrderNo).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != store.RechargePending || n != 0 || live != tenant.InitialQuota {
			t.Errorf("status=%s topups=%d balance=%d", row.Status, n, live)
		}
	})
	t.Run("refund multiplication", func(t *testing.T) {
		result, err := s.RefundAmount(ctx, tenant.ID, 184467440737095517, id+"-refund")
		if err == nil {
			t.Errorf("overflowing refund accepted: %+v", result)
		}
		var n int64
		if err := db.Model(&store.Topup{}).Where("ref_type=? AND ref_id=?", store.TopupRefund, id+"-refund").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("unexpected refund rows=%d", n)
		}
	})
}

func TestConcurrentRechargeRespectsCashLimit(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("amount-race-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: quota.MaxCashLua - 100}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	orders := make([]*store.RechargeOrder, 2)
	for i := range orders {
		o, err := s.CreateRechargeOrder(ctx, tenant.ID, 1, fmt.Sprintf("%s-%d", id, i))
		if err != nil {
			t.Fatal(err)
		}
		orders[i] = o
	}
	start := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for i := range orders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if _, err := s.PayRecharge(ctx, orders[i].OrderNo); err == nil {
				results <- i
			} else {
				results <- -1
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winner := -1
	count := 0
	for i := range results {
		if i >= 0 {
			winner = i
			count++
		}
	}
	if count != 1 {
		t.Fatalf("success=%d", count)
	}
	if _, err := s.PayRecharge(ctx, orders[winner].OrderNo); err != nil {
		t.Fatal(err)
	}
	var paid, pending, topups int64
	if err := db.Model(&store.RechargeOrder{}).Where("tenant_id=? AND status=?", tenant.ID, store.RechargePaid).Count(&paid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.RechargeOrder{}).Where("tenant_id=? AND status=?", tenant.ID, store.RechargePending).Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Topup{}).Where("tenant_id=? AND source=?", tenant.ID, store.TopupRecharge).Count(&topups).Error; err != nil {
		t.Fatal(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != quota.MaxCashLua || ledger != live || paid != 1 || pending != 1 || topups != 1 {
		t.Fatal(live, ledger, paid, pending, topups, err)
	}
	t.Logf("concurrent=2 paid=1 pending=1 topups=1 retry=noop balance=%d ledger=%d", live, ledger)
}
