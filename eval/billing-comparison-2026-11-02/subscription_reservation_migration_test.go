package comparison

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestSubscriptionReservationMigrationLegacyNull(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	// This test runs only against the random disposable DB created by its script.
	if err := db.Exec("ALTER TABLE bills DROP COLUMN reserved_tokens, DROP COLUMN reserved_tier").Error; err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("legacy-migration-%d", time.Now().UnixNano())
	if err := db.Exec("INSERT INTO bills (request_id,phase,status,tenant_id,api_key_id,model,funding,pre_quota,created_at) VALUES (?, 'reserve','pending',1,1,'legacy','subscription',0,NOW())", id).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "scripts", "migrations", "20261104_subscription_reservation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	var row store.Bill
	if err := db.Where("request_id=?", id).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ReservedTokens != nil || row.ReservedTier != nil {
		t.Fatalf("legacy row must remain unknown: %+v", row)
	}
	t.Log("migration applied to legacy schema; unresolved reservation remains NULL")
}
