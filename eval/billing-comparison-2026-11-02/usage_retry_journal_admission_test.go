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
	"path/filepath"
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

type journalAdmissionState struct {
	saved   atomic.Int32
	failed  atomic.Int32
	barrier chan struct{}
	dir     string
}
type journalAdmissionBridge struct {
	*gateway.Billing
	state  *journalAdmissionState
	tenant uint64
	calls  atomic.Int32
}

func (b *journalAdmissionBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	if b.calls.Add(1) == 1 {
		x := inboxUsage{RequestID: req.RequestId, TenantID: b.tenant, BillID: req.BillId, ProviderID: req.ProviderId, PromptTokens: req.PromptTokens, CompletionTokens: req.CompletionTokens}
		if err := saveLocalUsage(b.state.dir, x); err != nil {
			b.state.failed.Add(1)
			return nil, status.Error(codes.FailedPrecondition, "test journal save failed")
		}
		if b.state.saved.Add(1) == 128 {
			close(b.state.barrier)
		}
		select {
		case <-b.state.barrier:
			return nil, status.Error(codes.Unavailable, "synthetic before-send failure after local journal sync")
		case <-ctx.Done():
			return nil, status.Error(codes.DeadlineExceeded, "barrier canceled")
		}
	}
	select {
	case <-time.After(200 * time.Millisecond):
		return b.Billing.ReportUsage(ctx, req)
	case <-ctx.Done():
		return nil, status.Error(codes.DeadlineExceeded, "test delayed retry canceled")
	}
}

func TestUsageRetryJournalAdmissionRecovery(t *testing.T) {
	if os.Getenv("TEST_JOURNAL_ADMISSION") != "1" {
		t.Skip("explicit test-only journal/admission experiment")
	}
	failure := os.Getenv("TEST_JOURNAL_FAILURE") == "1"
	dir := t.TempDir()
	if failure {
		dir = filepath.Join(dir, "missing")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("journal-admission-%d", time.Now().UnixNano())
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
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("journal-admission")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "journal-admission")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	const n = 128
	state := &journalAdmissionState{dir: dir, barrier: make(chan struct{})}
	results := make(chan retryStormResult, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	began := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ts := time.Now()
			bridge := &journalAdmissionBridge{Billing: cli, state: state, tenant: tenants[i%len(tenants)].ID}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
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
	statusCounts := map[int]int{}
	callCounts := map[int32]int{}
	for x := range results {
		statusCounts[x.status]++
		callCounts[x.calls]++
	}
	if statusCounts[200] != n {
		t.Fatalf("status=%v", statusCounts)
	}
	var settled, pending, pendingNoUsage int
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("%s-%d", id, i)
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).First(&bill).Error; err != nil {
			t.Fatal(err)
		}
		switch bill.Status {
		case store.StatusSettled:
			settled++
		case store.StatusPending:
			pending++
			if bill.UsageReportedAt == nil {
				pendingNoUsage++
			}
		default:
			t.Fatalf("status=%s", bill.Status)
		}
	}
	t.Logf("JOURNAL_ADMISSION failure=%t saved=%d save_failed=%d http=%v calls=%v settled=%d pending=%d pending_no_usage=%d elapsed_ms=%d", failure, state.saved.Load(), state.failed.Load(), statusCounts, callCounts, settled, pending, pendingNoUsage, elapsed.Milliseconds())
	if failure {
		if state.saved.Load() != 0 || state.failed.Load() != n || settled != 0 || pendingNoUsage != n {
			t.Fatalf("unexpected journal failure result")
		}
		for i := 0; i < n; i++ {
			rid := fmt.Sprintf("%s-%d", id, i)
			for _, phase := range []string{"usage", "settle", "reverse"} {
				var count int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("unexpected event %s %s count=%d err=%v", rid, phase, count, err)
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
				t.Fatalf("failed journal tenant=%d live=%d ledger=%d err=%v", tenant.ID, live, ledger, err)
			}
		}
		return
	}
	if state.saved.Load() != n || state.failed.Load() != 0 || settled+pending != n {
		t.Fatalf("unexpected persisted journals")
	}
	// Test-only manual replay from trusted temporary files; repeated to verify accounting idempotency.
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < n; i++ {
			rid := fmt.Sprintf("%s-%d", id, i)
			x, err := readLocalUsage(dir, rid)
			if err != nil {
				t.Fatal(err)
			}
			if x.TenantID != tenants[i%len(tenants)].ID || x.ProviderID != int64(provider.ID) || x.PromptTokens != 4 || x.CompletionTokens != 2 {
				t.Fatalf("record identity mismatch: %+v", x)
			}
			var bill store.Bill
			if err := db.Where("id=? AND request_id=? AND tenant_id=? AND phase=?", x.BillID, rid, x.TenantID, store.PhaseReserve).First(&bill).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: rid, BillId: x.BillID, ProviderId: x.ProviderID, PromptTokens: x.PromptTokens, CompletionTokens: x.CompletionTokens}); err != nil {
				t.Fatal(err)
			}
			resp, err := s.Settle(ctx, &pb.SettleRequest{RequestId: rid, BillId: x.BillID})
			if err != nil || resp == nil || !resp.Settled {
				t.Fatalf("replay %s: %+v %v", rid, resp, err)
			}
		}
	}
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("%s-%d", id, i)
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
			t.Fatalf("bill %d: %+v %v", i, bill, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle"} {
			var count int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("event %s %s count=%d err=%v", rid, phase, count, err)
			}
		}
		var reverse int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":reverse").Count(&reverse).Error; err != nil || reverse != 0 {
			t.Fatalf("reverse %s=%d %v", rid, reverse, err)
		}
	}
	for _, tenant := range tenants {
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 952 || ledger != 952 {
			t.Fatalf("tenant %d live=%d ledger=%d err=%v", tenant.ID, live, ledger, err)
		}
	}
	t.Logf("RECOVERED files=%d replay_passes=2 settled=%d per_tenant_balance=952", state.saved.Load(), n)
}
