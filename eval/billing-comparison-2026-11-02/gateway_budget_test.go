package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

// Models gateway's separate 3s reserve and 10s usage/settle budgets at Store
// boundary; this is not a full HTTP or model-service test.
func TestGatewayBudgetLoad(t *testing.T) {
	db, rdb := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db}
	if e := c.migrate(); e != nil {
		t.Fatal(e)
	}
	a := store.NewStore(db, rdb)
	ctx := context.Background()
	for i, mode := range []string{"A", "B"} {
		tenant := uint64(300001 + i)
		if mode == "A" {
			if e := db.Create(&store.Tenant{ID: tenant, Name: "gateway-budget-a", InitialQuota: 10000000}).Error; e != nil {
				t.Fatal(e)
			}
			if e := rdb.Set(ctx, store.BalanceKey(tenant), 10000000, 0).Err(); e != nil {
				t.Fatal(e)
			}
		} else if e := c.seed(tenant, 10000000); e != nil {
			t.Fatal(e)
		}
		var seq, done, reserveErrors, reserveTimeouts, terminalErrors, terminalTimeouts atomic.Int64
		var wg, ready sync.WaitGroup
		start := make(chan struct{})
		var deadline time.Time
		ready.Add(64)
		for w := 0; w < 64; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready.Done()
				<-start
				for time.Now().Before(deadline) {
					n := seq.Add(1)
					id := fmt.Sprintf("gateway-budget-%s-%d", mode, n)
					req := &billingv1.ReserveRequest{RequestId: id, TenantId: int64(tenant), ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
					rc, cancel := context.WithTimeout(ctx, 3*time.Second)
					var r *billingv1.ReserveResponse
					var e error
					if mode == "A" {
						r, e = a.Reserve(rc, req)
					} else {
						r, e = c.reserve(rc, req)
					}
					timed := rc.Err() != nil
					cancel()
					if e != nil || r.Code != billingv1.ReserveCode_RESERVE_OK {
						reserveErrors.Add(1)
						if timed {
							reserveTimeouts.Add(1)
						}
						continue
					}
					tc, stop := context.WithTimeout(ctx, 10*time.Second)
					u := &billingv1.ReportUsageRequest{RequestId: id, PromptTokens: 50}
					if mode == "A" {
						e = a.ReportUsage(tc, u)
					} else {
						e = c.usage(tc, u)
					}
					if e == nil {
						if mode == "A" {
							var result *billingv1.SettleResponse
							result, e = a.Settle(tc, &billingv1.SettleRequest{RequestId: id})
							if e == nil && !result.Settled {
								e = fmt.Errorf("not settled")
							}
						} else {
							var ok bool
							ok, e = c.settle(tc, id)
							if e == nil && !ok {
								e = fmt.Errorf("not settled")
							}
						}
					}
					timed = tc.Err() != nil
					stop()
					if e != nil {
						terminalErrors.Add(1)
						if timed {
							terminalTimeouts.Add(1)
						}
					} else {
						done.Add(1)
					}
				}
			}()
		}
		ready.Wait()
		beg := time.Now()
		deadline = beg.Add(10 * time.Second)
		close(start)
		wg.Wait()
		elapsed := time.Since(beg).Seconds()
		var pending, settled, events int64
		prefix := "gateway-budget-" + mode + "-%"
		db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=? AND status=?", prefix, store.PhaseReserve, store.StatusPending).Count(&pending)
		db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=?", prefix, store.PhaseSettle).Count(&settled)
		db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?", prefix).Count(&events)
		ledger, e := store.LedgerBalance(ctx, db, tenant)
		if e != nil {
			t.Fatal(e)
		}
		var actual int64
		if mode == "A" {
			actual, e = rdb.Get(ctx, store.BalanceKey(tenant)).Int64()
		} else {
			actual, e = c.balance(ctx, tenant)
		}
		if e != nil {
			t.Fatal(e)
		}
		result := map[string]any{"mode": mode, "workers": 64, "seconds": elapsed, "cycles_per_second": float64(done.Load()) / elapsed, "completed": done.Load(), "reserve_errors": reserveErrors.Load(), "reserve_timeouts": reserveTimeouts.Load(), "terminal_errors": terminalErrors.Load(), "terminal_timeouts": terminalTimeouts.Load(), "pending": pending, "settled": settled, "outbox": events, "ledger": ledger, "balance": actual}
		b, _ := json.Marshal(result)
		t.Logf("GATEWAY_BUDGET %s", b)
		if ledger != actual {
			t.Errorf("%s ledger=%d balance=%d", mode, ledger, actual)
		}
	}
}
