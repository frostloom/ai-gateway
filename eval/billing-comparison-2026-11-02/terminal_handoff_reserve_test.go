package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTerminalLockHandoffWithNewReserve(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"settle", "reverse"} {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("handoff-%s-%d", mode, time.Now().UnixNano())
			next := id + "-next"
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
			req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
			res, err := s.Reserve(ctx, req)
			if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(res, err)
			}
			if mode == "settle" {
				if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
					t.Fatal(err)
				}
			}
			var fired atomic.Bool
			var injectionErr error
			var nextOK bool
			if err := db.Callback().Create().Before("gorm:create").Register("test:handoff_reserve", func(tx *gorm.DB) {
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
				if os.Getenv("TEST_HANDOFF_SUBPROCESS") == "1" {
					childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
					defer cancel()
					cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestTerminalHandoffChild$", "-test.v")
					cmd.Env = append(os.Environ(), "TEST_HANDOFF_CHILD=1", "HANDOFF_TENANT="+strconv.FormatUint(tenant.ID, 10), "HANDOFF_REQUEST="+next, "HANDOFF_MODEL="+id)
					output, e := cmd.CombinedOutput()
					if e != nil {
						injectionErr = fmt.Errorf("child reserve: %w: %s", e, output)
						return
					}
					nextOK = true
				} else {
					var result *pb.ReserveResponse
					result, injectionErr = s.Reserve(ctx, &pb.ReserveRequest{RequestId: next, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
					nextOK = result != nil && result.Code == pb.ReserveCode_RESERVE_OK
				}
			}); err != nil {
				t.Fatal(err)
			}
			if mode == "settle" {
				_, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id})
			} else {
				_, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
			}
			if e := db.Callback().Create().Remove("test:handoff_reserve"); e != nil {
				t.Fatal(e)
			}
			if err == nil || !fired.Load() || injectionErr != nil || !nextOK {
				t.Fatalf("terminal=%v fired=%v second_ok=%v error=%v", err, fired.Load(), nextOK, injectionErr)
			}
			live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if e != nil {
				t.Fatal(e)
			}
			ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
			if e != nil || live != 300 || ledger != 300 {
				t.Fatalf("after handoff live=%d ledger=%d err=%v", live, ledger, e)
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
			want := int64(400)
			if mode == "settle" {
				want = 360
			}
			if e != nil || live != want || ledger != want {
				t.Fatalf("after retry live=%d ledger=%d want=%d err=%v", live, ledger, want, e)
			}
			var nextBill store.Bill
			if e := db.Where("request_id=? AND phase=?", next, store.PhaseReserve).First(&nextBill).Error; e != nil || nextBill.Status != store.StatusPending {
				t.Fatalf("second status=%s err=%v", nextBill.Status, e)
			}
			var events int64
			if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":" + mode, next + ":reserve"}).Count(&events).Error; e != nil || events != 2 {
				t.Fatalf("events=%d err=%v", events, e)
			}
			t.Logf("mode=%s killed=true second_reserve_before_recovery=true post_recovery_balance=300 retry_balance=%d ledger=%d", mode, live, ledger)
		})
	}
}

func TestTerminalHandoffChild(t *testing.T) {
	if os.Getenv("TEST_HANDOFF_CHILD") != "1" {
		t.Skip("subprocess only")
	}
	tenant, err := strconv.ParseInt(os.Getenv("HANDOFF_TENANT"), 10, 64)
	if err != nil || tenant <= 0 {
		t.Fatalf("invalid tenant: %v", err)
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	result, err := store.NewStore(db, r).Reserve(context.Background(), &pb.ReserveRequest{RequestId: os.Getenv("HANDOFF_REQUEST"), TenantId: tenant, ApiKeyId: 1, Model: os.Getenv("HANDOFF_MODEL"), EstPromptTokens: 84})
	if err != nil || result == nil || result.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve=%v err=%v", result, err)
	}
}
