package comparison

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Test-only scan: no scheduler, durable cursor, authorization or poison-row isolation.
func experimentalRecoverCompletedAttempts(ctx context.Context, db *gorm.DB) (int, error) {
	return experimentalRecoverCompletedAttemptsHook(ctx, db, nil)
}

func experimentalRecoverCompletedAttemptsHook(ctx context.Context, db *gorm.DB, afterSelect func(int) error) (int, error) {
	type candidate struct {
		BillID    uint64
		AttemptID string
	}
	var candidates []candidate
	err := db.WithContext(ctx).Raw(`SELECT b.id AS bill_id, a.attempt_id FROM eval_route_attempt AS a JOIN bills AS b
 ON b.completion_attempt_id=a.attempt_id AND b.tenant_id=a.tenant_id AND b.request_id=a.request_id AND b.provider_id=a.provider_id
 WHERE b.phase='reserve' AND b.status IN ('pending','settled') AND b.usage_reported_at IS NOT NULL AND a.state='sending'
 ORDER BY a.attempt_id LIMIT 128`).Scan(&candidates).Error
	if err != nil {
		return 0, err
	}
	if afterSelect != nil {
		if err := afterSelect(len(candidates)); err != nil {
			return 0, err
		}
	}
	recovered := 0
	for _, c := range candidates {
		won := false
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Lock the bill first, matching the usage-report path, then the attempt.
			var bill struct {
				TenantID   uint64
				RequestID  string
				ProviderID uint64
			}
			e := tx.Table("bills").Select("tenant_id, request_id, provider_id").Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id=? AND completion_attempt_id=? AND phase='reserve' AND status IN ('pending','settled') AND usage_reported_at IS NOT NULL", c.BillID, c.AttemptID).Take(&bill).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			var row attemptRow
			e = tx.Table("eval_route_attempt").Clauses(clause.Locking{Strength: "UPDATE"}).Where("attempt_id=? AND tenant_id=? AND request_id=? AND provider_id=?", c.AttemptID, bill.TenantID, bill.RequestID, bill.ProviderID).Take(&row).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			if row.State != "sending" {
				return nil
			}
			q := tx.Table("eval_route_attempt").Where("attempt_id=? AND state='sending'", c.AttemptID).Updates(map[string]any{"state": "completed", "outcome": true})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return nil
			}
			if e := tx.Table("eval_attempt_outbox").Create(&attemptOutbox{AttemptID: c.AttemptID, ProviderID: row.ProviderID, Outcome: true}).Error; e != nil {
				return e
			}
			won = true
			return nil
		})
		if err != nil {
			return recovered, err
		}
		if won {
			recovered++
		}
	}
	return recovered, nil
}
