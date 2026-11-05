package comparison

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
)

func TestRouterRestartDoesNotRestoreOpenBreaker(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	model := fmt.Sprintf("restart-breaker-%d", time.Now().UnixNano())
	p := store.Provider{Name: model, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, model), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 2
	reg.Cooldown = time.Hour
	for i := 0; i < 2; i++ {
		if err := reg.Report(p.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	picked, err := reg.Route(model)
	if err != nil || picked != nil {
		t.Fatalf("before restart: provider=%v err=%v", picked, err)
	}
	var row store.Provider
	if err := db.First(&row, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.ConsecutiveFail != 2 || row.Status != 0 {
		t.Fatalf("stored failures=%d status=%d", row.ConsecutiveFail, row.Status)
	}
	restarted := router.New(db, slog.New(slog.DiscardHandler))
	restarted.FailThreshold = 2
	restarted.Cooldown = time.Hour
	picked, err = restarted.Route(model)
	if err != nil || picked == nil || picked.ID != p.ID {
		t.Fatalf("after restart: provider=%v err=%v", picked, err)
	}
	states := restarted.States()
	if len(states) != 1 || states[0].State != "closed" || states[0].ConsecutiveFail != 0 {
		t.Fatalf("states=%+v", states)
	}
	t.Log("new Registry selects the active provider despite stored failures and prior open state; prior cooldown is not stored")
}
