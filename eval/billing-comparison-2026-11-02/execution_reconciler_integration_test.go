package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Test-only bridge. There is no such authenticated production API.
type stateSweepBridge struct {
	pb.BillingServiceClient
	db   *gorm.DB
	conn *grpc.ClientConn
	s    *store.Store
}

func (b *stateSweepBridge) Reverse(ctx context.Context, req *pb.ReverseRequest, _ ...grpc.CallOption) (*pb.ReverseResponse, error) {
	var granted wrapperspb.Int64Value
	if err := b.conn.Invoke(ctx, "/eval.execution.v1.Execution/Transition", wrapperspb.String(req.RequestId+"|reversing|synthetic-recovery"), &granted); err != nil {
		return nil, err
	}
	if granted.Value != 1 {
		var state string
		if err := b.db.WithContext(ctx).Raw("SELECT execution_state FROM bills WHERE request_id=? AND phase=?", req.RequestId, store.PhaseReserve).Scan(&state).Error; err != nil {
			return nil, err
		}
		if state != "reversing" {
			return nil, status.Error(codes.FailedPrecondition, "not eligible for automatic reverse")
		}
	}
	return b.s.Reverse(ctx, req)
}
func TestExecutionReconcilerWithLeaseAndForwarding(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental billing state overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate missing")
	}
	id := fmt.Sprintf("execution-reconcile-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	server.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const method = "/eval.execution.v1.Execution/Transition"
	ids := []string{id + "-forward", id + "-reserved"}
	bills := make([]*pb.ReserveResponse, 2)
	digest := sha256.Sum256([]byte("synthetic-owner"))
	hash := hex.EncodeToString(digest[:])
	for i, rid := range ids {
		bill, err := candidate.ReserveWithExecutionIntent(ctx, &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}, hash, 30*time.Second)
		if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(bill, err)
		}
		bills[i] = bill
	}
	var permit wrapperspb.Int64Value
	if err := conn.Invoke(ctx, method, wrapperspb.String(ids[0]+"|forwarding|synthetic-owner"), &permit); err != nil || permit.Value != 1 {
		t.Fatal(permit.Value, err)
	}
	for _, bill := range bills {
		if err := db.Model(&store.Bill{}).Where("id=?", bill.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id=?", bill.BillId).Error; err != nil {
			t.Fatal(err)
		}
	}
	runner := reconciler.New(db, &stateSweepBridge{db: db, conn: conn, s: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
	first, err := runner.SweepOnce(ctx)
	if err != nil || first.Scanned != 2 || first.Reversed != 1 {
		t.Fatal(first, err)
	}
	second, err := runner.SweepOnce(ctx)
	if err != nil || second.Scanned != 1 || second.Reversed != 0 {
		t.Fatal(second, err)
	}
	var rows []store.Bill
	if err := db.Where("request_id IN ? AND phase=?", ids, store.PhaseReserve).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, row := range rows {
		states[row.RequestID] = row.Status
	}
	if states[ids[0]] != store.StatusPending || states[ids[1]] != store.StatusReversed {
		t.Fatal(states)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: ids[0], PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: ids[0], BillId: int64(bills[0].BillId)})
	if err != nil || !settled.Settled {
		t.Fatal(settled, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 460 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{ids[0] + ":reserve", ids[0] + ":usage", ids[0] + ":settle", ids[1] + ":reserve", ids[1] + ":reverse"}).Count(&events).Error; err != nil || events != 5 {
		t.Fatal(events, err)
	}
	t.Logf("first_scanned=%d first_reversed=%d second_scanned=%d second_reversed=%d late_usage_settled=true balance=%d ledger=%d events=%d", first.Scanned, first.Reversed, second.Scanned, second.Reversed, live, ledger, events)
}
