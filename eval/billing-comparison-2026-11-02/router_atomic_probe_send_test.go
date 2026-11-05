package comparison

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func beginBoundProbe(ctx context.Context, db *gorm.DB, id string) (bool, error) {
	var won bool
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p generationRow
		if err := tx.Table("eval_probe_generation").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&p).Error; err != nil {
			return err
		}
		var a boundProbeAttempt
		if err := tx.Table("eval_bound_probe_attempt").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&a).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT NOW(6)").Scan(&now).Error; err != nil {
			return err
		}
		if p.State != "half_open" || p.Token == nil || *p.Token != a.Token || p.Generation != a.Generation || p.Expires == nil || !p.Expires.After(now) {
			return errProbeStale
		}
		if a.State != "prepared" {
			return nil
		}
		if err := tx.Table("eval_bound_probe_attempt").Where("id=? AND state=?", id, "prepared").Update("state", "sending").Error; err != nil {
			return err
		}
		won = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return won, nil
}

func TestBoundProbeSendRejectsReplacedAndDuplicate(t *testing.T) {
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
	gen, err := allocateBoundProbe(ctx, db, "old", "token")
	if err != nil || gen != 1 {
		t.Fatalf("old=%d err=%v", gen, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	gen, err = allocateBoundProbe(ctx, db, "new", "token")
	if err != nil || gen != 2 {
		t.Fatalf("new=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "old"); ok || !errors.Is(err, errProbeStale) {
		t.Fatalf("old begin=%v err=%v", ok, err)
	}
	const workers = 8
	start := make(chan struct{})
	type result struct {
		ok  bool
		err error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := beginBoundProbe(ctx, db, "new")
			results <- result{ok, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.ok {
			wins++
		}
	}
	var old, fresh boundProbeAttempt
	if err := db.Table("eval_bound_probe_attempt").Where("id=?", "old").Take(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("eval_bound_probe_attempt").Where("id=?", "new").Take(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if wins != 1 || old.State != "prepared" || fresh.State != "sending" {
		t.Fatalf("wins=%d old=%+v new=%+v", wins, old, fresh)
	}
	t.Logf("old rejected; concurrent send winners=%d; old=prepared new=sending", wins)
}
