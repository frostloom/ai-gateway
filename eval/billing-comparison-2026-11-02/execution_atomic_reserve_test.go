package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type experimentalAtomicReserve interface {
	ReserveWithExecutionIntent(context.Context, *pb.ReserveRequest, string, time.Duration) (*pb.ReserveResponse, error)
}

func TestAtomicReserveOwnerAndRollback(t *testing.T) {
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
		t.Fatal("atomic execution intent interface absent")
	}
	id := fmt.Sprintf("atomic-exec-%d", time.Now().UnixNano())
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
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	digest := sha256.Sum256([]byte("synthetic-owner"))
	hash := hex.EncodeToString(digest[:])
	var hits atomic.Int32
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_execution_owner", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if ok && values["execution_owner_hash"] != nil && hits.CompareAndSwap(0, 1) {
			tx.AddError(errors.New("synthetic execution owner write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Update().Remove("test:fail_execution_owner") })
	if _, err := candidate.ReserveWithExecutionIntent(ctx, req, hash, 30*time.Second); err == nil || hits.Load() != 1 {
		t.Fatalf("reserve err=%v callback=%d", err, hits.Load())
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || bills != 0 || events != 0 {
		t.Fatal(bills, events, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 500 {
		t.Fatal(live, err)
	}
	done, err := candidate.ReserveWithExecutionIntent(ctx, req, hash, 30*time.Second)
	if err != nil || done.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(done, err)
	}
	var state struct {
		ExecutionState     string
		ExecutionOwnerHash string
		LeaseLive          bool
	}
	if err := db.Raw("SELECT execution_state,execution_owner_hash,execution_lease_until>CURRENT_TIMESTAMP(6) AS lease_live FROM bills WHERE id=?", done.BillId).Scan(&state).Error; err != nil || state.ExecutionState != "reserved" || state.ExecutionOwnerHash != hash || !state.LeaseLive {
		t.Fatal(state, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 400 || ledger != live {
		t.Fatal(live, ledger, err)
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
	var denied, granted wrapperspb.Int64Value
	if err := conn.Invoke(ctx, method, wrapperspb.String(id+"|forwarding|wrong-owner"), &denied); err != nil || denied.Value != 0 {
		t.Fatal(denied.Value, err)
	}
	if err := conn.Invoke(ctx, method, wrapperspb.String(id+"|forwarding|synthetic-owner"), &granted); err != nil || granted.Value != 1 {
		t.Fatal(granted.Value, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: int64(done.BillId)})
	if err != nil || !settled.Settled {
		t.Fatal(settled, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 460 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatal(events, err)
	}
	t.Logf("injected=%d rollback_bills=0 rollback_events=0 retry_state=%s lease_live=%v balance=%d ledger=%d events=%d", hits.Load(), state.ExecutionState, state.LeaseLive, live, ledger, events)
}
