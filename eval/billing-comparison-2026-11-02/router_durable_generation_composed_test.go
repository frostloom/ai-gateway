package comparison

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"
	"testing"
)

func TestDurableBreakerGenerationRejectsOldReusedToken(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_generation_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_generation_event", "DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,open_until,version) VALUES(1,'open',1,DATE_SUB(NOW(6),INTERVAL 1 SECOND),0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if generation, err := durableGenerationPermit(ctx, db, "same"); err != nil || generation == 0 {
		t.Fatal(generation, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	old := row.Version
	if err := db.Exec("UPDATE eval_durable_breaker SET probe_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if generation, err := durableGenerationPermit(ctx, db, "same"); err != nil || generation == 0 {
		t.Fatal(generation, err)
	}
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Version <= old {
		t.Fatal("generation did not advance")
	}
	if applied, err := durableGenerationReport(ctx, db, "old", "same", old, true); applied || !errors.Is(err, errProbeStale) {
		t.Fatalf("old accepted: %v %v", applied, err)
	}
	if applied, err := durableGenerationReport(ctx, db, "new", "same", row.Version, true); !applied || err != nil {
		t.Fatal(applied, err)
	}
	if applied, err := durableGenerationReport(ctx, db, "new", "same", row.Version, true); applied || err != nil {
		t.Fatal(applied, err)
	}
	if applied, err := durableGenerationReport(ctx, db, "new", "same", old, true); applied || !errors.Is(err, errBreakerConflict) {
		t.Fatal(applied, err)
	}
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Failures != 0 {
		t.Fatal(row, err)
	}
}

func TestDurableGenerationRejectsOverflow(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_generation_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_generation_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,probe_token,probe_until,version) VALUES(1,'half_open',1,'probe',DATE_ADD(NOW(6),INTERVAL 30 SECOND),9223372036854775807)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	applied, err := durableGenerationReport(context.Background(), db, "overflow", "probe", int64(9223372036854775807), true)
	if applied || err == nil {
		t.Fatalf("overflow accepted: applied=%v err=%v", applied, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Version != int64(9223372036854775807) {
		t.Fatal(row, err)
	}
	var n int64
	if err := db.Table("eval_durable_generation_event").Count(&n).Error; err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestDurableGenerationConcurrentDuplicate(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_generation_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_generation_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,probe_token,probe_until,version) VALUES(1,'half_open',1,'probe',DATE_ADD(NOW(6),INTERVAL 30 SECOND),7)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var applied atomic.Int32
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := durableGenerationReport(context.Background(), db, "same-event", "probe", 7, true)
			if ok {
				applied.Add(1)
			}
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var n int64
	if err := db.Table("eval_durable_generation_event").Count(&n).Error; err != nil || n != 1 || applied.Load() != 1 {
		t.Fatalf("events=%d applied=%d err=%v", n, applied.Load(), err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Version != 8 {
		t.Fatal(row, err)
	}
}

func TestDurableGenerationPermitOverflow(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,open_until,version) VALUES(1,'open',1,DATE_SUB(NOW(6),INTERVAL 1 SECOND),9223372036854775807)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	generation, err := durableGenerationPermit(context.Background(), db, "probe")
	if generation != 0 || err == nil {
		t.Fatalf("overflow accepted: generation=%d err=%v", generation, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Version != int64(9223372036854775807) || row.ProbeToken != nil {
		t.Fatal(row, err)
	}
}

func TestDurableGenerationConcurrentPermit(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,open_until,version) VALUES(1,'open',1,DATE_SUB(NOW(6),INTERVAL 1 SECOND),0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var winners atomic.Int32
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			generation, err := durableGenerationPermit(context.Background(), db, "same")
			if generation == 1 {
				winners.Add(1)
			} else if generation != 0 {
				t.Errorf("unexpected generation %d", generation)
			}
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if winners.Load() != 1 {
		t.Fatalf("winners=%d", winners.Load())
	}
}

func TestDurableGenerationCommitAcknowledgementLoss(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_generation_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_generation_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,open_until,version) VALUES(1,'open',1,DATE_SUB(NOW(6),INTERVAL 1 SECOND),0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	unknown := recoveryDBWithCommitError(t, errCommitAckLost)
	generation, err := durableGenerationPermit(ctx, unknown, "same")
	if generation != 0 || !errors.Is(err, errCommitAckLost) {
		t.Fatal(generation, err)
	}
	if generation, err = durableGenerationPermit(ctx, db, "same"); generation != 0 || err != nil {
		t.Fatal(generation, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Version != 1 {
		t.Fatal(row, err)
	}
	applied, err := durableGenerationReport(ctx, recoveryDBWithCommitError(t, errCommitAckLost), "result", "same", 1, true)
	if applied || !errors.Is(err, errCommitAckLost) {
		t.Fatal(applied, err)
	}
	applied, err = durableGenerationReport(ctx, db, "result", "same", 1, true)
	if applied || err != nil {
		t.Fatal(applied, err)
	}
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Version != 2 {
		t.Fatal(row, err)
	}
	var n int64
	if err := db.Table("eval_durable_generation_event").Count(&n).Error; err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestDurableGenerationResultUpdateRollback(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_generation_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_generation_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,probe_token,probe_until,version) VALUES(1,'half_open',1,'probe',DATE_ADD(NOW(6),INTERVAL 30 SECOND),7)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	injected := errors.New("synthetic breaker update failure")
	name := "eval:durable-generation-rollback"
	if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_durable_breaker" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(name)
	applied, err := durableGenerationReport(context.Background(), db, "result", "probe", 7, true)
	if applied || !errors.Is(err, injected) {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	var count int64
	if err := db.Table("eval_durable_generation_event").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("events=%d err=%v", count, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Version != 7 {
		t.Fatal(row, err)
	}
	if err := db.Callback().Update().Remove(name); err != nil {
		t.Fatal(err)
	}
	applied, err = durableGenerationReport(context.Background(), db, "result", "probe", 7, true)
	if !applied || err != nil {
		t.Fatal(applied, err)
	}
	if err := db.Table("eval_durable_generation_event").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Version != 8 {
		t.Fatal(row, err)
	}
}
