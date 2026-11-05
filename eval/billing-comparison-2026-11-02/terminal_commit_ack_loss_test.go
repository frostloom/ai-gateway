package comparison

import (
	"context"
	"database/sql/driver"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTerminalCommitAcknowledgementLoss(t *testing.T) {
	if os.Getenv("TEST_TERMINAL_ACK_LOSS") != "1" {
		t.Skip("requires isolated MySQL")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"settle", "reverse"} {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("terminal-ack-%s-%d", mode, time.Now().UnixNano())
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
			// Real driver commits first, then this test wrapper returns an artificial error.
			ackDB := recoveryDBWithLostCommitAck(t)
			if os.Getenv("TEST_ACK_BAD_CONNECTION") == "1" {
				ackDB = recoveryDBWithCommitError(t, driver.ErrBadConn)
			}
			var guardDeleted bool
			if os.Getenv("TEST_ACK_DROP_GUARD") == "1" {
				if err := ackDB.Callback().Create().After("gorm:create").Register("test:drop_ack_guard", func(tx *gorm.DB) {
					if tx.Statement.Table == "outbox_events" {
						n, e := r.Del(ctx, store.DeltaGuardKey(id)).Result()
						if e != nil {
							tx.AddError(e)
						} else {
							guardDeleted = n == 1
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			unreliable := store.NewStore(ackDB, r)
			if mode == "settle" {
				_, err = unreliable.Settle(ctx, &pb.SettleRequest{RequestId: id})
			} else {
				_, err = unreliable.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
			}
			if status.Code(err) != codes.Unavailable {
				t.Fatalf("expected unknown result error, got %v", err)
			}
			if os.Getenv("TEST_ACK_DROP_GUARD") == "1" && !guardDeleted {
				t.Fatal("guard deletion did not happen")
			}
			wantError := errCommitAckLost.Error()
			if os.Getenv("TEST_ACK_BAD_CONNECTION") == "1" {
				wantError = driver.ErrBadConn.Error()
			}
			if !strings.Contains(err.Error(), wantError) {
				t.Fatalf("missing injected error: %v", err)
			}
			guard, guardErr := r.Exists(ctx, store.DeltaGuardKey(id)).Result()
			if guardErr != nil || guard != 0 {
				t.Fatalf("guard=%d err=%v", guard, guardErr)
			}
			var row store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus := store.StatusSettled
			wantBalance := int64(460)
			if mode == "reverse" {
				wantStatus = store.StatusReversed
				wantBalance = 500
			}
			live, e := s.GetBalance(ctx, tenant.ID)
			ledger, le := store.LedgerBalance(ctx, db, tenant.ID)
			var events int64
			eventErr := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&events).Error
			if row.Status != wantStatus || e != nil || le != nil || eventErr != nil || live != wantBalance || ledger != wantBalance || events != 1 {
				t.Fatalf("status=%s live=%d ledger=%d events=%d err=%v/%v/%v", row.Status, live, ledger, events, e, le, eventErr)
			}
			if mode == "settle" {
				result, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
				if err != nil || !result.Settled {
					t.Fatal(result, err)
				}
			} else {
				result, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
				if err != nil || !result.Reversed {
					t.Fatal(result, err)
				}
			}
			live, e = s.GetBalance(ctx, tenant.ID)
			ledger, le = store.LedgerBalance(ctx, db, tenant.ID)
			eventErr = db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&events).Error
			if e != nil || le != nil || eventErr != nil || live != wantBalance || ledger != wantBalance || events != 1 {
				t.Fatalf("replay live=%d ledger=%d events=%d err=%v/%v/%v", live, ledger, events, e, le, eventErr)
			}
			t.Logf("mode=%s committed=true reported=unavailable balance=%d ledger=%d replay_events=%d", mode, live, ledger, events)
		})
	}
}
