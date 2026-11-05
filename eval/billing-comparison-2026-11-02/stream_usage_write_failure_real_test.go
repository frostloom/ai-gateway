package comparison

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

type failStreamUsageWriter struct {
	gin.ResponseWriter
	failures int
}

func (w *failStreamUsageWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), `"usage"`) {
		w.failures++
		return 0, errors.New("injected client write failure")
	}
	return w.ResponseWriter.Write(p)
}

func TestStreamUsageWriteFailureRealBilling(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("stream-disconnect-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("stream-usage-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "stream-usage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	failingWriter := &failStreamUsageWriter{ResponseWriter: c.Writer}
	c.Writer = failingWriter
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"stream":true,"messages":[{"role":"user","content":"hello"}]}`, id)))
	c.Set("tenant_id", int64(tenant.ID))
	c.Set("api_key_id", int64(1))
	c.Set("request_id", id)
	handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
	if failingWriter.failures != 1 {
		t.Fatalf("injected write failures=%d, want 1", failingWriter.failures)
	}
	if !strings.Contains(w.Body.String(), "hello") || strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatalf("body=%q", w.Body.String())
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.UsageReportedAt == nil || bill.PromptTokens == nil || *bill.PromptTokens != 4 || bill.CompletionTokens == nil || *bill.CompletionTokens != 2 || bill.ProviderID == nil || *bill.ProviderID != provider.ID {
		t.Fatalf("reserve=%+v err=%v", bill, err)
	}
	var settled store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseSettle).First(&settled).Error; err != nil || settled.ActualQuota == nil || *settled.ActualQuota != 6 {
		t.Fatalf("settle=%+v err=%v", settled, err)
	}
	var settledCount int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settledCount).Error; err != nil || settledCount != 1 {
		t.Fatalf("settlement rows=%d err=%v", settledCount, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		var n int64
		want := int64(1)
		if phase == "reverse" {
			want = 0
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil || n != want {
			t.Fatalf("event %s=%d want=%d err=%v", phase, n, want, err)
		}
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 994 || ledger != 994 {
		t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("client_write_injected_failure=true reserve=settled usage=4/2 actual=6 balance=%d outbox=reserve,usage,settle", live)
}
