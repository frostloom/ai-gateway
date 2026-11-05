package comparison

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errBreakerConflict = errors.New("breaker event conflict")

func TestDurableBreakerRestartDuplicateAndProbe(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	event := fmt.Sprintf("fail-%d", time.Now().UnixNano())
	var once atomic.Bool
	if err := db.Callback().Update().Before("gorm:update").Register("eval:breaker-fail-update", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_durable_breaker" && once.CompareAndSwap(false, true) {
			tx.AddError(errors.New("synthetic update failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := durableBreakerReport(ctx, db, event, false, ""); err == nil {
		t.Fatal("expected transaction failure")
	}
	if err := db.Callback().Update().Remove("eval:breaker-fail-update"); err != nil {
		t.Fatal(err)
	}
	var rolledBack int64
	if err := db.Table("eval_durable_breaker_event").Count(&rolledBack).Error; err != nil || rolledBack != 0 {
		t.Fatal(rolledBack, err)
	}
	if applied, err := durableBreakerReport(ctx, db, event, false, ""); err != nil || !applied {
		t.Fatal(applied, err)
	}
	// A new consumer has no in-memory state; a duplicate must not add a failure.
	if applied, err := durableBreakerReport(ctx, db, event, false, ""); err != nil || applied {
		t.Fatal(applied, err)
	}
	if _, err := durableBreakerReport(ctx, db, event, true, ""); !errors.Is(err, errBreakerConflict) {
		t.Fatal(err)
	}
	if permit, err := durableBreakerPermit(ctx, db, "premature"); err != nil || permit {
		t.Fatal(permit, err)
	}
	if err := db.Exec("UPDATE eval_durable_breaker SET open_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var winner string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprintf("probe-%d", i)
			allowed, err := durableBreakerPermit(ctx, db, token)
			if err != nil {
				t.Error(err)
				return
			}
			if allowed {
				wins.Add(1)
				mu.Lock()
				winner = token
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	if _, err := durableBreakerReport(ctx, db, "wrong-probe", true, "wrong"); err == nil {
		t.Fatal("wrong probe accepted")
	}
	if err := db.Exec("UPDATE eval_durable_breaker SET probe_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if allowed, err := durableBreakerPermit(ctx, db, "replacement"); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	if _, err := durableBreakerReport(ctx, db, "stale-probe", true, winner); err == nil {
		t.Fatal("expired probe accepted")
	}
	winner = "replacement"
	if applied, err := durableBreakerReport(ctx, db, "probe-ok", true, winner); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if applied, err := durableBreakerReport(ctx, db, "probe-ok", true, winner); err != nil || applied {
		t.Fatal(applied, err)
	}
	if permit, err := durableBreakerPermit(ctx, db, "normal"); err != nil || !permit {
		t.Fatal(permit, err)
	}
	var row struct {
		State    string
		Failures int
		Version  int64
	}
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Failures != 0 || row.Version != 4 {
		t.Fatal(row, err)
	}
	var count int64
	if err := db.Table("eval_durable_breaker_event").Count(&count).Error; err != nil || count != 2 {
		t.Fatal(count, err)
	}
	t.Logf("duplicate_noop=true conflicting_id_rejected=true probe_winners=%d final=%s version=%d events=%d", wins.Load(), row.State, row.Version, count)
}
