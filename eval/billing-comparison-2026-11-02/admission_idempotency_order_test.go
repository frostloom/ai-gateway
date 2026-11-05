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
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A test-only nonblocking gate; it is not installed in production.
func experimentalAdmission(slot chan struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		select {
		case slot <- struct{}{}:
			defer func() { <-slot }()
			c.Next()
		default:
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
	}
}

func TestAdmissionOrderingWithRealIdempotencyAndBilling(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, redis)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var upstreamCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer up.Close()
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
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("gate_before_idempotency_%t", before), func(t *testing.T) {
			id := fmt.Sprintf("admit-%d-%t", time.Now().UnixNano(), before)
			tenant := store.Tenant{Name: id, InitialQuota: 1000}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
				t.Fatal(err)
			}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			slot := make(chan struct{}, 1)
			slot <- struct{}{}
			engine := gin.New()
			identity := func(c *gin.Context) { c.Set("tenant_id", int64(tenant.ID)); c.Set("api_key_id", int64(1)); c.Next() }
			gate, idem := experimentalAdmission(slot), middleware.Idempotency(redis)
			chat := handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true)
			if before {
				engine.POST("/chat", identity, gate, idem, chat)
			} else {
				engine.POST("/chat", identity, idem, gate, chat)
			}
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)
			request := func() int {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
				req.Header.Set("Idempotency-Key", id)
				engine.ServeHTTP(w, req)
				return w.Code
			}
			first := request()
			if first != http.StatusServiceUnavailable {
				t.Fatalf("first=%d", first)
			}
			var bills int64
			if err := db.Model(&store.Bill{}).Where("request_id=?", "idem:"+id).Count(&bills).Error; err != nil || bills != 0 {
				t.Fatalf("after rejection bills=%d err=%v", bills, err)
			}
			exists, err := redis.Exists(ctx, "idem:"+id).Result()
			if err != nil {
				t.Fatal(err)
			}
			if (exists == 0) != before {
				t.Fatalf("before=%t idem_exists=%d", before, exists)
			}
			<-slot
			second := request()
			if before && second != http.StatusOK || !before && second != http.StatusConflict {
				t.Fatalf("before=%t retry=%d", before, second)
			}
			third := request()
			if third != http.StatusConflict {
				t.Fatalf("before=%t repeated retry=%d", before, third)
			}
			var bill store.Bill
			e := db.Where("request_id=? AND phase=?", "idem:"+id, store.PhaseReserve).Take(&bill).Error
			if before && (e != nil || bill.Status != store.StatusSettled) {
				t.Fatalf("bill=%+v err=%v", bill, e)
			}
			if !before && !errors.Is(e, gorm.ErrRecordNotFound) {
				t.Fatalf("unexpected bill=%+v err=%v", bill, e)
			}
			live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != ledger {
				t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
			}
			if before && live != 994 || !before && live != 1000 {
				t.Fatalf("before=%t balance=%d", before, live)
			}
			var events int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{"idem:" + id + ":reserve", "idem:" + id + ":usage", "idem:" + id + ":settle", "idem:" + id + ":reverse"}).Count(&events).Error; err != nil {
				t.Fatal(err)
			}
			if before && events != 3 || !before && events != 0 {
				t.Fatalf("before=%t events=%d", before, events)
			}
			for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", "idem:"+id+":"+phase).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				want := int64(0)
				if before && phase != "reverse" {
					want = 1
				}
				if n != want {
					t.Fatalf("phase=%s got=%d want=%d", phase, n, want)
				}
			}
			t.Logf("before=%t rejected=%d key_exists=%d retry=%d repeated_retry=%d balance=%d ledger=%d events=%d", before, first, exists, second, third, live, ledger, events)
		})
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls=%d", upstreamCalls.Load())
	}
}
