package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestStoreOptInAuthCache(t *testing.T) {
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	configurer, ok := any(st).(interface{ EnableAuthCache(time.Duration) })
	if !ok {
		t.Fatal("store has no opt-in auth cache")
	}
	configurer.EnableAuthCache(time.Second)
	name := fmt.Sprintf("cache-store-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: name}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: name, Name: "normal"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if k, err := st.ValidateAPIKey(ctx, name); err != nil || k == nil {
		t.Fatalf("warm: %v %v", k, err)
	}
	if err := db.Model(&key).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if k, err := st.ValidateAPIKey(ctx, name); err != nil || k == nil {
		t.Fatalf("expected cache hit: %v %v", k, err)
	}
	time.Sleep(150 * time.Millisecond)
	shorter := store.NewStore(db, redis)
	shorter.EnableAuthCache(100 * time.Millisecond)
	if k, err := shorter.ValidateAPIKey(ctx, name); err != nil || k != nil {
		t.Fatalf("shorter TTL must reject old cached key: %v %v", k, err)
	}
	time.Sleep(1200 * time.Millisecond)
	if k, err := st.ValidateAPIKey(ctx, name); err != nil || k != nil {
		t.Fatalf("revoked after TTL: %v %v", k, err)
	}
	user := store.PortalUser{Username: name, PasswordHash: "synthetic", TenantID: tenant.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	browser := store.APIKey{TenantID: tenant.ID, KeyHash: name + "-browser", Name: "__browser_session"}
	if err := db.Create(&browser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.PortalSession{UserID: user.ID, APIKeyID: browser.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if k, err := st.ValidateAPIKey(ctx, browser.KeyHash); err != nil || k == nil {
		t.Fatalf("browser warm: %v %v", k, err)
	}
	if err := db.Model(&browser).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if k, err := st.ValidateAPIKey(ctx, browser.KeyHash); err != nil || k != nil {
		t.Fatalf("browser revocation: %v %v", k, err)
	}
}

func authCacheReadCount(t *testing.T, outcome string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "billing_auth_cache_read_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "result" && label.GetValue() == outcome {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestStoreAuthCacheReadMetrics(t *testing.T) {
	db, redisClient := isolatedDB(t)
	st := store.NewStore(db, redisClient)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	st.EnableAuthCache(time.Second * 5)
	name := fmt.Sprintf("cache-metric-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: name}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: name, Name: "normal"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	hit, miss := authCacheReadCount(t, "hit"), authCacheReadCount(t, "miss")
	for i := 0; i < 2; i++ {
		if k, err := st.ValidateAPIKey(context.Background(), name); err != nil || k == nil {
			t.Fatalf("read: %v %v", k, err)
		}
	}
	if h, m := authCacheReadCount(t, "hit")-hit, authCacheReadCount(t, "miss")-miss; h != 1 || m != 1 {
		t.Fatalf("cache read metrics hit=%v miss=%v", h, m)
	}
	invalid := authCacheReadCount(t, "invalid")
	if err := redisClient.Set(context.Background(), "billing:auth:v1:"+name, "invalid-json", time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	if k, err := st.ValidateAPIKey(context.Background(), name); err != nil || k == nil {
		t.Fatalf("invalid fallback: %v %v", k, err)
	}
	if got := authCacheReadCount(t, "invalid") - invalid; got != 1 {
		t.Fatalf("invalid metric=%v", got)
	}
}

func TestStoreAuthCacheSlowReadDoesNotRefill(t *testing.T) {
	db, redisClient := isolatedDB(t)
	st := store.NewStore(db, redisClient)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	st.EnableAuthCache(200 * time.Millisecond)
	name := fmt.Sprintf("cache-slow-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: name}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: name, Name: "normal"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	callback := "test:cache-slow-" + name
	if err := db.Callback().Row().After("gorm:row").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "api_keys" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		k, err := st.ValidateAPIKey(context.Background(), name)
		if err != nil || k == nil {
			result <- fmt.Errorf("old in-flight read: %v %v", k, err)
			return
		}
		result <- nil
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("read did not reach barrier")
	}
	if err := db.Model(&key).Update("status", 1).Error; err != nil {
		close(release)
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if n, err := redisClient.Exists(context.Background(), "billing:auth:v1:"+name).Result(); err != nil || n != 0 {
		t.Fatalf("old cache refill: count=%d err=%v", n, err)
	}
	if k, err := st.ValidateAPIKey(context.Background(), name); err != nil || k != nil {
		t.Fatalf("new read after revoke: %v %v", k, err)
	}
}

func TestStoreAuthCacheFallbackAndExpiry(t *testing.T) {
	db, redisClient := isolatedDB(t)
	st := store.NewStore(db, redisClient)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("cache-expiry-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: name}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(500 * time.Millisecond)
	key := store.APIKey{TenantID: tenant.ID, KeyHash: name, Name: "normal", ExpiresAt: &expires}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	st.EnableAuthCache(time.Second)
	if k, err := st.ValidateAPIKey(context.Background(), name); err != nil || k == nil {
		t.Fatalf("warm: %v %v", k, err)
	}
	time.Sleep(600 * time.Millisecond)
	if k, err := st.ValidateAPIKey(context.Background(), name); err != nil || k != nil {
		t.Fatalf("expired key: %v %v", k, err)
	}
	key.KeyHash = name + "-fallback"
	key.ID = 0
	key.ExpiresAt = nil
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	offline := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond, MaxRetries: -1})
	defer offline.Close()
	fallback := store.NewStore(db, offline)
	fallback.EnableAuthCache(time.Second)
	beforeErrors := authCacheReadCount(t, "error")
	if k, err := fallback.ValidateAPIKey(context.Background(), key.KeyHash); err != nil || k == nil {
		t.Fatalf("fallback: %v %v", k, err)
	}
	if got := authCacheReadCount(t, "error") - beforeErrors; got != 1 {
		t.Fatalf("error metric=%v", got)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if k, err := fallback.ValidateAPIKey(canceled, key.KeyHash); err == nil || k != nil {
		t.Fatalf("canceled: %v %v", k, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if k, err := fallback.ValidateAPIKey(context.Background(), key.KeyHash); err == nil || k != nil {
		t.Fatalf("both dependencies unavailable: %v %v", k, err)
	}
}
