package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestLargeBalanceMustNotOverspendOnLuaRounding(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("lua-precision-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 9007199254740992}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 0, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 7505999378950828}
	response, err := s.ReserveWithReplaySignal(ctx, req)
	if status.Code(err) != codes.Unavailable || response != nil {
		t.Fatalf("out of safe range must fail closed: response=%v err=%v", response, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if live != tenant.InitialQuota || ledger != live || n != 0 {
		t.Fatalf("redis=%d ledger=%d bills=%d", live, ledger, n)
	}
	t.Logf("out-of-range operation safely rejected, balance=%d ledger=%d", live, ledger)
}
