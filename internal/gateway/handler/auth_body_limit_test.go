package handler

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminAuthRejectsOversizedBodyBeforeProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("oversized body forwarded"); w.WriteHeader(200) }))
	defer upstream.Close()
	router := gin.New()
	RegisterAdminAuth(router.Group("/admin/auth"), upstream.URL, slog.Default())
	for _, path := range []string{"/setup", "/login"} {
		req := httptest.NewRequest(http.MethodPost, "/admin/auth"+path, strings.NewReader(strings.Repeat("x", 65537)))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("got %d want 413", w.Code)
		}
	}
}
