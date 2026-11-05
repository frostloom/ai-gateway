package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func TestPortalSnapshotSubscriptionRechargeFields(t *testing.T) {
	if os.Getenv("TEST_PORTAL_SNAPSHOT_BUSINESS") != "1" {
		t.Skip("isolated business fields")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("portal-business-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	order, err := st.CreateRechargeOrder(ctx, tenant.ID, 1, tag+"-topup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PayRecharge(ctx, order.OrderNo); err != nil {
		t.Fatal(err)
	}
	plan := store.Plan{Name: tag, PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":300}`}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := st.SubscribePlan(ctx, tenant.ID, plan.ID); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `["文本"]`}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	res, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: tag + "-usage", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 100})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve=%+v err=%v", res, err)
	}
	if err := st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: tag + "-usage", BillId: res.BillId, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	check := func(status string) {
		t.Helper()
		view, err := st.MeOverview(ctx, tenant.ID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Balance != 1100 || view.LedgerBalance != 1100 || view.Subscription == nil || view.Subscription.Status != status || view.RefreshHours != 5 {
			t.Fatalf("view=%+v", view)
		}
		if len(view.Allowance) != 1 || view.Allowance[0].Tier != "文本" || view.Allowance[0].Quota != 300 || view.Allowance[0].Consumed != 40 || view.Allowance[0].Remaining != 260 {
			t.Fatalf("allowance=%+v", view.Allowance)
		}
		if len(view.Topups) != 1 || view.Topups[0].Amount != 100 {
			t.Fatalf("topups=%+v", view.Topups)
		}
		data, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var projection map[string]any
		if err := json.Unmarshal(data, &projection); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("TEST_PORTAL_MYSQL_SNAPSHOT") == "1" {
			if projection["projection_checked"] != false || view.Consistent {
				t.Fatalf("candidate projection=%s", data)
			}
		} else if !view.Consistent {
			t.Fatalf("baseline projection=%s", data)
		}
	}
	check(store.SubActive)
	if _, err := st.CancelSubscription(ctx, tenant.ID); err != nil {
		t.Fatal(err)
	}
	check(store.SubCancelled)
	t.Log("recharge=100 balance=1100, active/cancelled subscription entitlement remains 300-40=260")
}
