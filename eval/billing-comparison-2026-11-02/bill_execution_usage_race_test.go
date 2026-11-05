package comparison

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestReversingIntentRejectsLateUsage(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental billing state overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("execution-late-usage-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	bill, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	q := db.Exec("UPDATE bills SET execution_state='reversing' WHERE id=? AND phase=? AND status=? AND execution_state='reserved' AND usage_reported_at IS NULL", bill.BillId, store.PhaseReserve, store.StatusPending)
	if q.Error != nil || q.RowsAffected != 1 {
		t.Fatal(q.RowsAffected, q.Error)
	}
	err = s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("usage after reversing should be rejected: %v", err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":usage").Count(&events).Error; err != nil || events != 0 {
		t.Fatal(events, err)
	}
	reversed, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(bill.BillId), Reason: "synthetic"})
	if err != nil || !reversed.Reversed {
		t.Fatal(reversed, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 500 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("late_usage=FailedPrecondition reverse=true balance=%d ledger=%d", live, ledger)
}
