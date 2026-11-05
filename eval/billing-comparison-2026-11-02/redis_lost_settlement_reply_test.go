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

func TestSettlementRedisAppliedReplyLostThenRetry(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("settle-lost-reply-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	reserve, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	hook := &lostReserveReply{guard: store.DeltaGuardKey(id)}
	r.AddHook(hook)
	settleReq := &pb.SettleRequest{RequestId: id, BillId: int64(reserve.BillId)}
	_, err = s.Settle(ctx, settleReq)
	if status.Code(err) != codes.Unavailable || hook.hits.Load() != 1 {
		t.Fatal(err, hook.hits.Load())
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 960 {
		t.Fatal(live, err)
	}
	var reserveRow store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&reserveRow).Error; err != nil || reserveRow.Status != store.StatusPending || reserveRow.UsageReportedAt == nil {
		t.Fatal(reserveRow.Status, err)
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	done, err := s.Settle(ctx, settleReq)
	if err != nil || !done.Settled {
		t.Fatal(done, err)
	}
	repeat, err := s.Settle(ctx, settleReq)
	if err != nil || !repeat.Settled {
		t.Fatal(repeat, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatal(events, err)
	}
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	t.Logf("lost_balance=960 pending_with_usage=true retry_settled=%v repeat_settled=%v balance=%d ledger=%d settle_rows=%d events=%d", done.Settled, repeat.Settled, live, ledger, count, events)
}

func TestSettlementExtraDebitLostReply(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                                    string
		initial, usage, wantBalance, wantActual int64
		errorCode                               string
	}{{"fully-collected", 1000, 150, 850, 150, ""}, {"shortfall", 150, 250, 0, 150, "shortfall:100"}} {
		t.Run(tc.name, func(t *testing.T) {
			id := fmt.Sprintf("settle-lost-debit-%s-%d", tc.name, time.Now().UnixNano())
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
			if err != nil || live != tc.wantBalance {
				t.Fatal(live, err)
			}
			done, err := s.Settle(ctx, req)
			if err != nil || !done.Settled {
				t.Fatal(done, err)
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
			if err != nil || live != tc.wantBalance || ledger != live {
				t.Fatal(live, ledger, err)
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseSettle).First(&bill).Error; err != nil || bill.ActualQuota == nil || *bill.ActualQuota != tc.wantActual || bill.ErrorCode != tc.errorCode {
				t.Fatalf("bill=%+v err=%v", bill, err)
			}
			var bills, events int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&bills).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil || bills != 1 || events != 3 {
				t.Fatal(bills, events, err)
			}
			t.Logf("balance=%d ledger=%d actual=%d error_code=%q settle_rows=%d events=%d", live, ledger, *bill.ActualQuota, bill.ErrorCode, bills, events)
		})
	}
}
