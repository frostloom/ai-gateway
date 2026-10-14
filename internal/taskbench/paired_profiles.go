package taskbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"time"
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

// PairedProfiles is loaded from pinned development fixture bytes, not provider prices.
type PairedProfiles struct {
	archive runtimeProfileArchive
	SHA256  string
}

func LoadPairedProfiles(dir string) (PairedProfiles, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if err != nil {
		return PairedProfiles{}, err
	}
	const pinned = "83a78d7492c75e0d153cbb9ca4a1a87aa2f15714ad1e855534a084f4848e257d"
	if conversationHash(raw) != pinned {
		return PairedProfiles{}, fmt.Errorf("runtime profile archive changed")
	}
	var p runtimeProfileArchive
	if err = json.Unmarshal(raw, &p); err != nil || p.Schema != "customer-runtime-profiles.v1" || len(p.Accounts) != 6 {
		return PairedProfiles{}, fmt.Errorf("unsupported runtime profiles")
	}
	return PairedProfiles{p, pinned}, nil
}
func seedPairedCatalogue(ctx context.Context, db *gorm.DB, p runtimeProfileArchive) (map[string]store.Plan, error) {
	plans := map[string]store.Plan{}
	for _, name := range []string{"standard", "flagship"} {
		c := p.Catalogue.Plans[name]
		quota, _ := json.Marshal(c.Quota)
		plan := store.Plan{Name: c.Name, PriceMoney: c.Price, ValidityDays: c.Days, RefreshHours: c.Hours, TierQuota: string(quota), PlanType: store.PlanTypeRecurring}
		if err := db.WithContext(ctx).Create(&plan).Error; err != nil {
			return nil, err
		}
		plans[name] = plan
	}
	for _, name := range []string{"model-a", "model-b"} {
		c := p.Catalogue.Models[name]
		tags, _ := json.Marshal(c.Tags)
		m := store.Model{ModelID: name, Name: c.Name, Vendor: c.Vendor, Tags: string(tags), ContextLen: c.Context, InputPriceCent: c.In * 100, OutputPriceCent: c.Out * 100}
		if err := db.WithContext(ctx).Create(&m).Error; err != nil {
			return nil, err
		}
	}
	return plans, nil
}

func instantiatePairedAccount(ctx context.Context, db *gorm.DB, rdb *redis.Client, st *store.Store, p runtimeAccount, plans map[string]store.Plan, clock time.Time) (Fixture, uint64, error) {
	tenant := store.Tenant{Name: "runtime-" + randomID(), InitialQuota: p.Initial}
	if err := db.WithContext(ctx).Create(&tenant).Error; err != nil {
		return Fixture{}, 0, err
	}
	key := "sk-runtime-" + randomID() + randomID()
	h := sha256.Sum256([]byte(key))
	expiry := clock.Add(time.Hour)
	api := store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(h[:]), KeyPrefix: key[:8], Name: "isolated runtime fixture", ExpiresAt: &expiry}
	if err := db.WithContext(ctx).Create(&api).Error; err != nil {
		return Fixture{}, 0, err
	}
	if err := rdb.Set(ctx, store.BalanceKey(tenant.ID), p.Initial, 0).Err(); err != nil {
		return Fixture{}, 0, err
	}
	order, err := st.CreateRechargeOrder(ctx, tenant.ID, p.Order.AmountMoney, "runtime-"+randomID())
	if err != nil {
		return Fixture{}, 0, err
	}
	if _, err := st.PayRecharge(ctx, order.OrderNo); err != nil {
		return Fixture{}, 0, err
	}
	if p.Subscription != nil {
		sub, err := st.SubscribePlan(ctx, tenant.ID, plans[p.Subscription.Plan].ID)
		if err != nil {
			return Fixture{}, 0, err
		}
		start := clock.Add(-24 * time.Hour)
		end := start.Add(30 * 24 * time.Hour)
		if err := db.Model(sub).Updates(map[string]any{"cycle_start": start, "cycle_end": end}).Error; err != nil {
			return Fixture{}, 0, err
		}
		if p.Subscription.Status == store.SubCancelled {
			if _, err := st.CancelSubscription(ctx, tenant.ID); err != nil {
				return Fixture{}, 0, err
			}
		}
	}
	insertBill := func(b runtimeBill, created time.Time, phase string) error {
		req := "rt-" + tenant.Name[len("runtime-"):] + "-" + b.Request
		row := store.Bill{TenantID: tenant.ID, APIKeyID: api.ID, RequestID: req, Phase: phase, Status: store.StatusSettled, Model: b.Model, Funding: b.Funding, ActualQuota: &b.Amount, PromptTokens: &b.Input, CompletionTokens: &b.Output, CreatedAt: created}
		if err := db.WithContext(ctx).Create(&row).Error; err != nil {
			return err
		}
		return nil
	}
	for _, b := range p.Bills {
		if err := insertBill(b, clock.Add(-time.Duration(b.AgeHours)*time.Hour), store.PhaseSettle); err != nil {
			return Fixture{}, 0, err
		}
	}
	if p.Subscription != nil {
		u := p.Usage
		created := clock.Add(-time.Duration(u.AgeMinutes) * time.Minute)
		if err := insertBill(u.runtimeBill, created, store.PhaseReserve); err != nil {
			return Fixture{}, 0, err
		}
		if err := insertBill(u.runtimeBill, created, store.PhaseSettle); err != nil {
			return Fixture{}, 0, err
		}
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		return Fixture{}, 0, err
	}
	if ledger != p.Balance {
		return Fixture{}, 0, fmt.Errorf("seed ledger does not match profile")
	}
	if err := rdb.Set(ctx, store.BalanceKey(tenant.ID), ledger, 0).Err(); err != nil {
		return Fixture{}, 0, err
	}
	foreign := store.Tenant{Name: "foreign-" + randomID(), InitialQuota: 113400}
	if err := db.Create(&foreign).Error; err != nil {
		return Fixture{}, 0, err
	}
	if err := rdb.Set(ctx, store.BalanceKey(foreign.ID), 113400, 0).Err(); err != nil {
		return Fixture{}, 0, err
	}
	foreignOrder, err := st.CreateRechargeOrder(ctx, foreign.ID, 100, "foreign-"+randomID())
	if err != nil {
		return Fixture{}, 0, err
	}
	if _, err := st.PayRecharge(ctx, foreignOrder.OrderNo); err != nil {
		return Fixture{}, 0, err
	}
	return Fixture{TenantID: tenant.ID, TenantName: tenant.Name, APIKey: key, OrderNo: order.OrderNo, Standard: plans["standard"], Flagship: plans["flagship"]}, foreign.ID, nil
}
