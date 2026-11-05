package comparison

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Test-only composition; the production router does not use this table or API.
func durableGenerationReport(ctx context.Context, db *gorm.DB, event, token string, generation int64, success bool) (bool, error) {
	if event == "" || len(event) > 128 || token == "" || len(token) > 64 || generation <= 0 {
		return false, errProbeStale
	}
	applied := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state durableBreakerRow
		if err := tx.Table("eval_durable_breaker").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&state).Error; err != nil {
			return err
		}
		var prior struct {
			Success    bool
			ProbeToken string
			Generation int64
		}
		err := tx.Table("eval_durable_generation_event").Where("event_id=?", event).Take(&prior).Error
		if err == nil {
			if prior.Success != success || prior.ProbeToken != token || prior.Generation != generation {
				return errBreakerConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT NOW(6)").Scan(&now).Error; err != nil {
			return err
		}
		if state.State != "half_open" || state.Version != generation || state.ProbeToken == nil || *state.ProbeToken != token || state.ProbeUntil == nil || !state.ProbeUntil.After(now) {
			return errProbeStale
		}
		if state.Version == math.MaxInt64 {
			return errors.New("breaker version exhausted")
		}
		if err := tx.Table("eval_durable_generation_event").Create(map[string]any{"event_id": event, "success": success, "probe_token": token, "generation": generation}).Error; err != nil {
			return err
		}
		update := map[string]any{"probe_token": nil, "probe_until": nil, "version": state.Version + 1}
		if success {
			update["state"] = "closed"
			update["failures"] = 0
			update["open_until"] = nil
		} else {
			update["state"] = "open"
			update["failures"] = state.Failures + 1
			update["open_until"] = gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)")
		}
		if err := tx.Table("eval_durable_breaker").Where("id=1").Updates(update).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

// durableGenerationPermit grants only a half-open probe; zero means no grant.
func durableGenerationPermit(ctx context.Context, db *gorm.DB, token string) (int64, error) {
	if token == "" || len(token) > 64 {
		return 0, errProbeStale
	}
	var generation int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state durableBreakerRow
		if err := tx.Table("eval_durable_breaker").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&state).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT NOW(6)").Scan(&now).Error; err != nil {
			return err
		}
		if state.State == "open" && (state.OpenUntil == nil || state.OpenUntil.After(now)) {
			return nil
		}
		if state.State == "half_open" && state.ProbeUntil != nil && state.ProbeUntil.After(now) {
			return nil
		}
		if state.State != "open" && state.State != "half_open" {
			return errors.New("invalid breaker state")
		}
		if state.Version == math.MaxInt64 {
			return errors.New("breaker version exhausted")
		}
		next := state.Version + 1
		if err := tx.Table("eval_durable_breaker").Where("id=1").Updates(map[string]any{"state": "half_open", "probe_token": token, "probe_until": gorm.Expr("DATE_ADD(NOW(6),INTERVAL 30 SECOND)"), "version": next}).Error; err != nil {
			return err
		}
		generation = next
		return nil
	})
	if err != nil {
		return 0, err
	}
	return generation, nil
}
