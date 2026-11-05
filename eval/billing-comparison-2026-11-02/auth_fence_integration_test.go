package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

// Demonstrates the candidate's tenant-wide invalidation gap with the real store.
func TestGuardedTenantDisableLeavesCachedKeys(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := time.Now().UnixNano()
	tenant := store.Tenant{Name: fmt.Sprintf("auth-tenant-%d", id)}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	keys := []store.APIKey{{TenantID: tenant.ID, KeyHash: fmt.Sprintf("a-%d", id), KeyPrefix: "test"}, {TenantID: tenant.ID, KeyHash: fmt.Sprintf("b-%d", id), KeyPrefix: "test"}}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	c := guardedAuthCache{r: r, ttl: 600 * time.Millisecond, fenceTTL: time.Second, validate: svc.ValidateAPIKey}
	for _, key := range keys {
		if k, e := c.lookup(ctx, key.KeyHash); e != nil || k == nil {
			t.Fatalf("warm: %v %v", k, e)
		}
	}
	if err := db.Model(&tenant).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if k, e := svc.ValidateAPIKey(ctx, key.KeyHash); e != nil || k != nil {
			t.Fatalf("store after disable: %v %v", k, e)
		}
		if k, e := c.lookup(ctx, key.KeyHash); e != nil || k == nil {
			t.Fatalf("expected stale cache: %v %v", k, e)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, key := range keys {
		for {
			k, e := c.lookup(ctx, key.KeyHash)
			if e != nil {
				t.Fatal(e)
			}
			if k == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cache did not expire")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Log("tenant_disabled=true authoritative_keys_rejected=2 cached_keys_still_valid=2 after_ttl_rejected=2")
}

// Delete only the test fence to model loss of Redis state, without pretending
// that this reproduces a particular restart or failover mechanism.
func TestGuardedFenceLossBeforeCommit(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := time.Now().UnixNano()
	tenant := store.Tenant{Name: fmt.Sprintf("fence-loss-%d", id)}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("loss-%d", id), KeyPrefix: "test"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	c := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: svc.ValidateAPIKey}
	if k, e := c.lookup(ctx, key.KeyHash); e != nil || k == nil {
		t.Fatalf("warm: %v %v", k, e)
	}
	err := c.mutate(ctx, key.KeyHash, func() error {
		fence := "auth-guard-fence:" + key.KeyHash
		if n, e := r.Del(ctx, fence).Result(); e != nil || n != 1 {
			return fmt.Errorf("delete fence n=%d err=%v", n, e)
		}
		if k, e := c.lookup(ctx, key.KeyHash); e != nil || k == nil {
			return fmt.Errorf("old read: %v %v", k, e)
		}
		return db.Model(&key).Update("status", 1).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if k, e := svc.ValidateAPIKey(ctx, key.KeyHash); e != nil || k != nil {
		t.Fatalf("authoritative: %v %v", k, e)
	}
	if k, e := c.lookup(ctx, key.KeyHash); e != nil || k == nil {
		t.Fatalf("expected stale cache: %v %v", k, e)
	}
	t.Log("fence_deleted_before_db_write=true old_value_refilled=true authoritative_rejects=true candidate_accepts=true")
}

// Uses the same disposable database and Redis as the benchmark runner.
func TestFenceActualStoreMultiInstance(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	row := store.APIKey{TenantID: 777, KeyHash: "fence-integration-test-hash", KeyPrefix: "test"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	a := fencedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: svc.ValidateAPIKey}
	b := fencedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: svc.ValidateAPIKey}
	if k, e := a.lookup(ctx, row.KeyHash); e != nil || k == nil {
		t.Fatalf("warm: %v %v", k, e)
	}
	if k, e := b.lookup(ctx, row.KeyHash); e != nil || k == nil {
		t.Fatalf("cached: %v %v", k, e)
	}
	// Prototype exercises one known mutation path; it does not cover direct SQL writers.
	if e := a.invalidate(ctx, row.KeyHash); e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&row).Update("status", 1).Error; e != nil {
		t.Fatal(e)
	}
	for _, instance := range []*fencedAuthCache{&a, &b} {
		if k, e := instance.lookup(ctx, row.KeyHash); e != nil || k != nil {
			t.Fatalf("disabled: %v %v", k, e)
		}
	}
}

func TestGuardedActualStoreMultiInstance(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	row := store.APIKey{TenantID: 778, KeyHash: "guard-integration-test-hash", KeyPrefix: "test"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	a := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: svc.ValidateAPIKey}
	b := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: svc.ValidateAPIKey}
	if k, e := a.lookup(ctx, row.KeyHash); e != nil || k == nil {
		t.Fatalf("warm: %v %v", k, e)
	}
	if k, e := b.lookup(ctx, row.KeyHash); e != nil || k == nil {
		t.Fatalf("cached: %v %v", k, e)
	}
	if err := a.mutate(ctx, row.KeyHash, func() error { return db.Model(&row).Update("status", 1).Error }); err != nil {
		t.Fatal(err)
	}
	for _, instance := range []*guardedAuthCache{&a, &b} {
		if k, e := instance.lookup(ctx, row.KeyHash); e != nil || k != nil {
			t.Fatalf("disabled: %v %v", k, e)
		}
	}
}
