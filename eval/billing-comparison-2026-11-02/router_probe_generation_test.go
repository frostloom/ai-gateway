package comparison

import (
	"context"
	"errors"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var errProbeStale = errors.New("stale generation")

type generationRow struct {
	Generation int64
	Token      *string
	Expires    *time.Time
	State      string
}
type generationEvent struct {
	EventID    string
	Token      string
	Generation int64
	Success    bool
}

func generationPermit(ctx context.Context, db *gorm.DB, token string) (generation int64, err error) {
	if token == "" || len(token) > 64 {
		return 0, errors.New("invalid token")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		next := row.Generation + 1
		if e := tx.Table("eval_probe_generation").Where("id=1").Updates(map[string]any{"generation": next, "token": token, "expires": gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)"), "state": "half_open"}).Error; e != nil {
			return e
		}
		generation = next
		return nil
	})
	if err != nil {
		return 0, err
	}
	return generation, nil
}
func generationReport(ctx context.Context, db *gorm.DB, event, token string, gen int64, success bool) (applied bool, err error) {
	if event == "" || len(event) > 128 || token == "" || len(token) > 64 || gen <= 0 {
		return false, errors.New("invalid event")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row generationRow
		if e := tx.Table("eval_probe_generation").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&row).Error; e != nil {
			return e
		}
		var prior generationEvent
		e := tx.Table("eval_probe_generation_event").Where("event_id=?", event).Take(&prior).Error
		if e == nil {
			if prior.Token != token || prior.Generation != gen || prior.Success != success {
				return errBreakerConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var now time.Time
		if e := tx.Raw("SELECT NOW(6)").Scan(&now).Error; e != nil {
			return e
		}
		if row.State != "half_open" || row.Generation != gen || row.Token == nil || *row.Token != token || row.Expires == nil || !row.Expires.After(now) {
			return errProbeStale
		}
		if e := tx.Table("eval_probe_generation_event").Create(map[string]any{"event_id": event, "token": token, "generation": gen, "success": success}).Error; e != nil {
			return e
		}
		nextState := "open"
		if success {
			nextState = "closed"
		}
		if e := tx.Table("eval_probe_generation").Where("id=1").Updates(map[string]any{"state": nextState, "token": nil, "expires": nil}).Error; e != nil {
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

func TestProbeGenerationRejectsOldAndDeduplicates(t *testing.T) {
	if os.Getenv("TEST_PROBE_GENERATION") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_probe_generation (id BIGINT PRIMARY KEY, generation BIGINT NOT NULL, token VARBINARY(64), expires DATETIME(6), state VARCHAR(16) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_probe_generation_event (event_id VARBINARY(128) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, success BOOLEAN NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_probe_generation_event", "DELETE FROM eval_probe_generation", "INSERT INTO eval_probe_generation(id,generation,state) VALUES(1,0,'open')",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	first, err := generationPermit(ctx, db, "same-token")
	if err != nil || first != 1 {
		t.Fatal(first, err)
	}
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	results := make(chan int64, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			g, e := generationPermit(ctx, db, "same-token")
			results <- g
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	wins := 0
	var second int64
	for g := range results {
		if g != 0 {
			wins++
			second = g
		}
	}
	if wins != 1 || second != 2 {
		t.Fatalf("wins=%d gen=%d", wins, second)
	}
	if applied, err := generationReport(ctx, db, "old-event", "same-token", first, true); applied || !errors.Is(err, errProbeStale) {
		t.Fatalf("old applied=%v err=%v", applied, err)
	}
	if applied, err := generationReport(ctx, db, "new-event", "same-token", second, true); !applied || err != nil {
		t.Fatalf("new applied=%v err=%v", applied, err)
	}
	if applied, err := generationReport(ctx, db, "new-event", "same-token", second, true); applied || err != nil {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	if _, err := generationReport(ctx, db, "new-event", "same-token", second, false); !errors.Is(err, errBreakerConflict) {
		t.Fatalf("conflict=%v", err)
	}
	var n int64
	if err := db.Table("eval_probe_generation_event").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("events=%d err=%v", n, err)
	}
	t.Logf("first=%d second=%d concurrent_winners=%d stale_rejected=true replay_noop=true events=%d", first, second, wins, n)
}
