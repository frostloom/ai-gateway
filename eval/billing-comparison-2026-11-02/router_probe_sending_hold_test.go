package comparison

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func allocateProbeHoldingSending(ctx context.Context, db *gorm.DB, id, token string) (int64, error) {
	return allocateProbeWithPolicy(ctx, db, id, token, false)
}

func allocateProbeWithPolicy(ctx context.Context, db *gorm.DB, id, token string, cooldown bool) (int64, error) {
	if id == "" || len(id) > 32 || token == "" || len(token) > 64 {
		return 0, errors.New("invalid identity")
	}
	var gen int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row generationRow
		if e := tx.Table("eval_probe_generation").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&row).Error; e != nil {
			return e
		}
		var now time.Time
		if e := tx.Raw("SELECT NOW(6)").Scan(&now).Error; e != nil {
			return e
		}
		if row.State == "half_open" {
			if row.Expires != nil && row.Expires.After(now) {
				return nil
			}
			var sending int64
			if e := tx.Table("eval_bound_probe_attempt").Where("generation=? AND state IN ?", row.Generation, []string{"sending", "done"}).Count(&sending).Error; e != nil {
				return e
			}
			if sending != 0 {
				return nil
			}
		} else if row.State == "open" && cooldown && row.Expires != nil && row.Expires.After(now) {
			return nil
		} else if row.State != "open" {
			return errors.New("invalid state")
		}
		if row.Generation == math.MaxInt64 {
			return errors.New("generation exhausted")
		}
		gen = row.Generation + 1
		if e := tx.Table("eval_probe_generation").Where("id=1").Updates(map[string]any{"generation": gen, "token": token, "expires": gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)"), "state": "half_open"}).Error; e != nil {
			return e
		}
		return tx.Table("eval_bound_probe_attempt").Create(&boundProbeAttempt{ID: id, Token: token, Generation: gen, State: "prepared"}).Error
	})
	if err != nil {
		return 0, err
	}
	return gen, nil
}

func TestProbeReplacementHoldsUnknownSending(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_probe_generation (id BIGINT PRIMARY KEY, generation BIGINT NOT NULL, token VARBINARY(64), expires DATETIME(6), state VARCHAR(16) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_bound_probe_attempt (id VARBINARY(32) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, state VARCHAR(16) NOT NULL, outcome BOOLEAN NULL) ENGINE=InnoDB",
		"DELETE FROM eval_probe_generation", "INSERT INTO eval_probe_generation(id,generation,state) VALUES(1,0,'open')", "DELETE FROM eval_bound_probe_attempt",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if gen, err := allocateProbeHoldingSending(ctx, db, "old", "old-token"); err != nil || gen != 1 {
		t.Fatalf("old=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "old"); err != nil || !ok {
		t.Fatalf("begin=%v err=%v", ok, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if gen, err := allocateProbeHoldingSending(ctx, db, "new", "new-token"); err != nil || gen != 0 {
		t.Fatalf("unsafe replacement=%d err=%v", gen, err)
	}
	var n int64
	if err := db.Table("eval_bound_probe_attempt").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("attempts=%d err=%v", n, err)
	}
	var row generationRow
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.Generation != 1 || row.State != "half_open" {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	// Separate synthetic case: an expired, never-started attempt may be replaced.
	if err := db.Table("eval_bound_probe_attempt").Where("id=?", "old").Update("state", "prepared").Error; err != nil {
		t.Fatal(err)
	}
	if gen, err := allocateProbeHoldingSending(ctx, db, "new", "new-token"); err != nil || gen != 2 {
		t.Fatalf("prepared replacement=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "old"); ok || !errors.Is(err, errProbeStale) {
		t.Fatalf("old=%v err=%v", ok, err)
	}
	t.Log("expired sending retained; no replacement issued; manual resolution still required")
}
