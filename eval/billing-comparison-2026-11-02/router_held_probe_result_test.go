package comparison

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func applyHeldProbeResult(ctx context.Context, db *gorm.DB, id string) (bool, error) {
	return applyHeldProbeWithPolicy(ctx, db, id, false)
}

func applyHeldProbeWithPolicy(ctx context.Context, db *gorm.DB, id string, cooldown bool) (bool, error) {
	var applied bool
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state generationRow
		if e := tx.Table("eval_probe_generation").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&state).Error; e != nil {
			return e
		}
		var a boundProbeAttempt
		if e := tx.Table("eval_bound_probe_attempt").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&a).Error; e != nil {
			return e
		}
		var out boundProbeEvent
		if e := tx.Table("eval_bound_probe_outbox").Where("id=?", id).Take(&out).Error; e != nil {
			return e
		}
		if a.State != "done" || a.Outcome == nil || *a.Outcome != out.Outcome || a.Token != out.Token || a.Generation != out.Generation {
			return errAttemptConflict
		}
		var prior generationEvent
		e := tx.Table("eval_probe_generation_event").Where("event_id=?", id).Take(&prior).Error
		if e == nil {
			if prior.Token != out.Token || prior.Generation != out.Generation || prior.Success != out.Outcome {
				return errBreakerConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if state.State != "half_open" || state.Token == nil || *state.Token != out.Token || state.Generation != out.Generation {
			return errProbeStale
		}
		if e := tx.Table("eval_probe_generation_event").Create(map[string]any{"event_id": id, "token": out.Token, "generation": out.Generation, "success": out.Outcome}).Error; e != nil {
			return e
		}
		next := "open"
		if out.Outcome {
			next = "closed"
		}
		updates := map[string]any{"state": next, "token": nil, "expires": nil}
		if !out.Outcome && cooldown {
			updates["expires"] = gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)")
		}
		if e := tx.Table("eval_probe_generation").Where("id=1").Updates(updates).Error; e != nil {
			return e
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func TestHeldProbeLateResultCanResolveOnce(t *testing.T) {
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
	if gen, err := allocateProbeHoldingSending(ctx, db, "attempt", "token"); err != nil || gen != 1 {
		t.Fatalf("allocate=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "attempt"); err != nil || !ok {
		t.Fatalf("begin=%v err=%v", ok, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if gen, err := allocateProbeHoldingSending(ctx, db, "other", "other"); err != nil || gen != 0 {
		t.Fatalf("replacement=%d err=%v", gen, err)
	}
	if err := finishBoundProbe(ctx, db, "attempt", true); err != nil {
		t.Fatal(err)
	}
	if gen, err := allocateProbeHoldingSending(ctx, db, "other", "other"); err != nil || gen != 0 {
		t.Fatalf("unapplied result replacement=%d err=%v", gen, err)
	}
	if applied, err := generationReport(ctx, db, "attempt", "token", 1, true); applied || !errors.Is(err, errProbeStale) {
		t.Fatalf("old receiver=%v err=%v", applied, err)
	}
	if applied, err := applyHeldProbeResult(ctx, db, "attempt"); !applied || err != nil {
		t.Fatalf("held result=%v err=%v", applied, err)
	}
	if applied, err := applyHeldProbeResult(ctx, db, "attempt"); applied || err != nil {
		t.Fatalf("duplicate=%v err=%v", applied, err)
	}
	var row generationRow
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "closed" || row.Generation != 1 {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	var count int64
	if err := db.Table("eval_probe_generation_event").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("events=%d err=%v", count, err)
	}
	t.Log("expired held result applied exactly once in prototype; old receiver rejects")
}
