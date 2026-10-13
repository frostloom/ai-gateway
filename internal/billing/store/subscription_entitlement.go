package store

import (
	"context"
	"gorm.io/gorm"
	"time"
)

// getEntitledRecurring separates usable current-cycle rights from subscriptions
// that may still be cancelled, changed or renewed. Active rows keep precedence.
func (s *Store) getEntitledRecurring(ctx context.Context, tenantID uint64) (*Subscription, error) {
	sub, err := s.GetActiveRecurring(ctx, tenantID)
	if err != nil || sub != nil {
		return sub, err
	}
	var cancelled Subscription
	err = s.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND plan_type = ? AND cycle_end IS NOT NULL AND cycle_end > ?", tenantID, SubCancelled, PlanTypeRecurring, time.Now()).
		Order("cycle_end DESC, id DESC").First(&cancelled).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cancelled, nil
}
