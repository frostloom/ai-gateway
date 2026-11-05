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
)

// Test-only batched lookup and insert within the same provider transaction.
func applyReportEventsBulk(ctx context.Context, db *gorm.DB, events []reportEvent) (int, error) {
	if len(events) == 0 || len(events) > 256 || events[0].Provider == 0 {
		return 0, fmt.Errorf("invalid batch")
	}
	first := events[0]
	if !first.Success && len(events) != 1 {
		return 0, fmt.Errorf("failures cannot be batched")
	}
	unique := make(map[string]reportEvent, len(events))
	ids := make([]string, 0, len(events))
	for _, e := range events {
		if e.ID == "" || len(e.ID) > 128 || e.Provider != first.Provider || e.Success != first.Success {
			return 0, fmt.Errorf("invalid event batch")
		}
		if old, ok := unique[e.ID]; ok {
			if old.Provider != e.Provider || old.Success != e.Success {
				return 0, errReportConflict
			}
			continue
		}
		unique[e.ID] = e
		ids = append(ids, e.ID)
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
		var existing []routeEventRow
		if e := tx.Table("eval_route_event").Where("event_id IN ?", ids).Find(&existing).Error; e != nil {
			return e
		}
		for _, row := range existing {
			event, ok := unique[row.EventID]
			if !ok || row.ProviderID != event.Provider || row.Success != event.Success {
				return errReportConflict
			}
			delete(unique, row.EventID)
		}
		rows := make([]routeEventRow, 0, len(unique))
		for _, id := range ids {
			if e, ok := unique[id]; ok {
				rows = append(rows, routeEventRow{EventID: e.ID, ProviderID: e.Provider, Success: e.Success})
			}
		}
		if len(rows) == 0 {
			return nil
		}
		if e := tx.Table("eval_route_event").Create(&rows).Error; e != nil {
			return e
		}
		applied = len(rows)
		q := tx.Table("eval_route_provider").Where("id=? AND status=0", first.Provider)
		if first.Success {
			return q.UpdateColumn("consecutive_fail", 0).Error
		}
		return q.UpdateColumn("consecutive_fail", gorm.Expr("consecutive_fail + 1")).Error
	})
	if err != nil {
		return 0, err
	}
	return applied, nil
}

