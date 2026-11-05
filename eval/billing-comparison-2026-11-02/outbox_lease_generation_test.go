package comparison

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type leaseGenerationRow struct {
	ID         uint64
	Generation int64
	Owner      *string
	LeaseUntil *time.Time
	SentAt     *time.Time
}

func claimOutboxGeneration(ctx context.Context, db *gorm.DB, owner string) (leaseGenerationRow, error) {
	if owner == "" || len(owner) > 64 {
		return leaseGenerationRow{}, errors.New("invalid owner")
	}
	var row leaseGenerationRow
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Table("eval_outbox_lease_v2").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("sent_at IS NULL AND (lease_until IS NULL OR lease_until<=NOW(6))").Order("id").Limit(1).Find(&row)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected == 0 {
			return nil
		}
		if row.Generation == math.MaxInt64 {
			return errors.New("generation exhausted")
		}
		row.Generation++
		if err := tx.Table("eval_outbox_lease_v2").Where("id=?", row.ID).Updates(map[string]any{"generation": row.Generation, "owner": owner, "lease_until": gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)")}).Error; err != nil {
			return err
		}
		return tx.Table("eval_outbox_lease_v2").Where("id=?", row.ID).Take(&row).Error
	})
	if err != nil {
		return leaseGenerationRow{}, err
	}
	return row, nil
}
func markOutboxGeneration(ctx context.Context, db *gorm.DB, id uint64, owner string, generation int64) (bool, error) {
	if id == 0 || owner == "" || len(owner) > 64 || generation <= 0 {
		return false, errors.New("invalid mark")
	}
	q := db.WithContext(ctx).Table("eval_outbox_lease_v2").Where("id=? AND owner=? AND generation=? AND sent_at IS NULL AND lease_until>NOW(6)", id, owner, generation).Update("sent_at", gorm.Expr("NOW(6)"))
	return q.RowsAffected == 1, q.Error
}

func TestOutboxLeaseGenerationFencesReuseAndOverflow(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_outbox_lease_v2 (id BIGINT UNSIGNED PRIMARY KEY, generation BIGINT NOT NULL DEFAULT 0, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6)) ENGINE=InnoDB",
		"DELETE FROM eval_outbox_lease_v2", "INSERT INTO eval_outbox_lease_v2(id) VALUES(1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	first, err := claimOutboxGeneration(ctx, db, "same")
	if err != nil || first.ID != 1 || first.Generation != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var committed leaseGenerationRow
	if err := db.Table("eval_outbox_lease_v2").Where("id=1").Take(&committed).Error; err != nil {
		t.Fatal(err)
	}
	if first.Owner == nil || *first.Owner != "same" || first.LeaseUntil == nil || committed.LeaseUntil == nil || !first.LeaseUntil.Equal(*committed.LeaseUntil) {
		t.Fatalf("returned claim=%+v committed=%+v", first, committed)
	}
	if err := db.Exec("UPDATE eval_outbox_lease_v2 SET lease_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	second, err := claimOutboxGeneration(ctx, db, "same")
	if err != nil || second.ID != 1 || second.Generation != 2 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", first.Generation); err != nil || ok {
		t.Fatalf("stale=%v err=%v", ok, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", second.Generation); err != nil || !ok {
		t.Fatalf("current=%v err=%v", ok, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", second.Generation); err != nil || ok {
		t.Fatalf("replay=%v err=%v", ok, err)
	}
	if err := db.Exec("UPDATE eval_outbox_lease_v2 SET sent_at=NULL, lease_until=NULL, generation=? WHERE id=1", int64(math.MaxInt64)).Error; err != nil {
		t.Fatal(err)
	}
	if row, err := claimOutboxGeneration(ctx, db, "same"); err == nil || row.ID != 0 {
		t.Fatalf("overflow=%+v err=%v", row, err)
	}
	var got leaseGenerationRow
	if err := db.Table("eval_outbox_lease_v2").Where("id=1").Take(&got).Error; err != nil || got.Generation != math.MaxInt64 || got.LeaseUntil != nil || got.SentAt != nil {
		t.Fatalf("after overflow=%+v err=%v", got, err)
	}
	t.Log("same owner: stale generation rejected, current marked once, overflow rejected")
}

func TestOutboxGenerationConcurrentOneWinner(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_outbox_lease_v2 (id BIGINT UNSIGNED PRIMARY KEY, generation BIGINT NOT NULL DEFAULT 0, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6)) ENGINE=InnoDB",
		"DELETE FROM eval_outbox_lease_v2", "INSERT INTO eval_outbox_lease_v2(id) VALUES(1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		row leaseGenerationRow
		err error
	}
	const workers = 8
	results := make(chan result, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			row, err := claimOutboxGeneration(context.Background(), db, "same-owner")
			results <- result{row, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.row.ID != 0 {
			if r.row.ID != 1 || r.row.Generation != 1 {
				t.Fatalf("row=%+v", r.row)
			}
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
	t.Logf("same owner concurrent claim: wins=%d generation=1", wins)
}
