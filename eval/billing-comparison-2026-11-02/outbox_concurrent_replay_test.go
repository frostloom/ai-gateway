package comparison

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

func TestActualStoreConcurrentSameRequestReplay(t *testing.T) {
	if os.Getenv("TEST_ACTUAL_ORPHAN_OUTBOX") != "1" {
		t.Skip("isolated only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("outbox-concurrent-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	const n = 12
	parallel := func(f func() error) {
		t.Helper()
		start := make(chan struct{})
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; errs <- f() }()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	ids := make(chan int64, n)
	codes := make(chan pb.ReserveCode, n)
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	parallel(func() error {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		res, err := s.ReserveWithReplaySignal(c, req)
		if err != nil {
			return err
		}
		ids <- res.BillId
		codes <- res.Code
		return nil
	})
	close(ids)
	close(codes)
	var billID int64
	for id := range ids {
		if billID == 0 {
			billID = id
		}
		if id != billID || id <= 0 {
			t.Fatalf("different bill %d %d", billID, id)
		}
	}
	ok, dup := 0, 0
	for code := range codes {
		if code == pb.ReserveCode_RESERVE_OK {
			ok++
		} else if code == pb.ReserveCode_RESERVE_DUPLICATE {
			dup++
		} else {
			t.Fatalf("code=%v", code)
		}
	}
	if ok != 1 || dup != n-1 {
		t.Fatalf("ok=%d duplicate=%d", ok, dup)
	}
	parallel(func() error { return s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}) })
	parallel(func() error {
		res, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
		if err != nil {
			return err
		}
		if !res.Settled {
			return fmt.Errorf("not settled")
		}
		return nil
	})
	var bills, settles, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settles).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, e1 := s.GetBalance(ctx, tenant.ID)
	ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
	if bills != 1 || settles != 1 || events != 3 || live != 460 || ledger != 460 || e1 != nil || e2 != nil {
		t.Fatalf("bills=%d settles=%d events=%d live=%d ledger=%d errors=%v/%v", bills, settles, events, live, ledger, e1, e2)
	}
	t.Logf("concurrency=%d reserve_ok=%d duplicate=%d bills=%d settles=%d events=%d balance=%d ledger=%d", n, ok, dup, bills, settles, events, live, ledger)
}
