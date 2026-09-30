package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPortalSessionBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("agw_user")
		if err != nil || ck.Value != "valid" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"tenant_id":42,"api_key_id":7,"role":"user"}`)
	}))
	defer upstream.Close()
	g := gin.New()
	g.Use(BrowserSameOrigin())
	g.POST("/portal/test", PortalSession(upstream.URL, nil), func(c *gin.Context) {
		if c.GetInt64("tenant_id") != 42 || c.GetHeader("Authorization") != "Bearer valid" {
			t.Error("identity not derived from session")
		}
		c.Status(204)
	})
	g.GET("/admin/test", AdminSession(upstream.URL, "", slog.Default()), func(c *gin.Context) { c.Status(204) })
	for _, tc := range []struct {
		path, method, cookie, origin string
		code                         int
	}{
		{"/portal/test", "POST", "valid", "", 204},
		{"/portal/test", "POST", "expired", "", 401},
		{"/portal/test", "POST", "valid", "https://attacker.example", 403},
		{"/admin/test", "GET", "valid", "", 401},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"tenant_id":999}`))
		r.AddCookie(&http.Cookie{Name: "agw_user", Value: tc.cookie})
		r.Header.Set("Authorization", "Bearer another-tenants-key")
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s got %d: %s", tc.path, w.Code, w.Body.String())
		}
	}
}
