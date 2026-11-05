package comparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRouterCachedRouteProgressDuringBlockedReport(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("lock-scope-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if picked, err := reg.Route(name); err != nil || picked == nil || picked.ID != p.ID {
		t.Fatal(picked, err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	callback := fmt.Sprintf("eval:block-report-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	go func() { reg.Report(p.ID, true); close(done) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("report did not reach SQL callback")
	}
	result := make(chan error, 1)
	go func() {
		got, err := reg.Route(name)
		if err == nil && (got == nil || got.ID != p.ID) {
			err = fmt.Errorf("unexpected provider: %v", got)
		}
		result <- err
	}()
	var routeErr error
	select {
	case routeErr = <-result:
	case <-time.After(500 * time.Millisecond):
		routeErr = context.DeadlineExceeded
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("report failed to finish")
	}
	if routeErr != nil {
		t.Fatalf("cached Route stalled while report DB blocked: %v", routeErr)
	}
	t.Log("cached Route completed while a report was held before MySQL write; report completed after release")
}

func TestRouterReportSequencePersistsInOrder(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("report-order-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	entered, release, first, second := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	key := fmt.Sprintf("eval:report-order-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(key, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(key) })
	go func() { reg.Report(p.ID, true); close(first) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("first did not enter")
	}
	started := make(chan struct{})
	go func() { close(started); reg.Report(p.ID, false); close(second) }()
	<-started
	select {
	case <-second:
		close(release)
		t.Fatal("second overtook blocked first")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for _, ch := range []<-chan struct{}{first, second} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("report did not finish")
		}
	}
	var result store.Provider
	if err := db.WithContext(context.Background()).First(&result, p.ID).Error; err != nil || result.ConsecutiveFail != 1 {
		t.Fatal(result.ConsecutiveFail, err)
	}
	states := reg.States()
	if len(states) != 1 || states[0].ConsecutiveFail != 1 {
		t.Fatal(states)
	}
	t.Log("first successful report then failure persisted in order; final failures=1")
}

func TestRouterDifferentProvidersReportIndependently(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("independent-%d", time.Now().UnixNano())
	p1 := store.Provider{Name: base + "-1", BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s-1"]`, base), Weight: 1}
	p2 := store.Provider{Name: base + "-2", BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s-2"]`, base), Weight: 1}
	if err := db.Create(&p1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&p2).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	entered, release, first, second := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var selected atomic.Bool
	key := fmt.Sprintf("eval:report-independent-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(key, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" && selected.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(key) })
	go func() { reg.Report(p1.ID, false); close(first) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("first did not reach callback")
	}
	go func() { reg.Report(p2.ID, false); close(second) }()
	var blocked bool
	select {
	case <-second:
	case <-time.After(500 * time.Millisecond):
		blocked = true
	}
	close(release)
	for _, ch := range []<-chan struct{}{first, second} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("report failed to finish")
		}
	}
	if blocked {
		t.Fatal("unrelated provider report blocked by first provider")
	}
	var rows []store.Provider
	if err := db.Where("id IN ?", []uint64{p1.ID, p2.ID}).Find(&rows).Error; err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.ConsecutiveFail != 1 {
			t.Fatal(row.ID, row.ConsecutiveFail)
		}
	}
	t.Log("second provider failure persisted while first provider report paused; both failure counters=1")
}

func TestRouterReportWriteFailureReturnsUnavailable(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("report-failure-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	key := fmt.Sprintf("eval:report-write-fail-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(key, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			tx.AddError(errors.New("synthetic write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(key)
	req := httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(fmt.Sprintf(`{"provider_id":%d,"ok":false}`, p.ID)))
	req.Header.Set(controlplane.Header, "test-token")
	resp := httptest.NewRecorder()
	router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler)).ServeHTTP(resp, req)
	var row store.Provider
	if err := db.First(&row, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	states := reg.States()
	if resp.Code != http.StatusServiceUnavailable || row.ConsecutiveFail != 0 || len(states) != 1 || states[0].ConsecutiveFail != 1 {
		t.Fatalf("status=%d db=%d states=%v", resp.Code, row.ConsecutiveFail, states)
	}
	t.Log("synthetic DB failure returned 503; in-memory update remains and must not be blindly retried")
}

func TestRouterReportWriteFailureAllTransitions(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ok   bool
		ban  bool
	}{{"reset", true, false}, {"failure", false, false}, {"ban", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			name := fmt.Sprintf("report-error-%s-%d", tc.name, time.Now().UnixNano())
			p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
			if err := db.Create(&p).Error; err != nil {
				t.Fatal(err)
			}
			reg := router.New(db, slog.New(slog.DiscardHandler))
			if tc.ban {
				reg.AutobanAfter = 1
			}
			key := "test:report-error-" + name
			if err := db.Callback().Update().Before("gorm:update").Register(key, func(tx *gorm.DB) {
				if tx.Statement.Table == "providers" {
					tx.AddError(errors.New("synthetic failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(fmt.Sprintf(`{"provider_id":%d,"ok":%t}`, p.ID, tc.ok)))
			req.Header.Set(controlplane.Header, "test-token")
			resp := httptest.NewRecorder()
			router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler)).ServeHTTP(resp, req)
			if err := db.Callback().Update().Remove(key); err != nil {
				t.Fatal(err)
			}
			if resp.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			var row store.Provider
			if err := db.First(&row, p.ID).Error; err != nil || row.Status != 0 || row.ConsecutiveFail != 0 {
				t.Fatalf("row=%+v err=%v", row, err)
			}
		})
	}
}
