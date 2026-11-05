package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm/logger"
)

func TestForwardingSentOwnerChild(t *testing.T) {
	if os.Getenv("EXEC_SENT_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, _ := isolatedDB(t)
	gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
	id := os.Getenv("EXEC_SENT_ID")
	permit, err := gate.Transition(context.Background(), wrapperspb.String(id+"|forwarding|old-owner"))
	if err != nil || permit.Value != 1 {
		t.Fatal(permit, err)
	}
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(os.Getenv("EXEC_SENT_URL"), "application/json", strings.NewReader(`{"prompt":"synthetic"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
}
func TestSentRequestOwnerKilledCannotRefundOnExpiry(t *testing.T) {
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
	id := fmt.Sprintf("sent-owner-kill-%d", time.Now().UnixNano())
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
	digest := sha256.Sum256([]byte("old-owner"))
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	bill, err := candidate.ReserveWithExecutionIntent(ctx, req, hex.EncodeToString(digest[:]), 30*time.Second)
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	received := make(chan error, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil && string(body) != `{"prompt":"synthetic"}` {
			err = fmt.Errorf("unexpected request body")
		}
		calls.Add(1)
		received <- err
		<-release
		w.WriteHeader(200)
	}))
	defer upstream.Close()
	defer unblock()
	cmd := exec.Command(os.Args[0], "-test.run=^TestForwardingSentOwnerChild$")
	cmd.Env = append(os.Environ(), "EXEC_SENT_CHILD=1", "EXEC_SENT_ID="+id, "EXEC_SENT_URL="+upstream.URL)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	select {
	case err := <-received:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("upstream did not receive request")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	unblock()
	if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=? AND phase=?", id, store.PhaseReserve).Error; err != nil {
		t.Fatal(err)
	}
	gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
	retry, err := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|old-owner"))
	if err != nil || retry.Value != 0 {
		t.Fatal(retry, err)
	}
	recover, err := gate.Transition(ctx, wrapperspb.String(id+"|reversing|synthetic-recovery"))
	if err != nil || recover.Value != 0 {
		t.Fatal(recover, err)
	}
	if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(bill.BillId)}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	var row store.Bill
	if err := db.Where("id=?", bill.BillId).First(&row).Error; err != nil || row.Status != store.StatusPending || row.UsageReportedAt != nil {
		t.Fatal(row.Status, err)
	}
	var state string
	if err := db.Raw("SELECT execution_state FROM bills WHERE id=?", bill.BillId).Scan(&state).Error; err != nil || state != "forwarding" {
		t.Fatal(state, err)
	}
	balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || balance != 400 || ledger != balance {
		t.Fatal(balance, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse", id + ":usage"}).Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	t.Logf("upstream_calls=%d killed_after_receive=true state=%s retry=%d refund_permit=%d balance=%d ledger=%d events=%d", calls.Load(), state, retry.Value, recover.Value, balance, ledger, events)
}
