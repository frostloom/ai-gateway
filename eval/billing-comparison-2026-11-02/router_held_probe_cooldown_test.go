package comparison

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func applyHeldWithCooldown(ctx context.Context, db *gorm.DB, id string) (bool, error) {
	return applyHeldProbeWithPolicy(ctx, db, id, true)
}
func allocateProbeWithCooldown(ctx context.Context, db *gorm.DB, id, token string) (int64, error) {
	return allocateProbeWithPolicy(ctx, db, id, token, true)
}

func TestHeldFailureRespectsCooldown(t *testing.T) {
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
	if gen, err := allocateProbeWithCooldown(ctx, db, "first", "token-1"); err != nil || gen != 1 {
		t.Fatalf("first=%d err=%v", gen, err)
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
	if applied, err := applyHeldWithCooldown(ctx, db, "first"); err != nil || !applied {
		t.Fatalf("apply=%v err=%v", applied, err)
	}
	if applied, err := applyHeldWithCooldown(ctx, db, "first"); err != nil || applied {
		t.Fatalf("duplicate=%v err=%v", applied, err)
	}
	if gen, err := allocateProbeWithCooldown(ctx, db, "second", "token-2"); err != nil || gen != 0 {
		t.Fatalf("during cooldown=%d err=%v", gen, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if gen, err := allocateProbeWithCooldown(ctx, db, "second", "token-2"); err != nil || gen != 2 {
		t.Fatalf("after cooldown=%d err=%v", gen, err)
	}
	t.Log("failure applied once; immediate probe held; expired cooldown permits next generation")
}
