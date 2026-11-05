package comparison

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm/logger"
)

// A short local read-cost experiment, not a production capacity benchmark.
func TestPendingSummaryLargeSnapshot(t *testing.T) {
	if os.Getenv("TEST_PENDING_SUMMARY_LOAD") != "1" {
		t.Skip("explicit isolated load test only")
	}
	cfg, err := mysqldriver.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || cfg.Net != "tcp" || host != "127.0.0.1" || cfg.DBName != "billing_compare" {
		t.Fatal("requires disposable local billing_compare database")
	}
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("pending-load-%d-", time.Now().UnixNano())
	digits := "(SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)"
	id := "(a.n*10000+b.n*1000+c.n*100+d.n*10+e.n)"
	sql := `INSERT INTO bills(request_id,phase,status,tenant_id,api_key_id,funding,pre_quota,created_at,usage_reported_at) SELECT CONCAT(?,LPAD(` + id + `,5,'0')),'reserve',IF(MOD(` + id + `,2)=0,'pending','settled'),MOD(` + id + `,64)+1,1,'balance',1,NOW(3),IF(MOD(` + id + `,4)=0,NOW(3),NULL) FROM ` + digits + ` a CROSS JOIN ` + digits + ` b CROSS JOIN ` + digits + ` c CROSS JOIN ` + digits + ` d CROSS JOIN ` + digits + ` e`
	if err := db.Exec(sql, tag).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("request_id LIKE ?", tag+"%").Delete(&store.Bill{})
	var explain string
	query := `EXPLAIN FORMAT=JSON SELECT COUNT(*),COALESCE(SUM(CASE WHEN usage_reported_at IS NULL THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN usage_reported_at IS NOT NULL THEN 1 ELSE 0 END),0),MIN(CASE WHEN usage_reported_at IS NULL THEN created_at ELSE NULL END),MIN(CASE WHEN usage_reported_at IS NOT NULL THEN created_at ELSE NULL END) FROM bills WHERE phase='reserve' AND status='pending'`
	if err := db.Raw(query).Scan(&explain).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("mysql_explain=%s", strings.ReplaceAll(explain, "\n", " "))
	forcedQuery := `SELECT COUNT(*) AS pending_total, COALESCE(SUM(CASE WHEN usage_reported_at IS NULL THEN 1 ELSE 0 END),0) AS usage_marker_missing, COALESCE(SUM(CASE WHEN usage_reported_at IS NOT NULL THEN 1 ELSE 0 END),0) AS usage_marker_present, MIN(CASE WHEN usage_reported_at IS NULL THEN created_at ELSE NULL END) AS missing_oldest_created_at, MIN(CASE WHEN usage_reported_at IS NOT NULL THEN created_at ELSE NULL END) AS present_oldest_created_at FROM bills FORCE INDEX (eval_pending_summary_cover) WHERE phase=? AND status=?`
	check := func(ctx context.Context, forced bool) error {
		var s *store.PendingSummary
		var e error
		if forced {
			s = &store.PendingSummary{}
			e = db.WithContext(ctx).Raw(forcedQuery, store.PhaseReserve, store.StatusPending).Scan(s).Error
		} else {
			s, e = st.PendingReserveSummary(ctx)
		}
		if e != nil {
			return e
		}
		if s.PendingTotal != 50000 || s.UsageMarkerMissing != 25000 || s.UsageMarkerPresent != 25000 {
			return fmt.Errorf("invalid counts %+v", s)
		}
		return nil
	}
	if err := check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	measure := func(variant string, forced bool) {
		var wg sync.WaitGroup
		dur := make(chan time.Duration, 20)
		errs := make(chan error, 20)
		start := make(chan struct{})
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < 5; j++ {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					begin := time.Now()
					e := check(ctx, forced)
					dur <- time.Since(begin)
					cancel()
					if e != nil {
						errs <- e
					}
				}
			}()
		}
		begin := time.Now()
		close(start)
		wg.Wait()
		elapsed := time.Since(begin)
		close(dur)
		close(errs)
		for e := range errs {
			t.Error(e)
		}
		times := make([]time.Duration, 0, 20)
		for d := range dur {
			times = append(times, d)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		var total time.Duration
		for _, d := range times {
			total += d
		}
		t.Logf("variant=%s rows=100000 pending=50000 workers=4 n=%d elapsed_ms=%.1f mean_ms=%.1f p95_ms=%.1f max_ms=%.1f", variant, len(times), float64(elapsed.Microseconds())/1000, float64(total.Microseconds())/float64(len(times))/1000, float64(times[18].Microseconds())/1000, float64(times[19].Microseconds())/1000)
	}
	measure("baseline", false)
	if err := db.Exec("CREATE INDEX eval_pending_summary_cover ON bills(status,phase,usage_reported_at,created_at)").Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP INDEX eval_pending_summary_cover ON bills")
	if err := db.Raw(query).Scan(&explain).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("candidate_explain=%s", strings.ReplaceAll(explain, "\n", " "))
	measure("index_available", false)
	if err := db.Raw("EXPLAIN FORMAT=JSON "+forcedQuery, store.PhaseReserve, store.StatusPending).Scan(&explain).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("forced_explain=%s", strings.ReplaceAll(explain, "\n", " "))
	measure("index_forced", true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := st.PendingReserveSummary(ctx); err == nil || result != nil {
		t.Fatalf("canceled result=%+v err=%v", result, err)
	}
}
