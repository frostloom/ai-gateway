package taskbench

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func loadRuntimeProfileArchive(t *testing.T) runtimeProfileArchive {
	t.Helper()
	base := "../../eval/runtime-profiles/2026-10-13-six-profiles-v1/"
	raw, err := os.ReadFile(base + "profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := os.ReadFile(base + "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if manifest.Files["profiles.json"] != hex.EncodeToString(hash[:]) {
		t.Fatal("profile bytes changed")
	}
	var p runtimeProfileArchive
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.Schema != "customer-runtime-profiles.v1" || len(p.Accounts) != 6 {
		t.Fatal("unsupported runtime profile archive")
	}
	return p
}

func runtimeProfileTestDatabase(t *testing.T) (*gorm.DB, *redis.Client, *store.Store) {
	t.Helper()
	const adminDSN = "root:root@tcp(127.0.0.1:3307)/?timeout=2s&readTimeout=5s&writeTimeout=5s"
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Skipf("local MySQL test infrastructure unavailable: %v", err)
	}
	name := "ai_gateway_runtime_profile_test_" + randomID()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn, err := sql.Open("mysql", adminDSN)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, err := conn.ExecContext(cleanup, "DROP DATABASE "+name); err != nil {
			t.Error(err)
		}
		var remaining int
		if err := conn.QueryRowContext(cleanup, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("cleanup not verified: remaining=%d error=%v", remaining, err)
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
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	st := store.NewStore(db, rdb)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	return db, rdb, st
}

func seedRuntimeCatalogue(t *testing.T, db *gorm.DB, p runtimeProfileArchive) map[string]store.Plan {
	t.Helper()
	plans, err := seedPairedCatalogue(context.Background(), db, p)
	if err != nil {
		t.Fatal(err)
	}
	return plans
}
func instantiateRuntimeAccount(t *testing.T, ctx context.Context, db *gorm.DB, rdb *redis.Client, st *store.Store, name string, p runtimeAccount, plans map[string]store.Plan, clock time.Time) (Fixture, uint64) {
	t.Helper()
	f, foreign, err := instantiatePairedAccount(ctx, db, rdb, st, p, plans, clock)
	if err != nil {
		t.Fatal(err)
	}
	return f, foreign
}

func verifyRuntimeReads(t *testing.T, ctx context.Context, db *gorm.DB, st *store.Store, f Fixture, p runtimeAccount, plans map[string]store.Plan, clock time.Time) {
	t.Helper()
	view, err := st.MeOverview(ctx, f.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, d := range view.Consumption {
		total += d.Tokens
	}
	if total != 250 {
		t.Fatalf("cash consumption=%d want250", total)
	}
	for _, n := range []int{3, 10} {
		bills, count, err := st.ListBills(ctx, f.TenantID, store.PhaseSettle, store.StatusSettled, n, 0)
		if err != nil {
			t.Fatal(err)
		}
		expected := p.Expected.Bills.Latest3
		if n == 10 {
			expected = p.Expected.Bills.Latest10
		}
		if len(bills) != len(expected) || count != int64(len(p.Expected.Bills.Latest10)) {
			t.Fatal("bill count mismatch")
		}
		for i, b := range bills {
			e := expected[i]
			if !strings.HasSuffix(b.RequestID, "-"+e.Request) || b.TenantID != f.TenantID || b.Model != e.Model || b.Funding != e.Funding || b.ActualQuota == nil || *b.ActualQuota != e.Amount || b.PromptTokens == nil || *b.PromptTokens != e.Input || b.CompletionTokens == nil || *b.CompletionTokens != e.Output {
				t.Fatalf("bill %d does not match profile", i)
			}
			want := clock.Add(-time.Duration(e.AgeHours) * time.Hour)
			if e.Request == "subscription-recent" {
				want = clock.Add(-30 * time.Minute)
			}
			if b.CreatedAt.Sub(want) > time.Second || want.Sub(b.CreatedAt) > time.Second {
				t.Fatal("bill timestamp mismatch")
			}
		}
	}
	if p.Subscription == nil {
		if view.Subscription != nil || len(view.Allowance) != 0 {
			t.Fatal("unexpected subscription/allowance")
		}
		return
	}
	rows, err := st.ListSubscriptions(ctx, f.TenantID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("persisted subscription count mismatch: %v", err)
	}
	s := &rows[0]
	if s == nil || s.TenantID != f.TenantID || s.PlanID != plans[p.Subscription.Plan].ID || s.Status != p.Subscription.Status || s.AutoRenew != p.Subscription.AutoRenew || s.PendingPlanID != nil || s.CycleEnd == nil || s.CycleStart == nil {
		t.Fatal("subscription identity/state mismatch")
	}
	wantStart := clock.Add(-24 * time.Hour)
	wantEnd := wantStart.Add(30 * 24 * time.Hour)
	if s.CycleStart.Sub(wantStart) > time.Second || wantStart.Sub(*s.CycleStart) > time.Second || s.CycleEnd.Sub(wantEnd) > time.Second || wantEnd.Sub(*s.CycleEnd) > time.Second {
		t.Fatal("cycle boundaries mismatch")
	}
	plan := plans[p.Subscription.Plan]
	quotas := plan.TierQuotaMap()
	allowance := view.Allowance
	if view.Subscription == nil || view.Subscription.ID != s.ID {
		t.Fatal("current-cycle subscription missing from real read projection")
	}
	seen := map[string]bool{}
	for _, a := range allowance {
		used := int64(0)
		if a.Tier == "文本" {
			used = 3000
		}
		if a.Quota != quotas[a.Tier] || a.Consumed != used || a.Remaining != a.Quota-used {
			t.Fatal("window allowance mismatch or reserve/settle counted twice")
		}
		seen[a.Tier] = true
	}
	if len(seen) != len(quotas) || view.RefreshHours != 5 {
		t.Fatal("missing tier allowance")
	}
}
