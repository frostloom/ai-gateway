package comparison

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

func TestBreakerLostCommitAcknowledgement(t *testing.T) {
	if os.Getenv("TEST_BREAKER_ACK") != "1" {
		t.Skip("isolated test only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	id := fmt.Sprintf("breaker-ack-%d", time.Now().UnixNano())
	injected := errCommitAckLost
	if os.Getenv("TEST_ACK_BAD_CONNECTION") == "1" {
		injected = driver.ErrBadConn
	}
	unreliable := recoveryDBWithCommitError(t, injected)
	applied, err := durableBreakerReport(context.Background(), unreliable, id, false, "")
	if applied || !errors.Is(err, injected) {
		t.Fatalf("result applied=%v err=%v", applied, err)
	}
	applied, err = durableBreakerReport(context.Background(), db, id, false, "")
	if err != nil || applied {
		t.Fatalf("retry applied=%v err=%v", applied, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table("eval_durable_breaker_event").Where("event_id=?", id).Count(&count).Error; err != nil || count != 1 || row.State != "open" || row.Failures != 1 || row.Version != 1 {
		t.Fatal(row, count, err)
	}
	if _, err := durableBreakerReport(context.Background(), db, id, true, ""); !errors.Is(err, errBreakerConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	t.Logf("committed=true response_error=%v retry_applied=false state=%s failures=%d version=%d events=%d", injected, row.State, row.Failures, row.Version, count)
}
