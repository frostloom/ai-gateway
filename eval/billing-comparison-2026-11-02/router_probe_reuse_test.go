package comparison

import (
	"context"
	"os"
	"testing"

	"gorm.io/gorm/logger"
)

func TestDurableBreakerRejectsReusedProbeToken(t *testing.T) {
	if os.Getenv("TEST_PROBE_REUSE") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
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
	ok, err := durableBreakerPermit(ctx, db, "same-token")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := db.Exec("UPDATE eval_durable_breaker SET probe_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	ok, err = durableBreakerPermit(ctx, db, "same-token")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	applied, err := durableBreakerReport(ctx, db, "old-attempt-result", true, "same-token")
	if err == nil || applied {
		t.Fatalf("old probe response accepted after same token was reused: applied=%v err=%v", applied, err)
	}
}
