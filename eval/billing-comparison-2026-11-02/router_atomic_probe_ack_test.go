package comparison

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"gorm.io/gorm/logger"
)

func TestAtomicProbeAllocationCommitAckLost(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_probe_generation (id BIGINT PRIMARY KEY, generation BIGINT NOT NULL, token VARBINARY(64), expires DATETIME(6), state VARCHAR(16) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_bound_probe_attempt (id VARBINARY(32) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, state VARCHAR(16) NOT NULL, outcome BOOLEAN NULL) ENGINE=InnoDB",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		for _, q := range []string{"DELETE FROM eval_probe_generation", "INSERT INTO eval_probe_generation(id,generation,state) VALUES(1,0,'open')", "DELETE FROM eval_bound_probe_attempt"} {
			if err := db.Exec(q).Error; err != nil {
				t.Fatal(err)
			}
		}
		gen, err := allocateBoundProbe(ctx, recoveryDBWithCommitError(t, injected), "attempt", "token")
		if gen != 0 || !errors.Is(err, injected) {
			t.Fatalf("gen=%d err=%v", gen, err)
		}
		var row generationRow
		var attempt boundProbeAttempt
		if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Generation != 1 || row.Token == nil || *row.Token != "token" {
			t.Fatalf("state=%+v err=%v", row, err)
		}
		if err := db.Table("eval_bound_probe_attempt").Where("id=?", "attempt").Take(&attempt).Error; err != nil || attempt.State != "prepared" || attempt.Generation != 1 || attempt.Token != "token" {
			t.Fatalf("attempt=%+v err=%v", attempt, err)
		}
		gen, err = allocateBoundProbe(ctx, db, "attempt", "token")
		if err != nil || gen != 0 {
			t.Fatalf("same retry=%d err=%v", gen, err)
		}
		if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
			t.Fatal(err)
		}
		gen, err = allocateBoundProbe(ctx, db, "attempt", "token")
		if err == nil || gen != 0 {
			t.Fatalf("expired same ID retry=%d err=%v", gen, err)
		}
		if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.Generation != 1 {
			t.Fatalf("after duplicate=%+v err=%v", row, err)
		}
		gen, err = allocateBoundProbe(ctx, db, "replacement", "other")
		if err != nil || gen != 2 {
			t.Fatalf("replacement=%d err=%v", gen, err)
		}
		var n int64
		if err := db.Table("eval_bound_probe_attempt").Count(&n).Error; err != nil || n != 2 {
			t.Fatalf("attempts=%d err=%v", n, err)
		}
		t.Logf("injected=%v committed=1 same_retry=0 expired_duplicate_rolls_back=true replacement=2 attempts=%d", injected, n)
	}
}
