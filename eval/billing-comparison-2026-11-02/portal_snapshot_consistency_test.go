package comparison

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type portalSnapshotTestKey struct{}

func TestPortalSnapshotDuringConcurrentSettle(t *testing.T) {
	if os.Getenv("TEST_PORTAL_SNAPSHOT_CONSISTENCY") != "1" {
		t.Skip("isolated snapshot candidate only")
	}
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("portal-snapshot-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: tag, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve=%+v err=%v", res, err)
	}
	if err := st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: tag, BillId: res.BillId, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var fired atomic.Bool
	if err := db.Callback().Row().After("gorm:row").Register("test:portal_pause", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" && tx.Statement.Context.Value(portalSnapshotTestKey{}) != nil && fired.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Row().Remove("test:portal_pause")
	type result struct {
		view *store.MeView
		err  error
	}
	done := make(chan result, 1)
	go func() {
		view, err := st.MeOverview(context.WithValue(ctx, portalSnapshotTestKey{}, true), tenant.ID)
		done <- result{view, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		<-done
		t.Fatal("snapshot did not reach pending sum")
	}
	settleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	settled, err := st.Settle(settleCtx, &pb.SettleRequest{RequestId: tag, BillId: res.BillId})
	cancel()
	close(release)
	resultValue := <-done
	if err != nil || settled == nil || !settled.Settled {
		t.Fatalf("settlement=%+v err=%v", settled, err)
	}
	if resultValue.err != nil || resultValue.view == nil || resultValue.view.Balance != 400 || resultValue.view.LedgerBalance != 400 || resultValue.view.Consistent {
		t.Fatalf("old snapshot=%+v err=%v", resultValue.view, resultValue.err)
	}
	var oldConsumed int64
	for _, day := range resultValue.view.Consumption {
		oldConsumed += day.Tokens
	}
	if oldConsumed != 0 {
		t.Fatalf("old snapshot consumption=%d", oldConsumed)
	}
	latest, err := st.MeOverview(ctx, tenant.ID)
	if err != nil || latest.Balance != 460 || latest.LedgerBalance != 460 {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	var newConsumed int64
	for _, day := range latest.Consumption {
		newConsumed += day.Tokens
	}
	if newConsumed != 40 {
		t.Fatalf("new consumption=%d", newConsumed)
	}
	t.Log("concurrent snapshot balance=400 consumption=0; after committed settlement balance=460 consumption=40")
}
