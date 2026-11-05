package comparison

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"gorm.io/gorm/logger"
)

func TestOutboxGenerationCommitAckLost(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.Exec("CREATE TABLE IF NOT EXISTS eval_outbox_lease_v2 (id BIGINT UNSIGNED PRIMARY KEY, generation BIGINT NOT NULL DEFAULT 0, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6)) ENGINE=InnoDB").Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		if err := db.Exec("DELETE FROM eval_outbox_lease_v2").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO eval_outbox_lease_v2(id) VALUES(1)").Error; err != nil {
			t.Fatal(err)
		}
		row, err := claimOutboxGeneration(ctx, recoveryDBWithCommitError(t, injected), "same")
		if !errors.Is(err, injected) || row.ID != 0 {
			t.Fatalf("claim=%+v err=%v", row, err)
		}
		var committed leaseGenerationRow
		if err := db.Table("eval_outbox_lease_v2").Where("id=1").Take(&committed).Error; err != nil || committed.Generation != 1 || committed.Owner == nil || *committed.Owner != "same" || committed.LeaseUntil == nil {
			t.Fatalf("committed=%+v err=%v", committed, err)
		}
		if row, err := claimOutboxGeneration(ctx, db, "same"); err != nil || row.ID != 0 {
			t.Fatalf("immediate=%+v err=%v", row, err)
		}
		if err := db.Exec("UPDATE eval_outbox_lease_v2 SET lease_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
			t.Fatal(err)
		}
		replacement, err := claimOutboxGeneration(ctx, db, "same")
		if err != nil || replacement.ID != 1 || replacement.Generation != 2 {
			t.Fatalf("replacement=%+v err=%v", replacement, err)
		}
		if ok, err := markOutboxGeneration(ctx, db, 1, "same", 1); err != nil || ok {
			t.Fatalf("stale=%v err=%v", ok, err)
		}
		if ok, err := markOutboxGeneration(ctx, db, 1, "same", 2); err != nil || !ok {
			t.Fatalf("current=%v err=%v", ok, err)
		}
		t.Logf("injected=%v commit=1 returned_empty=true retry_blocked=true replacement=2 stale_rejected=true", injected)
	}
}
