package comparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRouterCanceledInflightCacheQuery(t *testing.T) {
	if os.Getenv("TEST_CANCELED_QUERY") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	model := fmt.Sprintf("cancel-query-%d", time.Now().UnixNano())
	p := store.Provider{Name: model, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, model), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	var once atomic.Bool
	entered := make(chan struct{})
	key := fmt.Sprintf("test:slow-query-%d", time.Now().UnixNano())
	if err := db.Callback().Query().Before("gorm:query").Register(key, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" && once.CompareAndSwap(false, true) {
			close(entered)
			var ignored int
			err := db.WithContext(tx.Statement.Context).Raw("SELECT SLEEP(5)").Scan(&ignored).Error
			if err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(key)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	started := time.Now()
	go func() { _, err := reg.RouteExcludeContext(ctx, model, nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("query not started")
	}
	var err error
	timely := false
	select {
	case err = <-done:
		timely = true
	case <-time.After(2 * time.Second):
		err = <-done
	}
	elapsed := time.Since(started)
	if !timely || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow query not canceled promptly: elapsed=%v err=%v", elapsed, err)
	}
	next, err := reg.Route(model)
	if err != nil || next == nil || next.ID != p.ID {
		t.Fatalf("subsequent route=%v err=%v", next, err)
	}
	t.Logf("query_cancel_elapsed_ms=%d subsequent_route_ok=true", elapsed.Milliseconds())
}
