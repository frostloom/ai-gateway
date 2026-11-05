package comparison

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Test-only: one statement supplies a single MySQL read view; no Redis cache.
func singleQueryAuth(ctx context.Context, db *gorm.DB, hash string) (*store.APIKey, error) {
	var row struct {
		store.APIKey
		SessionCount int64
	}
	q := db.WithContext(ctx).Table("api_keys").Select(`api_keys.*, CASE WHEN BINARY api_keys.name = BINARY ? THEN (SELECT COUNT(*) FROM portal_sessions JOIN portal_users ON portal_users.id = portal_sessions.user_id JOIN tenants session_tenant ON session_tenant.id = portal_users.tenant_id WHERE portal_sessions.api_key_id = api_keys.id AND portal_users.status = 0 AND session_tenant.status = 0 AND session_tenant.id = api_keys.tenant_id) ELSE 1 END AS session_count`, "__browser_session").Joins("JOIN tenants ON tenants.id = api_keys.tenant_id").Where("api_keys.key_hash = ? AND tenants.status = ?", hash, 0).Limit(1).Scan(&row)
	if q.Error != nil {
		return nil, q.Error
	}
	if q.RowsAffected == 0 || row.Status != 0 || (row.ExpiresAt != nil && time.Now().After(*row.ExpiresAt)) || row.SessionCount != 1 {
		return nil, nil
	}
	return &row.APIKey, nil
}

func TestAuthSingleQueryMatchesAuthority(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, r)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("single-auth-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	other := store.Tenant{Name: tag + "-other"}
	for _, v := range []*store.Tenant{&tenant, &other} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: tag, Name: "normal"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	user := store.PortalUser{Username: tag, PasswordHash: "synthetic", TenantID: tenant.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	session := store.PortalSession{UserID: user.ID, APIKeyID: key.ID}
	ctx := context.Background()
	check := func(label string, want bool) {
		t.Helper()
		a, e := st.ValidateAPIKey(ctx, tag)
		if e != nil {
			t.Fatalf("%s base %v", label, e)
		}
		b, e := singleQueryAuth(ctx, db, tag)
		if e != nil {
			t.Fatalf("%s candidate %v", label, e)
		}
		if (a != nil) != want || (b != nil) != want {
			t.Fatalf("%s baseline=%+v candidate=%+v want=%v", label, a, b, want)
		}
		if want && (a.ID != b.ID || a.TenantID != b.TenantID || a.Name != b.Name || a.KeyHash != b.KeyHash) {
			t.Fatalf("%s differing key: %+v %+v", label, a, b)
		}
	}
	update := func(model any, field string, v any) {
		t.Helper()
		if err := db.Model(model).Update(field, v).Error; err != nil {
			t.Fatal(err)
		}
	}
	check("regular", true)
	update(&key, "status", 1)
	check("disabled key", false)
	update(&key, "status", 0)
	update(&tenant, "status", 1)
	check("disabled tenant", false)
	update(&tenant, "status", 0)
	past := time.Now().Add(-time.Hour)
	update(&key, "expires_at", past)
	check("expired", false)
	update(&key, "expires_at", nil)
	update(&key, "name", "__browser_session")
	check("missing session", false)
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	check("browser", true)
	update(&user, "status", 1)
	check("disabled user", false)
	update(&user, "status", 0)
	update(&user, "tenant_id", other.ID)
	check("foreign user tenant", false)
	update(&user, "tenant_id", tenant.ID)
	if err := db.Delete(&session).Error; err != nil {
		t.Fatal(err)
	}
	check("deleted session", false)
	if a, e := singleQueryAuth(ctx, db, tag+"-absent"); e != nil || a != nil {
		t.Fatalf("missing=%+v err=%v", a, e)
	}
	t.Log("regular and browser key states matched real Store over 9 cases")
}

// Sequential local microbenchmark, not a gateway capacity test.
func TestAuthSingleQueryLatency(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, r)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("single-timing-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	user := store.PortalUser{Username: tag, PasswordHash: "synthetic", TenantID: tenant.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	normal := store.APIKey{TenantID: tenant.ID, KeyHash: tag + "-normal", Name: "normal"}
	browser := store.APIKey{TenantID: tenant.ID, KeyHash: tag + "-browser", Name: "__browser_session"}
	for _, key := range []*store.APIKey{&normal, &browser} {
		if err := db.Create(key).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&store.PortalSession{UserID: user.ID, APIKeyID: browser.ID}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const n = 200
	for _, key := range []store.APIKey{normal, browser} {
		for _, name := range []string{"baseline", "candidate", "candidate", "baseline"} {
			d := make([]time.Duration, 0, n)
			var total time.Duration
			for i := 0; i < n; i++ {
				start := time.Now()
				var got *store.APIKey
				var err error
				if name == "baseline" {
					got, err = st.ValidateAPIKey(ctx, key.KeyHash)
				} else {
					got, err = singleQueryAuth(ctx, db, key.KeyHash)
				}
				elapsed := time.Since(start)
				if err != nil || got == nil || got.ID != key.ID {
					t.Fatalf("%s result=%+v err=%v", name, got, err)
				}
				d = append(d, elapsed)
				total += elapsed
			}
			sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
			t.Logf("key=%s variant=%s n=%d mean_us=%d p95_us=%d p99_us=%d", key.Name, name, n, total.Microseconds()/n, d[n*95/100-1].Microseconds(), d[n*99/100-1].Microseconds())
		}
	}
}
