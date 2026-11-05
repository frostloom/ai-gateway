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
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
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

// An experimental 1 MiB threshold, not an established product limit.
func TestNonstreamLimitWithRealBilling(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, redis)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("limit-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "limit-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx := context.Background()
	const limit = 1 << 20
	const prefix = `{"choices":[{"message":{"content":"`
	const suffix = `"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`
	for _, size := range []int{limit, limit + 1} {
		t.Run(fmt.Sprintf("bytes_%d", size), func(t *testing.T) {
			id := fmt.Sprintf("body-real-%d-%d", time.Now().UnixNano(), size)
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
			payload := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, payload) }))
			defer up.Close()
			provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			gin.SetMode(gin.ReleaseMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
			c.Set("tenant_id", int64(tenant.ID))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", id)
			handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true)(c)
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&bill).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus, wantBill, wantEvents := http.StatusOK, store.StatusSettled, 3
			if size > limit {
				wantStatus, wantBill, wantEvents = http.StatusBadGateway, store.StatusPending, 1
			}
			if w.Code != wantStatus || bill.Status != wantBill || (bill.UsageReportedAt != nil) != (size <= limit) {
				t.Fatalf("size=%d http=%d bill=%s usage=%v", size, w.Code, bill.Status, bill.UsageReportedAt)
			}
			if size <= limit && (bill.PromptTokens == nil || *bill.PromptTokens != 4 || bill.CompletionTokens == nil || *bill.CompletionTokens != 2) {
				t.Fatalf("unexpected usage prompt=%v completion=%v", bill.PromptTokens, bill.CompletionTokens)
			}
			var events int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle", id + ":reverse"}).Count(&events).Error; err != nil || events != int64(wantEvents) {
				t.Fatalf("events=%d err=%v", events, err)
			}
			for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				want := int64(0)
				if phase == "reserve" || (size <= limit && (phase == "usage" || phase == "settle")) {
					want = 1
				}
				if n != want {
					t.Fatalf("phase=%s got=%d want=%d", phase, n, want)
				}
			}
			live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != ledger {
				t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
			}
			if size > limit {
				if err := db.Model(&store.Bill{}).Where("id=?", bill.ID).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				runner := reconciler.New(db, directSweepStore{s: s}, slog.New(slog.DiscardHandler), 10*time.Minute)
				stats, err := runner.SweepOnce(ctx)
				if err != nil || stats.Held < 1 || stats.Reversed != 0 {
					t.Fatalf("sweep=%+v err=%v", stats, err)
				}
				if err := db.First(&bill, bill.ID).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
					t.Fatalf("after sweep bill=%+v err=%v", bill, err)
				}
				var reverse int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&reverse).Error; err != nil || reverse != 0 {
					t.Fatalf("reverse=%d err=%v", reverse, err)
				}
			}
			t.Logf("size=%d http=%d status=%s balance=%d ledger=%d events=%d", size, w.Code, bill.Status, live, ledger, events)
		})
	}
}
