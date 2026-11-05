package comparison

import (
	"context"
	"errors"
	"testing"
)

// This exercises a synthetic inconsistent test-table state, not production data.
func TestHeldProbeDiscoveryMustExposeConflictingEvent(t *testing.T) {
	db := setupHeldFailure(t)
	if err := db.Exec("INSERT INTO eval_probe_generation_event(event_id,token,generation,success) VALUES(?,?,?,?)", "first", "wrong-token", 1, false).Error; err != nil {
		t.Fatal(err)
	}
	var oldPending int64
	if err := db.Raw("SELECT COUNT(*) FROM eval_bound_probe_outbox o LEFT JOIN eval_probe_generation_event e ON e.event_id=o.id WHERE e.event_id IS NULL").Scan(&oldPending).Error; err != nil {
		t.Fatal(err)
	}
	if oldPending != 0 {
		t.Fatalf("original discovery pending=%d", oldPending)
	}
	if _, err := applyHeldWithCooldown(context.Background(), db, "first"); !errors.Is(err, errBreakerConflict) {
		t.Fatalf("direct application expected conflict, got %v", err)
	}
	var conflicts int64
	if err := db.Raw(`SELECT COUNT(*) FROM eval_bound_probe_outbox o JOIN eval_probe_generation_event e ON e.event_id=o.id WHERE NOT (e.token = o.token AND e.generation = o.generation AND e.success = o.outcome)`).Scan(&conflicts).Error; err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts=%d", conflicts)
	}
	var row generationRow
	if err := db.Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Generation != 1 {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	t.Log("ID-only discovery hid conflict; direct application rejected it; separate comparison detected it; state remains held")
}
