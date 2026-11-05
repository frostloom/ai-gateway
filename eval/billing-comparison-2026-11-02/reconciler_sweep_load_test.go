package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

type sweepMeasuredClient struct {
	pb.BillingServiceClient
	mu   sync.Mutex
	lat  []float64
	errs int
}

func (c *sweepMeasuredClient) Settle(ctx context.Context, req *pb.SettleRequest, opts ...grpc.CallOption) (*pb.SettleResponse, error) {
	start := time.Now()
	resp, err := c.BillingServiceClient.Settle(ctx, req, opts...)
	ms := float64(time.Since(start).Microseconds()) / 1000
	c.mu.Lock()
	c.lat = append(c.lat, ms)
	if err != nil || resp == nil || !resp.Settled {
		c.errs++
	}
	c.mu.Unlock()
	return resp, err
}
func TestReconcilerSweepActualLoad(t *testing.T) {
	if os.Getenv("TEST_RECON_SWEEP_LOAD") != "1" {
		t.Skip("isolated sweep load")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("sweep-load")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "sweep-load")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, accounts := range []int{1, 8} {
		tag := fmt.Sprintf("sweep-load-%d-%d", accounts, time.Now().UnixNano())
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
		reqs := make([]*pb.SettleRequest, 0, n)
		perTenant := make([]int, accounts)
		for i := 0; i < n; i++ {
			k := i % accounts
			id := fmt.Sprintf("%s-%d", tag, i)
			res, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenants[k].ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
			if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatalf("reserve=%v err=%v", res, err)
			}
			if err := st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, BillId: res.BillId, PromptTokens: 40}); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			reqs = append(reqs, &pb.SettleRequest{RequestId: id, BillId: res.BillId})
			perTenant[k]++
		}
		measured := &sweepMeasuredClient{BillingServiceClient: pb.NewBillingServiceClient(conn)}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		before := sqlDB.Stats()
		start := time.Now()
		stats, err := reconciler.New(db, measured, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
		elapsed := time.Since(start)
		after := sqlDB.Stats()
		if err != nil || stats.Scanned != n || stats.Settled != n || stats.SettleErrors != 0 || measured.errs != 0 || len(measured.lat) != n {
			t.Fatalf("stats=%+v err=%v calls=%d errors=%d", stats, err, len(measured.lat), measured.errs)
		}
		sort.Float64s(measured.lat)
		var sum float64
		for _, v := range measured.lat {
			sum += v
		}
		for _, req := range reqs {
			if _, err := st.Settle(ctx, req); err != nil {
				t.Fatal(err)
			}
			var nRows int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", req.RequestId, store.PhaseSettle).Count(&nRows).Error; err != nil || nRows != 1 {
				t.Fatalf("settle=%d err=%v", nRows, err)
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
					t.Fatalf("event=%s count=%d", phase, count)
				}
			}
		}
		for i, tenant := range tenants {
			live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || ledger != live || live != int64(10000-40*perTenant[i]) {
				t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
			}
		}
		t.Logf("accounts=%d n=%d elapsed_ms=%.2f settle_s=%.2f mean_rpc_ms=%.2f p95_rpc_ms=%.2f p99_rpc_ms=%.2f errors=0 pool_waits=%d", accounts, n, float64(elapsed.Microseconds())/1000, float64(n)/elapsed.Seconds(), sum/n, measured.lat[30], measured.lat[31], after.WaitCount-before.WaitCount)
	}
}
