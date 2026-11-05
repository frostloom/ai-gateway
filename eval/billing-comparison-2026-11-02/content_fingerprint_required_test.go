package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	"gorm.io/gorm/logger"
)

func TestContentFingerprintRequiredMode(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("v2-required-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 100000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	plan := store.Plan{Name: id, PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":300}`}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant.ID, plan.ID); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `["文本"]`}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	pb.RegisterBillingServiceServer(rpc, billingserver.NewService(s, slog.New(slog.DiscardHandler), billingserver.RequireReserveV2()))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}
	ctxA := controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	ctxB := controlplane.WithReserveContent(ctx, strings.Repeat("b", 64))
	_, err = billing.Reserve(ctx, req)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("v1 must fail before reserve: %v", err)
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unexpected bills=%d err=%v", count, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unexpected outbox=%d err=%v", count, err)
	}
	first, err := billing.Reserve(ctxA, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	if _, err := billing.Reserve(ctxB, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	dup, err := billing.Reserve(ctxA, req)
	if err != nil || dup.Code != pb.ReserveCode_RESERVE_DUPLICATE {
		t.Fatal(dup, err)
	}
	var row store.Bill
	if err := db.First(&row, first.BillId).Error; err != nil || row.Funding != store.FundingSubscription {
		t.Fatal(row, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger {
		t.Fatal(live, ledger, err)
	}
	t.Logf("v1_before_write=rejected v2=accepted duplicate=true conflict=true balance=%d ledger=%d", live, ledger)
}
