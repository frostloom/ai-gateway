package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExperimentalSubscriptionIntentQuota(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental Store overlay")
	}
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
	rpc.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("missing experimental reserve")
	}
	owner := sha256.Sum256([]byte("synthetic-owner"))
	ownerHash := hex.EncodeToString(owner[:])
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Subscription intent must commit with the reservation and outbox.
	var injected atomic.Int32
	if err := db.Callback().Update().Before("gorm:update").Register("test:subscription_intent_failure", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if ok && values["execution_owner_hash"] != nil && injected.CompareAndSwap(0, 1) {
			tx.AddError(errors.New("injected intent failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failedID := id + "-rollback"
	failedCtx := controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	_, err = candidate.ReserveWithExecutionIntent(failedCtx, &pb.ReserveRequest{RequestId: failedID, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}, ownerHash, 30*time.Second)
	if removeErr := db.Callback().Update().Remove("test:subscription_intent_failure"); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err == nil || injected.Load() != 1 {
		t.Fatalf("injected=%d err=%v", injected.Load(), err)
	}
	var rolledBackBills, rolledBackEvents int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", failedID).Count(&rolledBackBills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", failedID+":reserve").Count(&rolledBackEvents).Error; err != nil {
		t.Fatal(err)
	}
	if rolledBackBills != 0 || rolledBackEvents != 0 {
		t.Fatalf("rollback bills=%d events=%d", rolledBackBills, rolledBackEvents)
	}
	var okCount, quotaCount, unexpected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rid := fmt.Sprintf("%s-%d", id, i)
			req := &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}
			reqCtx := controlplane.WithReserveContent(ctx, strings.Repeat(fmt.Sprintf("%x", i), 64))
			result, err := candidate.ReserveWithExecutionIntent(reqCtx, req, ownerHash, 30*time.Second)
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
		conflictCtx := controlplane.WithReserveContent(ctx, strings.Repeat("f", 64))
		_, conflict := candidate.ReserveWithExecutionIntent(conflictCtx, &pb.ReserveRequest{RequestId: row.RequestID, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}, ownerHash, 30*time.Second)
		if status.Code(conflict) != codes.FailedPrecondition {
			t.Fatalf("changed content should conflict: %v", conflict)
		}
	}
	// Bills reserved before disable must still finish, while new reserves fail.
	if err := db.Model(&store.Tenant{}).Where("id=?", tenant.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	_, denied := candidate.ReserveWithExecutionIntent(ctx, &pb.ReserveRequest{RequestId: id + "-disabled", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}, ownerHash, 30*time.Second)
	if status.Code(denied) != codes.PermissionDenied {
		t.Fatalf("disabled tenant reserve: %v", denied)
	}
	for _, row := range bills {
		if row.Funding != store.FundingSubscription || row.PreQuota != 0 || row.ReservedTokens == nil || *row.ReservedTokens != 100 {
			t.Fatal(row)
		}
		var intent struct {
			ExecutionOwnerHash  string
			ExecutionLeaseUntil *time.Time
		}
		if err := db.Table("bills").Select("execution_owner_hash", "execution_lease_until").Where("id = ?", row.ID).Take(&intent).Error; err != nil || intent.ExecutionOwnerHash != ownerHash || intent.ExecutionLeaseUntil == nil {
			t.Fatal(intent, err)
		}
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", row.RequestID+":reserve").Count(&n).Error; err != nil || n != 1 {
			t.Fatal(n, err)
		}
		var permit wrapperspb.Int64Value
		if err := conn.Invoke(ctx, "/eval.execution.v1.Execution/Transition", wrapperspb.String(row.RequestID+"|forwarding|synthetic-owner"), &permit); err != nil || permit.Value != 1 {
			t.Fatal(permit.Value, err)
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
