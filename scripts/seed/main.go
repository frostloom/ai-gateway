// seed 造面板演示数据：多租户 + 数千条历史账单 + M7 套餐/订阅/充值/入账。
// 关键约束：插完后按权威公式重算并 SET Redis balance（initial + Σtopups - Σsettled - Σpending），
// 保证「Redis 投影 == MySQL 账本」——面板、压测断言、对账三口径保持一致。
//
// 用法：
//
//	go run ./scripts/seed -n 5000 -days 7
//	go run ./scripts/seed -n 10000 -reset   # 先清掉上一次的 seed-* 账单再插
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
)

// extraTenants 除 demo（id=1）外按需补充的租户。name → initial_quota（分，×100 = 元）。
var extraTenants = []struct {
	name  string
	quota int64
}{
	{"acme", 10_000},    // ¥100
	{"startup", 50_000}, // ¥500
	{"trial", 2_000},    // ¥20
	{"corp", 30_000},    // ¥300
}

func main() {
	n := flag.Int("n", 5000, "历史请求条数（每个请求 = reserve + settle 两行）")
	days := flag.Int("days", 7, "历史数据跨度（天）")
	tenantCount := flag.Int("tenants", 4, "租户数（含 demo，最多 len(extraTenants)+1）")
	reset := flag.Bool("reset", false, "先删除上次 seed-* 账单再插入")
	flag.Parse()

	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Fatal("connect mysql: ", err)
	}
	rdb := redisx.New(config.Getenv("REDIS_ADDR", "127.0.0.1:6381"), config.Getenv("REDIS_PASSWORD", ""))
	if err := redisx.Ping(context.Background(), rdb); err != nil {
		log.Fatal("connect redis: ", err)
	}
	rand.Seed(time.Now().UnixNano())

	// 1) 租户 + api_key
	tenants := ensureTenants(db, rdb, *tenantCount)
	if *reset {
		if err := db.Where("request_id LIKE 'seed-%'").Delete(&store.Bill{}).Error; err != nil {
			log.Fatal("reset seed bills: ", err)
		}
		// M7：清 seed 造的入账/充值/订阅（套餐目录保留，plan_id 会被场景脚本复用）
		if err := db.Where("ref_id LIKE 'seed:%'").Delete(&store.Topup{}).Error; err != nil {
			log.Fatal("reset seed topups: ", err)
		}
		if err := db.Where("order_no LIKE 'seed-ord-%'").Delete(&store.RechargeOrder{}).Error; err != nil {
			log.Fatal("reset seed recharge orders: ", err)
		}
		if err := db.Where("tenant_id = ?", tenants[0].ID).Delete(&store.Subscription{}).Error; err != nil {
			log.Fatal("reset seed subscriptions: ", err)
		}
		log.Printf("已清理历史 seed-* 账单 / 充值 / 入账 / 订阅")
	}
	// 每个租户至少一个可用 key
	for _, t := range tenants {
		ensureAPIKey(db, t.ID, t.Name)
	}

	// 2) provider id 列表（账单随机引用；providers 表通常有 mock-a/b/c）
	var provIDs []uint64
	db.Model(&store.Provider{}).Pluck("id", &provIDs)

	// 3) 造账单（按目录单价计费：pre = 估算用量×1.2，actual = 实际用量）
	cat := catalogByID()
	now := time.Now()
	all := make([]store.Bill, 0, *n*2)
	for i := 0; i < *n; i++ {
		requestID := fmt.Sprintf("seed-%08d", i)
		tenant := pickTenant(tenants)
		m := pickModel(cat)
		prompt := int64(20 + rand.Intn(200))
		completion := int64(50 + rand.Intn(450))
		pre := quota.ReserveCostCent(prompt, completion, m.InputPriceCent, m.OutputPriceCent)
		actual := quota.CostCent(prompt, completion, m.InputPriceCent, m.OutputPriceCent)
		model := m.ModelID
		inC, outC := m.InputPriceCent, m.OutputPriceCent

		var providerID *uint64
		if len(provIDs) > 0 && rand.Intn(10) > 0 { // 10% 无 provider（异常单）
			pid := provIDs[rand.Intn(len(provIDs))]
			providerID = &pid
		}
		created := now.Add(-time.Duration(rand.Intn(*days*24*60)) * time.Minute)

		roll := rand.Intn(100)
		switch {
		case roll < 95: // settled：reserve + settle 成对
			reported := created.Add(time.Duration(rand.Intn(5000)) * time.Millisecond)
			delta := actual - pre
			all = append(all, store.Bill{
				RequestID: requestID, Phase: store.PhaseReserve, Status: store.StatusSettled,
				TenantID: tenant.ID, Model: model, PreQuota: pre,
				PromptTokens: &prompt, CompletionTokens: &completion,
				InPriceCent: &inC, OutPriceCent: &outC,
				ProviderID: providerID, UsageReportedAt: &reported, CreatedAt: created,
			})
			all = append(all, store.Bill{
				RequestID: requestID, Phase: store.PhaseSettle, Status: store.StatusSettled,
				TenantID: tenant.ID, Model: model, PreQuota: pre,
				ActualQuota: &actual, DeltaQuota: &delta,
				PromptTokens: &prompt, CompletionTokens: &completion,
				InPriceCent: &inC, OutPriceCent: &outC,
				ProviderID: providerID, CreatedAt: reported,
			})
		case roll < 99: // pending：只有 reserve 行（在途，未结算/未冲正）
			all = append(all, store.Bill{
				RequestID: requestID, Phase: store.PhaseReserve, Status: store.StatusPending,
				TenantID: tenant.ID, Model: model, PreQuota: pre,
				ProviderID: providerID, CreatedAt: created,
			})
		default: // reversed：只 reserve 行，已冲正
			all = append(all, store.Bill{
				RequestID: requestID, Phase: store.PhaseReserve, Status: store.StatusReversed,
				TenantID: tenant.ID, Model: model, PreQuota: pre,
				ProviderID: providerID, ErrorCode: "seeded_reversed", CreatedAt: created,
			})
		}
	}
	if err := db.CreateInBatches(all, 1000).Error; err != nil {
		log.Fatal("insert bills: ", err)
	}

	// 3.5) M7 种子：套餐目录 + demo 的订阅/充值/入账（幂等：已存在则跳过）
	demoTenantID := tenants[0].ID
	seedM7(db, demoTenantID)

	// 4) 按权威公式重建并 SET Redis 余额（保持投影 == 账本，含 M7 的 Σtopups 正项）
	ctx := context.Background()
	for _, t := range tenants {
		var settled, pending, topups int64
		db.Model(&store.Bill{}).
			Where("tenant_id = ? AND phase = ? AND status = ?", t.ID, store.PhaseSettle, store.StatusSettled).
			Select("COALESCE(SUM(actual_quota),0)").Scan(&settled)
		db.Model(&store.Bill{}).
			Where("tenant_id = ? AND phase = ? AND status = ?", t.ID, store.PhaseReserve, store.StatusPending).
			Select("COALESCE(SUM(pre_quota),0)").Scan(&pending)
		db.Model(&store.Topup{}).
			Where("tenant_id = ?", t.ID).
			Select("COALESCE(SUM(amount),0)").Scan(&topups)
		balance := t.InitialQuota + topups - settled - pending
		if err := rdb.Set(ctx, store.BalanceKey(t.ID), balance, 0).Err(); err != nil {
			log.Fatal("set redis balance: ", err)
		}
		log.Printf("tenant #%d %-8s initial=%-10d topups=%-9d settled=%-10d pending=%-8d balance=%d",
			t.ID, t.Name, t.InitialQuota, topups, settled, pending, balance)
	}
	fmt.Printf("\n✅ 完成：%d 条历史请求（%d 行账单）+ M7 套餐/订阅/充值已写入，Redis 余额已按账本重算一致。\n", *n, len(all))
	fmt.Printf("打开 http://localhost:18080/ 看面板（admin token 见 gateway 日志 / 默认 admin-demo）。\n")
}

