package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExecutionCandidateLegacyCallsRejected(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires candidate overlay")
	}
	db, r := isolatedDB(t)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("legacy-candidate-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(res, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("usage err=%v", err)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reverse err=%v", err)
	}
	var row store.Bill
	if err := db.First(&row, res.BillId).Error; err != nil || row.Status != store.StatusPending || row.UsageReportedAt != nil {
		t.Fatalf("bill=%+v err=%v", row, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":reverse"}).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("legacy_reserve=true usage=failed_precondition reverse=failed_precondition pending=true events=%d balance=%d ledger=%d", events, live, ledger)
}

func TestExecutionStrictCurrentGatewayRPCRejected(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("strict overlay only")
	}
	db, r := isolatedDB(t)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("strict-rpc-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	pb.RegisterBillingServiceServer(srv, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = srv.Serve(listener) }()
	defer srv.Stop()
	client, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx = controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	_, err = client.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: "unused", EstPromptTokens: 100})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("gateway v2 reserve err=%v", err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || bills != 0 || events != 0 || live != 1000 || ledger != 1000 {
		t.Fatalf("bills=%d events=%d live=%d ledger=%d err=%v", bills, events, live, ledger, err)
	}
	t.Logf("gateway_v2_rpc=failed_precondition bills=%d events=%d balance=%d ledger=%d", bills, events, live, ledger)
}

// In strict mode reject old callers before they can reserve funds.
func TestExecutionStrictRejectsLegacyBeforeReserve(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("strict overlay only")
	}
	db, r := isolatedDB(t)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("strict-legacy-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	_, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected precondition before reserve, got %v", err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || bills != 0 || events != 0 || live != 1000 || ledger != 1000 {
		t.Fatalf("bills=%d events=%d balance=%d ledger=%d err=%v", bills, events, live, ledger, err)
	}
	t.Logf("legacy_rejected=true bills=%d events=%d balance=%d ledger=%d", bills, events, live, ledger)
}
