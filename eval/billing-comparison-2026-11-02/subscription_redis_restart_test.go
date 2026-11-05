package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

func TestSubscriptionReservationAfterRedisRestart(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("sub-redis-restart-%d", time.Now().UnixNano())
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
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}
	ctxA := controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	ctxB := controlplane.WithReserveContent(ctx, strings.Repeat("b", 64))
	first, err := billing.Reserve(ctxA, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	container := os.Getenv("COMPARISON_OWNED_REDIS_CONTAINER")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") {
		t.Fatal("isolated Redis container required")
	}
	if out, err := exec.Command("docker", "kill", container).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	t.Cleanup(func() { _ = exec.Command("docker", "start", container).Run() })
	if out, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	ready := false
	for i := 0; i < 30; i++ {
		if exec.Command("docker", "exec", container, "redis-cli", "ping").Run() == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("Redis not ready")
	}
	if exists, err := r.Exists(ctx, store.BalanceKey(tenant.ID)).Result(); err != nil || exists != 0 {
		t.Fatal(exists, err)
	}
	// A new estimated request of 250 cannot fit while the 100-token hold survives.
	second := *req
	second.RequestId = id + "-second"
	second.EstPromptTokens = 250
	rejected, err := billing.Reserve(ctxB, &second)
	if err != nil || rejected.Code != pb.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatal(rejected, err)
	}
	third := *req
	third.RequestId = id + "-third"
	third.EstPromptTokens = 50
	accepted, err := billing.Reserve(ctxB, &third)
	if err != nil || accepted.Code != pb.ReserveCode_RESERVE_OK || accepted.BalanceAfter != tenant.InitialQuota {
		t.Fatalf("post-restart subscription reserve=%v err=%v", accepted, err)
	}
	balance, err := s.GetBalance(ctx, tenant.ID)
	if err != nil || balance != tenant.InitialQuota {
		t.Fatalf("post-restart balance=%d want=%d err=%v", balance, tenant.InitialQuota, err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != balance {
		t.Fatal(ledger, balance, err)
	}
	var row store.Bill
	if err := db.First(&row, first.BillId).Error; err != nil || row.Status != store.StatusPending || row.ReservedTokens == nil || *row.ReservedTokens != 100 {
		t.Fatal(row, err)
	}
	t.Logf("held=100 next250=rejected redis_restarted=true balance=%d ledger=%d", balance, ledger)
}
