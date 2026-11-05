package comparison

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestStrictReserveCommitAckLoss(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("strict candidate only")
	}
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		t.Run(injected.Error(), func(t *testing.T) {
			db, r := isolatedDB(t)
			ctx := context.Background()
			normal := store.NewStore(db, r)
			if err := normal.AutoMigrate(); err != nil {
				t.Fatal(err)
			}
			id := fmt.Sprintf("strict-ack-%d", time.Now().UnixNano())
			tenant := store.Tenant{Name: id, InitialQuota: 500}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
				t.Fatal(err)
			}
			broken := store.NewStore(recoveryDBWithCommitError(t, injected), r)
			first, ok := any(broken).(experimentalAtomicReserve)
			if !ok {
				t.Fatal("candidate missing")
			}
			stable, ok := any(normal).(experimentalAtomicReserve)
			if !ok {
				t.Fatal("candidate missing")
			}
			hash := sha256.Sum256([]byte("synthetic-owner"))
			owner := hex.EncodeToString(hash[:])
			req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
			resp, err := first.ReserveWithExecutionIntent(ctx, req, owner, 30*time.Second)
			if err == nil {
				t.Fatalf("expected injected failure, got %+v", resp)
			}
			var row store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			replay, err := stable.ReserveWithExecutionIntent(ctx, req, owner, 30*time.Second)
			if err != nil || replay.Code != pb.ReserveCode_RESERVE_DUPLICATE || replay.BillId != int64(row.ID) {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			otherHash := sha256.Sum256([]byte("other-owner"))
			other, err := stable.ReserveWithExecutionIntent(ctx, req, hex.EncodeToString(otherHash[:]), 30*time.Second)
			if err != nil || other.Code != pb.ReserveCode_RESERVE_DUPLICATE || other.BillId != replay.BillId {
				t.Fatalf("other owner replay=%+v err=%v", other, err)
			}
			changed := *req
			changed.EstPromptTokens++
			if _, err := stable.ReserveWithExecutionIntent(ctx, &changed, owner, 30*time.Second); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("conflict err=%v", err)
			}
			gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
			denied, err := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|other-owner"))
			if err != nil || denied.Value != 0 {
				t.Fatalf("other permit=%v err=%v", denied, err)
			}
			type result struct {
				n   int64
				err error
			}
			start := make(chan struct{})
			results := make(chan result, 16)
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					out, e := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|synthetic-owner"))
					if e != nil {
						results <- result{err: e}
						return
					}
					results <- result{n: out.Value}
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			wins := int64(0)
			for res := range results {
				if res.err != nil || (res.n != 0 && res.n != 1) {
					t.Fatalf("permit result=%+v", res)
				}
				wins += res.n
			}
			if wins != 1 {
				t.Fatalf("concurrent permits=%d", wins)
			}
			var events int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
				t.Fatal(err)
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != 400 || ledger != live || events != 1 {
				t.Fatalf("live=%d ledger=%d events=%d err=%v", live, ledger, events, err)
			}
			t.Logf("injected=%v commit_visible=true retry=duplicate other_owner_duplicate=true other_permit=0 same_owner_contenders=16 granted=%d changed_params=rejected balance=%d ledger=%d events=%d", injected, wins, live, ledger, events)
		})
	}
}