// ---------- 租户 / key ----------

type tenantRow struct {
	ID           uint64
	Name         string
	InitialQuota int64
}

func ensureTenants(db *gorm.DB, rdb *goredis.Client, count int) []tenantRow {
	var demo store.Tenant
	err := db.Where("name = ?", "demo").First(&demo).Error
	if err == gorm.ErrRecordNotFound {
		demo = store.Tenant{Name: "demo", InitialQuota: 5_000} // ¥50
		if err := db.Create(&demo).Error; err != nil {
			log.Fatal("create demo tenant: ", err)
		}
	}
	out := []tenantRow{{demo.ID, demo.Name, demo.InitialQuota}}

	for _, e := range extraTenants[:min(count-1, len(extraTenants))] {
		var t store.Tenant
		if err := db.Where("name = ?", e.name).First(&t).Error; err == gorm.ErrRecordNotFound {
			t = store.Tenant{Name: e.name, InitialQuota: e.quota}
			if err := db.Create(&t).Error; err != nil {
				log.Fatal("create tenant: ", err)
			}
			// 新租户补 Redis 余额 key（billing 未跑时这里就要建好，否则面板 balance 无投影可比）
			if err := rdb.Set(context.Background(), store.BalanceKey(t.ID), e.quota, 0).Err(); err != nil {
				log.Fatal("set balance key: ", err)
			}
		}
		out = append(out, tenantRow{t.ID, t.Name, t.InitialQuota})
	}
	return out
}

