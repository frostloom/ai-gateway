package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestSettlementLostReplyThenRedisRestart(t *testing.T) {
	container := os.Getenv("COMPARISON_OWNED_REDIS_CONTAINER")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") {
		t.Fatal("isolated Redis container required")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                         string
		initial, usage, want, actual int64
		code                         string
	}{{"refund", 1000, 40, 960, 40, ""}, {"extra", 1000, 150, 850, 150, ""}, {"shortfall", 150, 250, 0, 150, "shortfall:100"}} {
		t.Run(tc.name, func(t *testing.T) {
			id := fmt.Sprintf("settle-restart-%s-%d", tc.name, time.Now().UnixNano())
			model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			tenant := store.Tenant{Name: id, InitialQuota: tc.initial}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), tc.initial, 0).Err(); err != nil {
				t.Fatal(err)
			}
			reserve, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
			if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(reserve, err)
			}
			if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: tc.usage}); err != nil {
				t.Fatal(err)
			}
			hook := &lostReserveReply{guard: store.DeltaGuardKey(id)}
			r.AddHook(hook)
			req := &pb.SettleRequest{RequestId: id, BillId: int64(reserve.BillId)}
			if _, err := s.Settle(ctx, req); status.Code(err) != codes.Unavailable || hook.hits.Load() != 1 {
				t.Fatal(err, hook.hits.Load())
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil || live != tc.want {
				t.Fatal(live, err)
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt == nil {
				t.Fatal(bill.Status, err)
			}
			var count int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&count).Error; err != nil || count != 0 {
				t.Fatal(count, err)
			}
			if out, err := exec.Command("docker", "kill", container).CombinedOutput(); err != nil {
				t.Fatalf("kill: %v %s", err, out)
			}
			t.Cleanup(func() { _ = exec.Command("docker", "start", container).Run() })
			if out, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
				t.Fatalf("start: %v %s", err, out)
			}
			ready := false
			for i := 0; i < 30; i++ {
				if exec.Command("docker", "exec", container, "redis-cli", "ping").Run() == nil {
					ready = true
					break
				}
				time.Sleep(time.Second)
			}
			if !ready {
				t.Fatal("Redis not ready")
			}
			exists, err := r.Exists(ctx, store.BalanceKey(tenant.ID), store.DeltaGuardKey(id)).Result()
			if err != nil || exists != 0 {
				t.Fatal(exists, err)
			}
			wantBalance := tc.want
			if os.Getenv("TEST_RESTART_CONCURRENT") == "1" {
				start := make(chan struct{})
				results := make(chan error, 13)
				for i := 0; i < 12; i++ {
					go func() {
						<-start
						response, e := s.Settle(ctx, req)
						if e != nil {
							results <- e
							return
						}
						if !response.Settled {
							results <- fmt.Errorf("settlement returned false")
							return
						}
						results <- nil
					}()
				}
				otherID := id + "-other"
				otherResult := make(chan *pb.ReserveResponse, 1)
				hasOther := tc.name != "shortfall"
				if hasOther {
					go func() {
						<-start
						response, e := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: otherID, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
						if e != nil {
							results <- e
							return
						}
						if response.Code != pb.ReserveCode_RESERVE_OK {
							results <- fmt.Errorf("other reserve: %s", response.Code)
							return
						}
						otherResult <- response
						results <- nil
					}()
				}
				close(start)
				n := 12
				if hasOther {
					n++
				}
				for i := 0; i < n; i++ {
					if e := <-results; e != nil {
						t.Error(e)
					}
				}
				if t.Failed() {
					return
				}
				if hasOther {
					other := <-otherResult
					if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: otherID, PromptTokens: 20}); err != nil {
						t.Fatal(err)
					}
					if response, e := s.Settle(ctx, &pb.SettleRequest{RequestId: otherID, BillId: int64(other.BillId)}); e != nil || !response.Settled {
						t.Fatal(response, e)
					}
					var otherBill store.Bill
					if e := db.Where("request_id=? AND phase=?", otherID, store.PhaseSettle).Take(&otherBill).Error; e != nil || otherBill.ActualQuota == nil || *otherBill.ActualQuota != 20 {
						t.Fatalf("other actual=%v err=%v", otherBill.ActualQuota, e)
					}
					var otherCount, otherEvents int64
					if e := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", otherID, store.PhaseSettle).Count(&otherCount).Error; e != nil || otherCount != 1 {
						t.Fatal(otherCount, e)
					}
					if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{otherID + ":reserve", otherID + ":usage", otherID + ":settle"}).Count(&otherEvents).Error; e != nil || otherEvents != 3 {
						t.Fatal(otherEvents, e)
					}
					wantBalance -= 20
				}
				t.Logf("concurrent_settlements=12 other_reservation=%v", hasOther)
			} else {
				done, err := s.Settle(ctx, req)
				if err != nil || !done.Settled {
					t.Fatal(done, err)
				}
			}
			again, err := s.Settle(ctx, req)
			if err != nil || !again.Settled {
				t.Fatal(again, err)
			}
			live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != wantBalance || ledger != live {
				t.Fatal(live, ledger, err)
			}
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&count).Error; err != nil || count != 1 {
				t.Fatal(count, err)
			}
			var settled store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseSettle).Take(&settled).Error; err != nil || settled.ActualQuota == nil || *settled.ActualQuota != tc.actual || settled.ErrorCode != tc.code {
				t.Fatalf("settlement actual=%v code=%q err=%v", settled.ActualQuota, settled.ErrorCode, err)
			}
			var events int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil || events != 3 {
				t.Fatal(events, err)
			}
			t.Logf("lua_applied=%d pending_before_restart=true keys_after_restart=%d settled=true repeat=true balance=%d ledger=%d settle_rows=%d events=%d", hook.hits.Load(), exists, live, ledger, count, events)
		})
	}
}
