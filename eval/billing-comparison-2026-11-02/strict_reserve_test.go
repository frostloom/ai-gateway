package comparison

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm/logger"
)

// Tests the opt-in Store contract only; full gRPC/gateway integration is separate.
func TestStrictReserveReplayContract(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if e := s.AutoMigrate(); e != nil {
		t.Fatal(e)
	}
	id := fmt.Sprintf("strict-%d", time.Now().UnixNano())
	m := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&m).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	req := &billingv1.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	var wg sync.WaitGroup
	results := make(chan billingv1.ReserveCode, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.ReserveWithReplaySignal(ctx, req)
			if e != nil {
				errs <- e
				return
			}
			results <- v.Code
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	counts := map[billingv1.ReserveCode]int{}
	for c := range results {
		counts[c]++
	}
	if counts[billingv1.ReserveCode_RESERVE_OK] != 1 || counts[billingv1.ReserveCode_RESERVE_DUPLICATE] != 7 {
		t.Fatalf("codes=%v", counts)
	}
	changed := proto.Clone(req).(*billingv1.ReserveRequest)
	changed.EstPromptTokens++
	if _, e := s.ReserveWithReplaySignal(ctx, changed); status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("parameter conflict accepted: %v", e)
	}
	legacy, e := s.Reserve(ctx, req)
	if e != nil || legacy.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("legacy semantics: %+v %v", legacy, e)
	}
	var n int64
	if e := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&n).Error; e != nil || n != 1 {
		t.Fatalf("rows=%d err=%v", n, e)
	}
	bal, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil || bal != 900 {
		t.Fatalf("balance=%d err=%v", bal, e)
	}
}
