package comparison

import (
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
	"sync"
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
	"gorm.io/gorm/logger"
)

func TestAdmissionMultiAccountRetryWithRealMiddleware(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, redis)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tag := fmt.Sprintf("admission-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	type account struct {
		tenant store.Tenant
		key    string
	}
	var accounts []account
	for i := 0; i < 4; i++ {
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
		accounts = append(accounts, account{tenant, key})
	}
	arrived := make(chan struct{}, 16)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		arrived <- struct{}{}
		<-release
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); up.Close() }()
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
	gate := make(chan struct{}, 4)
	engine.POST("/chat", middleware.APIKey(cli), experimentalAdmission(gate), middleware.Idempotency(redis), middleware.RateLimit(redis, time.Minute, 1000), handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true))
	type result struct{ i, code int }
	results := make(chan result, 16)
	start := make(chan struct{})
	request := func(i int) int {
		w := httptest.NewRecorder()
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, tag)
		req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+accounts[i%4].key)
		req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%02d", tag, i))
		engine.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 16; i++ {
		go func(i int) { <-start; results <- result{i, request(i)} }(i)
	}
	close(start)
	for i := 0; i < 4; i++ {
		select {
		case <-arrived:
		case <-time.After(20 * time.Second):
			t.Fatal("upstream arrivals timed out")
		}
	}
	var rejected []int
	for i := 0; i < 12; i++ {
		select {
		case x := <-results:
			if x.code != 503 {
				t.Fatalf("first result=%+v", x)
			}
			rejected = append(rejected, x.i)
		case <-time.After(20 * time.Second):
			t.Fatal("rejections timed out")
		}
	}
	for _, i := range rejected {
		id := fmt.Sprintf("%s-%02d", tag, i)
		n, err := redis.Exists(ctx, "idem:"+id).Result()
		if err != nil || n != 0 {
			t.Fatalf("rejected idem=%d err=%v", n, err)
		}
		var count int64
		if err := db.Model(&store.Bill{}).Where("request_id=?", "idem:"+id).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rejected bills=%d err=%v", count, err)
		}
	}
	// With gate before idempotency, even a known duplicate sees overload
	// while every permit is held. After release it must see a duplicate.
	duplicateWhileFull := -1
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("%s-%02d", tag, i)
		n, err := redis.Exists(ctx, "idem:"+id).Result()
		if err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			duplicateWhileFull = i
			break
		}
	}
	if duplicateWhileFull < 0 {
		t.Fatal("no admitted idempotency key found")
	}
	if code := request(duplicateWhileFull); code != http.StatusServiceUnavailable {
		t.Fatalf("duplicate while full=%d", code)
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < 4; i++ {
		select {
		case x := <-results:
			if x.code != 200 {
				t.Fatalf("admitted=%+v", x)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("admitted timed out")
		}
	}
	for _, i := range rejected {
		if code := request(i); code != 200 {
			t.Fatalf("retry %d=%d", i, code)
		}
	}
	for i := 0; i < 16; i++ {
		if code := request(i); code != 409 {
			t.Fatalf("duplicate %d=%d", i, code)
		}
	}
	if calls.Load() != 16 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("idem:%s-%02d", tag, i)
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.PromptTokens == nil || *bill.PromptTokens != 4 || bill.CompletionTokens == nil || *bill.CompletionTokens != 2 {
			t.Fatalf("bill=%+v err=%v", bill, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var n int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if phase == "reverse" {
				want = 0
			}
			if n != want {
				t.Fatalf("%s event=%d", phase, n)
			}
		}
	}
	for _, a := range accounts {
		live, err := redis.Get(ctx, store.BalanceKey(a.tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, a.tenant.ID)
		if err != nil || live != 976 || ledger != live {
			t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
		}
		count, err := redis.ZCard(ctx, fmt.Sprintf("rate:win:%d", a.tenant.ID)).Result()
		if err != nil || count != 4 {
			t.Fatalf("rate count=%d err=%v", count, err)
		}
	}
	t.Log("4 admitted, 12 rejected before key/bill; duplicate while full=503; 12 retried successfully, 16 duplicates=409 after release; 4 account balances and event sets match")
}
