package comparison

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTerminalRollbackRedisOutage(t *testing.T) {
	container := os.Getenv("COMPARISON_OWNED_REDIS_CONTAINER")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, " /\\") {
		t.Fatal("owned Redis container required")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"settle", "reverse"} {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("terminal-redis-outage-%s-%d", mode, time.Now().UnixNano())
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
			if mode == "settle" {
				if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
					t.Fatal(err)
				}
			}
			var fired atomic.Bool
			var stopErr error
			if err := db.Callback().Create().Before("gorm:create").Register("test:stop_redis_terminal", func(tx *gorm.DB) {
				if tx.Statement.Table != "outbox_events" || !fired.CompareAndSwap(false, true) {
					return
				}
				out, e := exec.Command("docker", "kill", "--signal", "KILL", container).CombinedOutput()
				if e != nil {
					stopErr = fmt.Errorf("kill redis: %w: %s", e, out)
					tx.AddError(stopErr)
					return
				}
				tx.AddError(errors.New("injected rollback while Redis down"))
			}); err != nil {
				t.Fatal(err)
			}
			if mode == "settle" {
				_, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id})
			} else {
				_, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
			}
			if removeErr := db.Callback().Create().Remove("test:stop_redis_terminal"); removeErr != nil {
				t.Fatal(removeErr)
			}
			if err == nil || !fired.Load() || stopErr != nil {
				t.Fatalf("terminal=%v fired=%v stop=%v", err, fired.Load(), stopErr)
			}
			var row store.Bill
			if e := db.First(&row, res.BillId).Error; e != nil || row.Status != store.StatusPending {
				t.Fatalf("pending=%s err=%v", row.Status, e)
			}
			var count int64
			if e := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&count).Error; e != nil || count != 0 {
				t.Fatalf("events=%d err=%v", count, e)
			}
			if out, e := exec.Command("docker", "start", container).CombinedOutput(); e != nil {
				t.Fatalf("start redis: %v: %s", e, out)
			}
			deadline := time.Now().Add(30 * time.Second)
			for {
				pc, cancel := context.WithTimeout(ctx, time.Second)
				e := r.Ping(pc).Err()
				cancel()
				if e == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("redis readiness timeout")
				}
				time.Sleep(200 * time.Millisecond)
			}
			var final int64
			if mode == "settle" {
				got, e := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
				if e != nil || !got.Settled {
					t.Fatal(got, e)
				}
				final = 460
			} else {
				got, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
				if e != nil || !got.Reversed {
					t.Fatal(got, e)
				}
				final = 500
			}
			live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if e != nil {
				t.Fatal(e)
			}
			ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
			if e != nil || live != final || ledger != final {
				t.Fatalf("live=%d ledger=%d err=%v", live, ledger, e)
			}
			if e := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+mode).Count(&count).Error; e != nil || count != 1 {
				t.Fatalf("events=%d err=%v", count, e)
			}
			t.Logf("mode=%s redis_killed=true sql_rollback=true retry_balance=%d ledger=%d events=%d", mode, live, ledger, count)
		})
	}
}
