package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm/logger"
)

var cycleExperimentRound atomic.Int32

func TestStrictBillingCycleDistribution(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("strict candidate only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	mysqlMode := os.Getenv("TEST_CYCLE_MYSQL") == "1"
	mysqlCandidate := candidate{db: db}
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if mysqlMode {
		if err := mysqlCandidate.migrate(); err != nil {
			t.Fatal(err)
		}
	}
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate missing")
	}
	lockStats, ok := any(s).(interface {
		ExperimentalLockSamples(bool) []int64
		ExperimentalHeldSamples(bool) []int64
	})
	if !ok {
		t.Fatal("test-only lock instrumentation missing")
	}
	base := fmt.Sprintf("cycle-load-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: base, Name: base, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	const n = 32
	workers := 8
	if value := os.Getenv("TEST_CYCLE_WORKERS"); value != "" {
		var err error
		workers, err = strconv.Atoi(value)
		if err != nil || workers < 1 || workers > 32 {
			t.Fatal("invalid worker count")
		}
	}
	owner := sha256.Sum256([]byte("cycle-owner"))
	mysqlIntent := mysqlMode && os.Getenv("TEST_CYCLE_INTENT") == "1"
	if mysqlIntent {
		mysqlCandidate.intent = &candidateIntent{ownerHash: hex.EncodeToString(owner[:]), lease: 30 * time.Second}
	}
	gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
	order := []string{"same_account", "multiple_accounts"}
	round := cycleExperimentRound.Add(1)
	if round%2 == 0 {
		order[0], order[1] = order[1], order[0]
	}
	t.Logf("round=%d order=%s,%s", round, order[0], order[1])
	for _, mode := range order {
		tenants := make([]store.Tenant, n)
		count := n
		if mode == "same_account" {
			count = 1
		}
		for i := 0; i < count; i++ {
			tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%s-%d", base, mode, i), InitialQuota: 100000}
			if err := db.Create(&tenants[i]).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenants[i].ID), 100000, 0).Err(); err != nil {
				t.Fatal(err)
			}
			if mysqlMode {
				if err := db.Create(&candidateAccount{ID: tenants[i].ID, Initial: 100000, Balance: 100000}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		type result struct {
			duration time.Duration
			phases   [4]time.Duration
			err      error
		}
		results := make(chan result, n)
		jobs := make(chan int, n)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := range jobs {
					begin := time.Now()
					var phases [4]time.Duration
					idx := i
					if count == 1 {
						idx = 0
					}
					id := fmt.Sprintf("%s-%s-%d", base, mode, i)
					req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenants[idx].ID), ApiKeyId: 1, Model: base, EstPromptTokens: 84}
					var bill *pb.ReserveResponse
					var err error
					if mysqlMode {
						bill, err = mysqlCandidate.reserve(ctx, req)
					} else {
						bill, err = candidate.ReserveWithExecutionIntent(ctx, req, hex.EncodeToString(owner[:]), 30*time.Second)
					}
					phases[0] = time.Since(begin)
					if err == nil && bill.Code != pb.ReserveCode_RESERVE_OK {
						err = fmt.Errorf("reserve code %v", bill.Code)
					}
					if err == nil && (!mysqlMode || mysqlIntent) {
						phaseStart := time.Now()
						var permit *wrapperspb.Int64Value
						permit, err = gate.Transition(ctx, wrapperspb.String(id+"|forwarding|cycle-owner"))
						phases[1] = time.Since(phaseStart)
						if err == nil && permit.Value != 1 {
							err = fmt.Errorf("permit %d", permit.Value)
						}
					}
					if err == nil {
						phaseStart := time.Now()
						if mysqlMode {
							err = mysqlCandidate.usage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40})
						} else {
							err = s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40})
						}
						phases[2] = time.Since(phaseStart)
					}
					if err == nil {
						phaseStart := time.Now()
						var done *pb.SettleResponse
						if mysqlMode {
							var settled bool
							settled, err = mysqlCandidate.settle(ctx, id)
							done = &pb.SettleResponse{Settled: settled}
						} else {
							done, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: bill.BillId})
						}
						phases[3] = time.Since(phaseStart)
						if err == nil && !done.Settled {
							err = fmt.Errorf("not settled")
						}
					}
					results <- result{time.Since(begin), phases, err}
				}
			}()
		}
		for i := 0; i < n; i++ {
			jobs <- i
		}
		close(jobs)
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		before := sqlDB.Stats()
		lockStats.ExperimentalLockSamples(true)
		lockStats.ExperimentalHeldSamples(true)
		begin := time.Now()
		close(start)
		wg.Wait()
		wall := time.Since(begin)
		after := sqlDB.Stats()
		lockTimes := lockStats.ExperimentalLockSamples(true)
		heldTimes := lockStats.ExperimentalHeldSamples(true)
		close(results)
		var lat []time.Duration
		var sum time.Duration
		var phaseLat [4][]time.Duration
		errs := 0
		for res := range results {
			lat = append(lat, res.duration)
			sum += res.duration
			for i, v := range res.phases {
				phaseLat[i] = append(phaseLat[i], v)
			}
			if res.err != nil {
				errs++
				t.Logf("operation_error=%v", res.err)
			}
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		var settled, events int64
		if err := db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=? AND status=?", base+"-"+mode+"-%", store.PhaseReserve, store.StatusSettled).Count(&settled).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?", base+"-"+mode+"-%").Count(&events).Error; err != nil {
			t.Fatal(err)
		}
		mismatch := 0
		for i := 0; i < count; i++ {
			live, e := r.Get(ctx, store.BalanceKey(tenants[i].ID)).Int64()
			if mysqlMode {
				live, e = mysqlCandidate.balance(ctx, tenants[i].ID)
			}
			ledger, e2 := store.LedgerBalance(ctx, db, tenants[i].ID)
			if e != nil || e2 != nil || live != ledger {
				mismatch++
			}
		}
		t.Logf("mode=%s cycles=%d workers=%d errors=%d settled=%d outbox=%d balance_mismatch=%d wall_ms=%.2f cycles_s=%.2f avg_ms=%.2f p95_ms=%.2f p99_ms=%.2f", mode, n, workers, errs, settled, events, mismatch, float64(wall.Microseconds())/1000, float64(n-errs)/wall.Seconds(), float64(sum.Microseconds())/1000/n, float64(lat[30].Microseconds())/1000, float64(lat[31].Microseconds())/1000)
		for i, name := range []string{"reserve", "permit", "usage", "settle"} {
			var total time.Duration
			for _, v := range phaseLat[i] {
				total += v
			}
			sort.Slice(phaseLat[i], func(a, b int) bool { return phaseLat[i][a] < phaseLat[i][b] })
			t.Logf("mode=%s phase=%s avg_ms=%.2f p95_ms=%.2f", mode, name, float64(total.Microseconds())/1000/n, float64(phaseLat[i][30].Microseconds())/1000)
		}
		t.Logf("mode=%s sql_pool_wait_count=%d sql_pool_wait_ms=%.2f open=%d in_use=%d", mode, after.WaitCount-before.WaitCount, float64((after.WaitDuration-before.WaitDuration).Microseconds())/1000, after.OpenConnections, after.InUse)
		if mysqlMode {
			if errs != 0 || settled != n || events != 3*n || mismatch != 0 {
				t.Fatalf("cycle audit failed")
			}
			continue
		}
		if len(lockTimes) == 0 {
			t.Fatal("missing lock samples")
		}
		sort.Slice(lockTimes, func(i, j int) bool { return lockTimes[i] < lockTimes[j] })
		var lockSum int64
		for _, v := range lockTimes {
			lockSum += v
		}
		rank := (95*len(lockTimes)+99)/100 - 1
		t.Logf("mode=%s cash_lock_samples=%d avg_ms=%.2f p95_ms=%.2f", mode, len(lockTimes), float64(lockSum)/float64(len(lockTimes))/1e6, float64(lockTimes[rank])/1e6)
		if len(heldTimes) == 0 {
			t.Fatal("missing hold samples")
		}
		sort.Slice(heldTimes, func(i, j int) bool { return heldTimes[i] < heldTimes[j] })
		var heldSum int64
		for _, v := range heldTimes {
			heldSum += v
		}
		heldRank := (95*len(heldTimes)+99)/100 - 1
		t.Logf("mode=%s cash_hold_samples=%d avg_ms=%.2f p95_ms=%.2f", mode, len(heldTimes), float64(heldSum)/float64(len(heldTimes))/1e6, float64(heldTimes[heldRank])/1e6)
		if errs != 0 || settled != n || events != 3*n || mismatch != 0 {
			t.Fatalf("cycle audit failed")
		}
	}
}
