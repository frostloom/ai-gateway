package comparison

import (
	"context"
	"fmt"
	"os"
	"sync"

	"gorm.io/gorm"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
)

// PASS means the shared database ban was not propagated into another Registry's warm cache.
func TestRouterRefreshLocalGenerationDoesNotInvalidateOtherRegistry(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_CHECK") == "1" {
		t.Skip("characterizes local-cache-only variants")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("refresh-cross-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	one, two := router.New(db, slog.New(slog.DiscardHandler)), router.New(db, slog.New(slog.DiscardHandler))
	one.AutobanAfter = 1
	for _, r := range []*router.Registry{one, two} {
		got, err := r.Route(tag)
		if err != nil || got == nil || got.ID != p.ID {
			t.Fatalf("initial %+v %v", got, err)
		}
	}
	one.Report(p.ID, false)
	var actual store.Provider
	if err := db.First(&actual, p.ID).Error; err != nil || actual.Status != 2 {
		t.Fatalf("DB provider=%+v err=%v", actual, err)
	}
	got, err := two.Route(tag)
	if err != nil || got == nil || got.ID != p.ID {
		t.Fatalf("expected stale other-instance route, got %+v err=%v", got, err)
	}
	t.Log("database banned provider, second Registry still routed its cached active snapshot")
}

// A route started after a committed ban must not return its warmed provider.
func TestRouterFreshStatusAfterCrossRegistryBan(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_CHECK") != "1" {
		t.Skip("authority-check overlay only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("fresh-status-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	first, second := router.New(db, slog.New(slog.DiscardHandler)), router.New(db, slog.New(slog.DiscardHandler))
	first.AutobanAfter = 1
	for _, r := range []*router.Registry{first, second} {
		got, err := r.Route(tag)
		if err != nil || got == nil || got.ID != p.ID {
			t.Fatalf("warm=%+v err=%v", got, err)
		}
	}
	first.Report(p.ID, false)
	got, err := second.Route(tag)
	if err != nil || got != nil {
		t.Fatalf("committed ban returned stale route=%+v err=%v", got, err)
	}
}

// This controlled interleaving exposes the limit of a point-in-time check.
type authorityQueryKey struct{}

func TestRouterBanAfterStatusReadCanStillRoute(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_CHECK") != "1" {
		t.Skip("authority-check overlay only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("status-gap-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	first, second := router.New(db, slog.New(slog.DiscardHandler)), router.New(db, slog.New(slog.DiscardHandler))
	first.AutobanAfter = 1
	for _, r := range []*router.Registry{first, second} {
		got, err := r.Route(tag)
		if err != nil || got == nil || got.ID != p.ID {
			t.Fatalf("warm=%+v err=%v", got, err)
		}
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan *router.Provider, 1)
	errs := make(chan error, 1)
	var once sync.Once
	name := "eval:authority-read-" + tag
	if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" && tx.Statement.Context.Value(authorityQueryKey{}) == true {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	go func() {
		ctx := context.WithValue(context.Background(), authorityQueryKey{}, true)
		got, err := second.RouteExcludeContext(ctx, tag, nil)
		done <- got
		errs <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("authority check did not reach barrier")
	}
	first.Report(p.ID, false)
	close(release)
	got := <-done
	err := <-errs
	if err != nil || got == nil || got.ID != p.ID {
		t.Fatalf("expected in-flight stale decision, got=%+v err=%v", got, err)
	}
	if got, err := second.Route(tag); err != nil || got != nil {
		t.Fatalf("subsequent route=%+v err=%v", got, err)
	}
	t.Log("query observed active before ban; blocked in-flight route still selected provider after ban; later query denied")
}
func TestRouterAuthorityDatabaseUnavailableFailsClosed(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_CHECK") != "1" {
		t.Skip("authority-check overlay only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("authority-down-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if got, err := reg.Route(tag); err != nil || got == nil {
		t.Fatalf("warm=%+v err=%v", got, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err = sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := reg.Route(tag); err == nil || got != nil {
		t.Fatalf("unavailable DB route=%+v err=%v", got, err)
	}
}

// Status-only validation intentionally leaves other cached fields unchanged.
func TestRouterAuthorityStatusCheckDoesNotRefreshConfig(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_CHECK") != "1" || os.Getenv("TEST_ROUTER_AUTHORITY_METADATA") == "1" {
		t.Skip("authority-check overlay only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("config-gap-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", UpstreamKey: "old-synthetic", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	got, err := reg.Route(tag)
	if err != nil || got == nil {
		t.Fatalf("warm=%+v err=%v", got, err)
	}
	if err := db.Model(&p).Updates(map[string]any{"base_url": "http://127.0.0.1:2", "upstream_key": "new-synthetic"}).Error; err != nil {
		t.Fatal(err)
	}
	got, err = reg.Route(tag)
	if err != nil || got == nil || got.BaseURL != "http://127.0.0.1:1" || got.UpstreamKey != "old-synthetic" {
		t.Fatalf("expected cached old config, got=%+v err=%v", got, err)
	}
	t.Log("status-only query accepted active provider and retained cached old URL/key")
}

func TestRouterAuthorityNilDBFailsClosed(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_CHECK") != "1" {
		t.Skip("authority-check overlay only")
	}
	reg := router.New(nil, slog.New(slog.DiscardHandler))
	if got, err := reg.Route("synthetic-model"); err == nil || got != nil {
		t.Fatalf("nil database returned %+v err=%v", got, err)
	}
}

func TestRouterAuthorityRefreshesCachedMetadata(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_METADATA") != "1" {
		t.Skip("metadata authority overlay only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("metadata-fresh-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", UpstreamKey: "old-synthetic", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if got, err := reg.Route(tag); err != nil || got == nil {
		t.Fatalf("warm=%+v err=%v", got, err)
	}
	if err := db.Model(&p).Updates(map[string]any{"base_url": "http://127.0.0.1:2", "upstream_key": "new-synthetic", "weight": 2}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := reg.Route(tag)
	if err != nil || got == nil || got.BaseURL != "http://127.0.0.1:2" || got.UpstreamKey != "new-synthetic" || got.Weight != 2 {
		t.Fatalf("stale metadata returned: got=%+v err=%v", got, err)
	}
	if err := db.Model(&p).Update("models", `["other-model"]`).Error; err != nil {
		t.Fatal(err)
	}
	if got, err = reg.Route(tag); err != nil || got != nil {
		t.Fatalf("removed model routed: got=%+v err=%v", got, err)
	}
}

// A status/metadata refresh of cached IDs cannot discover a new provider.
func TestRouterAuthorityCannotDiscoverNewProviderFromEmptyCache(t *testing.T) {
	if os.Getenv("TEST_ROUTER_AUTHORITY_METADATA") != "1" {
		t.Skip("metadata authority overlay only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("new-provider-%d", time.Now().UnixNano())
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if got, err := reg.Route(tag); err != nil || got != nil {
		t.Fatalf("initial=%+v err=%v", got, err)
	}
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := reg.Route(tag); err != nil || got != nil {
		t.Fatalf("expected stale empty cache: got=%+v err=%v", got, err)
	}
	fresh := router.New(db, slog.New(slog.DiscardHandler))
	if got, err := fresh.Route(tag); err != nil || got == nil || got.ID != p.ID {
		t.Fatalf("fresh registry=%+v err=%v", got, err)
	}
	t.Log("existing Registry kept empty cache after provider insert; fresh Registry discovered provider")
}
