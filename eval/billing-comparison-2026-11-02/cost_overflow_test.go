package comparison

import (
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

func TestLargeUsageMustNotOverflowIntoRefund(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("large-usage-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 100000000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 10000000000, OutputPriceCent: 1, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	reserve, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 1})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 1000000000}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: reserve.BillId})
	if err != nil || !result.Settled {
		t.Fatal(result, err)
	}
	var row store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseSettle).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 0 || ledger != 0 || row.ActualQuota == nil || *row.ActualQuota != tenant.InitialQuota {
		t.Fatalf("balance=%d ledger=%d actual=%v settle=%+v err=%v", live, ledger, row.ActualQuota, result, err)
	}
	t.Logf("large_usage=1000000000 price=10000000000 expected_cost=10000000000000 collected=%d balance=%d ledger=%d", *row.ActualQuota, live, ledger)
}

func TestUnrepresentableReserveCostFailsBeforeCharge(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("unrepresentable-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: math.MaxInt64, OutputPriceCent: 1, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	_, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 1000000000})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("overflow err=%v", err)
	}
	var n int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&n).Error; err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&n).Error; err != nil || n != 0 {
		t.Fatal(n, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != tenant.InitialQuota {
		t.Fatal(live, err)
	}
	t.Logf("unrepresentable cost rejected, no bill/outbox, balance=%d", live)
}

func TestUnrepresentableSettlementCostStaysPending(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("settle-unrepresentable-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000000000000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 10000000000000000, OutputPriceCent: 1, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	reserve, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 1})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 1000000000}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: reserve.BillId})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected invalid settlement cost: %v", err)
	}
	var row store.Bill
	if err := db.First(&row, reserve.BillId).Error; err != nil || row.Status != store.StatusPending || row.UsageReportedAt == nil {
		t.Fatal(row, err)
	}
	var n int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&n).Error; err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":settle").Count(&n).Error; err != nil || n != 0 {
		t.Fatal(n, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != live || live != 988000000000 {
		t.Fatal(live, ledger, err)
	}
	t.Logf("unrepresentable settlement held pending with usage, no settle, balance=%d ledger=%d", live, ledger)
}
