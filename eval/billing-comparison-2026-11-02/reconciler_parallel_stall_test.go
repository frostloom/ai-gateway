package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestParallelBatchBlockedAndDuplicateRealLedger(t *testing.T) {
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
	done := make(chan []batchResult, 1)
	go func() {
		done <- parallelSettleBatch(batchCtx, pb.NewBillingServiceClient(conn), []*pb.SettleRequest{reqs[0], reqs[1], reqs[0]}, 3)
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("first request not blocked")
	}
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("independent request did not finish while first blocked")
	}
	close(release)
	var results []batchResult
	select {
	case results = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not finish")
	}
	for _, r := range results {
		if r.err != nil || !r.settled {
			t.Fatalf("result=%+v", r)
		}
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
	t.Log("same account: second bill completed before first released, duplicate first settled once, ledger=balance=920")
}
