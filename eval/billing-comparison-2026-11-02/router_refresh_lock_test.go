package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type blockRefreshKey struct{}
type refreshResult struct {
	provider *router.Provider
	err      error
}

func setupBlockedRouterRefresh(t *testing.T) (*router.Registry, *store.Provider, string, string, <-chan struct{}, chan struct{}, <-chan refreshResult, *atomic.Int32) {
	t.Helper()
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("refresh-lock-%d", time.Now().UnixNano())
	warm, cold := tag+"-warm", tag+"-cold"
	p := &store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s","%s"]`, warm, cold), Weight: 1}
	if err := db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if selected, err := reg.Route(warm); err != nil || selected == nil || selected.ID != p.ID {
		t.Fatalf("warm=%+v err=%v", selected, err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan refreshResult, 1)
	var once sync.Once
	var queries atomic.Int32
	name := "eval:blocked-refresh-" + tag
	if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" && tx.Statement.Context.Value(blockRefreshKey{}) == true {
			queries.Add(1)
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	go func() {
		ctx := context.WithValue(context.Background(), blockRefreshKey{}, true)
		got, err := reg.RouteExcludeContext(ctx, cold, nil)
		done <- refreshResult{got, err}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("refresh did not reach query barrier")
	}
	return reg, p, warm, cold, entered, release, done, &queries
}
func TestRouterCachedRouteDuringBlockedRefresh(t *testing.T) {
	reg, p, warm, _, _, release, done, _ := setupBlockedRouterRefresh(t)
	results := make(chan error, 1)
	go func() {
		got, err := reg.Route(warm)
		if err == nil && (got == nil || got.ID != p.ID) {
			err = fmt.Errorf("wrong provider: %+v", got)
		}
		results <- err
	}()
	var routeErr error
	select {
	case routeErr = <-results:
	case <-time.After(500 * time.Millisecond):
		routeErr = fmt.Errorf("cached route blocked by unrelated refresh")
	}
	close(release)
	if result := <-done; result.err != nil || result.provider == nil || result.provider.ID != p.ID {
		t.Fatalf("refresh=%+v", result)
	}
	if routeErr != nil {
		t.Fatal(routeErr)
	}
}
func TestRouterRefreshDoesNotRestoreBannedProvider(t *testing.T) {
	reg, p, _, cold, _, release, done, queries := setupBlockedRouterRefresh(t)
	reg.AutobanAfter = 1
	banned := make(chan struct{})
	go func() { reg.Report(p.ID, false); close(banned) }()
	var blocked bool
	select {
	case <-banned:
	case <-time.After(500 * time.Millisecond):
		blocked = true
	}
	close(release)
	if result := <-done; result.err != nil || result.provider != nil {
		t.Fatalf("refresh returned stale provider: %+v", result)
	}
	if blocked {
		<-banned
		t.Fatal("ban blocked behind refresh")
	}
	if queries.Load() < 2 {
		t.Fatalf("stale in-flight query was not discarded: queries=%d", queries.Load())
	}
	got, err := reg.Route(cold)
	if err != nil || got != nil {
		t.Fatalf("banned provider restored by stale refresh: got=%+v err=%v", got, err)
	}
}

func TestRouterRefreshWaiterHonorsCancellation(t *testing.T) {
	reg, _, _, cold, _, release, done, _ := setupBlockedRouterRefresh(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { _, err := reg.RouteExcludeContext(ctx, cold, nil); returned <- err }()
	var err error
	select {
	case err = <-returned:
	case <-time.After(350 * time.Millisecond):
		err = fmt.Errorf("waiting for refresh ignored cancellation")
	}
	close(release)
	if result := <-done; result.err != nil {
		t.Fatalf("first refresh=%+v", result)
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("waiter: %v", err)
	}
}
