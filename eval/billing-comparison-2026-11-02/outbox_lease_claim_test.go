package comparison

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type leaseOutboxRow struct {
	ID         uint64
	Owner      *string
	LeaseUntil *time.Time
	SentAt     *time.Time
}

func claimOutboxLease(ctx context.Context, db *gorm.DB, owner string, limit int) ([]leaseOutboxRow, error) {
	if owner == "" || len(owner) > 64 || limit < 1 || limit > 100 {
		return nil, errors.New("invalid claim")
	}
	var rows []leaseOutboxRow
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("eval_outbox_lease_v1").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("sent_at IS NULL AND (lease_until IS NULL OR lease_until<=NOW(6))").Order("id").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uint64, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		res := tx.Table("eval_outbox_lease_v1").Where("id IN ?", ids).Updates(map[string]any{"owner": owner, "lease_until": gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)")})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(ids)) {
			return errors.New("claim row count mismatch")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}
func markClaimedOutbox(ctx context.Context, db *gorm.DB, id uint64, owner string) (bool, error) {
	if id == 0 || owner == "" || len(owner) > 64 {
		return false, errors.New("invalid mark")
	}
	res := db.WithContext(ctx).Table("eval_outbox_lease_v1").Where("id=? AND owner=? AND sent_at IS NULL AND lease_until>NOW(6)", id, owner).Update("sent_at", gorm.Expr("NOW(6)"))
	return res.RowsAffected == 1, res.Error
}

func TestOutboxLeaseConcurrentAndStaleOwner(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_outbox_lease_v1 (id BIGINT UNSIGNED PRIMARY KEY, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6), KEY eligible (sent_at,lease_until,id)) ENGINE=InnoDB",
		"DELETE FROM eval_outbox_lease_v1",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 16; i++ {
		if err := db.Exec("INSERT INTO eval_outbox_lease_v1(id) VALUES(?)", i).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	start := make(chan struct{})
	type result struct {
		rows  []leaseOutboxRow
		err   error
		owner string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := fmt.Sprintf("owner-%d", i)
			<-start
			rows, err := claimOutboxLease(ctx, db, owner, 8)
			results <- result{rows, err, owner}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	owners := map[uint64]string{}
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if len(r.rows) != 8 {
			t.Fatalf("%s claimed %d", r.owner, len(r.rows))
		}
		for _, row := range r.rows {
			if _, exists := owners[row.ID]; exists {
				t.Fatalf("duplicate id %d", row.ID)
			}
			owners[row.ID] = r.owner
		}
	}
	if len(owners) != 16 {
		t.Fatalf("claimed=%d", len(owners))
	}
	var staleID uint64
	for id := range owners {
		staleID = id
		break
	}
	if err := db.Exec("UPDATE eval_outbox_lease_v1 SET lease_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=?", staleID).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := claimOutboxLease(ctx, db, "replacement", 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != staleID {
		t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
	}
	if ok, err := markClaimedOutbox(ctx, db, staleID, owners[staleID]); err != nil || ok {
		t.Fatalf("stale mark=%v err=%v", ok, err)
	}
	if ok, err := markClaimedOutbox(ctx, db, staleID, "replacement"); err != nil || !ok {
		t.Fatalf("new mark=%v err=%v", ok, err)
	}
	if ok, err := markClaimedOutbox(ctx, db, staleID, "replacement"); err != nil || ok {
		t.Fatalf("duplicate mark=%v err=%v", ok, err)
	}
	t.Logf("two owners claim disjoint 16 rows; expired id=%d reclaimed; old owner mark rejected", staleID)
}

// PASS demonstrates that owner reuse cannot fence an older operation.
func TestOutboxLeaseReusedOwnerAllowsStaleMark(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_outbox_lease_v1 (id BIGINT UNSIGNED PRIMARY KEY, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6), KEY eligible (sent_at,lease_until,id)) ENGINE=InnoDB",
		"DELETE FROM eval_outbox_lease_v1", "INSERT INTO eval_outbox_lease_v1(id) VALUES(1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if rows, err := claimOutboxLease(ctx, db, "reused", 1); err != nil || len(rows) != 1 {
		t.Fatalf("first=%v err=%v", rows, err)
	}
	if err := db.Exec("UPDATE eval_outbox_lease_v1 SET lease_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if rows, err := claimOutboxLease(ctx, db, "reused", 1); err != nil || len(rows) != 1 {
		t.Fatalf("reclaim=%v err=%v", rows, err)
	}
	if ok, err := markClaimedOutbox(ctx, db, 1, "reused"); err != nil || !ok {
		t.Fatalf("old mark=%v err=%v", ok, err)
	}
	t.Log("reused owner cannot distinguish old and new claim; stale mark accepted")
}
