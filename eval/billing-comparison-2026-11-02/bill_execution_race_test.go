package comparison

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// Experimental CAS only; not an authenticated gateway protocol.
func TestBillExecutionPermissionRace(t *testing.T) {
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
	base := fmt.Sprintf("execution-race-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: base, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("%s-%d", base, i)
		tenant := store.Tenant{Name: id, InitialQuota: 500}
		if err := db.Create(&tenant).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
			t.Fatal(err)
		}
		bill, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: base, EstPromptTokens: 84})
		if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(bill, err)
		}
		var state string
		if err := db.Raw("SELECT execution_state FROM bills WHERE id=?", bill.BillId).Scan(&state).Error; err != nil || state != "reserved" {
			t.Fatal(state, err)
		}
		type transition struct {
			state string
			won   bool
			err   error
		}
		results := make(chan transition, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, next := range []string{"forwarding", "reversing"} {
			wg.Add(1)
			go func(next string) {
				defer wg.Done()
				<-start
				q := db.Exec("UPDATE bills SET execution_state=? WHERE id=? AND phase=? AND status=? AND execution_state='reserved'", next, bill.BillId, store.PhaseReserve, store.StatusPending)
				results <- transition{next, q.RowsAffected == 1, q.Error}
			}(next)
		}
		close(start)
		wg.Wait()
		close(results)
		winner := ""
		for res := range results {
			if res.err != nil {
				t.Fatal(res.err)
			}
			if res.won {
				if winner != "" {
					t.Fatal("both transitions won")
				}
				winner = res.state
			}
		}
		if winner == "" {
			t.Fatal("neither won")
		}
		counts[winner]++
		rev := &pb.ReverseRequest{RequestId: id, BillId: int64(bill.BillId), Reason: "synthetic"}
		if winner == "forwarding" {
			if _, err := s.Reverse(ctx, rev); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("forwarding reverse: %v", err)
			}
			if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
				t.Fatal(err)
			}
			result, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: int64(bill.BillId)})
			if err != nil || !result.Settled {
				t.Fatal(result, err)
			}
		} else {
			result, err := s.Reverse(ctx, rev)
			if err != nil || !result.Reversed {
				t.Fatal(result, err)
			}
			again, err := s.Reverse(ctx, rev)
			if err != nil || !again.Reversed {
				t.Fatal(again, err)
			}
			q := db.Exec("UPDATE bills SET execution_state='forwarding' WHERE id=? AND phase=? AND status=? AND execution_state='reserved'", bill.BillId, store.PhaseReserve, store.StatusPending)
			if q.Error != nil || q.RowsAffected != 0 {
				t.Fatal(q.RowsAffected, q.Error)
			}
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || ledger != live {
			t.Fatal(live, ledger, err)
		}
		expected := int64(460)
		keys := []string{id + ":reserve", id + ":usage", id + ":settle"}
		if winner == "reversing" {
			expected = 500
			keys = []string{id + ":reserve", id + ":reverse"}
		}
		var events int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", keys).Count(&events).Error; err != nil || live != expected || events != int64(len(keys)) {
			t.Fatal(live, events, err)
		}
	}
	t.Logf("forwarding=%d reversing=%d per_request_checks=12", counts["forwarding"], counts["reversing"])
}
