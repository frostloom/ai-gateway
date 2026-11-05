package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// The compatibility value zero remains accepted; a nonzero ID must match.
func TestActualBillIDMismatchRejected(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("bill-id-mismatch-%d", time.Now().UnixNano())
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
	reserve := func(suffix string) *pb.ReserveResponse {
		result, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id + suffix, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
		if err != nil || result.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("reserve %s: %+v %v", suffix, result, err)
		}
		return result
	}
	a, b, c := reserve("-a"), reserve("-b"), reserve("-c")
	if a.BillId == b.BillId || b.BillId == c.BillId {
		t.Fatal("expected distinct IDs")
	}
	// Reject mismatch without recording usage, settlement, reversal, or events.
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", BillId: b.BillId, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("usage mismatch: %v", err)
	}
	var untouched store.Bill
	if err := db.First(&untouched, a.BillId).Error; err != nil {
		t.Fatal(err)
	}
	if untouched.UsageReportedAt != nil {
		t.Error("mismatched usage persisted")
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", BillId: -1, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("negative ID: %v", err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", BillId: a.BillId, PromptTokens: 40}); err != nil {
		t.Fatalf("valid usage: %v", err)
	}
	if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id + "-a", BillId: b.BillId}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("settle mismatch: %v", err)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id + "-b", BillId: c.BillId}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("reverse mismatch: %v", err)
	}
	var rows []store.Bill
	if err := db.Where("id IN ?", []int64{a.BillId, b.BillId, c.BillId}).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Status != store.StatusPending || rows[1].Status != store.StatusPending || rows[2].Status != store.StatusPending {
		t.Errorf("terminal transition on mismatch: %+v", rows)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + "-a:settle", id + "-b:reverse"}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Errorf("terminal events on mismatch: %d", events)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live != 700 || ledger != 700 {
		t.Errorf("live=%d ledger=%d", live, ledger)
	}
	if resp, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id + "-a", BillId: a.BillId}); err != nil || !resp.Settled {
		t.Fatalf("valid settle: %+v %v", resp, err)
	}
	if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id + "-a", BillId: b.BillId}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("settled replay mismatch: %v", err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id + "-a", BillId: b.BillId, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("usage replay mismatch: %v", err)
	}
	if resp, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id + "-b"}); err != nil || !resp.Reversed {
		t.Fatalf("legacy zero reverse: %+v %v", resp, err)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id + "-b", BillId: c.BillId}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("reversed replay mismatch: %v", err)
	}
}
