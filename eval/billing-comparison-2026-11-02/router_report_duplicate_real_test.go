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

// PASS demonstrates duplicate application when an identical report is retried.
func TestRouterIdenticalReportRetryCountsTwice(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("report-duplicate-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler)))
	defer srv.Close()
	body := fmt.Sprintf(`{"provider_id":%d,"ok":false}`, p.ID)
	for i := 0; i < 2; i++ {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/report", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(controlplane.Header, "test-token")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("report %d status=%d", i, resp.StatusCode)
		}
	}
	var row store.Provider
	if err := db.First(&row, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	states := reg.States()
	if len(states) != 1 || states[0].ConsecutiveFail != 2 || row.ConsecutiveFail != 2 {
		t.Fatalf("identical reports: db=%d states=%+v", row.ConsecutiveFail, states)
	}
	t.Logf("two identical HTTP reports=200/200, memory=%d database=%d; no event identity", states[0].ConsecutiveFail, row.ConsecutiveFail)
}
