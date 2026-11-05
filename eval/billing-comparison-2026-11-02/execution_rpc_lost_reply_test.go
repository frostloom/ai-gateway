package comparison

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm/logger"
)

func TestExecutionRPCCommitThenLostResponse(t *testing.T) {
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
	id := fmt.Sprintf("exec-rpc-lost-%d", time.Now().UnixNano())
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
	bill, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var dropped atomic.Bool
	inject := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil && info.FullMethod == "/eval.execution.v1.Execution/Transition" && resp.(*wrapperspb.Int64Value).Value == 1 && dropped.CompareAndSwap(false, true) {
			return nil, status.Error(codes.Unavailable, "synthetic lost response after commit")
		}
		return resp, err
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(controlplane.UnaryServer("test-token"), inject))
	server.RegisterService(&executionDesc, &executionServer{db})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const method = "/eval.execution.v1.Execution/Transition"
	var first wrapperspb.Int64Value
	err = conn.Invoke(ctx, method, wrapperspb.String(id+"|forwarding"), &first)
	if status.Code(err) != codes.Unavailable || !dropped.Load() {
		t.Fatal(err, dropped.Load())
	}
	var state string
	if err := db.Raw("SELECT execution_state FROM bills WHERE id=?", bill.BillId).Scan(&state).Error; err != nil || state != "forwarding" {
		t.Fatal(state, err)
	}
	var retry, refund wrapperspb.Int64Value
	if err := conn.Invoke(ctx, method, wrapperspb.String(id+"|forwarding"), &retry); err != nil || retry.Value != 0 {
		t.Fatal(retry.Value, err)
	}
	if err := conn.Invoke(ctx, method, wrapperspb.String(id+"|reversing"), &refund); err != nil || refund.Value != 0 {
		t.Fatal(refund.Value, err)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(bill.BillId)}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	var row store.Bill
	if err := db.Where("id=?", bill.BillId).First(&row).Error; err != nil || row.Status != store.StatusPending || row.UsageReportedAt != nil {
		t.Fatal(row.Status, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 400 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse", id + ":usage"}).Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	t.Logf("first=Unavailable stored=%s retry=%d refund_permit=%d bill=%s balance=%d ledger=%d events=%d", state, retry.Value, refund.Value, row.Status, live, ledger, events)
}
