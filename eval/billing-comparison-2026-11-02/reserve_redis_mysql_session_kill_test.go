package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	mysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReserveRedisAndSQLSessionKillRecovery(t *testing.T) {
	name, addr := os.Getenv("COMPARISON_REDIS_CONTAINER"), os.Getenv("COMPARISON_REDIS_ADDR")
	if os.Getenv("TEST_RESERVE_BOTH_KILL") != "1" || !regexp.MustCompile(`^auth-cache-test-redis-[0-9]+-[0-9]+$`).MatchString(name) {
		t.Skip("requires isolated Redis")
	}
	docker := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	port, err := docker("port", name, "6379/tcp")
	if err != nil || !strings.HasPrefix(addr, "127.0.0.1:") || !strings.Contains(port, addr) {
		t.Fatalf("port mismatch %s %s %v", addr, port, err)
	}
	mysqlName := os.Getenv("COMPARISON_MYSQL_CONTAINER")
	cfg, parseErr := mysql.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if parseErr != nil || !regexp.MustCompile(`^auth-cache-test-mysql-[0-9]+-[0-9]+$`).MatchString(mysqlName) || !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Fatal("refusing non-isolated MySQL")
	}
	mysqlPort, portErr := docker("port", mysqlName, "3306/tcp")
	if portErr != nil || !strings.Contains(mysqlPort, cfg.Addr) {
		t.Fatalf("MySQL container port mismatch: %s %v", mysqlPort, portErr)
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("reserve-both-kill-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var first atomic.Bool
	var mysqlConn uint64
	var connErr error
	callback := "eval:reserve-kill-" + id
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" && first.CompareAndSwap(false, true) {
			connErr = tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&mysqlConn)
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove(callback)
	type outcome struct {
		resp *pb.ReserveResponse
		err  error
	}
	done := make(chan outcome, 1)
	go func() { resp, err := s.ReserveWithReplaySignal(ctx, req); done <- outcome{resp, err} }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("reserve insert was not reached")
	}
	if connErr != nil || mysqlConn == 0 {
		unblock()
		t.Fatalf("transaction connection id=%d err=%v", mysqlConn, connErr)
	}
	balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || balance != 400 {
		unblock()
		t.Fatalf("before SQL insert balance=%d err=%v", balance, err)
	}
	if out, err := docker("kill", name); err != nil {
		unblock()
		t.Fatalf("kill: %s %v", out, err)
	}
	// Kill only the ID read from this test transaction on the verified disposable DB.
	if err := db.Exec(fmt.Sprintf("KILL CONNECTION %d", mysqlConn)).Error; err != nil {
		unblock()
		t.Fatalf("kill transaction connection: %v", err)
	}
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_, _ = docker("start", name)
		}
	})
	restartRedis := func() {
		t.Helper()
		if out, err := docker("start", name); err != nil {
			unblock()
			t.Fatalf("start: %s %v", out, err)
		}
		restarted = true
		deadline := time.Now().Add(20 * time.Second)
		for {
			pingCtx, cancel := context.WithTimeout(ctx, time.Second)
			err := r.Ping(pingCtx).Err()
			cancel()
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				unblock()
				t.Fatalf("redis ping: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	beforeInsert := os.Getenv("TEST_RESTART_BEFORE_INSERT") == "1"
	if beforeInsert {
		restartRedis()
	}
	unblock()
	var result outcome
	select {
	case result = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("reserve did not finish")
	}
	if result.err == nil {
		t.Fatalf("expected killed SQL connection failure, response=%+v", result.resp)
	}
	var preBills, preEvents int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&preBills).Error; err != nil || preBills != 0 {
		t.Fatalf("pre-retry bills=%d err=%v", preBills, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&preEvents).Error; err != nil || preEvents != 0 {
		t.Fatalf("pre-retry events=%d err=%v", preEvents, err)
	}
	if !beforeInsert {
		restartRedis()
	}
	fresh, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || fresh == nil || fresh.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("retry=%+v err=%v", fresh, err)
	}
	repeat, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || repeat == nil || repeat.Code != pb.ReserveCode_RESERVE_DUPLICATE || repeat.BillId != fresh.BillId {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}

	balance, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != 400 || balance != 400 {
		t.Fatalf("balance=%d ledger=%d err=%v", balance, ledger, err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&bills).Error; err != nil || bills != 1 {
		t.Fatalf("bills=%d err=%v", bills, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	t.Logf("Redis and transaction connection killed after debit, restart_before_insert=%v: initial error=%v, no bill/event before retry, retry and duplicate, final balance=%d ledger=%d events=%d", beforeInsert, result.err, balance, ledger, events)
}
