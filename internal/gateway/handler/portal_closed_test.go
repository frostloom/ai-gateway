package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortalRechargeClosed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("closed recharge forwarded upstream") }))
	defer upstream.Close()
	router := gin.New()
	RegisterPortal(router.Group("/portal"), upstream.URL, upstream.URL, slog.Default())
	for _, path := range []string{"/portal/recharge", "/portal/recharge/pay", "/portal/subscribe", "/portal/change-plan"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"amount_money":100,"order_no":"guessed"}`)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s returned %d", path, w.Code)
		}
	}
}

func TestPortalRedemptionBindsTenantAndRejectsNull(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["tenant_id"] != float64(7) {
			t.Errorf("untrusted tenant forwarded %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"balance_cent":1000}`))
	}))
	defer upstream.Close()
	router := gin.New()
	group := router.Group("/portal")
	group.Use(func(c *gin.Context) { c.Set("tenant_id", int64(7)); c.Next() })
	RegisterPortal(group, upstream.URL, upstream.URL, slog.Default())
	for _, body := range []string{`null`, `{"tenant_id":999,"code":"AGV-TEST"}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/portal/redeem", strings.NewReader(body)))
		want := 200
		if body == "null" {
			want = 400
		}
		if w.Code != want {
			t.Errorf("got %d want %d", w.Code, want)
		}
	}
	if calls != 1 {
		t.Fatalf("forwarded calls %d", calls)
	}
}
