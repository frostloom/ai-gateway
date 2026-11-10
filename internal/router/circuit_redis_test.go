package router

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestRedisWindowCountsPlatformFailuresOnly(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r := newTestRegistry()
	r.EnableFailureRedis(rdb)
	ps := oneProvider()
	ctx := context.Background()

	for i := 0; i < 19; i++ {
		r.Report(1, false)
		r.Report(1, true)
		if got := r.available(ps); len(got) != 1 {
			t.Fatalf("第 %d 次失败后应仍可用，got %d", i+1, len(got))
		}
	}
	n, err := rdb.Get(ctx, failureKey(1)).Int()
	if err != nil || n != 19 {
		t.Fatalf("redis count = %d (%v), want 19", n, err)
	}
	ttl := mr.TTL(failureKey(1))
	if ttl <= 0 || ttl > r.FailWindow {
		t.Fatalf("ttl = %s, want within %s", ttl, r.FailWindow)
	}
	if r.breaker(1).banned {
		t.Fatal("未满 20 次不应停用")
	}
}

func TestRedisWindowResetWithoutBan(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r := newTestRegistry()
	r.FailWindow = 40 * time.Millisecond
	r.EnableFailureRedis(rdb)
	ps := oneProvider()

	for i := 0; i < 19; i++ {
		r.Report(1, false)
	}
	time.Sleep(r.FailWindow + 20*time.Millisecond)
	mr.FastForward(r.FailWindow + 20*time.Millisecond)
	if _, err := rdb.Get(context.Background(), failureKey(1)).Result(); err != goredis.Nil {
		t.Fatalf("未满 20 次时窗口结束应清零，err=%v", err)
	}
	if got := r.available(ps); len(got) != 1 {
		t.Fatal("未停用的上游在窗口结束后应仍可用")
	}
	r.Report(1, false)
	n, err := rdb.Get(context.Background(), failureKey(1)).Int()
	if err != nil || n != 1 {
		t.Fatalf("新的一分钟应从 1 开始，got %d (%v)", n, err)
	}
}

func TestRedisTwentiethFailureBansUntilProbe(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r := newTestRegistry()
	r.FailWindow = 40 * time.Millisecond
	r.EnableFailureRedis(rdb)
	ps := oneProvider()

	for i := 0; i < 20; i++ {
		r.Report(1, false)
	}
	r.Report(1, true)
	if !r.breaker(1).banned {
		t.Fatal("满 20 次应停用")
	}
	time.Sleep(r.FailWindow + 20*time.Millisecond)
	mr.FastForward(r.FailWindow + 20*time.Millisecond)
	if got := r.available(ps); len(got) != 0 {
		t.Fatal("窗口结束不应自行恢复")
	}

	r.enableProvider(1)
	if r.breaker(1).banned {
		t.Fatal("探测成功后应取消停用")
	}
	if got := r.available(ps); len(got) != 1 {
		t.Fatal("恢复后应可以选中")
	}
	if _, err := rdb.Get(context.Background(), failureKey(1)).Result(); err != goredis.Nil {
		t.Fatalf("恢复后应清掉 redis 次数，err=%v", err)
	}
}

func TestRedisRouteBansStoredTwenty(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r := newTestRegistry()
	r.EnableFailureRedis(rdb)
	if err := rdb.Set(context.Background(), failureKey(1), "20", 30*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	r.cache["m"] = &cachedRoute{providers: oneProvider(), expireAt: time.Now().Add(time.Hour)}

	got, err := r.Route("m")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("重启后这一分钟已有 20 次失败，不应再选这个上游")
	}
	if !r.breaker(1).banned {
		t.Fatal("重启后满额应记成停用")
	}
}

func TestRedisRouteTellsGatewayToSkipSuccessReport(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r := newTestRegistry()
	r.EnableFailureRedis(rdb)
	r.cache["m"] = &cachedRoute{providers: oneProvider(), expireAt: time.Now().Add(time.Hour)}
	h := NewHTTPHandler(r, "secret", slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodGet, "/route?model=m", nil)
	req.Header.Set("X-Control-Token", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"skip_success_report":true`) {
		t.Fatalf("route = %d %s", rec.Code, rec.Body.String())
	}
}

func TestProbeUpstreamAcceptsModelList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			http.NotFound(w, req)
			return
		}
		if req.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if !probeUpstream(srv.URL, "sk-test") {
		t.Fatal("模型列表 200 应视为探测成功")
	}
	if probeUpstream(srv.URL, "wrong") {
		t.Fatal("鉴权失败不应恢复")
	}
}
