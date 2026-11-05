package comparison

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

func TestSettlementSQLFailureRepairsProjection(t *testing.T) {
	if os.Getenv("TEST_SETTLE_SQL_FAILURE") != "1" {
		t.Skip("requires disposable services")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, usage := range []int64{40, 150} {
		t.Run(fmt.Sprintf("tokens=%d", usage), func(t *testing.T) {
			id := fmt.Sprintf("settle-sql-%d-%d", usage, time.Now().UnixNano())
			tenant := store.Tenant{Name: id, InitialQuota: 500}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
				t.Fatal(err)
			}
			reserve, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
			if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(reserve, err)
			}
			if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: usage}); err != nil {
				t.Fatal(err)
			}
			trigger := `CREATE TRIGGER eval_settle_failure BEFORE INSERT ON bills FOR EACH ROW BEGIN IF NEW.phase='settle' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected settlement failure'; END IF; END`
			if os.Getenv("TEST_FAILURE_OUTBOX") == "1" {
				trigger = `CREATE TRIGGER eval_settle_failure BEFORE INSERT ON outbox_events FOR EACH ROW BEGIN IF NEW.event_key LIKE '%:settle' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected settlement outbox failure'; END IF; END`
			}
			if err := db.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			defer db.Exec("DROP TRIGGER IF EXISTS eval_settle_failure")
			if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); err == nil {
				t.Fatal("expected injected failure")
			}
			live, err := s.GetBalance(ctx, tenant.ID)
			ledger, le := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || le != nil || live != 400 || ledger != 400 {
				t.Fatalf("after failure: balance=%d ledger=%d err=%v/%v", live, ledger, err, le)
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.PromptTokens == nil || *bill.PromptTokens != usage {
				t.Fatalf("pending bill=%v err=%v", bill.Status, err)
			}
			var settleRows int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 0 {
				t.Fatalf("settle rows=%d err=%v", settleRows, err)
			}
			guard, err := r.Exists(ctx, store.DeltaGuardKey(id)).Result()
			if err != nil || guard != 0 {
				t.Fatalf("guard=%d err=%v", guard, err)
			}
			var count int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":settle").Count(&count).Error; err != nil || count != 0 {
				t.Fatal(count, err)
			}
			if err := db.Exec("DROP TRIGGER eval_settle_failure").Error; err != nil {
				t.Fatal(err)
			}
			done, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
			if err != nil || !done.Settled {
				t.Fatal(done, err)
			}
			if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); err != nil {
				t.Fatal(err)
			}
			live, err = s.GetBalance(ctx, tenant.ID)
			ledger, le = store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || le != nil || live != 500-usage || ledger != live {
				t.Fatalf("after retry: balance=%d ledger=%d err=%v/%v", live, ledger, err, le)
			}
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":settle").Count(&count).Error; err != nil || count != 1 {
				t.Fatal(count, err)
			}
			t.Logf("failure_balance=400 ledger=400 retry_balance=%d event_count=%d", live, count)
		})
	}
}

func TestSubscriptionSettlementSQLFailureRepairsGuard(t *testing.T) {
	if os.Getenv("TEST_SETTLE_SQL_FAILURE") != "1" {
		t.Skip("requires disposable services")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("sub-settle-fail-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `["文本"]`}
	plan := store.Plan{Name: id, PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":300}`}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant.ID, plan.ID); err != nil {
		t.Fatal(err)
	}
	reserve, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER eval_sub_settle_failure BEFORE INSERT ON outbox_events FOR EACH ROW BEGIN IF NEW.event_key LIKE '%:settle' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected settlement outbox failure'; END IF; END`).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TRIGGER IF EXISTS eval_sub_settle_failure")
	if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); err == nil {
		t.Fatal("expected settlement failure")
	}
	guard, err := r.Exists(ctx, store.DeltaGuardKey(id)).Result()
	if err != nil || guard != 0 {
		t.Fatalf("guard=%d err=%v", guard, err)
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.Funding != store.FundingSubscription {
		t.Fatalf("bill=%v err=%v", bill.Status, err)
	}
	if err := db.Exec("DROP TRIGGER eval_sub_settle_failure").Error; err != nil {
		t.Fatal(err)
	}
	result, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
	if err != nil || !result.Settled {
		t.Fatal(result, err)
	}
	if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); err != nil {
		t.Fatal(err)
	}
	live, err := s.GetBalance(ctx, tenant.ID)
	ledger, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
	var events int64
	eventErr := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":settle").Count(&events).Error
	if err != nil || ledgerErr != nil || eventErr != nil || live != ledger || events != 1 {
		t.Fatalf("balance=%d ledger=%d events=%d errors=%v/%v/%v", live, ledger, events, err, ledgerErr, eventErr)
	}
	t.Logf("subscription=true guard_cleared=true balance=%d ledger=%d events=%d", live, ledger, events)
}
