package comparison

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Test-only filtered discovery, deliberately page size one to force a cursor boundary.
// It is not the production scanner and does not provide its metrics or legacy mode.
func filteredSweepTest(ctx context.Context, db *gorm.DB, s *store.Store, afterPage func(), failID string) (int, error) {
	cutoff := time.Now().Add(-time.Minute)
	var upper uint64
	base := func() *gorm.DB {
		return db.WithContext(ctx).Model(&store.Bill{}).Where("phase=? AND status=? AND created_at<?", store.PhaseReserve, store.StatusPending, cutoff)
	}
	if err := base().Select("COALESCE(MAX(id),0)").Scan(&upper).Error; err != nil {
		return 0, err
	}
	var after uint64
	settled := 0
	for after < upper {
		var page []store.Bill
		if err := base().Where("usage_reported_at IS NOT NULL AND id>? AND id<=?", after, upper).Order("id ASC").Limit(1).Find(&page).Error; err != nil {
			return settled, err
		}
		if len(page) == 0 {
			break
		}
		after = page[0].ID
		if afterPage != nil {
			afterPage()
			afterPage = nil
		}
		if page[0].RequestID == failID {
			continue
		} // injected failed RPC before it reaches Store
		resp, err := s.Settle(ctx, &pb.SettleRequest{RequestId: page[0].RequestID, BillId: int64(page[0].ID)})
		if err != nil {
			return settled, err
		}
		if resp.Settled {
			settled++
		}
	}
	return settled, nil
}

func TestFilteredSweepUsageAfterCursorAndRetry(t *testing.T) {
	if os.Getenv("TEST_FILTERED_SWEEP") != "1" {
		t.Skip("isolated only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("filter-life-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	ids := []string{id + "-a", id + "-b", id + "-c"}
	for _, rid := range ids {
		res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
		if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(res, err)
		}
		if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, rid := range ids[1:] {
		if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: rid, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
	}
	var hookErr error
	first, err := filteredSweepTest(ctx, db, s, func() { hookErr = s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: ids[0], PromptTokens: 40}) }, ids[1])
	if err != nil || hookErr != nil || first != 1 {
		t.Fatalf("first=%d err=%v hook=%v", first, err, hookErr)
	}
	second, err := filteredSweepTest(ctx, db, s, nil, "")
	if err != nil || second != 2 {
		t.Fatalf("second=%d err=%v", second, err)
	}
	third, err := filteredSweepTest(ctx, db, s, nil, "")
	if err != nil || third != 0 {
		t.Fatalf("third=%d err=%v", third, err)
	}
	for _, rid := range ids {
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
			t.Fatalf("bill=%+v err=%v", bill, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var n int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if phase == "reverse" {
				want = 0
			}
			if n != want {
				t.Fatalf("%s %s=%d", rid, phase, n)
			}
		}
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 880 || ledger != live {
		t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("first=%d second=%d third=%d late_usage_deferred=true failed_rpc_retried=true balance=%d ledger=%d", first, second, third, live, ledger)
}

func TestFilteredSweepConcurrentWorkers(t *testing.T) {
	if os.Getenv("TEST_FILTERED_SWEEP") != "1" {
		t.Skip("isolated only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("filter-workers-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(res, err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	type result struct {
		n   int
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			n, err := filteredSweepTest(ctx, db, s, func() { ready <- struct{}{}; <-release }, "")
			results <- result{n, err}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatal("workers did not both discover pending row")
		}
	}
	close(release)
	total := 0
	for i := 0; i < 2; i++ {
		v := <-results
		if v.err != nil {
			t.Fatal(v.err)
		}
		total += v.n
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
		t.Fatalf("bill=%+v err=%v", bill, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		want := int64(1)
		if phase == "reverse" {
			want = 0
		}
		if n != want {
			t.Fatalf("event %s=%d", phase, n)
		}
	}
	var settleRows int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 1 {
		t.Fatalf("settle rows=%d err=%v", settleRows, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != live {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("two_workers_discovered_one_bill=true reported_settled_sum=%d unique_settle_rows=%d balance=%d ledger=%d", total, settleRows, live, ledger)
}
