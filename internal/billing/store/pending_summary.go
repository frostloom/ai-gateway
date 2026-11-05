package store

import (
	"context"
	"time"
)

// PendingSummary is a read-only snapshot of pending reserve rows. A usage
// marker alone does not guarantee that the row is complete or settleable.
type PendingSummary struct {
	PendingTotal           int64      `json:"pending_total"`
	UsageMarkerMissing     int64      `json:"usage_marker_missing"`
	UsageMarkerPresent     int64      `json:"usage_marker_present"`
	MissingOldestCreatedAt *time.Time `json:"missing_oldest_created_at"`
	PresentOldestCreatedAt *time.Time `json:"present_oldest_created_at"`
	ObservedAt             time.Time  `json:"observed_at"`
}

// PendingReserveSummary counts rows at one database statement snapshot.
// ObservedAt is the application time after that statement returns.
func (s *Store) PendingReserveSummary(ctx context.Context) (*PendingSummary, error) {
	var result PendingSummary
	err := s.db.WithContext(ctx).Table("bills").Select(`COUNT(*) AS pending_total,
 COALESCE(SUM(CASE WHEN usage_reported_at IS NULL THEN 1 ELSE 0 END),0) AS usage_marker_missing,
 COALESCE(SUM(CASE WHEN usage_reported_at IS NOT NULL THEN 1 ELSE 0 END),0) AS usage_marker_present,
 MIN(CASE WHEN usage_reported_at IS NULL THEN created_at ELSE NULL END) AS missing_oldest_created_at,
 MIN(CASE WHEN usage_reported_at IS NOT NULL THEN created_at ELSE NULL END) AS present_oldest_created_at`).
		Where("phase = ? AND status = ?", PhaseReserve, StatusPending).Scan(&result).Error
	if err != nil {
		return nil, err
	}
	result.ObservedAt = time.Now().UTC()
	return &result, nil
}
