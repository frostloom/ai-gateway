package comparison

import (
	"context"
	"testing"
)

// PASS characterizes the missing cooldown in the test-only state machine.
func TestHeldProbeFailureAllowsImmediateReplacement(t *testing.T) {
	db, _ := isolatedDB(t)
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
	if gen, err := allocateProbeHoldingSending(ctx, db, "failed", "token-1"); err != nil || gen != 1 {
		t.Fatalf("allocation=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "failed"); err != nil || !ok {
		t.Fatalf("begin=%v err=%v", ok, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := finishBoundProbe(ctx, db, "failed", false); err != nil {
		t.Fatal(err)
	}
	if applied, err := applyHeldProbeResult(ctx, db, "failed"); err != nil || !applied {
		t.Fatalf("result=%v err=%v", applied, err)
	}
	if applied, err := applyHeldProbeResult(ctx, db, "failed"); err != nil || applied {
		t.Fatalf("duplicate=%v err=%v", applied, err)
	}
	var row generationRow
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Generation != 1 {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	if gen, err := allocateProbeHoldingSending(ctx, db, "next", "token-2"); err != nil || gen != 2 {
		t.Fatalf("immediate replacement=%d err=%v", gen, err)
	}
	t.Log("late failure applied once but next generation issued immediately; cooldown missing")
}
