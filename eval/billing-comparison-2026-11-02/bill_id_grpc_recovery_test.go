package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestBillIDMismatchGRPCRecovery(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("bill-id-grpc-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("synthetic-token")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	t.Cleanup(rpc.Stop)
	client, err := gateway.NewBilling(lis.Addr().String(), "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	reserve := func(suffix string) *pb.ReserveResponse {
		res, e := client.Reserve(ctx, &pb.ReserveRequest{RequestId: id + suffix, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
		if e != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("reserve: %+v %v", res, e)
		}
		return res
	}
	a, b := reserve("-a"), reserve("-b")
	if _, err = client.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", BillId: b.BillId, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bad usage: %v", err)
	}
	if _, err = client.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", BillId: a.BillId, PromptTokens: 40}); err != nil {
		t.Fatalf("usage: %v", err)
	}
	if _, err = client.Settle(ctx, &pb.SettleRequest{RequestId: id + "-a", BillId: b.BillId}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bad settle: %v", err)
	}
	if _, err = client.Reverse(ctx, &pb.ReverseRequest{RequestId: id + "-b", BillId: a.BillId}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bad reverse: %v", err)
	}
	if err = db.Model(&store.Bill{}).Where("id=?", a.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	// Reconciler uses the real client and the bill ID obtained from its scan.
	conn, err := controlplane.Dial(lis.Addr().String(), "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	stats, err := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), 10*time.Minute).SweepOnce(ctx)
	if err != nil || stats.Settled != 1 {
		t.Fatalf("recovery stats=%+v err=%v", stats, err)
	}
	if resp, err := client.Reverse(ctx, &pb.ReverseRequest{RequestId: id + "-b", BillId: b.BillId}); err != nil || !resp.Reversed {
		t.Fatalf("reverse: %+v %v", resp, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != 960 {
		t.Fatalf("balances live=%d ledger=%d err=%v", live, ledger, err)
	}
	for _, key := range []string{id + "-a:reserve", id + "-a:usage", id + "-a:settle", id + "-b:reserve", id + "-b:reverse"} {
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", key).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("outbox %s=%d err=%v", key, n, err)
		}
	}
	for _, key := range []string{id + "-a:reverse", id + "-b:usage", id + "-b:settle"} {
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", key).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("unexpected outbox %s=%d err=%v", key, n, err)
		}
	}
	t.Logf("RPC mismatches rejected; reconciler settled A, B reversed; balances=%d", live)
}
