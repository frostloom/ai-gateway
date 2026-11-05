package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm/logger"
)

// Measures only test capability SQL updates, not full billing or HTTP load.
func TestExecutionGateDistribution(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("strict overlay only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := fmt.Sprintf("gate-load-%d", time.Now().UnixNano())
	h := sha256.Sum256([]byte("load-owner"))
	hash := hex.EncodeToString(h[:])
	expiry := time.Now().Add(5 * time.Minute)
	const n = 128
	for _, mode := range []string{"same_request", "same_account", "multiple_accounts"} {
		rows := make([]store.Bill, n)
		for i := range rows {
			tenant := uint64(i + 1)
			if mode == "same_account" {
				tenant = 1
			}
			rows[i] = store.Bill{RequestID: fmt.Sprintf("%s-%s-%d", base, mode, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance}
		}
		if err := db.CreateInBatches(&rows, 64).Error; err != nil {
			t.Fatal(err)
		}
		ids := make([]uint64, n)
		for i := range rows {
			ids[i] = rows[i].ID
		}
		if err := db.Model(&store.Bill{}).Where("id IN ?", ids).Updates(map[string]any{"execution_state": "reserved", "execution_owner_hash": hash, "execution_lease_until": expiry}).Error; err != nil {
			t.Fatal(err)
		}
		gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
		type result struct {
			duration time.Duration
			n        int64
			err      error
		}
		results := make(chan result, n)
		jobs := make(chan int, n)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := range jobs {
					j := i
					if mode == "same_request" {
						j = 0
					}
					begin := time.Now()
					out, err := gate.Transition(ctx, wrapperspb.String(rows[j].RequestID+"|forwarding|load-owner"))
					elapsed := time.Since(begin)
					var value int64
					if out != nil {
						value = out.Value
					}
					results <- result{elapsed, value, err}
				}
			}()
		}
		for i := 0; i < n; i++ {
			jobs <- i
		}
		close(jobs)
		begin := time.Now()
		close(start)
		wg.Wait()
		wall := time.Since(begin)
		close(results)
		lat := make([]time.Duration, 0, n)
		wins, errors := int64(0), 0
		var sum time.Duration
		for res := range results {
			lat = append(lat, res.duration)
			sum += res.duration
			wins += res.n
			if res.err != nil {
				errors++
			}
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		want := int64(n)
		if mode == "same_request" {
			want = 1
		}
		if wins != want || errors != 0 {
			t.Fatalf("mode=%s wins=%d errors=%d", mode, wins, errors)
		}
		var stored int64
		if err := db.Model(&store.Bill{}).Where("id IN ? AND execution_state=?", ids, "forwarding").Count(&stored).Error; err != nil || stored != want {
			t.Fatalf("mode=%s stored=%d err=%v", mode, stored, err)
		}
		t.Logf("mode=%s operations=%d workers=16 accepted=%d errors=%d wall_ms=%.2f ops_s=%.2f avg_ms=%.2f p95_ms=%.2f p99_ms=%.2f", mode, n, wins, errors, float64(wall.Microseconds())/1000, float64(n)/wall.Seconds(), float64(sum.Microseconds())/1000/n, float64(lat[121].Microseconds())/1000, float64(lat[126].Microseconds())/1000)
	}
}
