package handler

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAuthProxyHTTPSConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, configured := range []string{"", "1"} {
		t.Run("secure="+configured, func(t *testing.T) {
			t.Setenv("AUTH_COOKIE_SECURE", configured)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secure := r.Header.Get("X-Forwarded-Proto") == "https"
				if secure != (configured == "1") {
					t.Error("untrusted protocol or missing configured HTTPS")
				}
				http.SetCookie(w, &http.Cookie{Name: "agw_user", Value: "test-session", HttpOnly: true, Secure: secure, Path: "/"})
				w.Write([]byte(`{"role":"user","username":"test"}`))
			}))
			defer upstream.Close()
			router := gin.New()
			RegisterUserAuth(router.Group("/auth"), upstream.URL, slog.Default())
			req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"test","password":"password123"}`))
			req.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 200 || w.Result().Cookies()[0].Secure != (configured == "1") {
				t.Fatal("cookie was not relayed correctly")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("auth response can be cached")
			}
		})
	}
}
