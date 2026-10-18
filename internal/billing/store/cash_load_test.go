package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// Opt-in load probe; uses newTestStore's dedicated MySQL database and Redis DB 1.
// Never run alongside another store test process that cleans the same fixtures.
func TestCashLoad(t *testing.T) {
	if os.Getenv("BILLING_CASH_LOAD") != "1" {
		t.Skip("set BILLING_CASH_LOAD=1 to run the isolated cash load probe")
	}
	duration := 5 * time.Second
	if raw := os.Getenv("BILLING_CASH_LOAD_DURATION"); raw != "" {
		var err error
		duration, err = time.ParseDuration(raw)
		if err != nil || duration < 2*time.Second || duration > 30*time.Second {
			t.Fatal("BILLING_CASH_LOAD_DURATION must be between 2s and 30s")
		}
	}
	history := 0
	if raw := os.Getenv("BILLING_CASH_LOAD_HISTORY"); raw != "" {
		var err error
		history, err = strconv.Atoi(raw)
		if err != nil || history < 0 || history > 10_000 {
			t.Fatal("BILLING_CASH_LOAD_HISTORY must be between 0 and 10000 completed cycles per tenant")
		}
	}
	for _, tenants := range []int{1, 8} {
		for _, workers := range []int{1, 8, 32} {
			t.Run(fmt.Sprintf("tenants%d_workers%d", tenants, workers), func(t *testing.T) {
				runCashLoad(t, tenants, workers, history, duration)
			})
		}
	}
}

