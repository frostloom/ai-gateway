package comparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
)

func TestRouterProbeCancellationOrder(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("probe-order-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 1
	reg.Cooldown = -time.Second
	if got, err := reg.Route(name); err != nil || got == nil {
		t.Fatalf("warm: %v %v", got, err)
	}
	reg.Report(p.ID, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := reg.RouteExcludeContext(ctx, name, nil); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel before route: %v %v", got, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	got, err := reg.RouteExcludeContext(ctx, name, nil)
	if err != nil || got == nil || got.ID != p.ID {
		cancel()
		t.Fatalf("route before cancel: %v %v", got, err)
	}
	cancel() // Selection has already returned; no report is sent.
	if next, err := reg.Route(name); err != nil || next != nil {
		t.Fatalf("selected probe should remain held: %v %v", next, err)
	}
	reg.Report(p.ID, true)
	if next, err := reg.Route(name); err != nil || next == nil || next.ID != p.ID {
		t.Fatalf("report should close breaker: %v %v", next, err)
	}
	t.Log("cancel before selection rejected; cancel after selection holds probe until report or timeout")
}
