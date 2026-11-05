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

func TestActualSubscriptionOutboxConflicts(t *testing.T) {
	if os.Getenv("TEST_EXPECT_OUTBOX_REJECT") != "1" {
		t.Skip("isolated safety test only")
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		t.Run(phase, func(t *testing.T) {
			db, r := isolatedDB(t)
			db.Logger = db.Logger.LogMode(logger.Silent)
			s, ctx := store.NewStore(db, r), context.Background()
			if err := s.AutoMigrate(); err != nil {
				t.Fatal(err)
			}
			id := fmt.Sprintf("sub-ob-%s-%d", phase, time.Now().UnixNano())
			tenant := store.Tenant{Name: id, InitialQuota: 100000}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
				t.Fatal(err)
			}
			plan := store.Plan{Name: id, PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":300}`}
			if err := db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.SubscribePlan(ctx, tenant.ID, plan.ID); err != nil {
				t.Fatal(err)
			}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `["文本"]`}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			before, e := s.GetBalance(ctx, tenant.ID)
			if e != nil {
				t.Fatal(e)
			}
			req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 100}
			usage := &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}
			if phase != "reserve" {
				res, err := s.Reserve(ctx, req)
				if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
					t.Fatal(res, err)
				}
			}
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
				case "reserve":
					_, e := s.Reserve(ctx, req)
					return e
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
				t.Fatalf("subscription %s must reject orphan event", phase)
			}
			var bills []store.Bill
			if err := db.Where("request_id=?", id).Find(&bills).Error; err != nil {
				t.Fatal(err)
			}
			if phase == "reserve" {
				if len(bills) != 0 {
					t.Fatal("reserve must roll back", bills)
				}
			} else {
				if len(bills) != 1 || bills[0].Status != store.StatusPending || bills[0].Funding != store.FundingSubscription || (bills[0].UsageReportedAt != nil) != (phase == "settle") {
					t.Fatalf("phase=%s bills=%+v", phase, bills)
				}
			}
			live, e1 := s.GetBalance(ctx, tenant.ID)
			ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
			if e1 != nil || e2 != nil || live != before || ledger != before {
				t.Fatal(live, ledger, before, e1, e2)
			}
			if err := db.Where("event_key=?", id+":"+phase).Delete(&store.OutboxEvent{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := invoke(); err != nil {
				t.Fatalf("retry %s: %v", phase, err)
			}
			var after store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&after).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus := store.StatusPending
			if phase == "settle" {
				wantStatus = store.StatusSettled
			}
			if phase == "reverse" {
				wantStatus = store.StatusReversed
			}
			if after.Status != wantStatus || after.Funding != store.FundingSubscription || (after.UsageReportedAt != nil) != (phase == "usage" || phase == "settle") {
				t.Fatalf("phase=%s after=%+v", phase, after)
			}
			probeAmount, probeCode := int64(261), pb.ReserveCode_RESERVE_SUB_QUOTA
			if phase == "reserve" {
				probeAmount = 201
			}
			if phase == "reverse" {
				probeAmount, probeCode = 300, pb.ReserveCode_RESERVE_OK
			}
			probe, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id + "-probe", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: probeAmount})
			if err != nil || probe.Code != probeCode {
				t.Fatalf("phase=%s quota probe=%+v err=%v", phase, probe, err)
			}
			var ev store.OutboxEvent
			if err := db.Where("event_key=?", id+":"+phase).First(&ev).Error; err != nil || ev.Topic != store.TopicBillingEvents {
				t.Fatal(ev.Topic, err)
			}
			live, e1 = s.GetBalance(ctx, tenant.ID)
			ledger, e2 = store.LedgerBalance(ctx, db, tenant.ID)
			if e1 != nil || e2 != nil || live != before || ledger != before {
				t.Fatal(live, ledger, before, e1, e2)
			}
			t.Logf("phase=%s rejected_then_retry=true cash=%d ledger=%d", phase, live, ledger)
		})
	}
}
