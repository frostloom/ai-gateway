package comparison

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These tables and functions are an isolated prototype, not gateway code.
type boundProbeAttempt struct {
	ID         string
	Token      string
	Generation int64
	State      string
	Outcome    *bool
}
type boundProbeEvent struct {
	ID         string
	Token      string
	Generation int64
	Outcome    bool
}

func finishBoundProbe(ctx context.Context, db *gorm.DB, id string, outcome bool) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row boundProbeAttempt
		if err := tx.Table("eval_bound_probe_attempt").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.State == "done" {
			if row.Outcome == nil || *row.Outcome != outcome {
				return errAttemptConflict
			}
			return nil
		}
		if row.State != "sending" || row.Token == "" || row.Generation <= 0 {
			return errAttemptConflict
		}
		if err := tx.Table("eval_bound_probe_attempt").Where("id=?", id).Updates(map[string]any{"state": "done", "outcome": outcome}).Error; err != nil {
			return err
		}
		return tx.Table("eval_bound_probe_outbox").Create(&boundProbeEvent{ID: row.ID, Token: row.Token, Generation: row.Generation, Outcome: outcome}).Error
	})
}

func TestBoundProbeOutboxPreservesGeneration(t *testing.T) {
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
	const token = "shared-token"
	gen1, err := generationPermit(ctx, db, token)
	if err != nil || gen1 != 1 {
		t.Fatalf("permit=%d err=%v", gen1, err)
	}
	if err := db.Table("eval_bound_probe_attempt").Create(&boundProbeAttempt{ID: "old", Token: token, Generation: gen1, State: "sending"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := finishBoundProbe(ctx, db, "old", true); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	gen2, err := generationPermit(ctx, db, token)
	if err != nil || gen2 != 2 {
		t.Fatalf("replacement=%d err=%v", gen2, err)
	}
	if err := db.Table("eval_bound_probe_attempt").Create(&boundProbeAttempt{ID: "new", Token: token, Generation: gen2, State: "sending"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("eval:bound-outbox-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_bound_probe_outbox" {
			tx.AddError(errors.New("injected outbox failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := finishBoundProbe(ctx, db, "new", true); err == nil {
		t.Fatal("expected outbox failure")
	}
	if err := db.Callback().Create().Remove("eval:bound-outbox-fail"); err != nil {
		t.Fatal(err)
	}
	var rolledBack boundProbeAttempt
	if err := db.Table("eval_bound_probe_attempt").Where("id=?", "new").Take(&rolledBack).Error; err != nil || rolledBack.State != "sending" || rolledBack.Outcome != nil {
		t.Fatalf("rollback=%+v err=%v", rolledBack, err)
	}
	var rolledBackEvents int64
	if err := db.Table("eval_bound_probe_outbox").Where("id=?", "new").Count(&rolledBackEvents).Error; err != nil || rolledBackEvents != 0 {
		t.Fatalf("rollback events=%d err=%v", rolledBackEvents, err)
	}
	if err := finishBoundProbe(ctx, db, "new", true); err != nil {
		t.Fatal(err)
	}
	if err := finishBoundProbe(ctx, db, "new", true); err != nil {
		t.Fatal(err)
	}
	if err := finishBoundProbe(ctx, db, "new", false); !errors.Is(err, errAttemptConflict) {
		t.Fatalf("conflicting outcome err=%v", err)
	}
	var old, fresh boundProbeEvent
	if err := db.Table("eval_bound_probe_outbox").Where("id=?", "old").Take(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("eval_bound_probe_outbox").Where("id=?", "new").Take(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if old.Generation != 1 || fresh.Generation != 2 || old.Token != token || fresh.Token != token {
		t.Fatalf("old=%+v fresh=%+v", old, fresh)
	}
	if applied, err := generationReport(ctx, db, old.ID, old.Token, old.Generation, old.Outcome); applied || !errors.Is(err, errProbeStale) {
		t.Fatalf("stale applied=%v err=%v", applied, err)
	}
	if applied, err := generationReport(ctx, db, fresh.ID, fresh.Token, fresh.Generation, fresh.Outcome); !applied || err != nil {
		t.Fatalf("fresh applied=%v err=%v", applied, err)
	}
	if applied, err := generationReport(ctx, db, fresh.ID, fresh.Token, fresh.Generation, fresh.Outcome); applied || err != nil {
		t.Fatalf("duplicate applied=%v err=%v", applied, err)
	}
	var n int64
	if err := db.Table("eval_bound_probe_outbox").Count(&n).Error; err != nil || n != 2 {
		t.Fatalf("outbox=%d err=%v", n, err)
	}
	t.Log("old generation rejected; new generation applied once; conflicting outcome rejected; two outbox records")
}
