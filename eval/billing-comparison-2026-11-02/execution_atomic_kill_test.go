package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAtomicReserveKilledConnectionAndReplay(t *testing.T) {
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
		t.Fatal("atomic intent interface absent")
	}
	id := fmt.Sprintf("atomic-kill-%d", time.Now().UnixNano())
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
	oldDigest := sha256.Sum256([]byte("old-owner"))
	newDigest := sha256.Sum256([]byte("new-owner"))
	oldHash := hex.EncodeToString(oldDigest[:])
	newHash := hex.EncodeToString(newDigest[:])
	entered := make(chan error, 1)
	resume := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	var calls atomic.Int32
	if err := db.Callback().Update().Before("gorm:update").Register("test:kill_execution_owner", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if !ok || values["execution_owner_hash"] == nil || !calls.CompareAndSwap(0, 1) {
			return
		}
		var connection uint64
		if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&connection); err != nil {
			tx.AddError(err)
			entered <- err
			return
		}
		if err := db.Exec(fmt.Sprintf("KILL CONNECTION %d", connection)).Error; err != nil {
			tx.AddError(err)
			entered <- err
			return
		}
		entered <- nil
		<-resume
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Update().Remove("test:kill_execution_owner") })
	first := make(chan error, 1)
	go func() {
		_, err := candidate.ReserveWithExecutionIntent(ctx, req, oldHash, 30*time.Second)
		first <- err
	}()
	select {
	case err := <-entered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first owner update was not reached")
	}
	second, err := candidate.ReserveWithExecutionIntent(ctx, req, newHash, 30*time.Second)
	release()
	if err != nil || second.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(second, err)
	}
	select {
	case err := <-first:
		if err == nil {
			t.Fatal("killed first reserve returned success")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("first reserve did not finish")
	}
	var state struct {
		ExecutionState, ExecutionOwnerHash string
		LeaseLive                          bool
	}
	if err := db.Raw("SELECT execution_state,execution_owner_hash,execution_lease_until>CURRENT_TIMESTAMP(6) AS lease_live FROM bills WHERE id=?", second.BillId).Scan(&state).Error; err != nil || state.ExecutionState != "reserved" || state.ExecutionOwnerHash != newHash || !state.LeaseLive {
		t.Fatal(state, err)
	}
	service := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
	oldPermit, err := service.Transition(ctx, wrapperspb.String(id+"|forwarding|old-owner"))
	if err != nil || oldPermit.Value != 0 {
		t.Fatal(oldPermit, err)
	}
	newPermit, err := service.Transition(ctx, wrapperspb.String(id+"|forwarding|new-owner"))
	if err != nil || newPermit.Value != 1 {
		t.Fatal(newPermit, err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || bills != 1 || events != 1 {
		t.Fatal(bills, events, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 400 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("connection_killed=%d replacement_ok=true old_permit=0 new_permit=1 balance=%d ledger=%d bills=%d events=%d", calls.Load(), live, ledger, bills, events)
}
