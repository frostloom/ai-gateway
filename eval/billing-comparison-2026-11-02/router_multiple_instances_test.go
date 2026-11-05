package comparison

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm/logger"
)

// Characterizes separate in-memory registries, not a normative global threshold.
func TestRouterFailuresSplitAcrossRegistries(t *testing.T) {
	if os.Getenv("TEST_ROUTER_INSTANCES") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("two-routers-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := router.New(db, slog.New(slog.DiscardHandler))
	b := router.New(db, slog.New(slog.DiscardHandler))
	for i := 0; i < 10; i++ {
		a.Report(p.ID, false)
		b.Report(p.ID, false)
	}
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	as, bs := a.States(), b.States()
	if len(as) != 1 || len(bs) != 1 {
		t.Fatalf("states=%+v/%+v", as, bs)
	}
	if got.Status != 0 || got.ConsecutiveFail != 20 || as[0].ConsecutiveFail != 10 || bs[0].ConsecutiveFail != 10 {
		t.Fatalf("provider=%+v states=%+v/%+v", got, as, bs)
	}
	// Reconstruct the registry as on a restart; check whether its local breaker imports DB history.
	c := router.New(db, slog.New(slog.DiscardHandler))
	c.Report(p.ID, false)
	var after store.Provider
	if err := db.First(&after, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	cs := c.States()
	if len(cs) != 1 || cs[0].ConsecutiveFail != 1 || after.Status != 0 || after.ConsecutiveFail != 21 {
		t.Fatalf("after=%+v states=%+v", after, cs)
	}
	t.Logf("two_registry_failures=20 db_count=%d local=%d/%d status=%d; new_registry_local=%d db_after=%d status=%d", got.ConsecutiveFail, as[0].ConsecutiveFail, bs[0].ConsecutiveFail, got.Status, cs[0].ConsecutiveFail, after.ConsecutiveFail, after.Status)
}
