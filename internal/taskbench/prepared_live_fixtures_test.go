package taskbench

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise the real fixture backend against a disposable MySQL database and
// private Redis double. No agent, model, JEV, developer DB or shared Redis DB.
func TestPreparedDefinitionsInstantiateIsolatedLiveAccounts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("mysql", "root:root@tcp(127.0.0.1:3307)/?timeout=2s&readTimeout=5s&writeTimeout=5s")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		t.Skipf("isolated MySQL test infrastructure unavailable: %v", err)
	}
	// This immutable generated name is the only database this test creates/drops.
	name := "ai_gateway_fixture_test_" + randomID()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		// Deferred closes have already run, so use a new cleanup connection.
		conn, err := sql.Open("mysql", "root:root@tcp(127.0.0.1:3307)/?timeout=2s")
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if _, err := conn.ExecContext(cleanup, "DROP DATABASE "+name); err != nil {
			t.Error(err)
		}
	})
	db, err := gorm.Open(mysql.Open("root:root@tcp(127.0.0.1:3307)/"+name+"?parseTime=True&loc=Local&timeout=2s&readTimeout=5s&writeTimeout=5s"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(4)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	st := store.NewStore(db, rdb)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []store.Plan{{Name: "月度·标准", PlanType: store.PlanTypeRecurring, PriceMoney: 20, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`}, {Name: "月度·旗舰", PlanType: store.PlanTypeRecurring, PriceMoney: 50, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`}} {
		if err := db.Create(&plan).Error; err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../../eval/task-suites/2026-10-11-read-fixtures-dev-v1/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	backend := &LiveBackend{db: db, rdb: rdb, st: st}
	seen := map[uint64]bool{}
	for _, c := range cases {
		f, err := backend.Setup(ctx, "offline-fixture", c)
		if err != nil {
			t.Fatalf("%s setup: %v", c.ID, err)
		}
		if seen[f.TenantID] || f.TenantID == 0 || f.APIKey == "" {
			t.Fatalf("%s lacks independent tenant/key", c.ID)
		}
		seen[f.TenantID] = true
		snap, err := backend.Snapshot(ctx, f, "")
		if err != nil {
			t.Fatalf("%s snapshot: %v", c.ID, err)
		}
		if !snap.Consistent || snap.BalanceCent != 500000 || snap.LedgerBalance != 500000 || len(snap.Orders) != 1 || snap.Orders[0].Status != store.RechargePaid || snap.Orders[0].TenantID != f.TenantID {
			t.Fatalf("%s seed state incorrect: %+v", c.ID, snap)
		}
		want := 0
		if c.InitialSubscription {
			want = 1
		}
		if len(snap.Subscriptions) != want {
			t.Fatalf("%s subscription count=%d want%d", c.ID, len(snap.Subscriptions), want)
		}
		if want == 1 && (snap.Subscriptions[0].Status != store.SubActive || snap.Subscriptions[0].PlanID != f.Standard.ID || snap.Subscriptions[0].TenantID != f.TenantID) {
			t.Fatal("wrong tenant/plan/status")
		}
	}
	if len(seen) != 214 {
		t.Fatal(fmt.Sprintf("validated isolated tenants=%d want214", len(seen)))
	}
	t.Log("Validated 214 disposable synthetic accounts; no model/JEV calls; database removed on cleanup")
}