func ensureAPIKey(db *gorm.DB, tenantID uint64, tenantName string) {
	var count int64
	db.Model(&store.APIKey{}).Where("tenant_id = ?", tenantID).Count(&count)
	if count > 0 {
		return
	}
	plain := fmt.Sprintf("sk-seed-%s-%s", tenantName, randHex(8))
	hash := sha256Hex(plain)
	key := store.APIKey{
		TenantID: tenantID, KeyHash: hash, KeyPrefix: plain[:8], Name: tenantName + "-key",
	}
	if err := db.Create(&key).Error; err != nil {
		log.Fatal("create api key: ", err)
	}
	log.Printf("  新建 key %s（明文 %s，仅这里展示）", key.KeyPrefix+"…", plain)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = "0123456789abcdef"[rand.Intn(16)]
	}
	return string(b)
}

// ---------- M7 种子：套餐 / 订阅 / 充值 / 入账 ----------

// seedM7 幂等造 M9 演示数据：
//   - 2 个定期订阅套餐（GPT Plus 式：月费 + 每 5h 滚动各档次 token 额度），按 name upsert（更新额度/月费）。
//   - demo 不预置订阅（订阅走 portal 现场开通；否则 loadtest 打 demo 会打爆额度 → 402 FAIL）。
//   - demo 保留一笔已支付充值（余额故事）及其入账流水。
func seedM7(db *gorm.DB, demoID uint64) {
	// M9 目录 = 纯订阅：月度·标准 ¥80/5h、月度·旗舰 ¥300/5h（token/窗口）。
	seedPlans := []store.Plan{
		{
			Name: "月度·标准", PlanType: store.PlanTypeRecurring, PriceMoney: 80, ValidityDays: 30, RefreshHours: 5,
			TierQuota: `{"轻量":2000000,"文本":1000000,"长文本":600000,"视觉":300000,"推理":500000,"旗舰":200000}`,
		},
		{
			Name: "月度·旗舰", PlanType: store.PlanTypeRecurring, PriceMoney: 300, ValidityDays: 30, RefreshHours: 5,
			TierQuota: `{"轻量":8000000,"文本":4000000,"长文本":2500000,"视觉":1200000,"推理":2000000,"旗舰":800000}`,
		},
	}
	for i := range seedPlans {
		p := &seedPlans[i]
		var existing store.Plan
		if err := db.Where("name = ?", p.Name).First(&existing).Error; err == nil {
			// upsert：月费/周期/窗口/额度以新口径为准，重新启用。
			if err := db.Model(&store.Plan{}).Where("id = ?", existing.ID).Updates(map[string]any{
				"plan_type": p.PlanType, "price_money": p.PriceMoney, "validity_days": p.ValidityDays,
				"refresh_hours": p.RefreshHours, "tier_quota": p.TierQuota, "status": 0, "updated_at": time.Now(),
			}).Error; err != nil {
				log.Fatal("upsert plan: ", err)
			}
			log.Printf("  更新套餐 #%d %s（月费 ¥%d · 每 %d 小时刷新额度）", existing.ID, p.Name, p.PriceMoney, p.RefreshHours)
			continue
		}
		if err := db.Create(p).Error; err != nil {
			log.Fatal("create plan: ", err)
		}
		log.Printf("  新建套餐 #%d %s（月费 ¥%d · 每 %d 小时刷新额度）", p.ID, p.Name, p.PriceMoney, p.RefreshHours)
	}

	now := time.Now()

	// M9：一次性买断停售——下架目录里所有 one_time 套餐（订阅拒绝逻辑保留，历史订阅引用不破坏）。
	if err := db.Model(&store.Plan{}).Where("plan_type = ?", store.PlanTypeOneTime).Update("status", 1).Error; err != nil {
		log.Fatal("disable one_time plans: ", err)
	}

	// M9：demo 不再预置订阅——清掉历史 subscription 及其订阅入账（保持「充值余额」故事干净）。
	if err := db.Where("tenant_id = ?", demoID).Delete(&store.Subscription{}).Error; err != nil {
		log.Fatal("clear demo subscriptions: ", err)
	}
	if err := db.Where("tenant_id = ? AND source = ?", demoID, store.TopupSubscription).Delete(&store.Topup{}).Error; err != nil {
		log.Fatal("clear demo subscription topups: ", err)
	}

	// demo 充值：一笔已支付的 ¥100（= 10000 分），带入账流水
	var ordCount int64
	db.Model(&store.RechargeOrder{}).Where("tenant_id = ?", demoID).Count(&ordCount)
	if ordCount == 0 {
		paidAt := now.Add(-48 * time.Hour)
		ord := store.RechargeOrder{
			TenantID: demoID, OrderNo: "seed-ord-0001",
			AmountMoney: 100, AmountCent: 10_000, Status: store.RechargePaid,
			PaidAt: &paidAt, CreatedAt: paidAt, UpdatedAt: paidAt,
		}
		if err := db.Create(&ord).Error; err != nil {
			log.Fatal("create demo recharge order: ", err)
		}
		if err := db.Create(&store.Topup{
			TenantID: demoID, Source: store.TopupRecharge, Amount: 10_000,
			RefType: store.TopupRecharge, RefID: "seed:recharge:1", CreatedAt: paidAt,
		}).Error; err != nil {
			log.Fatal("create demo recharge topup: ", err)
		}
		log.Printf("  demo 新建充值订单 seed-ord-0001（¥100 = 10000 分，已支付）")
	}
}

