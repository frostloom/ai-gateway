package comparison

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupHeldFailure(t *testing.T) *gorm.DB {
	t.Helper()
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_probe_generation (id BIGINT PRIMARY KEY, generation BIGINT NOT NULL, token VARBINARY(64), expires DATETIME(6), state VARCHAR(16) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_probe_generation_event (event_id VARBINARY(128) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, success BOOLEAN NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_bound_probe_attempt (id VARBINARY(32) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, state VARCHAR(16) NOT NULL, outcome BOOLEAN NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_bound_probe_outbox (id VARBINARY(32) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, outcome BOOLEAN NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_probe_generation_event", "DELETE FROM eval_probe_generation", "INSERT INTO eval_probe_generation(id,generation,state) VALUES(1,0,'open')", "DELETE FROM eval_bound_probe_outbox", "DELETE FROM eval_bound_probe_attempt",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if gen, err := allocateProbeWithCooldown(ctx, db, "first", "token-1"); err != nil || gen != 1 {
		t.Fatalf("allocate=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "first"); err != nil || !ok {
		t.Fatalf("begin=%v err=%v", ok, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := finishBoundProbe(ctx, db, "first", false); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestHeldFailureAndAllocationConcurrent(t *testing.T) {
	db := setupHeldFailure(t)
	ctx := context.Background()
	const workers = 16
	start := make(chan struct{})
	type result struct {
		gen     int64
		applied bool
		err     error
	}
	results := make(chan result, workers+1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		applied, err := applyHeldWithCooldown(ctx, db, "first")
		results <- result{applied: applied, err: err}
	}()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			gen, err := allocateProbeWithCooldown(ctx, db, fmt.Sprintf("new-%d", i), fmt.Sprintf("token-%d", i))
			results <- result{gen: gen, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	applied := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.gen != 0 {
			t.Fatalf("cooldown bypassed, gen=%d", r.gen)
		}
		if r.applied {
			applied++
		}
	}
	var row generationRow
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Generation != 1 || row.Expires == nil {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	var attempts, events int64
	if err := db.Table("eval_bound_probe_attempt").Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("eval_probe_generation_event").Count(&events).Error; err != nil || applied != 1 || attempts != 1 || events != 1 {
		t.Fatalf("applied=%d attempts=%d events=%d err=%v", applied, attempts, events, err)
	}
	t.Logf("simultaneous result and %d allocations: result=%d allocations=0 attempts=%d events=%d", workers, applied, attempts, events)
}

func TestHeldFailureCommitAckLost(t *testing.T) {
	ctx := context.Background()
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		db := setupHeldFailure(t)
		if applied, err := applyHeldWithCooldown(ctx, recoveryDBWithCommitError(t, injected), "first"); applied || !errors.Is(err, injected) {
			t.Fatalf("apply=%v err=%v", applied, err)
		}
		var row generationRow
		if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Generation != 1 || row.Expires == nil {
			t.Fatalf("state=%+v err=%v", row, err)
		}
		if applied, err := applyHeldWithCooldown(ctx, db, "first"); err != nil || applied {
			t.Fatalf("replay=%v err=%v", applied, err)
		}
		if gen, err := allocateProbeWithCooldown(ctx, db, "next", "next-token"); err != nil || gen != 0 {
			t.Fatalf("cooldown allocation=%d err=%v", gen, err)
		}
		var events int64
		if err := db.Table("eval_probe_generation_event").Count(&events).Error; err != nil || events != 1 {
			t.Fatalf("events=%d err=%v", events, err)
		}
		t.Logf("injected=%v committed failure replay no-op, cooldown holds, events=%d", injected, events)
	}
}
