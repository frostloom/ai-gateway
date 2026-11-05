package comparison

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func allocateBoundProbe(ctx context.Context, db *gorm.DB, id, token string) (int64, error) {
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
		if row.State == "half_open" && row.Expires != nil && row.Expires.After(now) {
			return nil
		}
		if row.State != "open" && row.State != "half_open" {
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

func TestAtomicProbeAttemptAllocation(t *testing.T) {
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
	if err := db.Callback().Create().Before("gorm:create").Register("eval:probe-attempt-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_bound_probe_attempt" {
			tx.AddError(errors.New("injected attempt insertion failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := allocateBoundProbe(ctx, db, "rollback", "token"); err == nil {
		t.Fatal("expected insertion failure")
	}
	if err := db.Callback().Create().Remove("eval:probe-attempt-fail"); err != nil {
		t.Fatal(err)
	}
	var row generationRow
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Generation != 0 {
		t.Fatalf("rollback=%+v err=%v", row, err)
	}
	const workers = 8
	type result struct {
		gen int64
		err error
	}
	results := make(chan result, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			gen, err := allocateBoundProbe(ctx, db, fmt.Sprintf("attempt-%d", i), fmt.Sprintf("token-%d", i))
			results <- result{gen, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.gen == 1 {
			winners++
		} else if r.gen != 0 {
			t.Fatalf("unexpected generation %d", r.gen)
		}
	}
	var attempts int64
	if err := db.Table("eval_bound_probe_attempt").Count(&attempts).Error; err != nil || winners != 1 || attempts != 1 {
		t.Fatalf("winners=%d attempts=%d err=%v", winners, attempts, err)
	}
	var attempt boundProbeAttempt
	if err := db.Table("eval_bound_probe_attempt").Take(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Token == nil || *row.Token != attempt.Token || row.Generation != attempt.Generation || attempt.State != "prepared" {
		t.Fatalf("attempt=%+v state=%+v err=%v", attempt, row, err)
	}
	t.Logf("rollback preserved open; concurrent winners=%d attempts=%d bound_generation=%d", winners, attempts, row.Generation)
}
