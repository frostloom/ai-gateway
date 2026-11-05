package comparison

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

func TestConcurrentCanceledStreamsRemainPending(t *testing.T) {
	if os.Getenv("TEST_CONCURRENT_STREAM_CANCEL") != "1" {
		t.Skip("requires disposable services")
	}
	gin.SetMode(gin.TestMode)
	db, redisClient := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, redisClient), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("cancel-stream-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	const tenantCount, perTenant = 8, 2
	var tenants [tenantCount]store.Tenant
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", id, i), InitialQuota: 1000}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := redisClient.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	pb.RegisterBillingServiceServer(rpc, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	var upstreamCalls, upstreamCanceled atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			upstreamCanceled.Add(1)
		case <-time.After(10 * time.Second):
		}
	}))
	defer upstream.Close()
	provider := store.Provider{Name: id, BaseURL: upstream.URL, Models: "[]", Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{}, tenantCount*perTenant)
	engine := gin.New()
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		var n int
		if _, err := fmt.Sscanf(c.GetHeader("X-Test-Index"), "%d", &n); err != nil || n < 0 || n >= tenantCount*perTenant {
			c.AbortWithStatus(400)
			return
		}
		c.Set("tenant_id", int64(tenants[n%tenantCount].ID))
		c.Set("api_key_id", int64(1))
		c.Set("request_id", fmt.Sprintf("%s-r%d", id, n))
		defer func() { finished <- struct{}{} }()
		c.Next()
	}, handler.Chat(billing, gatewayExecutionRouter{url: upstream.URL, providerID: provider.ID}, slog.New(slog.DiscardHandler), time.Second, true))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"max_tokens":100,"stream":true}`, id)
	var wg sync.WaitGroup
	errCh := make(chan error, tenantCount*perTenant)
	start := make(chan struct{})
	for n := 0; n < tenantCount*perTenant; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
			if err != nil {
				errCh <- err
				return
			}
			req.Header.Set("X-Test-Index", fmt.Sprint(n))
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			line, err := bufio.NewReader(resp.Body).ReadString('\n')
			if err != nil || resp.StatusCode != 200 || !strings.Contains(line, "hello") {
				errCh <- fmt.Errorf("status=%d line=%q err=%v", resp.StatusCode, line, err)
				return
			}
		}(n)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for i := 0; i < tenantCount*perTenant; i++ {
		select {
		case <-finished:
		case <-deadline.C:
			t.Fatal("gateway handlers did not finish")
		}
	}
	// The mock waits on request cancellation; allow network cancellation propagation.
	until := time.Now().Add(5 * time.Second)
	for upstreamCanceled.Load() != tenantCount*perTenant && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if upstreamCalls.Load() != tenantCount*perTenant || upstreamCanceled.Load() != tenantCount*perTenant {
		t.Fatalf("upstream calls=%d canceled=%d", upstreamCalls.Load(), upstreamCanceled.Load())
	}
	// Force the test bills past the cutoff; this does not simulate a real wait.
	updated := db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=?", id+"-r%", store.PhaseReserve).UpdateColumn("created_at", time.Now().Add(-time.Hour))
	if updated.Error != nil || updated.RowsAffected != tenantCount*perTenant {
		t.Fatalf("aged=%d err=%v", updated.RowsAffected, updated.Error)
	}
	reconConn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer reconConn.Close()
	defaultSweep := os.Getenv("TEST_CANCEL_DEFAULT_SWEEP") == "1"
	actualDefault := os.Getenv("TEST_CANCEL_ACTUAL_DEFAULT") == "1"
	if defaultSweep && actualDefault {
		t.Fatal("conflicting reconciler test modes")
	}
	options := []reconciler.Option{}
	if defaultSweep {
		options = append(options, reconciler.WithAutoReverse())
	} else if !actualDefault {
		options = append(options, reconciler.WithoutAutoReverse())
	}
	recon := reconciler.New(db, pb.NewBillingServiceClient(reconConn), slog.New(slog.DiscardHandler), time.Minute, options...)
	for i := 0; i < 2; i++ {
		stats, err := recon.SweepOnce(ctx)
		if err != nil || stats.Settled != 0 {
			t.Fatalf("sweep=%+v err=%v", stats, err)
		}
		if defaultSweep && ((i == 0 && stats.Reversed < tenantCount*perTenant) || (i == 1 && stats.Reversed != 0)) {
			t.Fatalf("default sweep=%+v", stats)
		}
		if !defaultSweep && (stats.Held < tenantCount*perTenant || stats.Reversed != 0) {
			t.Fatalf("conservative sweep=%+v", stats)
		}
	}

	for n := 0; n < tenantCount*perTenant; n++ {
		rid := fmt.Sprintf("%s-r%d", id, n)
		var row store.Bill
		wantStatus := store.StatusPending
		if defaultSweep {
			wantStatus = store.StatusReversed
		}
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).First(&row).Error; err != nil || row.Status != wantStatus || row.UsageReportedAt != nil {
			t.Fatalf("request=%s status=%s usage=%v err=%v", rid, row.Status, row.UsageReportedAt, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var count int64
			want := int64(0)
			if phase == "reserve" || (phase == "reverse" && defaultSweep) {
				want = 1
			}
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&count).Error; err != nil || count != want {
				t.Fatalf("request=%s phase=%s count=%d err=%v", rid, phase, count, err)
			}
		}
	}
	for _, tenant := range tenants {
		live, err := redisClient.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		ledger, le := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || le != nil || live != ledger {
			t.Fatalf("tenant=%d live=%d ledger=%d err=%v/%v", tenant.ID, live, ledger, err, le)
		}
	}
	t.Logf("tenants=%d requests=%d upstream_calls=%d upstream_canceled=%d explicit_auto_reverse=%v actual_default=%v missing_usage=%d balances_match=true", tenantCount, tenantCount*perTenant, upstreamCalls.Load(), upstreamCanceled.Load(), defaultSweep, actualDefault, tenantCount*perTenant)
}
