// m7 场景前置：独立场景租户 + 套餐目录（M9 纯订阅）+ 场景 API key + 清理上次残留，输出 TID/REC1/REC2/TEST/SK。
// 用独立租户避免污染 demo 种子数据；plan 按 name 幂等复用 seed 同名套餐，保证 plan_id 稳定。
//
//	go run ./scripts/m7/setup.go   →   TID=2 REC1=2 REC2=3 TEST=4 SK=sk-m7-xxxxxxxx
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
)

func main() {
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect mysql: ", err)
		os.Exit(1)
	}
	rdb := redisx.New(config.Getenv("REDIS_ADDR", "127.0.0.1:6381"), config.Getenv("REDIS_PASSWORD", ""))
	if err := redisx.Ping(context.Background(), rdb); err != nil {
		fmt.Fprintln(os.Stderr, "connect redis: ", err)
		os.Exit(1)
	}
	ctx := context.Background()

	// 1) 确保 3 个定期订阅套餐（与 seed 同名幂等复用；TEST=场景·限量 专供快速超额）
	plans := []store.Plan{
		{
			Name: "月度·标准", PlanType: store.PlanTypeRecurring, PriceMoney: 80, ValidityDays: 30, RefreshHours: 5,
			TierQuota: `{"轻量":2000000,"文本":1000000,"长文本":600000,"视觉":300000,"推理":500000,"旗舰":200000}`,
		},
		{
			Name: "月度·旗舰", PlanType: store.PlanTypeRecurring, PriceMoney: 300, ValidityDays: 30, RefreshHours: 5,
			TierQuota: `{"轻量":8000000,"文本":4000000,"长文本":2500000,"视觉":1200000,"推理":2000000,"旗舰":800000}`,
		},
		{
			Name: "场景·限量", PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5,
			TierQuota: `{"文本":300}`, // 每 5h 仅 300 token，用于快速触发超额（④）
		},
	}
	ids := make([]uint64, len(plans))
	for i := range plans {
		p := &plans[i]
		var ex store.Plan
		if err := db.Where("name = ?", p.Name).First(&ex).Error; err == nil {
			// upsert：以新口径刷新（月费/周期/窗口/额度/重新启用）
			if err := db.Model(&store.Plan{}).Where("id = ?", ex.ID).Updates(map[string]any{
				"plan_type": p.PlanType, "price_money": p.PriceMoney, "validity_days": p.ValidityDays,
				"refresh_hours": p.RefreshHours, "tier_quota": p.TierQuota, "status": 0,
			}).Error; err != nil {
				fmt.Fprintln(os.Stderr, "upsert plan: ", err)
				os.Exit(1)
			}
			ids[i] = ex.ID
			continue
		}
		if err := db.Create(p).Error; err != nil {
			fmt.Fprintln(os.Stderr, "create plan: ", err)
			os.Exit(1)
		}
		ids[i] = p.ID
	}
	rec1, rec2, testPlan := ids[0], ids[1], ids[2]

	// M9：一次性买断停售——下架目录里所有 one_time 套餐，保证场景目录 = 纯订阅。
	if err := db.Model(&store.Plan{}).Where("plan_type = ?", store.PlanTypeOneTime).Update("status", 1).Error; err != nil {
		fmt.Fprintln(os.Stderr, "disable one_time plans: ", err)
		os.Exit(1)
	}

	// 2) 场景租户（initial=0；Redis 投影归零，与账本一致）
	var t store.Tenant
	if err := db.Where("name = ?", "m7scenario").First(&t).Error; err == gorm.ErrRecordNotFound {
		t = store.Tenant{Name: "m7scenario", InitialQuota: 0}
		if err := db.Create(&t).Error; err != nil {
			fmt.Fprintln(os.Stderr, "create tenant: ", err)
			os.Exit(1)
		}
	}
	// 清理上次残留：订阅/订单/入账/账单/旧 key（保证窗口消费与余额口径干净）
	for _, m := range []any{&store.Subscription{}, &store.RechargeOrder{}, &store.Topup{}, &store.Bill{}, &store.APIKey{}} {
		if err := db.Where("tenant_id = ?", t.ID).Delete(m).Error; err != nil {
			fmt.Fprintln(os.Stderr, "clean residual: ", err)
			os.Exit(1)
		}
	}
	if err := rdb.Set(ctx, store.BalanceKey(t.ID), 0, 0).Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reset redis balance: ", err)
		os.Exit(1)
	}

	// 3) 场景 API key（明文只在 setup 输出打印一次，落库只存 SHA-256 + 前 8 位前缀）
	key := "sk-m7-" + randHex(8)
	sum := sha256.Sum256([]byte(key))
	k := store.APIKey{
		TenantID: t.ID, KeyHash: hex.EncodeToString(sum[:]),
		KeyPrefix: key[:8], Name: "m7-scenario-key", Status: 0,
	}
	if err := db.Create(&k).Error; err != nil {
		fmt.Fprintln(os.Stderr, "create api key: ", err)
		os.Exit(1)
	}
	fmt.Printf("TID=%d REC1=%d REC2=%d TEST=%d SK=%s\n", t.ID, rec1, rec2, testPlan, key)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
