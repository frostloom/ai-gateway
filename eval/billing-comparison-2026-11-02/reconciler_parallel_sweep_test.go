package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestParallelSweepProgressWithBlockedFirst(t *testing.T) {
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("parallel-stall-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	reqs := make([]*pb.SettleRequest, 2)
	for i := range reqs {
		id := fmt.Sprintf("%s-%d", tag, i)
		res, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
		if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("reserve=%v err=%v", res, err)
		}
		if err := st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, BillId: res.BillId, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
		reqs[i] = &pb.SettleRequest{RequestId: id, BillId: res.BillId}
		if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	blocked := make(chan struct{})
	release := make(chan struct{})
	second := make(chan struct{})
	var firstOnce, secondOnce atomic.Bool
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if s, ok := req.(*pb.SettleRequest); ok && s.RequestId == reqs[0].RequestId && firstOnce.CompareAndSwap(false, true) {
			close(blocked)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		response, err := handler(ctx, req)
		if s, ok := req.(*pb.SettleRequest); ok && s.RequestId == reqs[1].RequestId && err == nil && secondOnce.CompareAndSwap(false, true) {
			close(second)
		}
		return response, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.ChainUnaryInterceptor(controlplane.UnaryServer("batch-test"), interceptor))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "batch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct {
		stats reconciler.Stats
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		stats, err := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), time.Minute).SweepOnce(batchCtx)
		done <- outcome{stats, err}
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("first request not blocked")
	}
	progressed := false
	select {
	case <-second:
		progressed = true
	case <-time.After(3 * time.Second):
	}
	close(release)
	var result outcome
	select {
	case result = <-done:
	case <-time.After(8 * time.Second):
		cancel()
		t.Fatal("sweep did not finish")
	}
	if !progressed {
		t.Fatal("second bill blocked behind first")
	}
	if result.err != nil || result.stats.Settled != 2 || result.stats.SettleErrors != 0 {
		t.Fatalf("stats=%+v err=%v", result.stats, result.err)
	}

	for _, req := range reqs {
		var n int64
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", req.RequestId, store.PhaseSettle).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("settle rows %d %v", n, err)
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", req.RequestId+":settle").Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("outbox %d %v", n, err)
		}
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != live || live != 920 {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Log("same account: second bill completed before first released, both settled once, ledger=balance=920")
}
