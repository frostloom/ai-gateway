package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Test-only adapter; old billing instances and direct Store calls bypass it.
type guardedSweepBilling struct {
	pb.BillingServiceClient
	db        *gorm.DB
	store     *store.Store
	interrupt bool
}

func (b *guardedSweepBilling) Reverse(ctx context.Context, req *pb.ReverseRequest, _ ...grpc.CallOption) (*pb.ReverseResponse, error) {
	var intent sqlIdemIntent
	if err := b.db.WithContext(ctx).Where("request_id=?", req.RequestId).First(&intent).Error; err != nil {
		return nil, err
	}
	if intent.State == "reserved" {
		won, err := markExpiredReserved(ctx, b.db, req.RequestId)
		if err != nil {
			return nil, err
		}
		if !won {
			return nil, status.Error(codes.FailedPrecondition, "not eligible for automatic reverse")
		}
		if b.interrupt {
			b.interrupt = false
			return nil, status.Error(codes.Unavailable, "synthetic stop after reversing intent")
		}
	} else if intent.State != "reversing" && intent.State != "reversed" {
		return nil, status.Error(codes.FailedPrecondition, "unknown or active upstream")
	}
	ok, err := finishReversing(ctx, b.db, b.store, req.RequestId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "reverse intent incomplete")
	}
	return &pb.ReverseResponse{Reversed: true}, nil
}

func TestGuardedSweepRetainsUnknownAndRecoversIntent(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	g := sqlIdemGate{db: db}
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := g.migrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("guarded-sweep-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
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
	ids := []string{id + "-sent", id + "-reserved"}
	bills := make([]*pb.ReserveResponse, 2)
	for i, rid := range ids {
		if state, err := g.claim(ctx, rid, "same", "owner", time.Minute); err != nil || state != gateOwned {
			t.Fatal(state, err)
		}
		if ok, err := beginReserving(ctx, db, rid, "owner"); err != nil || !ok {
			t.Fatal(ok, err)
		}
		bill, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
		if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(bill, err)
		}
		bills[i] = bill
		if ok, err := finishReserving(ctx, db, rid, "owner"); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if i == 0 {
			if ok, err := forwardReserved(ctx, db, rid, "owner"); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
		if err := db.Model(&store.Bill{}).Where("id=?", bill.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", rid).Error; err != nil {
			t.Fatal(err)
		}
	}
	cli := &guardedSweepBilling{db: db, store: s, interrupt: true}
	runner := reconciler.New(db, cli, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
	first, err := runner.SweepOnce(ctx)
	if err != nil || first.Scanned != 2 || first.Reversed != 0 {
		t.Fatal(first, err)
	}
	var intent sqlIdemIntent
	if err := db.Where("request_id=?", ids[1]).First(&intent).Error; err != nil || intent.State != "reversing" {
		t.Fatal(intent, err)
	}
	second, err := runner.SweepOnce(ctx)
	if err != nil || second.Scanned != 2 || second.Reversed != 1 {
		t.Fatal(second, err)
	}
	var rows []store.Bill
	if err := db.Where("request_id IN ? AND phase=?", ids, store.PhaseReserve).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, row := range rows {
		states[row.RequestID] = row.Status
	}
	if states[ids[0]] != store.StatusPending || states[ids[1]] != store.StatusReversed {
		t.Fatalf("states=%v", states)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: ids[0], PromptTokens: 84, CompletionTokens: 12}); err != nil {
		t.Fatal(err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: ids[0], BillId: int64(bills[0].BillId)})
	if err != nil || !settled.Settled {
		t.Fatal(settled, err)
	}
	balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || balance != ledger {
		t.Fatal(balance, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{ids[0] + ":reserve", ids[1] + ":reserve", ids[1] + ":reverse", ids[0] + ":usage", ids[0] + ":settle"}).Count(&events).Error; err != nil || events != 5 {
		t.Fatal(events, err)
	}
	t.Logf("first=%+v second=%+v unknown_retained=true late_usage_settled=true balance=%d ledger=%d events=%d", first, second, balance, ledger, events)
}
