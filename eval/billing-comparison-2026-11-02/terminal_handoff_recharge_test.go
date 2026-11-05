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

func TestTerminalLockHandoffWithRecharge(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"settle", "reverse"} {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("handoff-recharge-%s-%d", mode, time.Now().UnixNano())
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
			order, err := s.CreateRechargeOrder(ctx, tenant.ID, 1, id+"-order")
			if err != nil {
				t.Fatal(err)
			}
			refundMode := os.Getenv("TEST_HANDOFF_REFUND") == "1"
			if refundMode {
				if _, err := s.PayRecharge(ctx, order.OrderNo); err != nil {
					t.Fatal(err)
				}
			}
			reserve, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
			if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(reserve, err)
			}
			if mode == "settle" {
				if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
					t.Fatal(err)
				}
			}
			var fired atomic.Bool
			var injectionErr error
			var paid *store.RechargeOrder
			var refund *store.RefundResult
			if err := db.Callback().Create().Before("gorm:create").Register("test:handoff_recharge", func(tx *gorm.DB) {
				if tx.Statement.Table != "outbox_events" || !fired.CompareAndSwap(false, true) {
					return
				}
				if injectionErr = r.Del(ctx, store.DeltaGuardKey(id)).Err(); injectionErr != nil {
					return
				}
				var conn uint64
				if injectionErr = tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT CONNECTION_ID()").Scan(&conn).Error; injectionErr != nil {
					return
				}
				if injectionErr = db.Exec(fmt.Sprintf("KILL CONNECTION %d", conn)).Error; injectionErr != nil {
					return
				}
				if refundMode {
					refund, injectionErr = s.RefundRecharge(ctx, tenant.ID, order.OrderNo)
				} else {
					paid, injectionErr = s.PayRecharge(ctx, order.OrderNo)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if mode == "settle" {
				_, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id})
			} else {
				_, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
			}
			if e := db.Callback().Create().Remove("test:handoff_recharge"); e != nil {
				t.Fatal(e)
			}
			validCash := (!refundMode && paid != nil && paid.Status == store.RechargePaid) || (refundMode && refund != nil && refund.RefundCent == 100)
			if err == nil || !fired.Load() || injectionErr != nil || !validCash {
				t.Fatalf("terminal=%v fired=%v paid=%v refund=%v error=%v", err, fired.Load(), paid, refund, injectionErr)
			}
			live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if e != nil {
				t.Fatal(e)
			}
			ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
			wantRecovery := int64(500)
			if refundMode {
				wantRecovery = 400
			}
			if e != nil || live != wantRecovery || ledger != wantRecovery {
				t.Fatalf("handoff live=%d ledger=%d err=%v", live, ledger, e)
			}
			if mode == "settle" {
				v, e := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
				if e != nil || !v.Settled {
					t.Fatal(v, e)
				}
			} else {
				v, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
				if e != nil || !v.Reversed {
					t.Fatal(v, e)
				}
			}
			live, e = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if e != nil {
				t.Fatal(e)
			}
			ledger, e = store.LedgerBalance(ctx, db, tenant.ID)
			want := int64(600)
			if mode == "settle" {
				want = 560
			}
			if refundMode {
				want -= 100
			}
			if e != nil || live != want || ledger != want {
				t.Fatalf("retry live=%d ledger=%d want=%d err=%v", live, ledger, want, e)
			}
			var topups, events int64
			wantTopups := int64(1)
			if refundMode {
				wantTopups = 2
			}
			if e := db.Model(&store.Topup{}).Where("tenant_id=? AND ref_id=?", tenant.ID, order.OrderNo).Count(&topups).Error; e != nil || topups != wantTopups {
				t.Fatalf("topups=%d err=%v", topups, e)
			}
			if e := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&events).Error; e != nil || events != 1 {
				t.Fatalf("events=%d err=%v", events, e)
			}
			t.Logf("mode=%s killed=true refund_mode=%t cash_operation_before_recovery=true recovery_balance=%d retry_balance=%d ledger=%d topups=%d terminal_events=%d", mode, refundMode, wantRecovery, live, ledger, topups, events)
		})
	}
}
