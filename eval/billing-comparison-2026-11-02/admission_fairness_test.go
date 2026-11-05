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

// Test-only; not a distributed limit or a production-ready admission policy.
func experimentalTenantGate(globalLimit, tenantLimit int) gin.HandlerFunc {
	var mu sync.Mutex
	active := make(map[int64]int)
	total := 0
	return func(c *gin.Context) {
		id := c.GetInt64("tenant_id")
		mu.Lock()
		if total >= globalLimit || active[id] >= tenantLimit {
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "test admission full", "type": "overloaded"}})
			return
		}
		total++
		active[id]++
		mu.Unlock()
		defer func() {
			mu.Lock()
			total--
			active[id]--
			if active[id] == 0 {
				delete(active, id)
			}
			mu.Unlock()
		}()
		c.Next()
	}
}

func TestAdmissionTenantFairnessRealBilling(t *testing.T) {
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
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("fair-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "fair-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx := context.Background()
	gin.SetMode(gin.ReleaseMode)
	for _, fair := range []bool{false, true} {
		t.Run(fmt.Sprintf("tenant_limit_%t", fair), func(t *testing.T) {
			tag := fmt.Sprintf("fair-%d-%t", time.Now().UnixNano(), fair)
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
				hash := sha256.Sum256([]byte(key))
				if err := db.Create(&store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: "test"}).Error; err != nil {
					t.Fatal(err)
				}
				tenants = append(tenants, tenant)
				keys = append(keys, key)
			}
			arrived := make(chan struct{}, 4)
			release := make(chan struct{})
			var closeOnce sync.Once
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				calls.Add(1)
				if strings.Contains(string(body), `"content":"hot"`) {
					arrived <- struct{}{}
					<-release
				}
				_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
			}))
			defer func() { closeOnce.Do(func() { close(release) }); up.Close() }()
			provider := store.Provider{Name: tag, BaseURL: up.URL, Models: "[]"}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			engine := gin.New()
			var gate gin.HandlerFunc
			if fair {
				gate = experimentalTenantGate(4, 1)
			} else {
				gate = experimentalAdmission(make(chan struct{}, 4))
			}
			engine.POST("/chat", middleware.APIKey(cli), gate, middleware.Idempotency(redis), middleware.RateLimit(redis, time.Minute, 1000), handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true))
			request := func(account, i int) int {
				content := "hot"
				if account == 1 {
					content = "cold"
				}
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}]}`, tag, content)
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+keys[account])
				req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%d-%d", tag, account, i))
				engine.ServeHTTP(w, req)
				return w.Code
			}
			type result struct{ i, code int }
			results := make(chan result, 4)
			start := make(chan struct{})
			for i := 0; i < 4; i++ {
				go func(i int) { <-start; results <- result{i, request(0, i)} }(i)
			}
			close(start)
			nHot := 4
			if fair {
				nHot = 1
			}
			for i := 0; i < nHot; i++ {
				select {
				case <-arrived:
				case <-time.After(20 * time.Second):
					t.Fatal("hot arrival timeout")
				}
			}
			var rejected []int
			if fair {
				for i := 0; i < 3; i++ {
					select {
					case x := <-results:
						if x.code != 503 {
							t.Fatalf("hot result=%+v", x)
						}
						rejected = append(rejected, x.i)
					case <-time.After(20 * time.Second):
						t.Fatal("hot rejection timeout")
					}
				}
			}
			cold := request(1, 0)
			if fair && cold != 200 || !fair && cold != 503 {
				t.Fatalf("fair=%t cold=%d", fair, cold)
			}
			if !fair {
				key := fmt.Sprintf("idem:%s-1-0", tag)
				n, err := redis.Exists(ctx, key).Result()
				if err != nil || n != 0 {
					t.Fatalf("cold rejected key=%d err=%v", n, err)
				}
			}
			closeOnce.Do(func() { close(release) })
			for i := 0; i < nHot; i++ {
				select {
				case x := <-results:
					if x.code != 200 {
						t.Fatalf("hot accepted=%+v", x)
					}
				case <-time.After(20 * time.Second):
					t.Fatal("hot completion timeout")
				}
			}
			for _, i := range rejected {
				if code := request(0, i); code != 200 {
					t.Fatalf("hot retry %d=%d", i, code)
				}
			}
			if !fair {
				if code := request(1, 0); code != 200 {
					t.Fatalf("cold retry=%d", code)
				}
			}
			if calls.Load() != 5 {
				t.Fatalf("upstream calls=%d", calls.Load())
			}
			for account := 0; account < 2; account++ {
				count := 4
				if account == 1 {
					count = 1
				}
				for i := 0; i < count; i++ {
					id := fmt.Sprintf("idem:%s-%d-%d", tag, account, i)
					var b store.Bill
					if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&b).Error; err != nil || b.Status != store.StatusSettled {
						t.Fatalf("bill=%+v err=%v", b, err)
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
							t.Fatalf("%s=%d", phase, n)
						}
					}
				}
				live, err := redis.Get(ctx, store.BalanceKey(tenants[account].ID)).Int64()
				if err != nil {
					t.Fatal(err)
				}
				ledger, err := store.LedgerBalance(ctx, db, tenants[account].ID)
				if err != nil || live != 1000-int64(count*6) || ledger != live {
					t.Fatalf("account=%d balance=%d ledger=%d err=%v", account, live, ledger, err)
				}
			}
			t.Logf("tenant_limit=%t hot_held=%d hot_rejected=%d cold_initial=%d all_five_settled=true", fair, nHot, len(rejected), cold)
		})
	}
}
