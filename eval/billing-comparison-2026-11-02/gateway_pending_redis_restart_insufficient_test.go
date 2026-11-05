package comparison

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
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

func TestGatewayInsufficientBalanceAfterRedisRestartWhilePending(t *testing.T) {
	container := os.Getenv("COMPARISON_REDIS_CONTAINER")
	addr := os.Getenv("COMPARISON_REDIS_ADDR")
	if os.Getenv("TEST_PENDING_REDIS_RESTART") != "1" || !regexp.MustCompile(`^auth-cache-test-redis-[0-9]+-[0-9]+$`).MatchString(container) {
		t.Skip("requires isolated Redis")
	}
	out, err := exec.Command("docker", "port", container, "6379/tcp").CombinedOutput()
	if err != nil || !strings.HasPrefix(addr, "127.0.0.1:") || !strings.Contains(string(out), addr) {
		t.Fatalf("container port mismatch: %s %v", out, err)
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("pending-restart-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 2)
	keys := make([]string, 2)
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", id, i), InitialQuota: 10}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		keys[i] = fmt.Sprintf("%s-key-%d", id, i)
		hash := sha256.Sum256([]byte(keys[i]))
		key := store.APIKey{TenantID: tenants[i].ID, KeyHash: fmt.Sprintf("%x", hash), Name: "ordinary"}
		if err := db.Create(&key).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Del(ctx, store.BalanceKey(tenants[i].ID)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("projection-loss-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "projection-loss-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(middleware.APIKey(cli), middleware.Idempotency(r), middleware.RateLimit(r, time.Minute, 100))
	e.POST("/", handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true))
	type result struct{ tenant, status int }
	results := make(chan result, 8)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ti := i % 2
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"max_tokens":5,"messages":[{"role":"user","content":"hello"}]}`, id)))
			req.Header.Set("Authorization", "Bearer "+keys[ti])
			req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%d", id, i))
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			results <- result{ti, w.Code}
		}(i)
	}
	close(start)
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			unblock()
			wg.Wait()
			t.Fatal("expected one upstream request per account")
		}
	}
	counts := [2]map[int]int{{}, {}}
	for i := 0; i < 6; i++ {
		select {
		case x := <-results:
			counts[x.tenant][x.status]++
		case <-time.After(10 * time.Second):
			unblock()
			wg.Wait()
			t.Fatalf("six requests did not reject while upstream held; calls=%d", calls.Load())
		}
	}
	if calls.Load() != 2 || counts[0][402] != 3 || counts[1][402] != 3 {
		unblock()
		wg.Wait()
		t.Fatalf("before release calls=%d counts=%v", calls.Load(), counts)
	}
	for _, tenant := range tenants {
		var bills []store.Bill
		if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Find(&bills).Error; err != nil || len(bills) != 1 {
			unblock()
			wg.Wait()
			t.Fatalf("tenant %d bills=%d err=%v", tenant.ID, len(bills), err)
		}
		if bills[0].Status != store.StatusPending || bills[0].PreQuota <= 5 || bills[0].PreQuota > 10 {
			unblock()
			wg.Wait()
			t.Fatalf("unexpected pending reserve=%+v", bills[0])
		}
	}
	// Both upstream requests are still blocked and their reserve rows are pending.
	docker := func(args ...string) {
		t.Helper()
		dctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(dctx, "docker", args...).CombinedOutput()
		if err != nil {
			unblock()
			wg.Wait()
			t.Fatalf("docker %v: %s %v", args, out, err)
		}
	}
	docker("kill", container)
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_ = exec.Command("docker", "start", container).Run()
		}
	})
	docker("start", container)
	restarted = true
	deadline := time.Now().Add(20 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := r.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			unblock()
			wg.Wait()
			t.Fatalf("Redis did not recover: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, tenant := range tenants {
		n, err := r.Exists(ctx, store.BalanceKey(tenant.ID)).Result()
		if err != nil || n != 0 {
			unblock()
			wg.Wait()
			t.Fatalf("expected lost balance projection, exists=%d err=%v", n, err)
		}
	}
	wave := make(chan result, 6)
	waveStart := make(chan struct{})
	for i := 0; i < 6; i++ {
		go func(i int) {
			<-waveStart
			iTenant := i % 2
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"max_tokens":5,"messages":[{"role":"user","content":"hello"}]}`, id)))
			req.Header.Set("Authorization", "Bearer "+keys[iTenant])
			req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-post-%d", id, i))
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			wave <- result{iTenant, w.Code}
		}(i)
	}
	close(waveStart)
	waveCounts := [2]map[int]int{{}, {}}
	for i := 0; i < 6; i++ {
		select {
		case x := <-wave:
			waveCounts[x.tenant][x.status]++
		case <-time.After(10 * time.Second):
			unblock()
			wg.Wait()
			t.Fatalf("post-restart requests did not finish; upstream=%d", calls.Load())
		}
	}
	if calls.Load() != 2 || waveCounts[0][402] != 3 || waveCounts[1][402] != 3 {
		unblock()
		wg.Wait()
		t.Fatalf("post-restart counts=%v upstream=%d", waveCounts, calls.Load())
	}
	for i, tenant := range tenants {
		var bill store.Bill
		if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).First(&bill).Error; err != nil {
			unblock()
			wg.Wait()
			t.Fatal(err)
		}
		if !strings.HasPrefix(bill.RequestID, "idem:") {
			unblock()
			wg.Wait()
			t.Fatalf("unexpected request id %q", bill.RequestID)
		}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"max_tokens":5,"messages":[{"role":"user","content":"hello"}]}`, id)))
		req.Header.Set("Authorization", "Bearer "+keys[i])
		req.Header.Set("Idempotency-Key", strings.TrimPrefix(bill.RequestID, "idem:"))
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if w.Code != 409 || calls.Load() != 2 {
			unblock()
			wg.Wait()
			t.Fatalf("same-key retry status=%d upstream=%d", w.Code, calls.Load())
		}
	}
	t.Logf("Redis restarted while two reservations pending; lost keys rebuilt; post-restart each account rejected 3 requests: %v", waveCounts)
	unblock()
	wg.Wait()
	close(results)
	for x := range results {
		counts[x.tenant][x.status]++
	}
	for i, tenant := range tenants {
		if counts[i][200] != 1 || counts[i][402] != 3 {
			t.Fatalf("tenant %d statuses=%v", i, counts[i])
		}
		var bill store.Bill
		if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
			t.Fatalf("tenant %d bill=%+v err=%v", i, bill, err)
		}
		var settlementRows int64
		if err := db.Model(&store.Bill{}).Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseSettle).Count(&settlementRows).Error; err != nil || settlementRows != 1 {
			t.Fatalf("tenant %d settlement rows=%d err=%v", i, settlementRows, err)
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 4 || ledger != 4 {
			t.Fatalf("tenant %d balance=%d/%d err=%v", i, live, ledger, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var n int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", bill.RequestID+":"+phase).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			expected := int64(1)
			if phase == "reverse" {
				expected = 0
			}
			if n != expected {
				t.Fatalf("tenant %d phase %s count=%d", i, phase, n)
			}
		}
	}
	t.Logf("two accounts, each 4 concurrent requests: each 1 settled and 3 insufficient; upstream=%d final balances=4/4", calls.Load())
}
