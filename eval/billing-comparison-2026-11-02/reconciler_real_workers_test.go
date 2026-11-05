package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

type synchronizedSettlement struct {
	directSweepStore
	ready   chan<- struct{}
	release <-chan struct{}
}

func (b synchronizedSettlement) Settle(ctx context.Context, req *pb.SettleRequest, opts ...grpc.CallOption) (*pb.SettleResponse, error) {
	b.ready <- struct{}{}
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.directSweepStore.Settle(ctx, req, opts...)
}

func TestRealReconcilerConcurrentSettledCount(t *testing.T) {
	if os.Getenv("TEST_REAL_RECON_WORKERS") != "1" {
		t.Skip("isolated only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("real-recon-workers-%d", time.Now().UnixNano())
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
	cli := synchronizedSettlement{directSweepStore{s: s}, ready, release}
	type result struct {
		stats reconciler.Stats
		err   error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			stats, err := reconciler.New(db, cli, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
			results <- result{stats, err}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-ctx.Done():
			close(release)
			t.Fatal("workers did not reach settlement", ctx.Err())
		}
	}
	close(release)
	sum := 0
	for i := 0; i < 2; i++ {
		v := <-results
		if v.err != nil {
			t.Fatal(v.err)
		}
		sum += v.stats.Settled
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
			t.Fatalf("%s=%d", phase, n)
		}
	}
	var settleRows int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 1 {
		t.Fatalf("settles=%d err=%v", settleRows, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != live || sum != 2 {
		t.Fatalf("sum=%d balance=%d ledger=%d err=%v", sum, live, ledger, err)
	}
	t.Logf("real_sweepers=2 settled_stats_sum=%d unique_settlements=%d balance=%d ledger=%d", sum, settleRows, live, ledger)
}
