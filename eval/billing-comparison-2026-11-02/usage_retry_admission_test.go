package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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

type retryAdmissionState struct {
	arrivals atomic.Int32
	active   atomic.Int32
	peak     atomic.Int32
	attempts atomic.Int32
	settled  atomic.Int32
	barrier  chan struct{}
}
type retryAdmissionBilling struct {
	s     *retryAdmissionState
	calls atomic.Int32
}

func (b *retryAdmissionBilling) Reserve(context.Context, *pb.ReserveRequest) (*pb.ReserveResponse, error) {
	return &pb.ReserveResponse{Code: pb.ReserveCode_RESERVE_OK, BillId: 1}, nil
}
func (b *retryAdmissionBilling) ReportUsage(ctx context.Context, _ *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	if b.calls.Add(1) == 1 {
		if b.s.arrivals.Add(1) == 128 {
			close(b.s.barrier)
		}
		select {
		case <-b.s.barrier:
			return nil, status.Error(codes.Unavailable, "synthetic synchronized outage")
		case <-ctx.Done():
			return nil, status.Error(codes.DeadlineExceeded, "barrier timeout")
		}
	}
	b.s.attempts.Add(1)
	active := b.s.active.Add(1)
	defer b.s.active.Add(-1)
	for old := b.s.peak.Load(); active > old; old = b.s.peak.Load() {
		if b.s.peak.CompareAndSwap(old, active) {
			break
		}
	}
	select {
	case <-time.After(200 * time.Millisecond):
		return &pb.ReportUsageResponse{Accepted: true}, nil
	case <-ctx.Done():
		return nil, status.Error(codes.DeadlineExceeded, "retry canceled")
	}
}
func (b *retryAdmissionBilling) Settle(context.Context, *pb.SettleRequest) (*pb.SettleResponse, error) {
	b.s.settled.Add(1)
	return &pb.SettleResponse{Settled: true}, nil
}
func (b *retryAdmissionBilling) Reverse(context.Context, *pb.ReverseRequest) (*pb.ReverseResponse, error) {
	return nil, status.Error(codes.Internal, "unexpected reverse")
}

// Synthetic admission test: no SQL, Redis, or remote billing is used.
func TestUsageRetryAdmissionBound(t *testing.T) {
	if os.Getenv("TEST_RETRY_BOUND_CHECK") != "1" {
		t.Skip("explicit admission experiment only")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	gin.SetMode(gin.ReleaseMode)
	const n = 128
	s := &retryAdmissionState{barrier: make(chan struct{})}
	codesOut := make(chan int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	began := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			b := &retryAdmissionBilling{s: s}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
			c.Set("tenant_id", int64(i+1))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", fmt.Sprintf("retry-bound-%d", i))
			handler.Chat(b, usageRetryRouter{p: &gateway.RouteResult{ProviderID: 1, BaseURL: upstream.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
			codesOut <- w.Code
		}(i)
	}
	close(start)
	wg.Wait()
	elapsed := time.Since(began)
	close(codesOut)
	counts := map[int]int{}
	for code := range codesOut {
		counts[code]++
	}
	t.Logf("ADMISSION total=%d http=%v retries=%d peak_active=%d settled=%d not_settled=%d elapsed_ms=%d", n, counts, s.attempts.Load(), s.peak.Load(), s.settled.Load(), n-int(s.settled.Load()), elapsed.Milliseconds())
	if counts[200] != n || s.settled.Load() != s.attempts.Load() {
		t.Fatalf("incorrect outcome: status=%v retries=%d settled=%d", counts, s.attempts.Load(), s.settled.Load())
	}
	if s.peak.Load() > 16 {
		t.Fatalf("retry concurrency not bounded: peak=%d limit=16", s.peak.Load())
	}
}
