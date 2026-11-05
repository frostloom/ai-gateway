package comparison

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"testing"

	"gorm.io/gorm/logger"
)

func TestProbeGenerationCommitErrors(t *testing.T) {
	if os.Getenv("TEST_PROBE_GENERATION_ACK") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_probe_generation (id BIGINT PRIMARY KEY, generation BIGINT NOT NULL, token VARBINARY(64), expires DATETIME(6), state VARCHAR(16) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_probe_generation_event (event_id VARBINARY(128) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, success BOOLEAN NOT NULL) ENGINE=InnoDB",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		for _, q := range []string{"DELETE FROM eval_probe_generation_event", "DELETE FROM eval_probe_generation", "INSERT INTO eval_probe_generation(id,generation,state) VALUES(1,0,'open')"} {
			if err := db.Exec(q).Error; err != nil {
				t.Fatal(err)
			}
		}
		g, err := generationPermit(ctx, recoveryDBWithCommitError(t, injected), "same")
		if g != 0 || !errors.Is(err, injected) {
			t.Fatalf("permit generation=%d err=%v", g, err)
		}
		var row generationRow
		if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.Generation != 1 || row.State != "half_open" {
			t.Fatalf("row=%+v err=%v", row, err)
		}
		g, err = generationPermit(ctx, db, "same")
		if err != nil || g != 0 {
			t.Fatalf("retry=%d err=%v", g, err)
		}
		if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
			t.Fatal(err)
		}
		g, err = generationPermit(ctx, db, "same")
		if err != nil || g != 2 {
			t.Fatalf("replacement=%d err=%v", g, err)
		}
		if applied, err := generationReport(ctx, db, "old", "same", 1, true); applied || !errors.Is(err, errProbeStale) {
			t.Fatalf("old=%v err=%v", applied, err)
		}
		if applied, err := generationReport(ctx, recoveryDBWithCommitError(t, injected), "result", "same", g, true); applied || !errors.Is(err, injected) {
			t.Fatalf("report=%v err=%v", applied, err)
		}
		if applied, err := generationReport(ctx, db, "result", "same", g, true); applied || err != nil {
			t.Fatalf("report replay=%v err=%v", applied, err)
		}
		var n int64
		if err := db.Table("eval_probe_generation_event").Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("events=%d err=%v", n, err)
		}
		if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Generation != 2 {
			t.Fatalf("row=%+v err=%v", row, err)
		}
		t.Logf("injected=%v permit_commit_unknown_retry_blocked=true expiry_replaced_generation=2 stale_rejected=true report_retry_noop=true events=%d", injected, n)
	}
}
