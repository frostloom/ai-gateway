package comparison

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

// Characterization: PASS means the deliberately corrupted state was accepted.
func TestActualStoreOrphanOutboxConflictCharacterization(t *testing.T) {
	if os.Getenv("TEST_ACTUAL_ORPHAN_OUTBOX") != "1" {
		t.Skip("requires disposable database and Redis")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("actual-orphan-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.OutboxEvent{Topic: "test-orphan", EventKey: id + ":reserve", Payload: `{"synthetic":"conflict"}`, CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if os.Getenv("TEST_EXPECT_OUTBOX_REJECT") == "1" {
		if err == nil {
			t.Fatalf("conflicting outbox must reject reservation: result=%+v", res)
		}
		var bills, events int64
		if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
			t.Fatal(err)
		}
		live, e1 := s.GetBalance(ctx, tenant.ID)
		ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
		if e1 != nil || e2 != nil || bills != 0 || events != 1 || live != 500 || ledger != 500 {
			t.Fatalf("bills=%d events=%d live=%d ledger=%d errors=%v/%v", bills, events, live, ledger, e1, e2)
		}
		t.Logf("reserve_rejected=true bills=%d existing_events=%d balance=%d ledger=%d", bills, events, live, ledger)
		return
	}
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve=%+v err=%v", res, err)
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil {
		t.Fatal(err)
	}
	var events []store.OutboxEvent
	if err := db.Where("event_key=?", id+":reserve").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, err := s.GetBalance(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Topic != "test-orphan" || bill.PreQuota != 100 || live != 400 || ledger != 400 {
		t.Fatalf("bill=%+v events=%+v live=%d ledger=%d", bill, events, live, ledger)
	}
	t.Logf("unsafe_state_reproduced=true reserve=ok bill_pre=%d existing_topic=%s balance=%d ledger=%d", bill.PreQuota, events[0].Topic, live, ledger)
}

func TestActualStoreOutboxNormalReplay(t *testing.T) {
	if os.Getenv("TEST_ACTUAL_ORPHAN_OUTBOX") != "1" {
		t.Skip("requires disposable services")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("outbox-replay-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	first, err := s.Reserve(ctx, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	replay, err := s.Reserve(ctx, req)
	if err != nil || replay.BillId != first.BillId {
		t.Fatal(replay, err)
	}
	for i := 0; i < 2; i++ {
		if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		result, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
		if err != nil || !result.Settled {
			t.Fatal(result, err)
		}
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, e1 := s.GetBalance(ctx, tenant.ID)
	ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
	if events != 3 || live != 460 || ledger != 460 || e1 != nil || e2 != nil {
		t.Fatalf("events=%d live=%d ledger=%d err=%v/%v", events, live, ledger, e1, e2)
	}
	t.Logf("normal_replays=true events=%d balance=%d ledger=%d", events, live, ledger)
}

func TestActualStoreTerminalOutboxConflicts(t *testing.T) {
	if os.Getenv("TEST_ACTUAL_ORPHAN_OUTBOX") != "1" || os.Getenv("TEST_EXPECT_OUTBOX_REJECT") != "1" {
		t.Skip("requires isolated safety test")
	}
	for _, phase := range []string{"usage", "settle", "reverse"} {
		t.Run(phase, func(t *testing.T) {
			db, r := isolatedDB(t)
			db.Logger = db.Logger.LogMode(logger.Silent)
			s, ctx := store.NewStore(db, r), context.Background()
			if err := s.AutoMigrate(); err != nil {
				t.Fatal(err)
			}
			id := fmt.Sprintf("outbox-%s-%d", phase, time.Now().UnixNano())
			tenant := store.Tenant{Name: id, InitialQuota: 500}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
				t.Fatal(err)
			}
			res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
			if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(res, err)
			}
			usage := &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}
			if phase == "settle" {
				if err := s.ReportUsage(ctx, usage); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Create(&store.OutboxEvent{Topic: "test-orphan", EventKey: id + ":" + phase, Payload: `{"synthetic":"conflict"}`, CreatedAt: time.Now()}).Error; err != nil {
				t.Fatal(err)
			}
			invoke := func() error {
				switch phase {
				case "usage":
					return s.ReportUsage(ctx, usage)
				case "settle":
					_, e := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
					return e
				default:
					_, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
					return e
				}
			}
			if err := invoke(); err == nil {
				t.Fatalf("phase=%s conflicting outbox must reject", phase)
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil {
				t.Fatal(err)
			}
			var settled int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settled).Error; err != nil {
				t.Fatal(err)
			}
			live, e1 := s.GetBalance(ctx, tenant.ID)
			ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
			if bill.Status != store.StatusPending || settled != 0 || live != 400 || ledger != 400 || e1 != nil || e2 != nil || (bill.UsageReportedAt != nil) != (phase == "settle") {
				t.Fatalf("phase=%s status=%s usage=%v settled=%d live=%d ledger=%d err=%v/%v", phase, bill.Status, bill.UsageReportedAt, settled, live, ledger, e1, e2)
			}
			// Remove only the artificial test corruption, then retry the same operation.
			if err := db.Where("event_key=?", id+":"+phase).Delete(&store.OutboxEvent{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := invoke(); err != nil {
				t.Fatalf("retry %s: %v", phase, err)
			}
			var event store.OutboxEvent
			if err := db.Where("event_key=?", id+":"+phase).First(&event).Error; err != nil || event.Topic != store.TopicBillingEvents {
				t.Fatalf("event=%+v err=%v", event, err)
			}
			live, e1 = s.GetBalance(ctx, tenant.ID)
			ledger, e2 = store.LedgerBalance(ctx, db, tenant.ID)
			want := int64(400)
			if phase == "settle" {
				want = 460
			}
			if phase == "reverse" {
				want = 500
			}
			if live != want || ledger != want || e1 != nil || e2 != nil {
				t.Fatalf("retry phase=%s live=%d ledger=%d expected=%d err=%v/%v", phase, live, ledger, want, e1, e2)
			}
			t.Logf("phase=%s rejected_then_retry=true prior_balance=400 final_balance=%d ledger=%d", phase, live, ledger)
		})
	}
}
