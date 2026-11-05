package loadtest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestAdmissionOrderChangesDuplicateRateSemantics(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	for _, tc := range []struct {
		name      string
		limit     int
		rateFirst bool
		want      []int
	}{{"current_limit1", 1, false, []int{204, 409, 429}}, {"candidate_limit1", 1, true, []int{204, 429, 429}}, {"current_limit2", 2, false, []int{204, 409, 204}}, {"candidate_limit2", 2, true, []int{204, 409, 429}}} {
		t.Run(tc.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer rdb.Close()
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set("tenant_id", int64(1)); c.Next() })
			if tc.rateFirst {
				r.Use(middleware.RateLimit(rdb, time.Minute, tc.limit), middleware.Idempotency(rdb))
			} else {
				r.Use(middleware.Idempotency(rdb), middleware.RateLimit(rdb, time.Minute, tc.limit))
			}
			r.POST("/", func(c *gin.Context) { c.Status(204) })
			send := func(key string) int {
				req := httptest.NewRequest(http.MethodPost, "/", nil)
				req.Header.Set("Idempotency-Key", key)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w.Code
			}
			got := []int{send("a"), send("a"), send("b")}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got=%v want=%v", got, tc.want)
				}
			}
			t.Logf("statuses=%v", got)
		})
	}
}
