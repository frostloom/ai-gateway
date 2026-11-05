package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// Demonstrates one specific pre-forward recovery path using the real Store.
// The gate is not wired into production and no provider call is made.
func TestSQLGateBillingRecovery(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	g := sqlIdemGate{db: db}
	if e := s.AutoMigrate(); e != nil {
		t.Fatal(e)
	}
	if e := g.migrate(); e != nil {
		t.Fatal(e)
	}
	id := fmt.Sprintf("gate-bill-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&model).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	req := &billingv1.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	if state, e := g.claim(ctx, id, "same-params", "old", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	first, e := s.Reserve(ctx, req)
	if e != nil || first.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatal(first, e)
	}
	// Simulate recovery after the owner disappears before forwarding.
	if e := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", id).Error; e != nil {
		t.Fatal(e)
	}
	if state, e := g.claim(ctx, id, "same-params", "new", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	if ok, e := g.beginForward(ctx, id, "old"); e != nil || ok {
		t.Fatalf("old owner accepted: %v %v", ok, e)
	}
	replay, e := s.Reserve(ctx, req)
	if e != nil || replay.BillId != first.BillId {
		t.Fatalf("reserve replay: %+v %v", replay, e)
	}
	if ok, e := g.beginForward(ctx, id, "new"); e != nil || !ok {
		t.Fatalf("new owner denied: %v %v", ok, e)
	}
	if e := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: id, BillId: first.BillId, PromptTokens: 50}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Reserve(ctx, req); status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("usage reported reserve retry accepted: %v", e)
	}
	done, e := s.Settle(ctx, &billingv1.SettleRequest{RequestId: id, BillId: first.BillId})
	if e != nil || !done.Settled {
		t.Fatal(done, e)
	}
	if _, e := s.Reserve(ctx, req); status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("settled reserve retry accepted: %v", e)
	}
	var reserves, settles, events int64
	if e := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&reserves).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settles).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; e != nil {
		t.Fatal(e)
	}
	balance, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil {
		t.Fatal(e)
	}
	if reserves != 1 || settles != 1 || events != 3 || balance != 950 || ledger != balance {
		t.Fatalf("reserves=%d settles=%d events=%d balance=%d ledger=%d", reserves, settles, events, balance, ledger)
	}
}

// Regression: a reversed reserve must not be treated as a new authorization.
func TestSQLGateBillingReversedReserveRejected(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	g := sqlIdemGate{db: db}
	if e := s.AutoMigrate(); e != nil {
		t.Fatal(e)
	}
	if e := g.migrate(); e != nil {
		t.Fatal(e)
	}
	id := fmt.Sprintf("gate-reverse-%d", time.Now().UnixNano())
	m := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&m).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	req := &billingv1.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	if state, e := g.claim(ctx, id, "same-params", "old", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	first, e := s.Reserve(ctx, req)
	if e != nil || first.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatal(first, e)
	}
	// Reconciliation can reverse an abandoned reserve before a new owner starts.
	reversed, e := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: id, BillId: first.BillId, Reason: "recon:no_usage"})
	if e != nil || !reversed.Reversed {
		t.Fatal(reversed, e)
	}
	if e := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", id).Error; e != nil {
		t.Fatal(e)
	}
	if state, e := g.claim(ctx, id, "same-params", "new", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	replay, e := s.Reserve(ctx, req)
	if status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("reversed replay must be rejected: %+v %v", replay, e)
	}
	// The unintegrated gate alone still admits forwarding; callers must stop
	// after the billing rejection. This is not a cross-system atomicity proof.
	var row store.Bill
	if e := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; e != nil {
		t.Fatal(e)
	}
	balance, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	if row.Status != store.StatusReversed || balance != 1000 {
		t.Fatalf("expected reversed reserve and restored balance: status=%s balance=%d", row.Status, balance)
	}
	err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: id, BillId: first.BillId, PromptTokens: 50})
	if err == nil {
		t.Fatal("expected usage against reversed reserve to fail")
	}
	t.Logf("reversed reserve retry rejected; status=%s; usage rejected (%v)", row.Status, err)
}
