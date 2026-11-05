package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
)

// Invalid weights can exist in the current schema; only positive weights are routable.
func TestRouterInvalidWeightsFailClosed(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("weight-test-%d", time.Now().UnixNano())
	defer db.Where("name LIKE ?", tag+"%").Delete(&store.Provider{})
	for i, weight := range []int{0, -1} {
		p := store.Provider{Name: fmt.Sprintf("%s-%d", tag, i), BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf("[\"%s\"]", tag), Weight: 1}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&p).UpdateColumn("weight", weight).Error; err != nil {
			t.Fatal(err)
		}
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	got, err := reg.RouteExcludeContext(context.Background(), tag, nil)
	if err != nil || got != nil {
		t.Fatalf("invalid-only: provider=%+v err=%v", got, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/route?model="+url.QueryEscape(tag), nil)
	request.Header.Set(controlplane.Header, "isolated")
	response := httptest.NewRecorder()
	router.NewHTTPHandler(reg, "isolated", slog.New(slog.DiscardHandler)).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid-only HTTP status=%d", response.Code)
	}
	p := store.Provider{Name: tag + "-valid", BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf("[\"%s\"]", tag), Weight: 2}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg = router.New(db, slog.New(slog.DiscardHandler))
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selected, e := reg.RouteExcludeContext(context.Background(), tag, nil)
			if e != nil || selected == nil || selected.ID != p.ID {
				errs <- fmt.Errorf("selected=%+v err=%v", selected, e)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	for i := 0; i < 100; i++ {
		got, err = reg.RouteExcludeContext(context.Background(), tag, nil)
		if err != nil || got == nil || got.ID != p.ID {
			t.Fatalf("mixed: provider=%+v err=%v", got, err)
		}
	}
}
