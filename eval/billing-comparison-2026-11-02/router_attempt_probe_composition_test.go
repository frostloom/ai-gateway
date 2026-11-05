package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// This characterizes a gap between two test-only prototypes, not production.
func TestAttemptOutboxCannotRecoverProbeToken(t *testing.T) {
	db := setupAttemptDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,open_until,version) VALUES(1,'open',1,DATE_SUB(NOW(6),INTERVAL 1 SECOND),0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	const token = "test-probe-token"
	if allowed, err := durableBreakerPermit(ctx, db, token); err != nil || !allowed {
		t.Fatalf("permit=%v err=%v", allowed, err)
	}
	a, err := prepareAttempt(ctx, db, 1, fmt.Sprintf("probe-composition-%d", time.Now().UnixNano()), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if won, err := beginAttempt(ctx, db, a.AttemptID); err != nil || !won {
		t.Fatalf("begin=%v err=%v", won, err)
	}
	if won, err := completeAttempt(ctx, db, a.AttemptID, true); err != nil || !won {
		t.Fatalf("complete=%v err=%v", won, err)
	}
	var event attemptOutbox
	if err := db.Table("eval_attempt_outbox").Where("attempt_id=?", a.AttemptID).Take(&event).Error; err != nil {
		t.Fatal(err)
	}
	// Only outbox fields are available after sender loss; there is no token field.
	if applied, err := durableBreakerReport(ctx, db, event.AttemptID, event.Outcome, ""); err == nil || applied {
		t.Fatalf("missing probe token accepted: applied=%v err=%v", applied, err)
	}
	var count int64
	if err := db.Table("eval_durable_breaker_event").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("events=%d err=%v", count, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	// This externally supplied token is a test control, not recoverable outbox data.
	if applied, err := durableBreakerReport(ctx, db, event.AttemptID, event.Outcome, token); err != nil || !applied {
		t.Fatalf("control applied=%v err=%v", applied, err)
	}
	if applied, err := durableBreakerReport(ctx, db, event.AttemptID, event.Outcome, token); err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	t.Log("outbox lacks probe token: replay rejected; test-only external token permits idempotent application")
}
