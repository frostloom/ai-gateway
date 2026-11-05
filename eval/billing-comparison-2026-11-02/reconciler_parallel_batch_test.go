package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

type batchResult struct {
	latency time.Duration
	err     error
	settled bool
}

// Test-only bounded batch, not the production reconciler's scan or status accounting.
func parallelSettleBatch(ctx context.Context, cli pb.BillingServiceClient, requests []*pb.SettleRequest, workers int) []batchResult {
	jobs := make(chan int)
	results := make([]batchResult, len(requests))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				start := time.Now()
				call, cancel := context.WithTimeout(ctx, 10*time.Second)
				resp, err := cli.Settle(call, requests[i])
				cancel()
				results[i] = batchResult{latency: time.Since(start), err: err, settled: resp != nil && resp.Settled}
			}
		}()
	}
	for i := range requests {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func TestParallelReconcileBatchRealLedger(t *testing.T) {
	if os.Getenv("TEST_RECON_PARALLEL") != "1" {
		t.Skip("isolated batch experiment")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("batch-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(lis.Addr().String(), "batch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cli := pb.NewBillingServiceClient(conn)
	for _, accounts := range []int{1, 8} {
		for _, workers := range []int{1, 4} {
			tag := fmt.Sprintf("par-%d-%d-%d", accounts, workers, time.Now().UnixNano())
			model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			tenants := make([]store.Tenant, accounts)
			for i := range tenants {
				tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", tag, i), InitialQuota: 10000}
				if err := db.Create(&tenants[i]).Error; err != nil {
					t.Fatal(err)
				}
				if err := redis.Set(ctx, store.BalanceKey(tenants[i].ID), 10000, 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			const n = 32
			requests := make([]*pb.SettleRequest, 0, n)
			perTenant := make([]int, accounts)
			for i := 0; i < n; i++ {
				index := i % accounts
				id := fmt.Sprintf("%s-%d", tag, i)
				res, e := st.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenants[index].ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
				if e != nil || res.Code != pb.ReserveCode_RESERVE_OK {
					t.Fatalf("reserve %v %v", res, e)
				}
				if e = st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, BillId: res.BillId, PromptTokens: 40}); e != nil {
					t.Fatal(e)
				}
				requests = append(requests, &pb.SettleRequest{RequestId: id, BillId: res.BillId})
				perTenant[index]++
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			readWaits := func() (int64, int64) {
				var k string
				var waits, millis int64
				if err := sqlDB.QueryRowContext(ctx, "SHOW GLOBAL STATUS LIKE 'Innodb_row_lock_waits'").Scan(&k, &waits); err != nil {
					t.Fatal(err)
				}
				if err := sqlDB.QueryRowContext(ctx, "SHOW GLOBAL STATUS LIKE 'Innodb_row_lock_time'").Scan(&k, &millis); err != nil {
					t.Fatal(err)
				}
				return waits, millis
			}
			readRedisCommands := func() int64 {
				raw, err := redis.Info(ctx, "stats").Result()
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range strings.Split(raw, "\n") {
					if strings.HasPrefix(line, "total_commands_processed:") {
						n, e := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "total_commands_processed:")), 10, 64)
						if e != nil {
							t.Fatal(e)
						}
						return n
					}
				}
				t.Fatal("missing Redis command count")
				return 0
			}
			redisBefore := readRedisCommands()
			waitsBefore, waitTimeBefore := readWaits()
			before := sqlDB.Stats()
			start := time.Now()
			results := parallelSettleBatch(ctx, cli, requests, workers)
			elapsed := time.Since(start)
			after := sqlDB.Stats()
			waitsAfter, waitTimeAfter := readWaits()
			redisAfter := readRedisCommands()
			var lat []float64
			for _, r := range results {
				if r.err != nil || !r.settled {
					t.Fatalf("settle=%+v", r)
				}
				lat = append(lat, float64(r.latency.Microseconds())/1000)
			}
			sort.Float64s(lat)
			var sum float64
			for _, v := range lat {
				sum += v
			}
			// Repeat every operation to verify idempotent state, not only aggregate balance.
			retries := parallelSettleBatch(ctx, cli, requests, workers)
			for _, r := range retries {
				if r.err != nil || !r.settled {
					t.Fatalf("retry=%+v", r)
				}
			}
			for _, req := range requests {
				var reserve store.Bill
				if err := db.Where("request_id=? AND phase=?", req.RequestId, store.PhaseReserve).Take(&reserve).Error; err != nil || reserve.Status != store.StatusSettled {
					t.Fatalf("reserve=%+v err=%v", reserve, err)
				}
				for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
					var count int64
					if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", req.RequestId+":"+phase).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					want := int64(1)
					if phase == "reverse" {
						want = 0
					}
					if count != want {
						t.Fatalf("%s:%s count=%d", req.RequestId, phase, count)
					}
				}
				var count int64
				if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", req.RequestId, store.PhaseSettle).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("settle rows=%d err=%v", count, err)
				}
			}
			for i, tenant := range tenants {
				live, e := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
				if e != nil {
					t.Fatal(e)
				}
				ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
				if e != nil || ledger != live || live != int64(10000-40*perTenant[i]) {
					t.Fatalf("tenant %d live=%d ledger=%d err=%v", tenant.ID, live, ledger, e)
				}
			}
			t.Logf("accounts=%d workers=%d n=%d elapsed_ms=%.1f ops_s=%.1f mean_ms=%.2f p95_ms=%.2f p99_ms=%.2f errors=0 timeouts=0 pool_waits=%d mysql_lock_waits=%d mysql_lock_wait_ms=%d redis_commands_including_info=%d", accounts, workers, n, float64(elapsed.Microseconds())/1000, float64(n)/elapsed.Seconds(), sum/n, lat[30], lat[31], after.WaitCount-before.WaitCount, waitsAfter-waitsBefore, waitTimeAfter-waitTimeBefore, redisAfter-redisBefore)
		}
	}
}
