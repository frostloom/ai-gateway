package comparison

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

func TestAdmissionStreamCancelReleasesSlotAndRetainsUnknown(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, redis)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tag := fmt.Sprintf("streamgate-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var tenants []store.Tenant
	var keys []string
	for i := 0; i < 2; i++ {
		tenant := store.Tenant{Name: fmt.Sprintf("%s-%d", tag, i), InitialQuota: 1000}
		if err := db.Create(&tenant).Error; err != nil {
			t.Fatal(err)
		}
		if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("synthetic-%s-%d", tag, i)
		sum := sha256.Sum256([]byte(key))
		if err := db.Create(&store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(sum[:]), KeyPrefix: "synthetic", Name: "test"}).Error; err != nil {
			t.Fatal(err)
		}
		tenants = append(tenants, tenant)
		keys = append(keys, key)
	}
	var calls atomic.Int32
	canceled := make(chan struct{}, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	provider := store.Provider{Name: tag, BaseURL: up.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("admission-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "admission-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	slots := make(chan struct{}, 1)
	engine.POST("/chat", middleware.APIKey(cli), experimentalAdmission(slots), middleware.Idempotency(redis), middleware.RateLimit(redis, time.Minute, 1000), handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true))
	gw := httptest.NewServer(engine)
	defer gw.Close()
	httpClient := &http.Client{Timeout: 10 * time.Second}
	makeRequest := func(c context.Context, i int) *http.Request {
		body := fmt.Sprintf(`{"model":%q,"stream":true,"messages":[{"role":"user","content":"hello"}]}`, tag)
		req, err := http.NewRequestWithContext(c, http.MethodPost, gw.URL+"/chat", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+keys[i])
		req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%d", tag, i))
		return req
	}
	firstCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	first, err := httpClient.Do(makeRequest(firstCtx, 0))
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(first.Body).ReadString('\n')
	if err != nil || first.StatusCode != 200 || !strings.Contains(line, "first") {
		t.Fatalf("first=%d line=%q err=%v", first.StatusCode, line, err)
	}
	blocked, err := httpClient.Do(makeRequest(ctx, 1))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, blocked.Body)
	blocked.Body.Close()
	if blocked.StatusCode != 503 {
		t.Fatalf("blocked=%d", blocked.StatusCode)
	}
	secondID := fmt.Sprintf("%s-1", tag)
	exists, err := redis.Exists(ctx, "idem:"+secondID).Result()
	if err != nil || exists != 0 {
		t.Fatalf("rejected key=%d err=%v", exists, err)
	}
	cancel()
	first.Body.Close()
	select {
	case <-canceled:
	case <-time.After(10 * time.Second):
		t.Fatal("upstream did not observe cancellation")
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(slots) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(slots) != 0 {
		t.Fatal("slot remained occupied after cancellation")
	}
	second, err := httpClient.Do(makeRequest(ctx, 1))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(second.Body)
	second.Body.Close()
	if err != nil || second.StatusCode != 200 || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("retry=%d body=%q err=%v", second.StatusCode, body, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
	var firstBill, secondBill store.Bill
	if err := db.Where("request_id=? AND phase=?", "idem:"+tag+"-0", store.PhaseReserve).Take(&firstBill).Error; err != nil || firstBill.Status != store.StatusPending || firstBill.UsageReportedAt != nil {
		t.Fatalf("first=%+v err=%v", firstBill, err)
	}
	if err := db.Where("request_id=? AND phase=?", "idem:"+tag+"-1", store.PhaseReserve).Take(&secondBill).Error; err != nil || secondBill.Status != store.StatusSettled || secondBill.PromptTokens == nil || *secondBill.PromptTokens != 4 || secondBill.CompletionTokens == nil || *secondBill.CompletionTokens != 2 {
		t.Fatalf("second=%+v err=%v", secondBill, err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", firstBill.ID).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	runner := reconciler.New(db, directSweepStore{s: s}, slog.New(slog.DiscardHandler), 10*time.Minute)
	stats, err := runner.SweepOnce(ctx)
	if err != nil || stats.Held < 1 || stats.Reversed != 0 {
		t.Fatalf("sweep=%+v err=%v", stats, err)
	}
	if err := db.First(&firstBill, firstBill.ID).Error; err != nil || firstBill.Status != store.StatusPending {
		t.Fatalf("after sweep=%+v err=%v", firstBill, err)
	}
	for i, tenant := range tenants {
		live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || ledger != live {
			t.Fatalf("account=%d live=%d ledger=%d err=%v", i, live, ledger, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var n int64
			id := fmt.Sprintf("idem:%s-%d:%s", tag, i, phase)
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if phase == "reserve" || (i == 1 && (phase == "usage" || phase == "settle")) {
				want = 1
			}
			if n != want {
				t.Fatalf("account=%d phase=%s n=%d want=%d", i, phase, n, want)
			}
		}
	}
	t.Log("first stream canceled, slot released, second key retried and settled; first unknown held through sweep; balances and events verified")
}
