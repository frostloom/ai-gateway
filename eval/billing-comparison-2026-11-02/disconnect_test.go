package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCandidateKilledTransaction(t *testing.T) {
	db, _ := isolatedDB(t)
	c := candidate{db: db}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	if err := c.seed(110000, 1000); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var connID uint64
	if err := tx.Raw("SELECT CONNECTION_ID()").Scan(&connID).Error; err != nil {
		t.Fatal(err)
	}
	var account candidateAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, 110000).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&account).Update("balance", gorm.Expr("balance - ?", 100)).Error; err != nil {
		t.Fatal(err)
	}
	fingerprint := "test-incomplete"
	row := store.Bill{RequestID: "kill-before-commit", ReserveFingerprint: &fingerprint, Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: 110000, APIKeyID: 1, Model: "comparison-mock", Funding: store.FundingBalance, PreQuota: 100, CreatedAt: time.Now()}
	if err := tx.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := candidateEvent(tx, &row, store.PhaseReserve, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf("KILL CONNECTION %d", connID)).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err == nil {
		t.Fatal("commit unexpectedly succeeded after connection kill")
	}
	var count int64
	if e := db.Model(&store.Bill{}).Where("request_id=?", row.RequestID).Count(&count).Error; e != nil || count != 0 {
		t.Fatalf("bill=%d err=%v", count, e)
	}
	if e := db.Model(&store.OutboxEvent{}).Where("event_key=?", row.RequestID+":reserve").Count(&count).Error; e != nil || count != 0 {
		t.Fatalf("outbox=%d err=%v", count, e)
	}
	b, e := c.balance(context.Background(), 110000)
	if e != nil || b != 1000 {
		t.Fatalf("balance=%d err=%v", b, e)
	}
	req := &billingv1.ReserveRequest{RequestId: row.RequestID, TenantId: 110000, ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
	resp, e := c.reserve(context.Background(), req)
	if e != nil || resp.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("retry=%v %v", resp, e)
	}
	if e = c.audit(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Log("uncommitted balance, bill and outbox rolled back after MySQL connection kill; retry reserved once")
}
