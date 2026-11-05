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

func TestUnknownBillingCandidateRefundAndSettlement(t *testing.T) {
	if os.Getenv("UNKNOWN_BILLING_CANDIDATE") != "1" {
		t.Skip("requires test-only billing overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("unknown-candidate-%d", time.Now().UnixNano())
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
	reserve, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	_, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(reserve.BillId), Reason: "build upstream request failed"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("candidate must refuse even known unsent refund: %v", err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: int64(reserve.BillId)})
	if err != nil || !settled.Settled {
		t.Fatal(settled, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatal(events, err)
	}
	t.Logf("known_unsent_refund=FailedPrecondition usage_settled=true balance=%d ledger=%d events=%d", live, ledger, events)
}
