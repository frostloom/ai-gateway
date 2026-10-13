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

type runtimeBill struct {
	Request  string `json:"request_id"`
	AgeHours int    `json:"age_hours"`
	Model    string `json:"model"`
	Amount   int64  `json:"amount_cent"`
	Funding  string `json:"funding"`
	Input    int64  `json:"input_tokens"`
	Output   int64  `json:"output_tokens"`
}
type runtimeAccount struct {
	Balance int64 `json:"balance_cent"`
	Initial int64 `json:"initial_quota_cent"`
	Order   struct {
		AmountMoney int64 `json:"amount_money"`
		AmountCent  int64 `json:"amount_cent"`
	} `json:"paid_order"`
	Subscription *struct {
		Plan      string `json:"plan"`
		Status    string `json:"status"`
		AutoRenew bool   `json:"auto_renew"`
	} `json:"subscription"`
	Bills []runtimeBill `json:"cash_bill_fixture"`
	Usage struct {
		runtimeBill
		AgeMinutes int `json:"age_minutes"`
	} `json:"subscription_usage_fixture"`
	Expected struct {
		Bills struct {
			Latest3  []runtimeBill `json:"latest_3"`
			Latest10 []runtimeBill `json:"latest_10"`
		} `json:"get_recent_bills"`
	} `json:"tool_expected"`
}
type runtimeProfileArchive struct {
	Schema    string                    `json:"schema"`
	Accounts  map[string]runtimeAccount `json:"accounts"`
	Catalogue struct {
		Plans map[string]struct {
			Name  string           `json:"name"`
			Price int64            `json:"price_yuan"`
			Days  int              `json:"validity_days"`
			Hours int              `json:"refresh_hours"`
			Quota map[string]int64 `json:"tier_quota_tokens"`
		} `json:"plans"`
		Models map[string]struct {
			Name    string   `json:"name"`
			Vendor  string   `json:"vendor"`
			Tags    []string `json:"tags"`
			Context int      `json:"context_tokens"`
			In      int64    `json:"input_yuan_per_million"`
			Out     int64    `json:"output_yuan_per_million"`
		} `json:"models"`
	} `json:"catalogue"`
}

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
	plans := map[string]store.Plan{}
	for _, name := range []string{"standard", "flagship"} {
		c := p.Catalogue.Plans[name]
		quota, _ := json.Marshal(c.Quota)
		plan := store.Plan{Name: c.Name, PriceMoney: c.Price, ValidityDays: c.Days, RefreshHours: c.Hours, TierQuota: string(quota), PlanType: store.PlanTypeRecurring}
		if err := db.Create(&plan).Error; err != nil {
			t.Fatal(err)
		}
		plans[name] = plan
	}
	for _, name := range []string{"model-a", "model-b"} {
		c := p.Catalogue.Models[name]
		tags, _ := json.Marshal(c.Tags)
		m := store.Model{ModelID: name, Name: c.Name, Vendor: c.Vendor, Tags: string(tags), ContextLen: c.Context, InputPriceCent: c.In * 100, OutputPriceCent: c.Out * 100}
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	return plans
}

func instantiateRuntimeAccount(t *testing.T, ctx context.Context, db *gorm.DB, rdb *redis.Client, st *store.Store, name string, p runtimeAccount, plans map[string]store.Plan, clock time.Time) (Fixture, uint64) {
	t.Helper()
	tenant := store.Tenant{Name: "runtime-" + randomID(), InitialQuota: p.Initial}
	if err := db.WithContext(ctx).Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := "sk-runtime-" + randomID() + randomID()
	h := sha256.Sum256([]byte(key))
	expiry := clock.Add(time.Hour)
	api := store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(h[:]), KeyPrefix: key[:8], Name: "isolated runtime fixture", ExpiresAt: &expiry}
	if err := db.WithContext(ctx).Create(&api).Error; err != nil {
		t.Fatal(err)
	}
	if err := rdb.Set(ctx, store.BalanceKey(tenant.ID), p.Initial, 0).Err(); err != nil {
		t.Fatal(err)
	}
	order, err := st.CreateRechargeOrder(ctx, tenant.ID, p.Order.AmountMoney, "runtime-"+randomID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PayRecharge(ctx, order.OrderNo); err != nil {
		t.Fatal(err)
	}
	if p.Subscription != nil {
		sub, err := st.SubscribePlan(ctx, tenant.ID, plans[p.Subscription.Plan].ID)
		if err != nil {
			t.Fatal(err)
		}
		start := clock.Add(-24 * time.Hour)
		end := start.Add(30 * 24 * time.Hour)
		if err := db.Model(sub).Updates(map[string]any{"cycle_start": start, "cycle_end": end}).Error; err != nil {
			t.Fatal(err)
		}
		if p.Subscription.Status == store.SubCancelled {
			if _, err := st.CancelSubscription(ctx, tenant.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	insertBill := func(b runtimeBill, created time.Time, phase string) {
		req := "rt-" + tenant.Name[len("runtime-"):] + "-" + b.Request
		row := store.Bill{TenantID: tenant.ID, APIKeyID: api.ID, RequestID: req, Phase: phase, Status: store.StatusSettled, Model: b.Model, Funding: b.Funding, ActualQuota: &b.Amount, PromptTokens: &b.Input, CompletionTokens: &b.Output, CreatedAt: created}
		if err := db.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range p.Bills {
		insertBill(b, clock.Add(-time.Duration(b.AgeHours)*time.Hour), store.PhaseSettle)
	}
	if p.Subscription != nil {
		u := p.Usage
		created := clock.Add(-time.Duration(u.AgeMinutes) * time.Minute)
		insertBill(u.runtimeBill, created, store.PhaseReserve)
		insertBill(u.runtimeBill, created, store.PhaseSettle)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ledger != p.Balance {
		t.Fatalf("seed ledger=%d want%d", ledger, p.Balance)
	}
	if err := rdb.Set(ctx, store.BalanceKey(tenant.ID), ledger, 0).Err(); err != nil {
		t.Fatal(err)
	}
	foreign := store.Tenant{Name: "foreign-" + randomID(), InitialQuota: 113400}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	if err := rdb.Set(ctx, store.BalanceKey(foreign.ID), 113400, 0).Err(); err != nil {
		t.Fatal(err)
	}
	foreignOrder, err := st.CreateRechargeOrder(ctx, foreign.ID, 100, "foreign-"+randomID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PayRecharge(ctx, foreignOrder.OrderNo); err != nil {
		t.Fatal(err)
	}
	return Fixture{TenantID: tenant.ID, TenantName: tenant.Name, APIKey: key, OrderNo: order.OrderNo, Standard: plans["standard"], Flagship: plans["flagship"]}, foreign.ID
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
