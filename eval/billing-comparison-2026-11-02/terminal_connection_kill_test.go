package comparison

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTerminalConnectionKilledAfterRedisDelta(t *testing.T) {
	if os.Getenv("TEST_TERMINAL_KILL") != "1" {
		t.Skip("isolated failure injection only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"settle", "reverse"} {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("terminal-kill-%s-%d", mode, time.Now().UnixNano())
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
			var killErr error
			callback := func(tx *gorm.DB) {
				if tx.Statement.Table != "outbox_events" || !fired.CompareAndSwap(false, true) {
					return
				}
				if os.Getenv("TEST_KILL_DROP_GUARD") == "1" {
					if killErr = r.Del(ctx, store.DeltaGuardKey(id)).Err(); killErr != nil {
						return
					}
				}
				var connection uint64
				if killErr = tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT CONNECTION_ID()").Scan(&connection).Error; killErr == nil {
					killErr = db.Exec(fmt.Sprintf("KILL CONNECTION %d", connection)).Error
				}
			}
			if err := db.Callback().Create().Before("gorm:create").Register("test:kill_terminal_connection", callback); err != nil {
				t.Fatal(err)
			}
			if mode == "settle" {
				_, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id})
			} else {
				_, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
			}
			db.Callback().Create().Remove("test:kill_terminal_connection")
			if !fired.Load() || killErr != nil || err == nil {
				t.Fatalf("fired=%v kill=%v terminal=%v", fired.Load(), killErr, err)
			}
			if os.Getenv("TEST_EXPECT_FRESH_RECOVERY") != "1" && !strings.Contains(err.Error(), "projection repair") {
				t.Fatalf("expected visible repair failure: %v", err)
			}
			live, e := s.GetBalance(ctx, tenant.ID)
			ledger, le := store.LedgerBalance(ctx, db, tenant.ID)
			if e != nil || le != nil || ledger != 400 {
				t.Fatalf("balance=%d ledger=%d err=%v/%v", live, ledger, e, le)
			}
			before := live
			if os.Getenv("TEST_EXPECT_FRESH_RECOVERY") == "1" && live != ledger {
				t.Fatalf("fresh recovery failed: live=%d ledger=%d", live, ledger)
			}
			var reserveRow store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&reserveRow).Error; err != nil || reserveRow.Status != store.StatusPending {
				t.Fatalf("pending bill=%s err=%v", reserveRow.Status, err)
			}
			var beforeEvents int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&beforeEvents).Error; err != nil || beforeEvents != 0 {
				t.Fatalf("before events=%d err=%v", beforeEvents, err)
			}
			if os.Getenv("TEST_EXPECT_FRESH_RECOVERY") == "1" {
				guard, err := r.Exists(ctx, store.DeltaGuardKey(id)).Result()
				if err != nil || guard != 0 {
					t.Fatalf("guard=%d err=%v", guard, err)
				}
			}
			if mode == "settle" {
				var result *pb.SettleResponse
				result, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id})
				if err != nil || !result.Settled {
					t.Fatal(result, err)
				}
			} else {
				var result *pb.ReverseResponse
				result, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
				if err != nil || !result.Reversed {
					t.Fatal(result, err)
				}
			}
			live, e = s.GetBalance(ctx, tenant.ID)
			ledger, le = store.LedgerBalance(ctx, db, tenant.ID)
			want := int64(500)
			if mode == "settle" {
				want = 460
			}
			if e != nil || le != nil || live != want || ledger != want {
				t.Fatalf("retry balance=%d ledger=%d err=%v/%v", live, ledger, e, le)
			}
			var events int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&events).Error; err != nil || events != 1 {
				t.Fatal(events, err)
			}
			t.Logf("kill=success mode=%s balance_before_retry=%d ledger_before_retry=400 balance_after=%d ledger_after=%d events=%d", mode, before, live, ledger, events)
		})
	}
}
