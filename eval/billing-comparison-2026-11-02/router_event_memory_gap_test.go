package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
)

// A characterization of an unsafe composition, not a production implementation.
func TestDurableEventRetryCannotRecoverLostMemoryTransition(t *testing.T) {
	db := setupReportEventDB(t)
	ctx := context.Background()
	id := fmt.Sprintf("memory-gap-%d", time.Now().UnixNano())
	event := reportEvent{ID: id, Provider: 1, Success: false}
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	provider := store.Provider{ID: 1, Name: "memory-gap-provider", BaseURL: "http://127.0.0.1:1", Models: `["event-memory-gap"]`, Weight: 1}
	if err := db.Where("id=?", 1).FirstOrCreate(&provider).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 1
	if picked, e := reg.Route("event-memory-gap"); e != nil || picked == nil || picked.ID != 1 {
		t.Fatal(picked, e)
	}
	n, err := applyReportEventsBulk(ctx, db, []reportEvent{event})
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// Simulate the consumer stopping after commit but before the in-memory Report.
	n, err = applyReportEventsBulk(ctx, db, []reportEvent{event})
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
	// A naive consumer applies only newly inserted events to memory.
	if n > 0 {
		reg.Report(event.Provider, event.Success)
	}
	var failures int
	if err := db.Raw("SELECT consecutive_fail FROM eval_route_provider WHERE id=1").Scan(&failures).Error; err != nil {
		t.Fatal(err)
	}
	var events int64
	if err := db.Table("eval_route_event").Where("event_id=?", id).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	states := reg.States()
	picked, routeErr := reg.Route("event-memory-gap")
	if failures != 1 || events != 1 || len(states) != 1 || states[0].ConsecutiveFail != 0 || states[0].State != "closed" || routeErr != nil || picked == nil || picked.ID != 1 {
		t.Fatalf("db_failures=%d events=%d memory=%v", failures, events, states)
	}
	control := router.New(nil, slog.New(slog.DiscardHandler))
	control.FailThreshold = 1
	control.Report(1, false)
	controlStates := control.States()
	if len(controlStates) != 1 || controlStates[0].State != "open" {
		t.Fatal(controlStates)
	}
	t.Logf("committed=1 retry_applied=%d durable_events=%d db_failures=%d memory_state=%s routed=true control_state=%s", n, events, failures, states[0].State, controlStates[0].State)
}
