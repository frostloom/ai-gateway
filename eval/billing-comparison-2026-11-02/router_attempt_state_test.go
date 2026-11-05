package comparison

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type attemptRow struct {
	AttemptID  string
	TenantID   uint64
	RequestID  string
	Ordinal    int
	ProviderID uint64
	State      string
	Outcome    *bool
	RetrySafe  *bool
}

var errAttemptConflict = errors.New("attempt conflict")

// Test-only state machine; authorization and real gateway integration are absent.
func prepareAttempt(ctx context.Context, db *gorm.DB, tenant uint64, request string, ordinal int, provider uint64) (attemptRow, error) {
	if tenant == 0 || request == "" || len(request) > 128 || ordinal < 0 || provider == 0 {
		return attemptRow{}, fmt.Errorf("invalid attempt")
	}
	var raw [16]byte
	if _, e := rand.Read(raw[:]); e != nil {
		return attemptRow{}, e
	}
	a := attemptRow{AttemptID: hex.EncodeToString(raw[:]), TenantID: tenant, RequestID: request, Ordinal: ordinal, ProviderID: provider, State: "prepared"}
	e := db.WithContext(ctx).Table("eval_route_attempt").Create(&a).Error
	if e == nil {
		return a, nil
	}
	var me *drivermysql.MySQLError
	if !errors.As(e, &me) || me.Number != 1062 {
		return attemptRow{}, e
	}
	var existing attemptRow
	if e = db.WithContext(ctx).Table("eval_route_attempt").Where("tenant_id=? AND request_id=? AND ordinal=?", tenant, request, ordinal).Take(&existing).Error; e != nil {
		return attemptRow{}, e
	}
	if existing.ProviderID != provider {
		return attemptRow{}, errAttemptConflict
	}
	return existing, nil
}
func beginAttempt(ctx context.Context, db *gorm.DB, id string) (bool, error) {
	q := db.WithContext(ctx).Table("eval_route_attempt").Where("attempt_id=? AND state=?", id, "prepared").Update("state", "sending")
	return q.RowsAffected == 1, q.Error
}

type attemptOutbox struct {
	AttemptID  string
	ProviderID uint64
	Outcome    bool
}

