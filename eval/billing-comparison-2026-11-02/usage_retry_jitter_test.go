package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type retryPulse struct {
	arrivals atomic.Int32
	retries  chan time.Time
	barrier  chan struct{}
	settles  atomic.Int32
}
type retryPulseBilling struct {
	p     *retryPulse
	calls atomic.Int32
}

func (b *retryPulseBilling) Reserve(context.Context, *pb.ReserveRequest) (*pb.ReserveResponse, error) {
	return &pb.ReserveResponse{Code: pb.ReserveCode_RESERVE_OK, BillId: 1}, nil
}
func (b *retryPulseBilling) ReportUsage(ctx context.Context, _ *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	if b.calls.Add(1) == 1 {
		if b.p.arrivals.Add(1) == 128 {
			close(b.p.barrier)
		}
		select {
		case <-b.p.barrier:
			return nil, status.Error(codes.Unavailable, "synthetic pulse")
		case <-ctx.Done():
			return nil, status.Error(codes.DeadlineExceeded, "pulse barrier timeout")
		}
	}
	b.p.retries <- time.Now()
	return &pb.ReportUsageResponse{Accepted: true}, nil
}
func (b *retryPulseBilling) Settle(context.Context, *pb.SettleRequest) (*pb.SettleResponse, error) {
	b.p.settles.Add(1)
	return &pb.SettleResponse{Settled: true}, nil
}
func (b *retryPulseBilling) Reverse(context.Context, *pb.ReverseRequest) (*pb.ReverseResponse, error) {
	return nil, status.Error(codes.Internal, "unexpected reversal")
}

// Synthetic timing isolation only: billing, database and model provider are substitutes.
func TestUsageRetryPulseTiming(t *testing.T) {
	if os.Getenv("TEST_USAGE_RETRY") != "1" {
		t.Skip("requires a test-only retry overlay")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	gin.SetMode(gin.ReleaseMode)
	const n = 128
	p := &retryPulse{barrier: make(chan struct{}), retries: make(chan time.Time, n)}
	codesOut := make(chan int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			b := &retryPulseBilling{p: p}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
			c.Set("tenant_id", int64(i+1))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", fmt.Sprintf("pulse-%d", i))
			handler.Chat(b, usageRetryRouter{p: &gateway.RouteResult{ProviderID: 1, BaseURL: upstream.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
			if b.calls.Load() != 2 {
				codesOut <- 0
				return
			}
			codesOut <- w.Code
		}(i)
	}
	close(start)
	wg.Wait()
	close(codesOut)
	close(p.retries)
	for code := range codesOut {
		if code != 200 {
			t.Fatalf("HTTP/status or retries mismatch: %d", code)
		}
	}
	times := make([]time.Time, 0, n)
	for x := range p.retries {
		times = append(times, x)
	}
	if len(times) != n || p.settles.Load() != n {
		t.Fatalf("retries=%d settles=%d", len(times), p.settles.Load())
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	peak := 0
	j := 0
	for i := range times {
		for j < len(times) && times[j].Sub(times[i]) < 10*time.Millisecond {
			j++
		}
		if j-i > peak {
			peak = j - i
		}
	}
	span := times[n-1].Sub(times[0])
	t.Logf("PULSE total=%d span_ms=%.3f peak_10ms=%d", n, float64(span.Microseconds())/1000, peak)
}
