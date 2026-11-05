package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm/logger"
)

func TestGatewayReserveCommittedBeforeDeadline(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reserve-timeout-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `[]`}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, req.Body)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`)
	}))
	defer mock.Close()
	provider := store.Provider{Name: id, BaseURL: mock.URL, Models: `[]`, Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	committed := make(chan struct{}, 1)
	var capturedRequest *pb.ReserveRequest
	var capturedDigest string
	var delayed atomic.Bool
	delay := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		resp, e := next(ctx, req)
		if info.FullMethod == "/billing.v1.BillingService/Reserve" && e == nil && delayed.CompareAndSwap(false, true) {
			capturedRequest = proto.Clone(req.(*pb.ReserveRequest)).(*pb.ReserveRequest)
			md, _ := metadata.FromIncomingContext(ctx)
			if values := md.Get(controlplane.ReserveContentKey); len(values) == 1 {
				capturedDigest = values[0]
			}
			committed <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return resp, e
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(controlplane.UnaryServer("test-token"), delay))
	pb.RegisterBillingServiceServer(server, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	billing, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	route := &sequentialExecutionRouter{routes: []*gatewayclient.RouteResult{{ProviderID: provider.ID, BaseURL: mock.URL}}}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	rid := id + "-request"
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("tenant_id", int64(tenant.ID))
		c.Set("api_key_id", int64(1))
		c.Set("request_id", rid)
	}, handler.Chat(billing, route, slog.New(slog.DiscardHandler), time.Second))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)
	response, err := (&http.Client{Timeout: 8 * time.Second}).Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	select {
	case <-committed:
	default:
		t.Fatal("reserve did not commit")
	}
	if response.StatusCode != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatal(response.StatusCode, calls.Load())
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
		t.Fatal(bill.Status, err)
	}
	// Retry only the billing operation with identical parameters. It must report
	// a duplicate; this is not model response replay.
	if capturedRequest == nil || capturedDigest == "" {
		t.Fatal("missing captured request or digest")
	}
	replay, e := billing.Reserve(controlplane.WithReserveContent(ctx, capturedDigest), capturedRequest)
	if e != nil || replay == nil || replay.Code != pb.ReserveCode_RESERVE_DUPLICATE || replay.BillId != int64(bill.ID) {
		t.Fatalf("billing replay=%v err=%v", replay, e)
	}
	var count int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":reserve").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// Artificially age the test bill; this is not a natural 600-second wait.
	if err := db.Model(&store.Bill{}).Where("id=?", bill.ID).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Current default retains unknown outcomes, even in this test where the
	// interceptor proves the upstream was never called.
	holdRunner := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), 10*time.Minute)
	for i := 0; i < 2; i++ {
		held, holdErr := holdRunner.SweepOnce(ctx)
		if holdErr != nil || held.Scanned != 1 || held.Held != 1 || held.Reversed != 0 || held.Settled != 0 {
			t.Fatalf("default sweep %d: stats=%+v err=%v", i, held, holdErr)
		}
	}
	if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
		t.Fatalf("default retained bill=%+v err=%v", bill, err)
	}
	heldBalance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	heldLedger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || heldBalance != heldLedger || heldBalance >= 1000 {
		t.Fatalf("default retained balance=%d ledger=%d err=%v", heldBalance, heldLedger, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{rid + ":reserve", rid + ":usage", rid + ":settle", rid + ":reverse"}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("default retained outbox=%d err=%v", count, err)
	}
	t.Logf("default sweeps retained unknown twice: balance=%d ledger=%d outbox=%d mock_calls=%d", heldBalance, heldLedger, count, calls.Load())
	// Explicit legacy option is demonstrated separately; it is not the default.
	runner := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
	stats, err := runner.SweepOnce(ctx)
	if err != nil || stats.Scanned != 1 || stats.Reversed != 1 {
		t.Fatal(stats, err)
	}
	again, err := runner.SweepOnce(ctx)
	if err != nil || again.Scanned != 0 {
		t.Fatal(again, err)
	}
	if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusReversed {
		t.Fatal(bill.Status, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 1000 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{rid + ":reserve", rid + ":usage", rid + ":settle", rid + ":reverse"}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal(count, err)
	}
	eventsCount := count
	if err := db.Model(&store.Bill{}).Where("request_id=?", rid).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if calls.Load() != 0 {
		t.Fatal("mock called after recovery", calls.Load())
	}
	t.Logf("reserve_committed=true delayed_response=true http=%d mock_calls=%d billing_replay=duplicate pending_before_sweep=true reversed=%d ledger=%d balance=%d outbox=%d", response.StatusCode, calls.Load(), stats.Reversed, ledger, live, eventsCount)
}