// ---------- 随机取值 ----------

func pickTenant(ts []tenantRow) tenantRow {
	// 权重：demo 占一半（演示时它最忙），其余均分
	if len(ts) > 1 && rand.Intn(2) == 0 {
		return ts[1+rand.Intn(len(ts)-1)]
	}
	return ts[0]
}

// catalogByID 目录 model_id → Model（计费种子用）。
func catalogByID() map[string]store.Model {
	out := make(map[string]store.Model, len(store.DefaultModels()))
	for _, m := range store.DefaultModels() {
		out[m.ModelID] = m
	}
	return out
}

// pickModel 从目录加权取一个模型：轻量/免费的多点（更真实），旗舰偶尔用。
func pickModel(cat map[string]store.Model) store.Model {
	byName := make([]store.Model, 0, len(cat))
	for _, m := range cat {
		byName = append(byName, m)
	}
	// 简单加权：免费与轻量模型权重高
	total := 0
	for _, m := range byName {
		w := 1
		switch {
		case m.InputPriceCent == 0:
			w = 8
		case m.InputPriceCent <= 100:
			w = 4
		case m.InputPriceCent >= 1000:
			w = 1
		default:
			w = 2
		}
		total += w
	}
	r := rand.Intn(total)
	for _, m := range byName {
		w := 1
		switch {
		case m.InputPriceCent == 0:
			w = 8
		case m.InputPriceCent <= 100:
			w = 4
		case m.InputPriceCent >= 1000:
			w = 1
		default:
			w = 2
		}
		if r < w {
			return m
		}
		r -= w
	}
	return byName[0]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
