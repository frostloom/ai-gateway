package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPortalKeyDeletionBindsAuthenticatedTenant(t *testing.T) {
	calls := 0
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/portal/api-keys/delete" {
			t.Errorf("unexpected target %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["tenant_id"] != float64(7) || body["id"] != float64(3) {
			t.Errorf("untrusted tenant or missing key ID: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer billing.Close()
	router := gin.New()
	group := router.Group("/portal", func(c *gin.Context) { c.Set("tenant_id", int64(7)); c.Next() })
	RegisterPortal(group, billing.URL, billing.URL, slog.Default())
	for _, tc := range []struct {
		body   string
		status int
	}{{`null`, 400}, {`{"tenant_id":999,"id":3}`, 200}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/portal/api-keys/delete", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
	if calls != 1 {
		t.Fatalf("forwarded %d calls", calls)
	}
}
