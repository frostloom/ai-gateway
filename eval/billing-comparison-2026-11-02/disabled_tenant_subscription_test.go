package comparison

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// A disabled tenant must not authenticate a new ordinary key or reserve quota.
func TestDisabledTenantOrdinaryKeyAndSubscriptionReserve(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("disabled-tenant-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 100000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(id)))
	key := store.APIKey{TenantID: tenant.ID, KeyHash: hash, KeyPrefix: "eval", Name: "ordinary"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	plan := store.Plan{Name: id, PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":300}`}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant.ID, plan.ID); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `["文本"]`}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("isolated-control")))
	pb.RegisterBillingServiceServer(rpc, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	billing, e := gatewayclient.NewBilling(listener.Addr().String(), "isolated-control")
	if e != nil {
		t.Fatal(e)
	}
	defer billing.Close()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/probe", middleware.APIKey(billing), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	probe := func() int {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.Header.Set("Authorization", "Bearer "+id)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := probe(); got != http.StatusNoContent {
		t.Fatalf("active gateway status=%d", got)
	}
	verified, err := s.ValidateAPIKey(ctx, hash)
	if err != nil || verified == nil || verified.ID != key.ID {
		t.Fatalf("active key: %v %v", verified, err)
	}
	activeRequest := id + "-inflight"
	active, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: activeRequest, TenantId: int64(tenant.ID), ApiKeyId: int64(key.ID), Model: id, EstPromptTokens: 100})
	if err != nil || active.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("active reserve=%v err=%v", active, err)
	}
	cashModel := store.Model{Vendor: "test", ModelID: id + "-cash", Name: id + "-cash", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `[]`}
	if err := db.Create(&cashModel).Error; err != nil {
		t.Fatal(err)
	}
	cashIDs := []string{id + "-cash-settle", id + "-cash-reverse"}
	for _, rid := range cashIDs {
		resp, e := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: int64(key.ID), Model: cashModel.ModelID, EstPromptTokens: 84})
		if e != nil || resp.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("cash reserve=%v err=%v", resp, e)
		}
	}
	if err := db.Model(&store.Tenant{}).Where("id=?", tenant.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if got := probe(); got != http.StatusUnauthorized {
		t.Errorf("disabled gateway status=%d", got)
	}
	_, rpcErr := billing.Reserve(ctx, &pb.ReserveRequest{RequestId: id + "-rpc", TenantId: int64(tenant.ID), ApiKeyId: int64(key.ID), Model: id, EstPromptTokens: 100})
	if status.Code(rpcErr) != codes.PermissionDenied {
		t.Errorf("disabled billing RPC err=%v", rpcErr)
	}
	legacyReplay, replayErr := s.Reserve(ctx, &pb.ReserveRequest{RequestId: activeRequest, TenantId: int64(tenant.ID), ApiKeyId: int64(key.ID), Model: id, EstPromptTokens: 100})
	if status.Code(replayErr) != codes.PermissionDenied {
		t.Errorf("disabled legacy replay=%v err=%v", legacyReplay, replayErr)
	}
	verified, err = s.ValidateAPIKey(ctx, hash)
	if err != nil || verified != nil {
		t.Errorf("disabled tenant key accepted: %v err=%v", verified, err)
	}
	request := id + "-request"
	result, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: request, TenantId: int64(tenant.ID), ApiKeyId: int64(key.ID), Model: id, EstPromptTokens: 100})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("disabled reserve result=%v err=%v", result, err)
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", request).Count(&count).Error; err != nil || count != 0 {
		t.Errorf("bills=%d err=%v", count, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":reserve").Count(&count).Error; err != nil || count != 0 {
		t.Errorf("outbox=%d err=%v", count, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: activeRequest, PromptTokens: 100}); err != nil {
		t.Errorf("inflight usage: %v", err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: activeRequest, BillId: active.BillId})
	if err != nil || settled == nil || !settled.Settled {
		t.Errorf("inflight settlement=%v err=%v", settled, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: cashIDs[0], PromptTokens: 40}); err != nil {
		t.Errorf("cash usage: %v", err)
	}
	cashSettled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: cashIDs[0]})
	if err != nil || cashSettled == nil || !cashSettled.Settled {
		t.Errorf("cash settlement=%v err=%v", cashSettled, err)
	}
	cashReversed, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: cashIDs[1], Reason: "synthetic pre-send failure"})
	if err != nil || cashReversed == nil || !cashReversed.Reversed {
		t.Errorf("cash reverse=%v err=%v", cashReversed, err)
	}
	if again, e := s.Settle(ctx, &pb.SettleRequest{RequestId: cashIDs[0]}); e != nil || again == nil || !again.Settled {
		t.Errorf("repeat settle=%v err=%v", again, e)
	}
	if again, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: cashIDs[1]}); e != nil || again == nil || !again.Reversed {
		t.Errorf("repeat reverse=%v err=%v", again, e)
	}
	if _, e := s.RebuildBalance(ctx, tenant.ID); e == nil {
		t.Error("ordinary rebuild unexpectedly accepted disabled tenant")
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || live != ledger {
		t.Errorf("live=%d ledger=%d err=%v", live, ledger, e)
	}
	for _, test := range []struct{ id, phase string }{{activeRequest, "settle"}, {cashIDs[0], "settle"}, {cashIDs[1], "reverse"}} {
		if e := db.Model(&store.OutboxEvent{}).Where("event_key=?", test.id+":"+test.phase).Count(&count).Error; e != nil || count != 1 {
			t.Errorf("event=%s:%s count=%d err=%v", test.id, test.phase, count, e)
		}
	}
}