func TestReportEventBulkContracts(t *testing.T) {
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
			n, e := applyReportEventsBulk(ctx, db, []reportEvent{failure})
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
	if _, e := applyReportEventsBulk(ctx, db, []reportEvent{mismatch}); !errors.Is(e, errReportConflict) {
		t.Fatalf("conflict error=%v", e)
	}
	fresh := reportEvent{ID: failure.ID + "-conflict-fresh", Provider: 1, Success: true}
	if _, e := applyReportEventsBulk(ctx, db, []reportEvent{fresh, mismatch}); !errors.Is(e, errReportConflict) {
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
	if n, e := applyReportEventsBulk(ctx, db, successes); e != nil || n != 16 {
		t.Fatalf("batch applied=%d err=%v", n, e)
	}
	if n, e := applyReportEventsBulk(ctx, db, successes); e != nil || n != 0 {
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
	if _, e := applyReportEventsBulk(ctx, db, []reportEvent{rollback}); e == nil {
		t.Fatal("expected injected update failure")
	}
	db.Callback().Update().Remove("test:route-event-rollback")
	var count int64
	if e := db.Table("eval_route_event").Where("event_id=?", rollback.ID).Count(&count).Error; e != nil || count != 0 {
		t.Fatalf("rolled-back event count=%d err=%v", count, e)
	}
	if n, e := applyReportEventsBulk(ctx, db, []reportEvent{rollback}); e != nil || n != 1 {
		t.Fatalf("retry applied=%d err=%v", n, e)
	}
	if e := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&value).Error; e != nil || value != 1 {
		t.Fatalf("final fail count=%d err=%v", value, e)
	}
}

func TestReportEventBulkCrossProviderConflict(t *testing.T) {
	db := setupReportEventDB(t)
	if e := db.Exec("INSERT INTO eval_route_provider VALUES(2,0,0)").Error; e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := fmt.Sprintf("cross-provider-%d", time.Now().UnixNano())
	type result struct {
		provider uint64
		applied  int
		err      error
	}
	out := make(chan result, 2)
	start := make(chan struct{})
	for _, provider := range []uint64{1, 2} {
		go func(provider uint64) {
			<-start
			n, e := applyReportEventsBulk(ctx, db, []reportEvent{{ID: id, Provider: provider, Success: true}})
			out <- result{provider, n, e}
		}(provider)
	}
	close(start)
	a, b := <-out, <-out
	var winner, loser result
	if a.err == nil && a.applied == 1 {
		winner, loser = a, b
	} else {
		winner, loser = b, a
	}
	if winner.err != nil || winner.applied != 1 || loser.err == nil {
		t.Fatalf("results: %+v %+v", a, b)
	}
	if _, e := applyReportEventsBulk(ctx, db, []reportEvent{{ID: id, Provider: loser.provider, Success: true}}); !errors.Is(e, errReportConflict) {
		t.Fatalf("loser retry must conflict: %v", e)
	}
	var count int64
	if e := db.Table("eval_route_event").Where("event_id=?", id).Count(&count).Error; e != nil || count != 1 {
		t.Fatalf("events=%d err=%v", count, e)
	}
}

func TestReportEventBulkOldReplayDoesNotEraseNewFailure(t *testing.T) {
	db := setupReportEventDB(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("ordered-%d", time.Now().UnixNano())
	oldSuccess := reportEvent{ID: prefix + "-success-old", Provider: 1, Success: true}
	firstFailure := reportEvent{ID: prefix + "-failure-1", Provider: 1, Success: false}
	newSuccess := reportEvent{ID: prefix + "-success-new", Provider: 1, Success: true}
	secondFailure := reportEvent{ID: prefix + "-failure-2", Provider: 1, Success: false}
	apply := func(event reportEvent, wantApplied, wantCount int) {
		t.Helper()
		n, e := applyReportEventsBulk(ctx, db, []reportEvent{event})
		if e != nil || n != wantApplied {
			t.Fatalf("event=%s applied=%d err=%v", event.ID, n, e)
		}
		var count int
		if e := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&count).Error; e != nil || count != wantCount {
			t.Fatalf("event=%s count=%d err=%v", event.ID, count, e)
		}
	}
	apply(oldSuccess, 1, 0)
	apply(firstFailure, 1, 1)
	apply(oldSuccess, 0, 1)
	apply(newSuccess, 1, 0)
	apply(firstFailure, 0, 0)
	apply(secondFailure, 1, 1)
	apply(secondFailure, 0, 1)
}

func TestReportEventBulkCancelledBeforeLock(t *testing.T) {
	db := setupReportEventDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var n int
	if e := tx.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1 FOR UPDATE").Scan(&n).Error; e != nil {
		t.Fatal(e)
	}
	event := reportEvent{ID: fmt.Sprintf("timeout-%d", time.Now().UnixNano()), Provider: 1, Success: false}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, e := applyReportEventsBulk(ctx, db, []reportEvent{event}); e == nil {
		t.Fatal("locked transaction unexpectedly succeeded")
	}
	if e := tx.Rollback().Error; e != nil {
		t.Fatal(e)
	}
	var count int64
	if e := db.Table("eval_route_event").Where("event_id=?", event.ID).Count(&count).Error; e != nil || count != 0 {
		t.Fatalf("event after timeout=%d err=%v", count, e)
	}
	if applied, e := applyReportEventsBulk(context.Background(), db, []reportEvent{event}); e != nil || applied != 1 {
		t.Fatalf("retry applied=%d err=%v", applied, e)
	}
	if applied, e := applyReportEventsBulk(context.Background(), db, []reportEvent{event}); e != nil || applied != 0 {
		t.Fatalf("duplicate applied=%d err=%v", applied, e)
	}
	if e := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&n).Error; e != nil || n != 1 {
		t.Fatalf("failure count=%d err=%v", n, e)
	}
}
