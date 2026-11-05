package comparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

type sweepCancelBilling struct {
	pb.BillingServiceClient
	started chan struct{}
	once    sync.Once
	calls   atomic.Int32
	active  atomic.Int32
	peak    atomic.Int32
}

func (b *sweepCancelBilling) Settle(ctx context.Context, _ *pb.SettleRequest, _ ...grpc.CallOption) (*pb.SettleResponse, error) {
	now := b.active.Add(1)
	defer b.active.Add(-1)
	for {
		old := b.peak.Load()
		if now <= old || b.peak.CompareAndSwap(old, now) {
			break
		}
	}
	if b.calls.Add(1) == 4 {
		b.once.Do(func() { close(b.started) })
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestParallelSweepCancelJoinsWorkers(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Tenant{}, &store.Bill{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("parallel-cancel-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&tenant)
	defer db.Where("tenant_id=?", tenant.ID).Delete(&store.Bill{})
	usage := time.Now().Add(-time.Hour)
	for i := 0; i < 10; i++ {
		row := store.Bill{RequestID: fmt.Sprintf("%s-%d", tag, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance, CreatedAt: usage, UsageReportedAt: &usage}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	fake := &sweepCancelBilling{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		stats reconciler.Stats
		err   error
	}
	done := make(chan result, 1)
	go func() {
		s, e := reconciler.New(db, fake, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
		done <- result{s, e}
	}()
	select {
	case <-fake.started:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("four workers did not reach RPC")
	}
	cancel()
	var got result
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("workers not joined after cancel")
	}
	if !errors.Is(got.err, context.Canceled) || fake.active.Load() != 0 || fake.calls.Load() != 4 || fake.peak.Load() != 4 || got.stats.SettleErrors != 4 || got.stats.Settled != 0 {
		t.Fatalf("result=%+v calls=%d active=%d peak=%d", got, fake.calls.Load(), fake.active.Load(), fake.peak.Load())
	}
	var pending int64
	if err := db.Model(&store.Bill{}).Where("tenant_id=? AND status=?", tenant.ID, store.StatusPending).Count(&pending).Error; err != nil || pending != 10 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	t.Log("four in-flight calls canceled and joined; queued six not sent; ten synthetic records remain pending")
}
