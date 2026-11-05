package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
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

func TestContentFingerprintSubscriptionConcurrentQuota(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("v2-sub-concurrent-%d", time.Now().UnixNano())
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
	var okCount, quotaCount, unexpected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rid := fmt.Sprintf("%s-%d", id, i)
			req := &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}
			reqCtx := controlplane.WithReserveContent(ctx, strings.Repeat(fmt.Sprintf("%x", i), 64))
			result, err := billing.Reserve(reqCtx, req)
			if err != nil {
				unexpected.Add(1)
				return
			}
			switch result.Code {
			case pb.ReserveCode_RESERVE_OK:
				okCount.Add(1)
			case pb.ReserveCode_RESERVE_SUB_QUOTA:
				quotaCount.Add(1)
			default:
				unexpected.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if okCount.Load() != 3 || quotaCount.Load() != 5 || unexpected.Load() != 0 {
		t.Fatalf("ok=%d quota=%d other=%d", okCount.Load(), quotaCount.Load(), unexpected.Load())
	}
	var bills []store.Bill
	if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Find(&bills).Error; err != nil || len(bills) != 3 {
		t.Fatal(len(bills), err)
	}
	for _, row := range bills {
		if row.Funding != store.FundingSubscription || row.PreQuota != 0 {
			t.Fatal(row)
		}
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", row.RequestID+":reserve").Count(&n).Error; err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if _, err := billing.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: row.RequestID, PromptTokens: 100}); err != nil {
			t.Fatal(err)
		}
		settled, err := billing.Settle(ctx, &pb.SettleRequest{RequestId: row.RequestID, BillId: int64(row.ID)})
		if err != nil || !settled.Settled || settled.DeltaQuota != 0 {
			t.Fatal(settled, err)
		}
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger {
		t.Fatal(live, ledger, err)
	}
	t.Logf("quota=300 concurrent=8 reserve=3 rejected=5 settled=3 balance=%d ledger=%d", live, ledger)
}