func runCashLoad(t *testing.T, tenantCount, workers, history int, duration time.Duration) {
	s, closeStore := newTestStore(t)
	defer closeStore()
	seedMockModel(t, s)
	const initial = int64(100_000_000)
	tenants := make([]uint64, tenantCount)
	for i := range tenants {
		tenants[i] = seedTenant(t, s, initial)
		if history > 0 {
			rows := make([]Bill, 0, 2*history)
			actual := int64(50)
			for j := 0; j < history; j++ {
				request := fmt.Sprintf("cash-history-%d-%d", i, j)
				for _, phase := range []string{PhaseReserve, PhaseSettle} {
					rows = append(rows, Bill{RequestID: request, Phase: phase, Status: StatusSettled,
						TenantID: tenants[i], Model: "mock-model", Funding: FundingBalance,
						PreQuota: 100, ActualQuota: &actual, CreatedAt: time.Now().Add(-72 * time.Hour)})
				}
			}
			if err := s.db.CreateInBatches(rows, 250).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.RebuildBalance(context.Background(), tenants[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	var profile *cashLoadProfile
	if os.Getenv("BILLING_CASH_PROFILE") == "1" {
		profile = attachCashLoadProfile(t, s)
	}
	cycle := func(tenant uint64, requestID string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		stage := time.Now()
		r, err := s.Reserve(ctx, reserveReq(requestID, tenant, 84)) // reserve=100
		if profile != nil {
			profile.observe("stage.reserve", stage, err)
		}
		if err != nil {
			return fmt.Errorf("reserve: %w", err)
		}
		if r.Code != billingv1.ReserveCode_RESERVE_OK {
			return fmt.Errorf("reserve code: %v", r.Code)
		}
		stage = time.Now()
		err = s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: requestID, PromptTokens: 50})
		if profile != nil {
			profile.observe("stage.usage", stage, err)
		}
		if err != nil {
			return fmt.Errorf("usage: %w", err)
		}
		stage = time.Now()
		settled, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: requestID}) // actual=50
		if profile != nil {
			profile.observe("stage.settle", stage, err)
		}
		if err != nil {
			return fmt.Errorf("settle: %w", err)
		}
		if !settled.Settled {
			return fmt.Errorf("request not settled")
		}
		return nil
	}
	// Warm one complete lifecycle per tenant, outside the measured interval.
	for i, tenant := range tenants {
		if err := cycle(tenant, fmt.Sprintf("cash-load-warm-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	type workerResult struct {
		latencies []float64
		completed []int64
		errors    []string
	}
	results := make([]workerResult, workers)
	for i := range results {
		results[i].completed = make([]int64, tenantCount)
	}
	var sequence atomic.Uint64
	ready, start := sync.WaitGroup{}, make(chan struct{})
	var wg sync.WaitGroup
	ready.Add(workers)
	runID := time.Now().UnixNano()
	var deadline time.Time
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-start
			r := &results[i]
			for time.Now().Before(deadline) {
				n := sequence.Add(1)
				ti := int((n - 1) % uint64(tenantCount))
				began := time.Now()
				err := cycle(tenants[ti], fmt.Sprintf("cash-load-%d-%d", runID, n))
				r.latencies = append(r.latencies, float64(time.Since(began))/float64(time.Millisecond))
				if err != nil {
					r.errors = append(r.errors, err.Error())
				} else {
					r.completed[ti]++
				}
			}
		}(i)
	}
	ready.Wait()
	if profile != nil {
		profile.begin()
	}
	began := time.Now()
	deadline = began.Add(duration)
	close(start)
	wg.Wait() // Include draining in throughput; never count uncompleted work.
	elapsed := time.Since(began).Seconds()
	var profileResult map[string]any
	if profile != nil {
		profileResult = profile.finish()
	}
	counts := make([]int64, tenantCount)
	var latencies []float64
	var completed, failures int64
	var samples []string
	for _, r := range results {
		latencies = append(latencies, r.latencies...)
		failures += int64(len(r.errors))
		for _, err := range r.errors {
			if len(samples) < 3 {
				samples = append(samples, err)
			}
		}
		for i, n := range r.completed {
			counts[i] += n
			completed += n
		}
	}
	sort.Float64s(latencies)
	percentile := func(percent int) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[(len(latencies)-1)*percent/100]
	}
	ctx := context.Background()
	var violations []string
	for i, tenant := range tenants {
		want := initial - (counts[i]+1+int64(history))*50 // History and warm-up each cost 50.
		live, liveErr := s.getBalance(ctx, tenant)
		ledger, ledgerErr := LedgerBalance(ctx, s.db, tenant)
		if liveErr != nil || ledgerErr != nil || live != want || ledger != want || live < 0 {
			violations = append(violations, fmt.Sprintf("tenant %d: live=%d ledger=%d want=%d errors=%v/%v", tenant, live, ledger, want, liveErr, ledgerErr))
		}
	}
	var pending, settled, duplicates int64
	if err := s.db.Model(&Bill{}).Where("phase = ? AND status = ?", PhaseReserve, StatusPending).Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Bill{}).Where("phase = ?", PhaseSettle).Count(&settled).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Raw("SELECT COUNT(*) FROM (SELECT request_id, phase FROM bills GROUP BY request_id, phase HAVING COUNT(*) > 1) AS duplicates").Scan(&duplicates).Error; err != nil {
		t.Fatal(err)
	}
	wantSettled := completed + int64(tenantCount)*int64(history+1)
	if pending != 0 || duplicates != 0 || settled != wantSettled {
		violations = append(violations, fmt.Sprintf("pending=%d duplicates=%d settled=%d want=%d", pending, duplicates, settled, wantSettled))
	}
	report := map[string]any{
		"tag": os.Getenv("BILLING_CASH_LOAD_TAG"), "case": t.Name(),
		"tenants": tenantCount, "workers": workers, "gomaxprocs": runtime.GOMAXPROCS(0),
		"history_cycles_per_tenant": history, "history_bill_rows": 2 * history * tenantCount,
		"requested_seconds": duration.Seconds(), "elapsed_seconds": elapsed,
		"completed": completed, "errors": failures, "cycles_per_second": float64(completed) / elapsed,
		"p50_ms": percentile(50), "p95_ms": percentile(95), "p99_ms": percentile(99),
		"pending": pending, "duplicate_phases": duplicates, "invariant_errors": len(violations),
		"error_samples": samples,
	}
	if profile != nil {
		report["profile"] = profileResult
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("CASH_LOAD %s", encoded)
	if completed == 0 || failures != 0 || len(violations) != 0 {
		t.Errorf("cash load failed: completed=%d errors=%d samples=%v invariant errors=%v", completed, failures, samples, violations)
	}
}
