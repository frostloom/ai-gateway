package comparison

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestProductionBrowserAuthUsesOneStatement(t *testing.T) {
	if os.Getenv("TEST_AUTH_SINGLE") != "1" && os.Getenv("TEST_AUTH_SINGLE_ASSERT") != "1" {
		t.Skip("single-query overlay only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, r)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("single-prod-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	user := store.PortalUser{Username: tag, PasswordHash: "synthetic", TenantID: tenant.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: tag, Name: "__browser_session"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.PortalSession{UserID: user.ID, APIKeyID: key.ID}).Error; err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int64
	const callback = "test:count_single_auth_statements"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(*gorm.DB) { queries.Add(1) }); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	if err := db.Callback().Row().Before("gorm:row").Register(callback, func(*gorm.DB) { queries.Add(1) }); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Row().Remove(callback)
	found, err := st.ValidateAPIKey(context.Background(), tag)
	if err != nil || found == nil || found.ID != key.ID {
		t.Fatalf("auth=%+v err=%v", found, err)
	}
	if n := queries.Load(); n != 1 {
		t.Fatalf("browser auth issued %d SQL statements, expected 1", n)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := st.ValidateAPIKey(canceled, tag); err == nil || result != nil {
		t.Fatalf("canceled context authorized: result=%+v err=%v", result, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := st.ValidateAPIKey(context.Background(), tag); err == nil || result != nil {
		t.Fatalf("closed database authorized: result=%+v err=%v", result, err)
	}
}
