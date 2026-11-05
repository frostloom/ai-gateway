package comparison

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Diagnostic only: run against a disposable, empty database and Redis instance.
func TestReserveConflictingTenant(t *testing.T) {
	dsn, addr := os.Getenv("COMPARISON_MYSQL_DSN"), os.Getenv("COMPARISON_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("requires explicitly supplied disposable services")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	s := store.NewStore(db, rdb)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: "comparison-mock", Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	a := store.Tenant{Name: "comparison-a", InitialQuota: 1000}
	b := store.Tenant{Name: "comparison-b", InitialQuota: 1000}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tenant := range []store.Tenant{a, b} {
		if err := rdb.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	req := &billingv1.ReserveRequest{RequestId: "comparison-conflict", TenantId: int64(a.ID), ApiKeyId: 1, Model: model.ModelID, EstPromptTokens: 84}
	first, err := s.Reserve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Reserve(ctx, req)
	if err != nil || replay.BillId != first.BillId {
		t.Fatalf("same request replay: %+v %v", replay, err)
	}
	req.TenantId = int64(b.ID)
	req.EstPromptTokens = 168
	second, err := s.Reserve(ctx, req)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflict should fail: response=%+v error=%v", second, err)
	}
	conflictCode := status.Code(err)
	var bill store.Bill
	if err := db.Where("request_id = ? AND phase = ?", req.RequestId, store.PhaseReserve).First(&bill).Error; err != nil {
		t.Fatal(err)
	}
	balA, err := rdb.Get(ctx, store.BalanceKey(a.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	balB, err := rdb.Get(ctx, store.BalanceKey(b.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first code=%s bill=%d; conflicting second status=%s; stored tenant=%d pre=%d; balances=%d,%d", first.Code, first.BillId, conflictCode, bill.TenantID, bill.PreQuota, balA, balB)
	if first.Code != billingv1.ReserveCode_RESERVE_OK || bill.TenantID != a.ID || balA != 900 || balB != 1000 {
		t.Fatal("observed behavior changed; investigate")
	}
	// Historical rows cannot prove their original parameters and must fail closed.
	if err := db.Model(&bill).Update("reserve_fingerprint", nil).Error; err != nil {
		t.Fatal(err)
	}
	req.TenantId = int64(a.ID)
	req.EstPromptTokens = 84
	if _, err := s.Reserve(ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("legacy replay: %v", err)
	}

	// Competing tenants using the same new ID must produce exactly one reservation.
	var wg sync.WaitGroup
	type result struct {
		response *billingv1.ReserveResponse
		err      error
	}
	results := make([]result, 2)
	for i, tenant := range []store.Tenant{a, b} {
		wg.Add(1)
		go func(i int, tenant store.Tenant) {
			defer wg.Done()
			results[i].response, results[i].err = s.Reserve(ctx, &billingv1.ReserveRequest{RequestId: "comparison-race", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: model.ModelID, EstPromptTokens: 84})
		}(i, tenant)
	}
	wg.Wait()
	successes, conflicts := 0, 0
	for _, result := range results {
		if result.err == nil && result.response.Code == billingv1.ReserveCode_RESERVE_OK {
			successes++
		} else if status.Code(result.err) == codes.FailedPrecondition {
			conflicts++
		} else {
			t.Fatalf("race result: %+v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("race success=%d conflicts=%d", successes, conflicts)
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id = ? AND phase = ?", "comparison-race", store.PhaseReserve).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("race bills=%d error=%v", count, err)
	}
	balA, _ = rdb.Get(ctx, store.BalanceKey(a.ID)).Int64()
	balB, _ = rdb.Get(ctx, store.BalanceKey(b.ID)).Int64()
	if balA+balB != 1800 {
		t.Fatalf("race balances: %d, %d", balA, balB)
	}

}
