package comparison

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type reportEvent struct {
	ID       string
	Provider uint64
	Success  bool
}

var errReportConflict = errors.New("report event id conflict")

// Test-only SQL transaction. Success events may be batched; failures are individual.
type routeEventRow struct {
	EventID    string `gorm:"column:event_id"`
	ProviderID uint64 `gorm:"column:provider_id"`
	Success    bool   `gorm:"column:success"`
}

func applyReportEvents(ctx context.Context, db *gorm.DB, events []reportEvent) (int, error) {
	if len(events) == 0 || len(events) > 256 || events[0].Provider == 0 {
		return 0, fmt.Errorf("invalid batch")
	}
	first := events[0]
	if !first.Success && len(events) != 1 {
		return 0, fmt.Errorf("failures cannot be batched")
	}
	for _, event := range events {
		if event.ID == "" || len(event.ID) > 128 || event.Provider != first.Provider || event.Success != first.Success {
			return 0, fmt.Errorf("invalid event batch")
		}
	}
	applied := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var provider struct {
			ID              uint64
			Status          int
			ConsecutiveFail int
		}
		if e := tx.Table("eval_route_provider").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", first.Provider).Take(&provider).Error; e != nil {
			return e
		}
		if provider.Status != 0 {
			return fmt.Errorf("provider inactive")
		}
		for _, event := range events {
			var existing routeEventRow
			e := tx.Table("eval_route_event").Where("event_id=?", event.ID).Take(&existing).Error
			if e == nil {
				if existing.ProviderID != event.Provider || existing.Success != event.Success {
					return errReportConflict
				}
				continue
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			row := routeEventRow{EventID: event.ID, ProviderID: event.Provider, Success: event.Success}
			if e := tx.Table("eval_route_event").Create(&row).Error; e != nil {
				return e
			}
			applied++
		}
		if applied == 0 {
			return nil
		}
		query := tx.Table("eval_route_provider").Where("id=? AND status=0", first.Provider)
		if first.Success {
			return query.UpdateColumn("consecutive_fail", 0).Error
		}
		return query.UpdateColumn("consecutive_fail", gorm.Expr("consecutive_fail + 1")).Error
	})
	if err != nil {
		return 0, err
	}
	return applied, nil
}
func setupReportEventDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_route_provider (id BIGINT UNSIGNED PRIMARY KEY,status INT NOT NULL,consecutive_fail INT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_route_event (event_id VARBINARY(128) PRIMARY KEY,provider_id BIGINT UNSIGNED NOT NULL,success BOOLEAN NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_route_event", "DELETE FROM eval_route_provider", "INSERT INTO eval_route_provider VALUES(1,0,0)",
	} {
		if e := db.Exec(q).Error; e != nil {
			t.Fatal(e)
		}
	}
	return db
}
func TestReportEventIdempotencyAndRollback(t *testing.T) {
	db := setupReportEventDB(t)
	ctx := context.Background()
	failure := reportEvent{ID: fmt.Sprintf("event-%d", time.Now().UnixNano()), Provider: 1, Success: false}
	var applied atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, e := applyReportEvents(ctx, db, []reportEvent{failure})
			if e != nil {
				errs <- e
			} else {
				applied.Add(int32(n))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if applied.Load() != 1 {
		t.Fatalf("duplicate failure applied=%d", applied.Load())
	}
	var value int
	if e := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&value).Error; e != nil || value != 1 {
		t.Fatalf("fail count=%d err=%v", value, e)
	}
	mismatch := failure
	mismatch.Success = true
	if _, e := applyReportEvents(ctx, db, []reportEvent{mismatch}); !errors.Is(e, errReportConflict) {
		t.Fatalf("conflict error=%v", e)
	}
	fresh := reportEvent{ID: failure.ID + "-conflict-fresh", Provider: 1, Success: true}
	if _, e := applyReportEvents(ctx, db, []reportEvent{fresh, mismatch}); !errors.Is(e, errReportConflict) {
		t.Fatalf("batch conflict error=%v", e)
	}
	var freshCount int64
	if e := db.Table("eval_route_event").Where("event_id=?", fresh.ID).Count(&freshCount).Error; e != nil || freshCount != 0 {
		t.Fatalf("conflicting batch committed fresh event=%d err=%v", freshCount, e)
	}
	if e := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&value).Error; e != nil || value != 1 {
		t.Fatalf("conflict changed failure count=%d err=%v", value, e)
	}
	var successes []reportEvent
	for i := 0; i < 16; i++ {
		successes = append(successes, reportEvent{ID: fmt.Sprintf("%s-success-%d", failure.ID, i), Provider: 1, Success: true})
	}
	if n, e := applyReportEvents(ctx, db, successes); e != nil || n != 16 {
		t.Fatalf("batch applied=%d err=%v", n, e)
	}
	if n, e := applyReportEvents(ctx, db, successes); e != nil || n != 0 {
		t.Fatalf("batch retry applied=%d err=%v", n, e)
	}
	// Inject a failure after inserting the event but before its provider update.
	rollback := reportEvent{ID: failure.ID + "-rollback", Provider: 1, Success: false}
	var once atomic.Bool
	if e := db.Callback().Update().Before("gorm:update").Register("test:route-event-rollback", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_route_provider" && once.CompareAndSwap(false, true) {
			tx.AddError(errors.New("injected update failure"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := applyReportEvents(ctx, db, []reportEvent{rollback}); e == nil {
		t.Fatal("expected injected update failure")
	}
	db.Callback().Update().Remove("test:route-event-rollback")
	var count int64
	if e := db.Table("eval_route_event").Where("event_id=?", rollback.ID).Count(&count).Error; e != nil || count != 0 {
		t.Fatalf("rolled-back event count=%d err=%v", count, e)
	}
	if n, e := applyReportEvents(ctx, db, []reportEvent{rollback}); e != nil || n != 1 {
		t.Fatalf("retry applied=%d err=%v", n, e)
	}
	if e := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&value).Error; e != nil || value != 1 {
		t.Fatalf("final fail count=%d err=%v", value, e)
	}
}
