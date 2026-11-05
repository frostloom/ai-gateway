package comparison

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm/logger"
)

func TestRouterOpenThresholdPersistsFailure(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("threshold-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 3
	reg.AutobanAfter = 20
	srv := httptest.NewServer(router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler)))
	defer srv.Close()
	report := func(ok bool) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/report", strings.NewReader(fmt.Sprintf(`{"provider_id":%d,"ok":%t}`, p.ID, ok)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(controlplane.Header, "test-token")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("report status=%d", resp.StatusCode)
		}
	}
	for i := 1; i <= 4; i++ {
		report(false)
		var got store.Provider
		if err := db.First(&got, p.ID).Error; err != nil {
			t.Fatal(err)
		}
		if got.ConsecutiveFail != i {
			t.Fatalf("failure %d: persisted=%d", i, got.ConsecutiveFail)
		}
	}
	states := reg.States()
	if len(states) != 1 || states[0].ConsecutiveFail != 4 {
		t.Fatalf("states=%+v", states)
	}
	report(true)
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil || got.ConsecutiveFail != 0 {
		t.Fatalf("after success=%+v err=%v", got, err)
	}
	t.Log("four reports including threshold persisted exactly once; success reset database count")
}
