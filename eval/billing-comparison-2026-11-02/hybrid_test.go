package comparison

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"os"
	"testing"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
)

func TestHybridCacheBoundaries(t *testing.T) {
	db, rdb := isolatedDB(t)
	base := candidate{db: db}
	if e := base.migrate(); e != nil {
		t.Fatal(e)
	}
	if e := base.seed(80001, 1000); e != nil {
		t.Fatal(e)
	}
	h := hybrid{candidate: base, redis: rdb, ttl: time.Second}
	ctx := context.Background()
	if x, e := h.read(ctx, 80001); e != nil || x != 1000 {
		t.Fatalf("initial %d %v", x, e)
	}
	generation, e := h.generation(ctx, 80001)
	if e != nil {
		t.Fatal(e)
	}
	req := &billingv1.ReserveRequest{RequestId: "hybrid-r1", TenantId: 80001, ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
	if _, e = h.reserve(ctx, req); e != nil {
		t.Fatal(e)
	}
	filled, e := h.fill(ctx, 80001, generation, 1000)
	if e != nil || filled {
		t.Fatalf("stale read filled cache: %v %v", filled, e)
	}
	// A Redis reset can erase the generation, recreating the initial value.
	// This deliberately exposes the ABA race; do not treat the prototype as safe.
	genKey, _ := h.keys(80001)
	if e := rdb.Del(ctx, genKey).Err(); e != nil {
		t.Fatal(e)
	}
	accepted, e := h.fill(ctx, 80001, generation, 1000)
	if e != nil || !accepted {
		t.Fatalf("expected generation-reset ABA, accepted=%v err=%v", accepted, e)
	}
	if e = h.invalidate(ctx, 80001); e != nil {
		t.Fatal(e)
	}
	if x, e := h.read(ctx, 80001); e != nil || x != 900 {
		t.Fatalf("after reserve %d %v", x, e)
	}
	if e = base.usage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50}); e != nil {
		t.Fatal(e)
	}
	if _, e = h.settle(ctx, req.RequestId, 80001); e != nil {
		t.Fatal(e)
	}
	if x, e := h.read(ctx, 80001); e != nil || x != 950 {
		t.Fatalf("after settle %d %v", x, e)
	}
	// Simulate process death after SQL commit and before cache invalidation.
	req.RequestId = "hybrid-crash"
	if _, e = base.reserve(ctx, req); e != nil {
		t.Fatal(e)
	}
	stale, e := h.read(ctx, 80001)
	if e != nil || stale != 950 {
		t.Fatalf("expected observed stale cache %d %v", stale, e)
	}
	authoritative, e := base.balance(ctx, 80001)
	if e != nil || authoritative != 850 {
		t.Fatalf("SQL %d %v", authoritative, e)
	}
	// Test-only one-event recovery: the SQL transaction included this outbox row.
	var outbox store.OutboxEvent
	if e = db.Where("event_key=?", "hybrid-crash:reserve").First(&outbox).Error; e != nil {
		t.Fatal(e)
	}
	var event store.BillingEvent
	if e = json.Unmarshal([]byte(outbox.Payload), &event); e != nil || event.TenantID != 80001 {
		t.Fatalf("outbox event=%+v err=%v", event, e)
	}
	if e = h.invalidate(ctx, event.TenantID); e != nil {
		t.Fatal(e)
	}
	if value, e := h.read(ctx, 80001); e != nil || value != 850 {
		t.Fatalf("outbox recovery %d %v", value, e)
	}
	// Separate crash demonstrates expiry when no event is processed.
	req.RequestId = "hybrid-ttl"
	if _, e = base.reserve(ctx, req); e != nil {
		t.Fatal(e)
	}
	if value, e := h.read(ctx, 80001); e != nil || value != 850 {
		t.Fatalf("pre-expiry %d %v", value, e)
	}
	time.Sleep(1200 * time.Millisecond)
	recovered, e := h.read(ctx, 80001)
	if e != nil || recovered != 750 {
		t.Fatalf("TTL recovery %d %v", recovered, e)
	}
	// Force invalidation failure after MySQL commit; retry must not debit again.
	if e = rdb.Close(); e != nil {
		t.Fatal(e)
	}
	req.RequestId = "hybrid-invalidation-error"
	if _, e = h.reserve(ctx, req); e == nil {
		t.Fatal("expected invalidation error")
	}
	sqlBalance, e := base.balance(ctx, 80001)
	if e != nil || sqlBalance != 650 {
		t.Fatalf("committed balance=%d err=%v", sqlBalance, e)
	}
	replacement := redis.NewClient(&redis.Options{Addr: os.Getenv("COMPARISON_REDIS_ADDR")})
	defer replacement.Close()
	h.redis = replacement
	cached, e := h.read(ctx, 80001)
	if e != nil || cached != 750 {
		t.Fatalf("expected stale cache %d %v", cached, e)
	}
	if _, e = h.reserve(ctx, req); e != nil {
		t.Fatal(e)
	}
	final, e := h.read(ctx, 80001)
	if e != nil || final != 650 {
		t.Fatalf("idempotent retry cache=%d err=%v", final, e)
	}
	if e = base.audit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Logf("first committed MySQL=%d and cached=%d; outbox replay recovered 850; separate TTL recovery=%d; invalidation retry recovered 650", authoritative, stale, recovered)
}
