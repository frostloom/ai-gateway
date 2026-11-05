package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

// MySQL collation must not change Go's exact browser-key name comparison.
func TestAuthSingleQueryNameMatchesGoBytes(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("auth-name-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&tenant)
	for i, name := range []string{"__browser_session", "__BROWSER_SESSION", "__browser_session ", "__browser_sessioN"} {
		key := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("%s-%d", tag, i), Name: name}
		if err := db.Create(&key).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Delete(&key)
		base, err := st.ValidateAPIKey(context.Background(), key.KeyHash)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := singleQueryAuth(context.Background(), db, key.KeyHash)
		if err != nil {
			t.Fatal(err)
		}
		if (base != nil) != (candidate != nil) {
			t.Errorf("name=%q baseline_valid=%t candidate_valid=%t", name, base != nil, candidate != nil)
		}
	}
}