func completeAttempt(ctx context.Context, db *gorm.DB, id string, outcome bool) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row attemptRow
		if e := tx.Table("eval_route_attempt").Clauses(clause.Locking{Strength: "UPDATE"}).Where("attempt_id=?", id).Take(&row).Error; e != nil {
			return e
		}
		if row.State == "completed" {
			if row.Outcome == nil || *row.Outcome != outcome {
				return errAttemptConflict
			}
			return nil
		}
		if row.State != "sending" {
			return fmt.Errorf("attempt not sending")
		}
		if e := tx.Table("eval_route_attempt").Where("attempt_id=? AND state=?", id, "sending").Updates(map[string]any{"state": "completed", "outcome": outcome}).Error; e != nil {
			return e
		}
		event := attemptOutbox{AttemptID: id, ProviderID: row.ProviderID, Outcome: outcome}
		if e := tx.Table("eval_attempt_outbox").Create(&event).Error; e != nil {
			return e
		}
		won = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return won, nil
}
func setupAttemptDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_route_attempt (attempt_id VARBINARY(32) PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,request_id VARBINARY(128) NOT NULL,ordinal INT NOT NULL,provider_id BIGINT UNSIGNED NOT NULL,state VARCHAR(24) NOT NULL,outcome BOOLEAN NULL,retry_safe BOOLEAN NULL,UNIQUE KEY uq_scope (tenant_id,request_id,ordinal)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_attempt_outbox (attempt_id VARBINARY(32) PRIMARY KEY,provider_id BIGINT UNSIGNED NOT NULL,outcome BOOLEAN NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_attempt_outbox", "DELETE FROM eval_route_attempt",
	} {
		if e := db.Exec(q).Error; e != nil {
			t.Fatal(e)
		}
	}
	return db
}
func TestDurableAttemptIdentityAndOutbox(t *testing.T) {
	db := setupAttemptDB(t)
	ctx := context.Background()
	request := fmt.Sprintf("attempt-%d", time.Now().UnixNano())
	var wg sync.WaitGroup
	results := make(chan attemptRow, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := prepareAttempt(ctx, db, 1, request, 0, 7)
			if e != nil {
				errs <- e
			} else {
				results <- a
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	id := ""
	for a := range results {
		if id == "" {
			id = a.AttemptID
		}
		if a.AttemptID != id || len(id) != 32 {
			t.Fatalf("unstable id %q versus %q", a.AttemptID, id)
		}
	}
	if id == "" {
		t.Fatal("no identity")
	}
	if _, e := prepareAttempt(ctx, db, 1, request, 0, 8); !errors.Is(e, errAttemptConflict) {
		t.Fatalf("provider conflict=%v", e)
	}
	other, e := prepareAttempt(ctx, db, 1, request, 1, 8)
	if e != nil || other.AttemptID == id || len(other.AttemptID) != 32 {
		t.Fatalf("next=%+v err=%v", other, e)
	}
	tenant, e := prepareAttempt(ctx, db, 2, request, 0, 7)
	if e != nil || tenant.AttemptID == id {
		t.Fatalf("tenant=%+v err=%v", tenant, e)
	}
	var sends atomic.Int32
	errs = make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := beginAttempt(ctx, db, id)
			if e != nil {
				errs <- e
			} else if ok {
				sends.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if sends.Load() != 1 {
		t.Fatalf("send permissions=%d", sends.Load())
	}
	// A 'sending' marker remains uncertain until an outcome is durably known.
	if ok, e := beginAttempt(ctx, db, id); e != nil || ok {
		t.Fatalf("repeated send=%v err=%v", ok, e)
	}
	var completions atomic.Int32
	errs = make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := completeAttempt(ctx, db, id, false)
			if e != nil {
				errs <- e
			} else if ok {
				completions.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if completions.Load() != 1 {
		t.Fatalf("completions=%d", completions.Load())
	}
	if _, e := completeAttempt(ctx, db, id, true); !errors.Is(e, errAttemptConflict) {
		t.Fatalf("outcome conflict=%v", e)
	}
	var outboxes int64
	if e := db.Table("eval_attempt_outbox").Where("attempt_id=?", id).Count(&outboxes).Error; e != nil || outboxes != 1 {
		t.Fatalf("outboxes=%d err=%v", outboxes, e)
	}
	if ok, e := beginAttempt(ctx, db, other.AttemptID); e != nil || !ok {
		t.Fatal(ok, e)
	}
	var once atomic.Bool
	if e := db.Callback().Create().Before("gorm:create").Register("test:attempt-outbox-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_attempt_outbox" && once.CompareAndSwap(false, true) {
			tx.AddError(errors.New("injected outbox failure"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := completeAttempt(ctx, db, other.AttemptID, true); e == nil {
		t.Fatal("expected outbox failure")
	}
	db.Callback().Create().Remove("test:attempt-outbox-fail")
	var row attemptRow
	if e := db.Table("eval_route_attempt").Where("attempt_id=?", other.AttemptID).Take(&row).Error; e != nil || row.State != "sending" || row.Outcome != nil {
		t.Fatalf("rollback row=%+v err=%v", row, e)
	}
	if ok, e := completeAttempt(ctx, db, other.AttemptID, true); e != nil || !ok {
		t.Fatalf("retry complete=%v err=%v", ok, e)
	}
	if e := db.Table("eval_attempt_outbox").Where("attempt_id=?", other.AttemptID).Count(&outboxes).Error; e != nil || outboxes != 1 {
		t.Fatalf("retry outboxes=%d err=%v", outboxes, e)
	}
}

func TestAttemptOutboxDeliveredTwiceToIdempotentReceiver(t *testing.T) {
	db := setupAttemptDB(t)
	receiver := setupReportEventDB(t)
	ctx := context.Background()
	request := fmt.Sprintf("attempt-delivery-%d", time.Now().UnixNano())
	a, e := prepareAttempt(ctx, db, 1, request, 0, 1)
	if e != nil {
		t.Fatal(e)
	}
	if ok, e := beginAttempt(ctx, db, a.AttemptID); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if ok, e := completeAttempt(ctx, db, a.AttemptID, false); e != nil || !ok {
		t.Fatal(ok, e)
	}
	var event attemptOutbox
	if e := db.Table("eval_attempt_outbox").Where("attempt_id=?", a.AttemptID).Take(&event).Error; e != nil {
		t.Fatal(e)
	}
	delivered := reportEvent{ID: event.AttemptID, Provider: event.ProviderID, Success: event.Outcome}
	if n, e := applyReportEventsBulk(ctx, receiver, []reportEvent{delivered}); e != nil || n != 1 {
		t.Fatalf("first=%d err=%v", n, e)
	}
	if n, e := applyReportEventsBulk(ctx, receiver, []reportEvent{delivered}); e != nil || n != 0 {
		t.Fatalf("duplicate=%d err=%v", n, e)
	}
	var count int
	if e := receiver.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&count).Error; e != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, e)
	}
}
