package comparison

import (
	"context"
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
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUsageRetryWhileFirstTransactionInFlight(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("usage-overlap-%d", time.Now().UnixNano())
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	provider := store.Provider{Name: id, BaseURL: upstream.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	var calls, updates atomic.Int32
	firstWritten := make(chan struct{})
	secondStarted := make(chan struct{})
	firstDone := make(chan struct{})
	firstStatus := make(chan string, 1)
	canceled := make(chan bool, 1)
	callbackTimedOut := make(chan struct{}, 1)
	cbName := "test:usage_inflight_" + id
	if err := db.Callback().Update().After("gorm:update").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" || tx.Error != nil || tx.RowsAffected != 1 || !updates.CompareAndSwap(0, 1) {
			return
		}
		close(firstWritten)
		select {
		case <-secondStarted:
			canceled <- tx.Statement.Context.Err() != nil
		case <-time.After(8 * time.Second):
			callbackTimedOut <- struct{}{}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(cbName)
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if strings.HasSuffix(info.FullMethod, "/ReportUsage") {
			n := calls.Add(1)
			if n == 2 {
				close(secondStarted)
			}
			if n == 1 {
				response, err := next(ctx, req)
				firstStatus <- status.Code(err).String()
				close(firstDone)
				return response, err
			}
		}
		return next(ctx, req)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.ChainUnaryInterceptor(controlplane.UnaryServer("retry-overlap"), interceptor))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(listener.Addr().String(), "retry-overlap")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
	c.Set("tenant_id", int64(tenant.ID))
	c.Set("api_key_id", int64(1))
	c.Set("request_id", id)
	handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: upstream.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	select {
	case <-firstWritten:
	default:
		t.Fatal("first transaction was not held after UPDATE")
	}
	select {
	case <-secondStarted:
	default:
		t.Fatal("retry did not start while first transaction was held")
	}
	select {
	case flag := <-canceled:
		if !flag {
			t.Fatal("first transaction context was not canceled")
		}
	case <-callbackTimedOut:
		t.Fatal("first transaction wait timed out")
	case <-time.After(5 * time.Second):
		t.Fatal("first callback did not finish")
	}
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first RPC did not finish")
	}
	if calls.Load() != 2 {
		t.Fatalf("usage calls=%d", calls.Load())
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.PromptTokens == nil || *bill.PromptTokens != 4 || bill.CompletionTokens == nil || *bill.CompletionTokens != 2 {
		t.Fatalf("bill=%+v err=%v", bill, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle"} {
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("phase %s events=%d err=%v", phase, n, err)
		}
	}
	var reversed int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&reversed).Error; err != nil || reversed != 0 {
		t.Fatalf("reverse=%d err=%v", reversed, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 994 || ledger != 994 {
		t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("first_update_held=true first_context_canceled=true first_rpc_status=%s usage_calls=%d settled=true balance=%d", <-firstStatus, calls.Load(), live)
}
