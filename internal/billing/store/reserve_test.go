package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/pkg/config"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// 并发不超卖测试必须在真 Redis + 真 MySQL 上跑（miniredis 单线程测不出真竞态）。
// 关键隔离决策：测试用 Redis DB 1（生产/开发用 DB 0）+ 独立 MySQL 库。
// 否则 cleanRedisKeys 清掉 billing:* 时会误删开发环境的 billing:balance:1（两边租户 id=1 撞 key）。
const (
	testRedisAddr = "127.0.0.1:6381"
	testRedisDB   = 1
	testMySQLHost = "root:root@tcp(127.0.0.1:3307)/"
	testMySQLDB   = "ai_gateway_test"
)

// newTestStore 连真实基础设施，返回隔离的测试库。
func newTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	// 确保测试库存在（不污染开发库 ai_gateway）
	admin, err := sql.Open("mysql", testMySQLHost)
	if err != nil {
		t.Fatalf("open admin mysql: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + testMySQLDB + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create test db: %v", err)
	}
	admin.Close()

	rdb := goredis.NewClient(&goredis.Options{Addr: config.Getenv("REDIS_ADDR", testRedisAddr), DB: testRedisDB})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", testRedisAddr, err)
	}

	dsn := testMySQLHost + testMySQLDB + "?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	// 并发测试 100 goroutine 同开连接 + 本机在跑的 billing/router 也占着连接，
	// 会把 MySQL 默认 max_connections=151 打满（Error 1040）。给测试池设上限
	// 让并发排队而不是报错——生产 cmd/billing 也是同样的 SetMaxOpenConns 显式上限。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(40)
	sqlDB.SetMaxIdleConns(10)

	s := NewStore(db, rdb)
	if err := s.AutoMigrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 清库 + 清 Redis 守卫 key，保证可重跑（guard TTL 24h，只清 DB 会残留）。
	// M7 表有唯一键（plans.uk_name / recharge_orders.uk_order_no / topups.uk_ref / models.uk_model_id），
	// 不清会跨轮次撞唯一索引 → 一并清理。
	for _, tbl := range []string{"models", "agent_audit_log", "agent_sessions", "topups", "recharge_orders", "subscriptions", "plans", "bills", "tenants", "outbox_events", "audit_events"} {
		if err := db.Exec("DELETE FROM " + tbl).Error; err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}
	cleanRedisKeys(t, rdb)
	// 种入真实商品目录（幂等，SeedModels 已存在跳过）——Reserve/ConsumptionByModel/SalesStats
	// 都按目录取价/取厂商，测试需要一个可用的模型集合。
	if err := s.SeedModels(context.Background()); err != nil {
		t.Fatalf("seed models: %v", err)
	}
	return s, func() { rdb.Close(); sqlDB, _ := db.DB(); sqlDB.Close() }
}

// cleanRedisKeys 按前缀删除测试可能残留的守卫 key。
func cleanRedisKeys(t *testing.T, rdb *goredis.Client) {
	t.Helper()
	ctx := context.Background()
	var keys []string
	for _, prefix := range []string{"billing:*", "usage:*"} {
		iter := rdb.Scan(ctx, 0, prefix, 0).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if err := iter.Err(); err != nil {
			t.Fatalf("scan redis: %v", err)
		}
	}
	if len(keys) > 0 {
		if err := rdb.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("clean redis: %v", err)
		}
	}
}

func seedTenant(t *testing.T, s *Store, balance int64) uint64 {
	t.Helper()
	tenant := Tenant{
		Name:         fmt.Sprintf("t-%d", time.Now().UnixNano()),
		InitialQuota: balance,
		Status:       0,
	}
	if err := s.db.Create(&tenant).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := s.rdb.Set(context.Background(), BalanceKey(tenant.ID), balance, 0).Err(); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	return tenant.ID
}

// seedMockModel 种测试专用模型 mock-model，单价 10000 元/M（1_000_000 分/百万 token）。
// 让 pre 的数学变成整值：pre = CostCent(estPrompt,0) × 1.2 = estPrompt×1.2，
// 于是 estPrompt=84→pre=100、estPrompt=7500→pre=9000、estPrompt=8334→pre=10000（精确）。
func seedMockModel(t *testing.T, s *Store) {
	t.Helper()
	m := Model{
		Vendor: "测试", ModelID: "mock-model", Name: "Mock Model",
		InputPriceCent: 1_000_000, OutputPriceCent: 1_000_000, Status: 0,
		Tags: "[]", // 合法 JSON（空数组），models.tags 列是 JSON 类型
	}
	if err := s.db.Create(&m).Error; err != nil {
		t.Fatalf("seed mock model: %v", err)
	}
}

