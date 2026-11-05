package comparison

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

type probeDiscoveryRow struct {
	ID     string
	Status string
}

// Test-only keyset scan: no durable quarantine or concurrent scan snapshot.
func scanProbeOutbox(ctx context.Context, db *gorm.DB, cursor string, limit int) ([]probeDiscoveryRow, error) {
	if limit < 1 || limit > 100 || len(cursor) > 32 {
		return nil, fmt.Errorf("invalid scan bounds")
	}
	var rows []probeDiscoveryRow
	err := db.WithContext(ctx).Raw(`SELECT o.id,
      CASE WHEN a.id IS NULL OR NOT (a.state <=> 'done' AND a.token <=> o.token AND a.generation <=> o.generation AND a.outcome <=> o.outcome)
        OR (e.event_id IS NOT NULL AND NOT (e.token <=> o.token AND e.generation <=> o.generation AND e.success <=> o.outcome))
        THEN 'conflict' ELSE 'pending' END AS status
      FROM eval_bound_probe_outbox o LEFT JOIN eval_probe_generation_event e ON e.event_id=o.id
      LEFT JOIN eval_bound_probe_attempt a ON a.id=o.id
      WHERE o.id > ? AND (a.id IS NULL OR NOT (a.state <=> 'done' AND a.token <=> o.token AND a.generation <=> o.generation AND a.outcome <=> o.outcome)
        OR e.event_id IS NULL OR NOT (e.token <=> o.token AND e.generation <=> o.generation AND e.success <=> o.outcome))
      ORDER BY o.id LIMIT ?`, cursor, limit).Scan(&rows).Error
	return rows, err
}
