package comparison

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Test-only single-provider state machine: threshold 1, no AutoBan.
type durableBreakerRow struct {
	State      string
	Failures   int
	OpenUntil  *time.Time
	ProbeToken *string
	ProbeUntil *time.Time
	Version    int64
}
type durableBreakerEvent struct {
	EventID    string
	Success    bool
	ProbeToken string
}

func durableBreakerReport(ctx context.Context, db *gorm.DB, id string, success bool, probe string) (applied bool, err error) {
	if id == "" || len(id) > 128 || len(probe) > 64 {
		return false, fmt.Errorf("invalid event")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row durableBreakerRow
		if e := tx.Table("eval_durable_breaker").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&row).Error; e != nil {
			return e
		}
		var existing durableBreakerEvent
		e := tx.Table("eval_durable_breaker_event").Where("event_id=?", id).Take(&existing).Error
		if e == nil {
			if existing.Success != success || existing.ProbeToken != probe {
				return errBreakerConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var now time.Time
		if e := tx.Raw("SELECT NOW(6)").Scan(&now).Error; e != nil {
			return e
		}
		if row.State == "half_open" {
			if row.ProbeToken == nil || *row.ProbeToken != probe || row.ProbeUntil == nil || !row.ProbeUntil.After(now) {
				return fmt.Errorf("stale probe")
			}
		} else if row.State != "closed" || probe != "" {
			return fmt.Errorf("invalid breaker state")
		}
		if e := tx.Table("eval_durable_breaker_event").Create(&durableBreakerEvent{EventID: id, Success: success, ProbeToken: probe}).Error; e != nil {
			return e
		}
		updates := map[string]any{"version": row.Version + 1, "probe_token": nil, "probe_until": nil}
		if success {
			updates["state"] = "closed"
			updates["failures"] = 0
			updates["open_until"] = nil
		} else {
			updates["state"] = "open"
			updates["failures"] = row.Failures + 1
			updates["open_until"] = gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)")
		}
		if e := tx.Table("eval_durable_breaker").Where("id=1").Updates(updates).Error; e != nil {
			return e
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}
func durableBreakerPermit(ctx context.Context, db *gorm.DB, probe string) (allowed bool, err error) {
	if probe == "" || len(probe) > 64 {
		return false, fmt.Errorf("invalid probe")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row durableBreakerRow
		if e := tx.Table("eval_durable_breaker").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&row).Error; e != nil {
			return e
		}
		if row.State == "closed" {
			allowed = true
			return nil
		}
		var now time.Time
		if e := tx.Raw("SELECT NOW(6)").Scan(&now).Error; e != nil {
			return e
		}
		if row.State == "open" && (row.OpenUntil == nil || row.OpenUntil.After(now)) {
			return nil
		}
		if row.State == "half_open" && row.ProbeUntil != nil && row.ProbeUntil.After(now) {
			return nil
		}
		if row.State != "open" && row.State != "half_open" {
			return fmt.Errorf("invalid state")
		}
		if e := tx.Table("eval_durable_breaker").Where("id=1").Updates(map[string]any{"state": "half_open", "probe_token": probe, "probe_until": gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)"), "version": row.Version + 1}).Error; e != nil {
			return e
		}
		allowed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return allowed, nil
}
