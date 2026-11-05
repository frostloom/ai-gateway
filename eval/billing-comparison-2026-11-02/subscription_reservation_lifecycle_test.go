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

func TestSubscriptionReservedQuotaLifecycle(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("sub-reservation-lifecycle-%d", time.Now().UnixNano())
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
	pb.RegisterBillingServiceServer(rpc, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	ctxA := controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	targetModel := id
	reserve := func(suffix string, amount int64) (*pb.ReserveResponse, error) {
		return billing.Reserve(ctxA, &pb.ReserveRequest{RequestId: id + suffix, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: targetModel, EstPromptTokens: amount})
	}
	first, err := reserve("-a", 200)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	other := store.Model{Vendor: "test", ModelID: id + "-other", Name: id + "-other", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `["文本"]`}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Model{}).Where("id=?", model.ID).Update("tags", `["旗舰"]`).Error; err != nil {
		t.Fatal(err)
	}
	targetModel = other.ModelID
	reject, err := reserve("-b", 150)
	if err != nil || reject.Code != pb.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatal(reject, err)
	}
	if _, err := billing.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", PromptTokens: 50}); err != nil {
		t.Fatal(err)
	}
	b, err := reserve("-b", 150)
	if err != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, err)
	}
	reversed, err := billing.Reverse(ctx, &pb.ReverseRequest{RequestId: id + "-b", BillId: b.BillId})
	if err != nil || !reversed.Reversed {
		t.Fatal(reversed, err)
	}
	c, err := reserve("-c", 250)
	if err != nil || c.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(c, err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", c.BillId).UpdateColumn("created_at", time.Now().Add(-6*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	aged, err := reserve("-aged", 1)
	if err != nil || aged.Code != pb.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatalf("unresolved reservation outside rolling window must remain held: %v %v", aged, err)
	}
	// Emulate an old unresolved row: its original estimate cannot be reconstructed.
	if err := db.Model(&store.Bill{}).Where("id=?", c.BillId).Updates(map[string]any{"reserved_tokens": nil, "reserved_tier": nil}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = reserve("-d", 1)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("legacy pending should fail closed: %v", err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger {
		t.Fatal(live, ledger, err)
	}
	t.Logf("held=200 reject=150 actual=50 then allow=150 reverse releases=true allow=250 legacy=unavailable balance=%d ledger=%d", live, ledger)
}
