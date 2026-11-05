package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCanceledRouteDoesNotConsumeHalfOpenProbe(t *testing.T) {
	if os.Getenv("TEST_CANCELED_ROUTE") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	model := fmt.Sprintf("cancel-probe-%d", time.Now().UnixNano())
	a := store.Provider{Name: model + "-a", BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, model), Weight: 1}
	b := store.Provider{Name: model + "-b", BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 1
	reg.Cooldown = -time.Second
	if p, err := reg.Route(model); err != nil || p == nil || p.ID != a.ID {
		t.Fatalf("initial=%v err=%v", p, err)
	}
	reg.Report(a.ID, false)
	entered, release, reportDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	key := fmt.Sprintf("test:block-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(key, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(key)
	go func() { reg.Report(b.ID, true); close(reportDone) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("report did not hold lock")
	}
	started, finished := make(chan struct{}), make(chan struct{})
	mux := router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		defer close(finished)
		// Make cancellation happen before selection, independent of report locks.
		<-r.Context().Done()
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/route?model="+model, nil)
	req.Header.Set(controlplane.Header, "test-token")
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		cancel()
		t.Fatal("route not started")
	}
	cancel()
	if err := <-clientDone; err == nil {
		close(release)
		t.Fatal("expected canceled client")
	}
	close(release)
	select {
	case <-reportDone:
	case <-time.After(3 * time.Second):
		t.Fatal("report did not finish")
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("server handler did not finish")
	}
	selected, err := reg.Route(model)
	if err != nil || selected == nil || selected.ID != a.ID {
		t.Fatalf("canceled route consumed half-open probe: selected=%v err=%v", selected, err)
	}
}

func TestCanceledRouteSkipsCacheRefresh(t *testing.T) {
	if os.Getenv("TEST_CANCELED_ROUTE") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	model := fmt.Sprintf("cancel-refresh-%d", time.Now().UnixNano())
	p := store.Provider{Name: model, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, model), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	entered, release, reportDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	key := fmt.Sprintf("test:block-refresh-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(key, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(key)
	var queries atomic.Int32
	qkey := key + "-query"
	if err := db.Callback().Query().Before("gorm:query").Register(qkey, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			queries.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(qkey)
	go func() { reg.Report(p.ID, true); close(reportDone) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("report did not hold lock")
	}
	started, finished := make(chan struct{}), make(chan struct{})
	mux := router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		defer close(finished)
		// Do not infer selection order from arrival at the HTTP wrapper.
		<-r.Context().Done()
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/route?model="+model, nil)
	req.Header.Set(controlplane.Header, "test-token")
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		cancel()
		t.Fatal("route not started")
	}
	cancel()
	if err := <-clientDone; err == nil {
		close(release)
		t.Fatal("expected canceled client")
	}
	close(release)
	select {
	case <-reportDone:
	case <-time.After(3 * time.Second):
		t.Fatal("report did not finish")
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish")
	}
	if n := queries.Load(); n != 0 {
		t.Fatalf("canceled route issued %d provider queries after lock released", n)
	}
}