func reserveReq(rid string, tenantID uint64, estPrompt int64) *billingv1.ReserveRequest {
	return &billingv1.ReserveRequest{
		RequestId:       rid,
		TenantId:        int64(tenantID),
		ApiKeyId:        1,
		ProviderId:      0,
		Model:           "mock-model",
		EstPromptTokens: estPrompt,
	}
}

func countReserveRows(s *Store) int64 {
	var n int64
	s.db.Model(&Bill{}).Where("phase = ?", PhaseReserve).Count(&n)
	return n
}

// 核心测试：并发 100 协程从 1000 余额里各预占 100（estPrompt=84 → pre=100），
// 恰好 10 个成功、90 个余额不足；余额归零且从不负。若 Lua 没有原子守卫，
// check-then-act 会让超过 10 个通过检查，成功数 > 10（超卖）。所以成功数 == 10
// 就是「不超卖」的直接证明。
func TestReserveConcurrentNoOversell(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	const (
		balance    = int64(1000) // 1000 分
		goroutines = 100
	)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, balance)

	var (
		wg           sync.WaitGroup
		success      atomic.Int64
		insufficient atomic.Int64
		minBal       atomic.Int64
	)
	minBal.Store(balance)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			req := reserveReq(fmt.Sprintf("rid-%d-%d", time.Now().UnixNano(), i), tenantID, 84)
			resp, err := s.Reserve(ctx, req)
			if err != nil {
				t.Errorf("reserve err: %v", err)
				return
			}
			if b := resp.BalanceAfter; b < minBal.Load() {
				minBal.CompareAndSwap(minBal.Load(), b)
			}
			switch resp.Code {
			case billingv1.ReserveCode_RESERVE_OK:
				success.Add(1)
			case billingv1.ReserveCode_RESERVE_INSUFFICIENT:
				insufficient.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if got := success.Load(); got != 10 {
		t.Fatalf("success = %d, want 10（超卖会 >10）", got)
	}
	if got := insufficient.Load(); got != 90 {
		t.Fatalf("insufficient = %d, want 90", got)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 0 {
		t.Fatalf("final balance = %d, want 0", bal)
	}
	if n := countReserveRows(s); n != 10 {
		t.Fatalf("reserve rows = %d, want 10", n)
	}
	if minBal.Load() < 0 {
		t.Fatalf("balance went negative: %d", minBal.Load())
	}
}

// 幂等：同一 request_id 重复预占只扣一次、只落一行。
func TestReserveIdempotentSameRequest(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 100)
	req := reserveReq("rid-dup", tenantID, 84) // pre=100

	resp1, err := s.Reserve(ctx, req)
	if err != nil || resp1.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("first reserve: code=%v err=%v", resp1.Code, err)
	}
	resp2, err := s.Reserve(ctx, req)
	if err != nil || resp2.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("second reserve: code=%v err=%v", resp2.Code, err)
	}
	if resp2.BillId != resp1.BillId {
		t.Fatalf("bill_id changed: %d vs %d", resp1.BillId, resp2.BillId)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 0 {
		t.Fatalf("balance = %d, want 0（重复预占不应二次扣减）", bal)
	}
	if n := countReserveRows(s); n != 1 {
		t.Fatalf("reserve rows = %d, want 1", n)
	}
}

// 并发下同一 request_id：两个 goroutine 同时预占，也只扣一次、一行。
func TestReserveConcurrentSameRequest(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 100)
	var wg sync.WaitGroup
	wg.Add(2)
	var okCount atomic.Int64
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			resp, err := s.Reserve(ctx, reserveReq("rid-race", tenantID, 84))
			if err == nil && resp.Code == billingv1.ReserveCode_RESERVE_OK {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if okCount.Load() != 2 {
		t.Fatalf("两个并发调用都应返回 OK（幂等重放），got %d", okCount.Load())
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 0 {
		t.Fatalf("balance = %d, want 0（只扣一次）", bal)
	}
	if n := countReserveRows(s); n != 1 {
		t.Fatalf("reserve rows = %d, want 1", n)
	}
}

// 余额不足：返回 INSUFFICIENT，不落行、不动余额。
func TestReserveInsufficient(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 10) // 余额 10 < 预扣 100
	resp, err := s.Reserve(ctx, reserveReq("rid-insuf", tenantID, 84))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if resp.Code != billingv1.ReserveCode_RESERVE_INSUFFICIENT {
		t.Fatalf("code = %v, want INSUFFICIENT", resp.Code)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 10 {
		t.Fatalf("balance = %d, want 10（不应扣减）", bal)
	}
	if n := countReserveRows(s); n != 0 {
		t.Fatalf("reserve rows = %d, want 0", n)
	}
}
