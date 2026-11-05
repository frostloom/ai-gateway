package comparison

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestRefundRequestMigrationLegacyNull(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("refund-legacy-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 900, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE topups DROP COLUMN requested_cent").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO topups (tenant_id,source,amount,ref_type,ref_id,created_at) VALUES (?,'refund',-100,'refund',?,NOW())", tenant.ID, id).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "scripts", "migrations", "20261104_refund_request_amount.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	var row store.Topup
	if err := db.Where("ref_type=? AND ref_id=?", store.TopupRefund, id).First(&row).Error; err != nil || row.RequestedCent != nil {
		t.Fatal(row, err)
	}
	if resp, err := s.RefundAmount(ctx, tenant.ID, 1, id); err == nil {
		t.Fatalf("legacy unverifiable replay accepted: %+v", resp)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 900 {
		t.Fatal(live, err)
	}
	t.Log("legacy requested amount stays NULL; replay rejected without balance change")
}
