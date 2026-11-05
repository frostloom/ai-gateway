package loadtest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// Characterizes the current middleware, not the desired tenant isolation.
func TestCurrentIdempotencyKeyConflictsAcrossTenants(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer client.Close()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Tenant") == "1" {
			c.Set("tenant_id", int64(1))
		} else {
			c.Set("tenant_id", int64(2))
		}
		c.Next()
	})
	router.Use(middleware.Idempotency(client))
	var rid string
	router.POST("/", func(c *gin.Context) { rid = c.GetString("request_id"); c.Status(http.StatusNoContent) })
	for i, tenant := range []string{"1", "2"} {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Test-Tenant", tenant)
		req.Header.Set("Idempotency-Key", "shared-client-key")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		want := []int{http.StatusNoContent, http.StatusConflict}[i]
		if w.Code != want {
			t.Fatalf("tenant %s: got %d, want %d", tenant, w.Code, want)
		}
	}
	if rid != "idem:shared-client-key" {
		t.Fatalf("request id %q", rid)
	}
}
