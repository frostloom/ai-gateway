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

// Characterization of the current Store; this must not run against real data.
func TestReverseSQLFailureLeavesVisibleStaleBalance(t *testing.T) {
	if os.Getenv("TEST_REVERSE_SQL_FAILURE") != "1" {
		t.Skip("isolated failure injection only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reverse-sql-fail-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: "test", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
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
	triggerSQL := `CREATE TRIGGER eval_reverse_failure BEFORE UPDATE ON bills FOR EACH ROW BEGIN IF OLD.status='pending' AND NEW.status='reversed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected reverse failure'; END IF; END`
	if os.Getenv("TEST_FAILURE_OUTBOX") == "1" {
		triggerSQL = `CREATE TRIGGER eval_reverse_failure BEFORE INSERT ON outbox_events FOR EACH ROW BEGIN IF NEW.event_key LIKE '%:reverse' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected reverse outbox failure'; END IF; END`
	}
	if err := db.Exec(triggerSQL).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TRIGGER IF EXISTS eval_reverse_failure")
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id}); err == nil {
		t.Fatal("expected injected MySQL failure")
	}
	live, err := s.GetBalance(ctx, tenant.ID)
	ledger, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
	wantLive := int64(500)
	if os.Getenv("TEST_EXPECT_REPAIRED") == "1" {
		wantLive = 400
	}
	if err != nil || ledgerErr != nil || live != wantLive || ledger != 400 {
		t.Fatalf("live=%d want=%d ledger=%d err=%v/%v", live, wantLive, ledger, err, ledgerErr)
	}
	guardCount, err := r.Exists(ctx, store.DeltaGuardKey(id)).Result()
	if err != nil {
		t.Fatal(err)
	}
	wantGuard := int64(1)
	if os.Getenv("TEST_EXPECT_REPAIRED") == "1" {
		wantGuard = 0
	}
	if guardCount != wantGuard {
		t.Fatalf("guard=%d want=%d", guardCount, wantGuard)
	}
	var reverseEvents int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 0 {
		t.Fatalf("reverse events=%d err=%v", reverseEvents, err)
	}
	var row store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; err != nil || row.Status != store.StatusPending {
		t.Fatal(row.Status, err)
	}
	if err := db.Exec("DROP TRIGGER eval_reverse_failure").Error; err != nil {
		t.Fatal(err)
	}
	resp, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
	if err != nil || !resp.Reversed {
		t.Fatalf("retry=%v err=%v", resp, err)
	}
	after, err := s.GetBalance(ctx, tenant.ID)
	ledgerAfter, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledgerErr != nil || after != 500 || ledgerAfter != 500 {
		t.Fatalf("after=%d ledger=%d err=%v/%v", after, ledgerAfter, err, ledgerErr)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 1 {
		t.Fatalf("retry reverse events=%d err=%v", reverseEvents, err)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 1 {
		t.Fatalf("replay reverse events=%d err=%v", reverseEvents, err)
	}
	t.Logf("injected_sql_failure=true pending=true visible_balance=%d ledger=%d retry_reversed=true final_balance=%d ledger=%d", live, ledger, after, ledgerAfter)
}
