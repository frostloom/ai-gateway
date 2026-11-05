package comparison

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	gatewayhandler "github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

func TestPortalProxyRejectsTruncatedUpstreamResponse(t *testing.T) {
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("portal-truncated-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(tag))
	if err := db.Create(&store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: tag}).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("truncate-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(lis.Addr().String(), "truncate-test")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	gatewayhandler.SetControlToken("truncate-test")
	defer gatewayhandler.SetControlToken("")
	var broken atomic.Bool
	var upstreamFailure atomic.Bool
	broken.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(controlplane.Header) != "truncate-test" {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Set-Cookie", "test_cookie=synthetic; Path=/")
		if broken.Load() {
			w.Header().Set("Content-Length", "50")
			_, _ = w.Write([]byte(`{"balance":`))
			return
		}
		if upstreamFailure.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"synthetic failure"}`))
			return
		}
		_, _ = w.Write([]byte(`{"balance":1000}`))
	}))
	defer upstream.Close()
	engine := gin.New()
	group := engine.Group("/portal")
	group.Use(middleware.APIKey(billing))
	gatewayhandler.RegisterPortal(group, upstream.URL, "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	httpClient := &http.Client{Timeout: 10 * time.Second}
	defer httpClient.CloseIdleConnections()
	send := func() (int, string, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, gateway.URL+"/portal/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tag)
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body), resp.Header.Get("Set-Cookie")
	}
	code, body, cookie := send()
	if code != http.StatusBadGateway || cookie != "" || strings.Contains(body, `{"balance":`) {
		t.Fatalf("truncated response: status=%d body=%q cookie=%q", code, body, cookie)
	}
	broken.Store(false)
	code, body, cookie = send()
	var payload struct {
		Balance int64 `json:"balance"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil || code != 200 || payload.Balance != 1000 || !strings.Contains(cookie, "test_cookie=synthetic") {
		t.Fatalf("normal status=%d body=%q cookie=%q err=%v", code, body, cookie, err)
	}
	upstreamFailure.Store(true)
	code, body, cookie = send()
	if code != http.StatusInternalServerError || body != `{"error":"synthetic failure"}` || !strings.Contains(cookie, "test_cookie=synthetic") {
		t.Fatalf("upstream error status=%d body=%q cookie=%q", code, body, cookie)
	}
	t.Log("truncated response returned 502 without partial body/cookie; normal response and cookie forwarded")
}
