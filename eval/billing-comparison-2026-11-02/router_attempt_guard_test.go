package comparison

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errAttemptNotReady = errors.New("previous attempt not safely failed")

// Test-only parent lock binds request fingerprint and serializes attempt creation.
func prepareGuardedAttempt(ctx context.Context, db *gorm.DB, tenant uint64, request, fingerprint string, ordinal int, provider uint64) (result attemptRow, err error) {
	if tenant == 0 || request == "" || len(request) > 128 || len(fingerprint) != 64 || ordinal < 0 || provider == 0 {
		return attemptRow{}, fmt.Errorf("invalid attempt")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent struct{ Fingerprint string }
		if e := tx.Table("eval_attempt_request").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND request_id=?", tenant, request).Take(&parent).Error; e != nil {
			return e
		}
		if parent.Fingerprint != fingerprint {
			return errAttemptConflict
		}
		var row attemptRow
		e := tx.Table("eval_route_attempt").Where("tenant_id=? AND request_id=? AND ordinal=?", tenant, request, ordinal).Take(&row).Error
		if e == nil {
			if row.ProviderID != provider {
				return errAttemptConflict
			}
			result = row
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if ordinal > 0 {
			var previous attemptRow
			e = tx.Table("eval_route_attempt").Where("tenant_id=? AND request_id=? AND ordinal=?", tenant, request, ordinal-1).Take(&previous).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errAttemptNotReady
			}
			if e != nil {
				return e
			}
			if previous.State != "completed" || previous.Outcome == nil || *previous.Outcome || previous.RetrySafe == nil || !*previous.RetrySafe {
				return errAttemptNotReady
			}
		}
		var used int64
		if e := tx.Table("eval_route_attempt").Where("tenant_id=? AND request_id=? AND provider_id=? AND ordinal < ?", tenant, request, provider, ordinal).Count(&used).Error; e != nil {
			return e
		}
		if used != 0 {
			return errAttemptConflict
		}
		var raw [16]byte
		if _, e := rand.Read(raw[:]); e != nil {
			return e
		}
		row = attemptRow{AttemptID: hex.EncodeToString(raw[:]), TenantID: tenant, RequestID: request, Ordinal: ordinal, ProviderID: provider, State: "prepared"}
		if e := tx.Table("eval_route_attempt").Create(&row).Error; e != nil {
			return e
		}
		result = row
		return nil
	})
	if err != nil {
		return attemptRow{}, err
	}
	return result, nil
}
func TestAttemptGuardRejectsUnknownAndConflictingRequests(t *testing.T) {
	db := setupAttemptDB(t)
	ctx := context.Background()
	if e := db.Exec("CREATE TABLE IF NOT EXISTS eval_attempt_request (tenant_id BIGINT UNSIGNED NOT NULL,request_id VARBINARY(128) NOT NULL,fingerprint VARBINARY(64) NOT NULL,PRIMARY KEY(tenant_id,request_id)) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM eval_attempt_request").Error; e != nil {
		t.Fatal(e)
	}
	request := fmt.Sprintf("guarded-%d", time.Now().UnixNano())
	fp := strings.Repeat("a", 64)
	if e := db.Exec("INSERT INTO eval_attempt_request VALUES(1,?,?),(2,?,?)", request, fp, request, fp).Error; e != nil {
		t.Fatal(e)
	}
	first, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 0, 7)
	if e != nil || len(first.AttemptID) != 32 {
		t.Fatalf("first=%+v err=%v", first, e)
	}
	if _, e := prepareGuardedAttempt(ctx, db, 1, request, strings.Repeat("b", 64), 0, 7); !errors.Is(e, errAttemptConflict) {
		t.Fatalf("fingerprint mismatch=%v", e)
	}
	if ok, e := beginAttempt(ctx, db, first.AttemptID); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 1, 8); !errors.Is(e, errAttemptNotReady) {
		t.Fatalf("unknown first allowed next: %v", e)
	}
	if _, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 2, 8); !errors.Is(e, errAttemptNotReady) {
		t.Fatalf("skipped ordinal allowed: %v", e)
	}
	if ok, e := completeAttempt(ctx, db, first.AttemptID, false); e != nil || !ok {
		t.Fatal(ok, e)
	}
	// A generic failure does not prove the upstream never executed.
	if _, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 1, 8); !errors.Is(e, errAttemptNotReady) {
		t.Fatalf("unverified failure allowed retry: %v", e)
	}
	// Test-only stand-in for separately verified retry-safe evidence; no such
	// verification mechanism is implemented by this candidate.
	if e := db.Exec("UPDATE eval_route_attempt SET retry_safe=1 WHERE attempt_id=?", first.AttemptID).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 1, 7); !errors.Is(e, errAttemptConflict) {
		t.Fatalf("reused provider accepted: %v", e)
	}
	var wg sync.WaitGroup
	results := make(chan attemptRow, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 1, 8)
			if e != nil {
				errs <- e
			} else {
				results <- a
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for e := range errs {
		t.Error(e)
	}
	id := ""
	for a := range results {
		if id == "" {
			id = a.AttemptID
		}
		if id != a.AttemptID || id == first.AttemptID {
			t.Fatalf("unstable next=%s %s", id, a.AttemptID)
		}
	}
	if id == "" {
		t.Fatal("no second attempt")
	}
	if ok, e := beginAttempt(ctx, db, id); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if ok, e := completeAttempt(ctx, db, id, true); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := prepareGuardedAttempt(ctx, db, 1, request, fp, 2, 9); !errors.Is(e, errAttemptNotReady) {
		t.Fatalf("after success allowed next: %v", e)
	}
	other, e := prepareGuardedAttempt(ctx, db, 2, request, fp, 0, 7)
	if e != nil || other.AttemptID == first.AttemptID {
		t.Fatalf("tenant isolation=%+v err=%v", other, e)
	}
	var count int64
	if e := db.Table("eval_attempt_outbox").Count(&count).Error; e != nil || count != 2 {
		t.Fatalf("outbox=%d err=%v", count, e)
	}
}
