package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAdminChannelDeleteForwardsID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/admin/providers" || r.URL.Query().Get("id") != "17" {
			t.Errorf("wrong upstream request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	gin.SetMode(gin.TestMode)
	g := gin.New()
	RegisterAdmin(g.Group("/admin"), upstream.URL, "", "", "", slog.Default())
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/admin/channels?id=17", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUnifiedLoginForwardedWithoutRoleChoice(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/unified-login" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		http.SetCookie(w, &http.Cookie{Name: "agw_admin", Value: "test"})
		_, _ = io.WriteString(w, `{"username":"test","role":"admin"}`)
	}))
	defer upstream.Close()
	gin.SetMode(gin.TestMode)
	g := gin.New()
	RegisterUserAuth(g.Group("/auth"), upstream.URL, slog.Default())
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/unified-login", nil))
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 {
		t.Fatalf("expected forwarded login and cookie, got %d", w.Code)
	}
}
