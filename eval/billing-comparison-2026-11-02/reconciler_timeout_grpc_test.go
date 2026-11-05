package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestReconcilerGRPCTimeoutAfterCommitDoesNotDoubleCharge(t *testing.T) {
	db, redis := isolatedDB(t)
	s := store.NewStore(db, redis)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("timeout-commit-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{tag + "-1", tag + "-2"}
	for _, id := range ids {
		res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
		if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("reserve=%v err=%v", res, err)
		}
		if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		if settle, ok := req.(*pb.SettleRequest); ok && settle.RequestId == ids[0] && err == nil {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return response, err
	}
	rpc := grpc.NewServer(grpc.ChainUnaryInterceptor(controlplane.UnaryServer("synthetic-token"), interceptor))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	runner := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), time.Minute)
	cancelCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	stats, err := runner.SweepOnce(cancelCtx)
	if err != nil || stats.SettleErrors != 1 || stats.Settled != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	for _, id := range ids {
		if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("id=%s settle rows=%d err=%v", id, n, err)
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":settle").Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("id=%s settle events=%d err=%v", id, n, err)
		}
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != live || live != 920 {
		t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("loopback gRPC: first commit succeeded before artificial delayed response; two bills settled once balance=%d", live)
}
