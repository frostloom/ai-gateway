package comparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestReconcilerCancellationStopsPageAndReportsError(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Tenant{}, &store.Bill{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("cancel-page-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&tenant)
	defer db.Where("tenant_id=?", tenant.ID).Delete(&store.Bill{})
	usage := time.Now().Add(-time.Hour)
	for i := 0; i < 2; i++ {
		bill := store.Bill{RequestID: fmt.Sprintf("%s-%d", tag, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance, CreatedAt: usage, UsageReportedAt: &usage}
		if err := db.Create(&bill).Error; err != nil {
			t.Fatal(err)
		}
	}
	fake := &controlledStallBilling{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := reconciler.New(db, fake, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
		done <- err
	}()
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first RPC did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || fake.calls.Load() != 1 {
			t.Fatalf("err=%v calls=%d", err, fake.calls.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled sweep did not return")
	}
}

type controlledStallBilling struct {
	pb.BillingServiceClient
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (b *controlledStallBilling) Settle(ctx context.Context, _ *pb.SettleRequest, _ ...grpc.CallOption) (*pb.SettleResponse, error) {
	if b.calls.Add(1) == 1 {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &pb.SettleResponse{Settled: true}, nil
}
func TestReconcilerBlockedRPCDelaysLaterBill(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Tenant{}, &store.Bill{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("stalled-rpc-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&tenant)
	defer db.Where("tenant_id=?", tenant.ID).Delete(&store.Bill{})
	usage := time.Now().Add(-time.Hour)
	for i := 0; i < 2; i++ {
		bill := store.Bill{RequestID: fmt.Sprintf("%s-%d", tag, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance, CreatedAt: usage, UsageReportedAt: &usage}
		if err := db.Create(&bill).Error; err != nil {
			t.Fatal(err)
		}
	}
	fake := &controlledStallBilling{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := reconciler.New(db, fake, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
		done <- err
	}()
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first RPC did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("sweep unexpectedly returned: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if calls := fake.calls.Load(); calls != 1 {
		t.Fatalf("expected later RPC to wait, got %d calls", calls)
	}
	close(fake.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not resume")
	}
	if calls := fake.calls.Load(); calls != 2 {
		t.Fatalf("expected second RPC after release, got %d", calls)
	}
	t.Log("blocked first mock RPC delayed the second until release; both responses are synthetic")
}

func TestReconcilerStalledSettleTimesOutAndContinues(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Tenant{}, &store.Bill{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("bounded-settle-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&tenant)
	defer db.Where("tenant_id=?", tenant.ID).Delete(&store.Bill{})
	usage := time.Now().Add(-time.Hour)
	for i := 0; i < 2; i++ {
		bill := store.Bill{RequestID: fmt.Sprintf("%s-%d", tag, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance, CreatedAt: usage, UsageReportedAt: &usage}
		if err := db.Create(&bill).Error; err != nil {
			t.Fatal(err)
		}
	}
	fake := &controlledStallBilling{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		stats reconciler.Stats
		err   error
	}
	done := make(chan result, 1)
	go func() {
		stats, err := reconciler.New(db, fake, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
		done <- result{stats, err}
	}()
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first RPC did not start")
	}
	select {
	case got := <-done:
		if got.err != nil || got.stats.SettleErrors != 1 || got.stats.Settled != 1 || fake.calls.Load() != 2 {
			t.Fatalf("stats=%+v err=%v calls=%d", got.stats, got.err, fake.calls.Load())
		}
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("first RPC blocked later bill beyond 12 seconds")
	}
}
