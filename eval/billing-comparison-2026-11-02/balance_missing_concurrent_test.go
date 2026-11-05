package comparison

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

func TestBalanceMissingConcurrentWithReserve(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("balance-missing-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	first, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id + "-first", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	if err := r.Del(ctx, store.BalanceKey(tenant.ID)).Err(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 21)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			b, err := s.GetBalance(ctx, tenant.ID)
			if err != nil || (b != 900 && b != 800) {
				errs <- fmt.Errorf("read balance=%d err=%v", b, err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		resp, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id + "-second", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
		if err != nil || resp.Code != pb.ReserveCode_RESERVE_OK {
			errs <- fmt.Errorf("reserve=%v err=%v", resp, err)
		}
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 800 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("readers=20 concurrent_reserve=1 final_balance=%d ledger=%d", live, ledger)
}
