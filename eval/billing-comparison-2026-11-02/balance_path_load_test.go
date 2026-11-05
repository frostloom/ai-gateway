package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/admin"
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

func TestBalanceGRPCAndPortalLoad(t *testing.T) {
	if os.Getenv("TEST_BALANCE_PATH_LOAD") != "1" {
		t.Skip("isolated load only")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	const accounts = 16
	const workers = 8
	const requests = 128
	tenants := make([]store.Tenant, accounts)
	keys := make([]string, accounts)
	tag := fmt.Sprintf("balance-load-%d", time.Now().UnixNano())
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", tag, i), InitialQuota: 1000}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := redis.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
		keys[i] = fmt.Sprintf("%s-key-%d", tag, i)
		hash := sha256.Sum256([]byte(keys[i]))
		key := store.APIKey{TenantID: tenants[i].ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: tag}
		if err := db.Create(&key).Error; err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("balance-load")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "balance-load")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	grpcClient := pb.NewBillingServiceClient(conn)
	billingHTTP := httptest.NewServer(admin.Handler(st, slog.New(slog.DiscardHandler), "balance-load"))
	defer billingHTTP.Close()
	billingClient, err := gatewayclient.NewBilling(listener.Addr().String(), "balance-load")
	if err != nil {
		t.Fatal(err)
	}
	defer billingClient.Close()
	gatewayhandler.SetControlToken("balance-load")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/portal")
	group.Use(middleware.APIKey(billingClient))
	gatewayhandler.RegisterPortal(group, billingHTTP.URL, "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	httpClient := &http.Client{Timeout: 15 * time.Second}
	defer httpClient.CloseIdleConnections()
	for _, mode := range []string{"grpc", "portal"} {
		latencies := make([]float64, requests)
		errorsSeen := make([]error, requests)
		jobs := make(chan int)
		var wg sync.WaitGroup
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		before := sqlDB.Stats()
		start := time.Now()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					begin := time.Now()
					c, cancel := context.WithTimeout(ctx, 15*time.Second)
					index := i % accounts
					if mode == "grpc" {
						response, err := grpcClient.GetBalance(c, &pb.GetBalanceRequest{TenantId: int64(tenants[index].ID)})
						if err != nil {
							errorsSeen[i] = err
						} else if response.Balance != 1000 {
							errorsSeen[i] = fmt.Errorf("balance=%d", response.Balance)
						}
					} else {
						req, err := http.NewRequestWithContext(c, http.MethodGet, gateway.URL+"/portal/me", nil)
						if err != nil {
							errorsSeen[i] = err
						} else {
							req.Header.Set("Authorization", "Bearer "+keys[index])
							response, e := httpClient.Do(req)
							if e != nil {
								errorsSeen[i] = e
							} else {
								var view store.MeView
								e = json.NewDecoder(response.Body).Decode(&view)
								response.Body.Close()
								if e != nil || response.StatusCode != 200 || view.Balance != 1000 || view.LedgerBalance != 1000 {
									errorsSeen[i] = fmt.Errorf("status=%d balance=%d ledger=%d err=%v", response.StatusCode, view.Balance, view.LedgerBalance, e)
								}
							}
						}
					}
					cancel()
					latencies[i] = float64(time.Since(begin).Microseconds()) / 1000
				}
			}()
		}
		for i := 0; i < requests; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		elapsed := time.Since(start)
		after := sqlDB.Stats()
		var failures, timeouts int
		classes := make(map[string]int)
		for _, err := range errorsSeen {
			if err == nil {
				continue
			}
			failures++
			message := strings.ToLower(err.Error())
			if errors.Is(err, context.DeadlineExceeded) || strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded") {
				timeouts++
			}
			switch {
			case strings.Contains(message, "status=502"):
				classes["http_502"]++
			case strings.Contains(message, "status=503"):
				classes["http_503"]++
			case strings.Contains(message, "status=500"):
				classes["http_500"]++
			default:
				classes["other"]++
			}
		}
		sort.Float64s(latencies)
		var sum float64
		for _, v := range latencies {
			sum += v
		}
		t.Logf("mode=%s accounts=%d workers=%d requests=%d completed_s=%.1f success_s=%.1f mean_ms=%.2f p95_ms=%.2f p99_ms=%.2f errors=%d client_timeouts=%d classes=%v pool_waits=%d", mode, accounts, workers, requests, float64(requests)/elapsed.Seconds(), float64(requests-failures)/elapsed.Seconds(), sum/requests, latencies[121], latencies[126], failures, timeouts, classes, after.WaitCount-before.WaitCount)
		if failures != 0 {
			t.Fatalf("mode=%s had %d failed responses", mode, failures)
		}
	}
}
