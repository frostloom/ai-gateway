package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

type timedRetryBridge struct {
	*gateway.Billing
	calls   atomic.Int32
	retries chan time.Time
}

func (b *timedRetryBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	n := b.calls.Add(1)
	if n == 1 {
		resp, err := b.Billing.ReportUsage(ctx, req)
		if err != nil {
			return resp, err
		}
		return nil, status.Error(codes.Unavailable, "synthetic response loss")
	}
	b.retries <- time.Now()
	return b.Billing.ReportUsage(ctx, req)
}

func TestUsageRetryRealJitterBurst(t *testing.T) {
	if os.Getenv("TEST_USAGE_RETRY") != "1" {
		t.Skip("requires a test-only retry overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("retry-storm-%d", time.Now().UnixNano())
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
	tenants := make([]store.Tenant, 16)
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", id, i), InitialQuota: 1000}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("retry-storm")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "retry-storm")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	const n = 128
	var wg sync.WaitGroup
	results := make(chan retryStormResult, n)
	retryTimes := make(chan time.Time, n)
	start := make(chan struct{})
	began := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ts := time.Now()
			bridge := &timedRetryBridge{Billing: cli, retries: retryTimes}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
			c.Set("tenant_id", int64(tenants[i%len(tenants)].ID))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", fmt.Sprintf("%s-%d", id, i))
			handler.Chat(bridge, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: upstream.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
			results <- retryStormResult{status: w.Code, calls: bridge.calls.Load(), elapsed: time.Since(ts)}
		}(i)
	}
	close(start)
	wg.Wait()
	elapsed := time.Since(began)
	close(results)
	close(retryTimes)
	retrySamples := make([]time.Time, 0, n)
	for tm := range retryTimes {
		retrySamples = append(retrySamples, tm)
	}
	sort.Slice(retrySamples, func(i, j int) bool { return retrySamples[i].Before(retrySamples[j]) })
	peak, j := 0, 0
	for i := range retrySamples {
		for j < len(retrySamples) && retrySamples[j].Sub(retrySamples[i]) < 10*time.Millisecond {
			j++
		}
		if j-i > peak {
			peak = j - i
		}
	}
	var span time.Duration
	if len(retrySamples) > 1 {
		span = retrySamples[len(retrySamples)-1].Sub(retrySamples[0])
	}
	t.Logf("RETRY_TIMING attempts=%d span_ms=%.3f peak_10ms=%d", len(retrySamples), float64(span.Microseconds())/1000, peak)
	statusCounts := map[int]int{}
	callCounts := map[int32]int{}
	times := make([]time.Duration, 0, n)
	for x := range results {
		statusCounts[x.status]++
		callCounts[x.calls]++
		times = append(times, x.elapsed)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	var settled, pending, missing, usageCount int
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("%s-%d", id, i)
		var rows []store.Bill
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).Find(&rows).Error; err != nil || len(rows) > 1 {
			t.Fatalf("reserve %d rows=%d err=%v", i, len(rows), err)
		}
		if len(rows) == 0 {
			missing++
			continue
		}
		bill := rows[0]
		switch bill.Status {
		case store.StatusSettled:
			settled++
		case store.StatusPending:
			pending++
		default:
			t.Errorf("unexpected status %s", bill.Status)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var count int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&count).Error; err != nil || count > 1 {
				t.Fatalf("event %d %s count=%d err=%v", i, phase, count, err)
			}
			if phase == "reserve" && count != 1 {
				t.Errorf("reserve event missing %d", i)
			}
			if phase == "reverse" && count != 0 {
				t.Errorf("unexpected reverse %d", i)
			}
			if phase == "usage" {
				usageCount += int(count)
			}
			if phase == "settle" && ((bill.Status == store.StatusSettled && count != 1) || (bill.Status == store.StatusPending && count != 0)) {
				t.Errorf("settlement mismatch %d status=%s count=%d", i, bill.Status, count)
			}
		}
	}
	for _, tenant := range tenants {
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != ledger {
			t.Errorf("tenant=%d balance=%d ledger=%d err=%v", tenant.ID, live, ledger, err)
		}
	}
	t.Logf("REAL_JITTER mode=%s workers=%d tenants=%d elapsed_ms=%d p95_ms=%d p99_ms=%d http=%v report_calls=%v settled=%d pending=%d missing=%d usage_events=%d", os.Getenv("TEST_USAGE_RETRY"), n, len(tenants), elapsed.Milliseconds(), times[(n-1)*95/100].Milliseconds(), times[(n-1)*99/100].Milliseconds(), statusCounts, callCounts, settled, pending, missing, usageCount)
}
