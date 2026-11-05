package comparison

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayhandler "github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	"github.com/gin-gonic/gin"
)

func TestRouterObservationControlPlaneAndGateway(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	p := store.Provider{Name: "observation-auth-test", BaseURL: "https://example.invalid/private", Models: `["synthetic"]`, Weight: 1, Status: 0}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&p)
	log := slog.New(slog.DiscardHandler)
	reg := router.New(db, log)
	reg.Report(p.ID, false)
	srv := httptest.NewServer(router.NewHTTPHandler(reg, "synthetic-control", log))
	defer srv.Close()
	get := func(path, token string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set(controlplane.Header, token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(value)
	}
	for _, path := range []string{"/providers", "/breakers"} {
		for _, token := range []string{"", "wrong"} {
			code, body := get(path, token)
			if code != http.StatusUnauthorized {
				t.Errorf("%s token=%q status=%d body=%s", path, token, code, body)
			}
		}
		code, body := get(path, "synthetic-control")
		if code != http.StatusOK || !strings.Contains(body, "observation-auth-test") && path == "/providers" {
			t.Errorf("authorized %s status=%d body=%s", path, code, body)
		}
	}
	code, _ := get("/healthz", "")
	if code != http.StatusOK {
		t.Fatalf("health status=%d", code)
	}
	gatewayhandler.SetControlToken("synthetic-control")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/admin")
	group.Use(middleware.AdminSession("http://127.0.0.1:1", "synthetic-admin", "synthetic-control", log))
	gatewayhandler.RegisterAdmin(group, "http://127.0.0.1:1", srv.URL, "http://127.0.0.1:1", "http://127.0.0.1:1", log)
	for _, tc := range []struct {
		token string
		code  int
	}{{"", 401}, {"synthetic-admin", 200}} {
		req := httptest.NewRequest(http.MethodGet, "/admin/providers", nil)
		req.Header.Set("X-Admin-Token", tc.token)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != tc.code || tc.code == 200 && !strings.Contains(w.Body.String(), "observation-auth-test") {
			t.Errorf("gateway status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
