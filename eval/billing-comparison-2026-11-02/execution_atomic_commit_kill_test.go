package comparison

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm/logger"
)

func TestAtomicCommitOwnerChild(t *testing.T) {
	if os.Getenv("EXEC_COMMIT_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, r := isolatedDB(t)
	s := store.NewStore(db, r)
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate missing")
	}
	tenant, err := strconv.ParseInt(os.Getenv("EXEC_COMMIT_TENANT"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	id := os.Getenv("EXEC_COMMIT_ID")
	digest := sha256.Sum256([]byte("old-owner"))
	hash := hex.EncodeToString(digest[:])
	resp, err := candidate.ReserveWithExecutionIntent(context.Background(), &pb.ReserveRequest{RequestId: id, TenantId: tenant, ApiKeyId: 1, Model: id, EstPromptTokens: 84}, hash, 30*time.Second)
	if err != nil || resp.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(resp, err)
	}
	fmt.Println("EXEC_COMMIT_CHILD_READY")
	time.Sleep(time.Minute)
}
func TestAtomicCommitOwnerDiesBeforeSend(t *testing.T) {
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
	id := fmt.Sprintf("atomic-commit-kill-%d", time.Now().UnixNano())
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
	cmd := exec.Command(os.Args[0], "-test.run=^TestAtomicCommitOwnerChild$")
	cmd.Env = append(os.Environ(), "EXEC_COMMIT_CHILD=1", "EXEC_COMMIT_ID="+id, fmt.Sprintf("EXEC_COMMIT_TENANT=%d", tenant.ID))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "EXEC_COMMIT_CHILD_READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("child exited before commit marker")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child timeout")
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	_ = cmd.Wait()
	digest := sha256.Sum256([]byte("new-owner"))
	hash := hex.EncodeToString(digest[:])
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	replay, err := candidate.ReserveWithExecutionIntent(ctx, req, hash, 30*time.Second)
	if err != nil || replay.Code != pb.ReserveCode_RESERVE_DUPLICATE {
		t.Fatal(replay, err)
	}
	gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
	newPermit, err := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|new-owner"))
	if err != nil || newPermit.Value != 0 {
		t.Fatal(newPermit, err)
	}
	early, err := gate.Transition(ctx, wrapperspb.String(id+"|reversing|synthetic-recovery"))
	if err != nil || early.Value != 0 {
		t.Fatal(early, err)
	}
	if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=? AND phase=?", id, store.PhaseReserve).Error; err != nil {
		t.Fatal(err)
	}
	recovery, err := gate.Transition(ctx, wrapperspb.String(id+"|reversing|synthetic-recovery"))
	if err != nil || recovery.Value != 1 {
		t.Fatal(recovery, err)
	}
	old, err := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|old-owner"))
	if err != nil || old.Value != 0 {
		t.Fatal(old, err)
	}
	reversed, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(replay.BillId)})
	if err != nil || !reversed.Reversed {
		t.Fatal(reversed, err)
	}
	bal, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || bal != 500 || ledger != bal {
		t.Fatal(bal, ledger, err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse"}).Count(&events).Error; err != nil || bills != 1 || events != 2 {
		t.Fatal(bills, events, err)
	}
	t.Logf("child_killed_after_commit=true replay=%s new_permit=%d early_refund=%d after_expiry_refund=%d old_permit=%d balance=%d ledger=%d bills=%d events=%d", replay.Code, newPermit.Value, early.Value, recovery.Value, old.Value, bal, ledger, bills, events)
}
