package comparison

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Test-only coordinated path. All writers must use the same bill-first order.
func completeAttemptCoordinated(ctx context.Context, db *gorm.DB, tenant uint64, request, id string, outcome bool) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var bill struct{ CompletionAttemptID *string }
		if err := tx.Table("bills").Select("completion_attempt_id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND request_id=? AND phase='reserve'", tenant, request).Take(&bill).Error; err != nil {
			return err
		}
		if !outcome && bill.CompletionAttemptID != nil && *bill.CompletionAttemptID == id {
			return errAttemptConflict
		}
		var row attemptRow
		if err := tx.Table("eval_route_attempt").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND request_id=? AND attempt_id=?", tenant, request, id).Take(&row).Error; err != nil {
			return err
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
		q := tx.Table("eval_route_attempt").Where("attempt_id=? AND state='sending'", id).Updates(map[string]any{"state": "completed", "outcome": outcome})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return errAttemptConflict
		}
		if err := tx.Table("eval_attempt_outbox").Create(&attemptOutbox{AttemptID: id, ProviderID: row.ProviderID, Outcome: outcome}).Error; err != nil {
			return err
		}
		won = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return won, nil
}
