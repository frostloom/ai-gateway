package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// This child runs a separately compiled, unmodified Store against the same
// disposable MySQL/Redis as the parent. It is not a historical release binary.
func TestMixedVersionLegacyReverseChild(t *testing.T) {
	id := os.Getenv("MIXED_TEST_REQUEST")
	if id == "" {
		t.Skip("child only")
	}
	db, r := isolatedDB(t)
	resp, err := store.NewStore(db, r).Reverse(context.Background(), &pb.ReverseRequest{RequestId: id})
	if os.Getenv("MIXED_TEST_TRIGGER") == "1" {
		if err == nil || !strings.Contains(err.Error(), "experimental guard") {
			t.Fatalf("expected database guard to reject legacy reverse: resp=%v err=%v", resp, err)
		}
		return
	}
	if err != nil || !resp.Reversed {
		t.Fatalf("legacy reverse=%v err=%v", resp, err)
	}
}

func TestMixedVersionLegacyCanReverseForwardingBill(t *testing.T) {
	legacy := os.Getenv("MIXED_TEST_LEGACY_BINARY")
	if legacy == "" || os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires isolated mixed-version harness")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("mixed-forward-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: "test", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	reserve, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	q := db.Exec("UPDATE bills SET execution_state='forwarding' WHERE request_id=? AND phase=? AND execution_state='reserved'", id, store.PhaseReserve)
	if q.Error != nil || q.RowsAffected != 1 {
		t.Fatal(q.RowsAffected, q.Error)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("new store should refuse: %v", err)
	}
	if os.Getenv("MIXED_TEST_TRIGGER") == "1" {
		if err := db.Exec(`CREATE TRIGGER eval_mixed_guard BEFORE UPDATE ON bills FOR EACH ROW BEGIN IF OLD.execution_state='forwarding' AND OLD.status='pending' AND NEW.status='reversed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='experimental guard'; END IF; END`).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Exec("DROP TRIGGER IF EXISTS eval_mixed_guard")
	}
	child := exec.Command(legacy, "-test.run=^TestMixedVersionLegacyReverseChild$", "-test.v")
	child.Env = append(os.Environ(), "MIXED_TEST_REQUEST="+id)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy child failed: %v: %s", err, output)
	}
	if os.Getenv("MIXED_TEST_TRIGGER") == "1" {
		var row store.Bill
		if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		ledger, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || ledgerErr != nil {
			t.Fatal(err, ledgerErr)
		}
		if row.Status != store.StatusPending || live != 500 || ledger != 400 {
			t.Fatalf("unexpected post-rejection state bill=%s live=%d ledger=%d", row.Status, live, ledger)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
		pb.RegisterBillingServiceServer(server, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
		go func() { _ = server.Serve(listener) }()
		defer server.Stop()
		conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		balance, err := pb.NewBillingServiceClient(conn).GetBalance(ctx, &pb.GetBalanceRequest{TenantId: int64(tenant.ID)})
		if err != nil || balance.Balance != 500 {
			t.Fatalf("get balance=%v err=%v", balance, err)
		}
		if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
		settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
		if err != nil || !settled.Settled {
			t.Fatalf("settle=%v err=%v", settled, err)
		}
		liveAfter, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		ledgerAfter, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || ledgerErr != nil || liveAfter != 460 || ledgerAfter != 460 {
			t.Fatalf("recovered balance=%d ledger=%d err=%v/%v", liveAfter, ledgerAfter, err, ledgerErr)
		}
		t.Logf("legacy_reverse=rejected bill_before=pending live_before=%d ledger_before=%d grpc_balance=500 settled=true live_after=%d ledger_after=%d", live, ledger, liveAfter, ledgerAfter)
		return
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late usage: %v", err)
	}
	var row store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; err != nil || row.Status != store.StatusReversed || row.UsageReportedAt != nil {
		t.Fatalf("bill=%+v err=%v", row, err)
	}
	var reverses, usage int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&reverses).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":usage").Count(&usage).Error; err != nil {
		t.Fatal(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	ledger, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledgerErr != nil || live != 500 || ledger != live || reverses != 1 || usage != 0 {
		t.Fatalf("balance=%d ledger=%d reverse=%d usage=%d err=%v/%v", live, ledger, reverses, usage, err, ledgerErr)
	}
	t.Logf("new_reverse=rejected old_process_reverse=success late_usage=rejected reverse_events=%d usage_events=%d balance=%d ledger=%d", reverses, usage, live, ledger)
}
