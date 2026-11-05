package comparison

import (
	"context"
	"database/sql"
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
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReserveCommittedAckLostRedisCrashOneWinner(t *testing.T) {
	name, addr := os.Getenv("COMPARISON_REDIS_CONTAINER"), os.Getenv("COMPARISON_REDIS_ADDR")
	if os.Getenv("TEST_COMMIT_ACK_REDIS_CRASH") != "1" || !regexp.MustCompile(`^auth-cache-test-redis-[0-9]+-[0-9]+$`).MatchString(name) {
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
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	base := store.NewStore(db, r)
	if err := base.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("ack-redis-onewinner-%d", time.Now().UnixNano())
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
	cfg, err := mysqlDriver.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	connector, err := mysqlDriver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	committed, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var armed atomic.Bool
	armed.Store(true)
	sqlDB := sql.OpenDB(commitPauseConnector{Connector: connector, armed: &armed, committed: committed, release: release})
	defer func() { unblock(); _ = sqlDB.Close() }()
	wrappedDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	s := store.NewStore(wrappedDB, r)
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	type outcome struct {
		resp *pb.ReserveResponse
		err  error
	}
	done := make(chan outcome, 1)
	go func() { resp, err := s.ReserveWithReplaySignal(ctx, req); done <- outcome{resp, err} }()
	select {
	case <-committed:
	case <-time.After(15 * time.Second):
		t.Fatal("commit did not finish")
	}
	var n, e int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&n).Error; err != nil || n != 1 {
		unblock()
		t.Fatalf("committed bills=%d err=%v", n, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&e).Error; err != nil || e != 1 {
		unblock()
		t.Fatalf("committed events=%d err=%v", e, err)
	}
	if out, err := docker("kill", name); err != nil {
		unblock()
		t.Fatalf("kill=%s %v", out, err)
	}
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_, _ = docker("start", name)
		}
	})
	unblock()
	var initial outcome
	select {
	case initial = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("reserve did not finish")
	}
	if initial.err == nil {
		t.Fatalf("expected acknowledgement/recovery error, response=%+v", initial.resp)
	}
	if out, err := docker("start", name); err != nil {
		t.Fatalf("start=%s %v", out, err)
	}
	restarted = true
	deadline := time.Now().Add(20 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		err = r.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("redis unavailable %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if exists, err := r.Exists(ctx, store.BalanceKey(tenant.ID)).Result(); err != nil || exists != 0 {
		t.Fatalf("expected lost projection exists=%d err=%v", exists, err)
	}
	// The committed 100-cent reservation must remain accounted for even if a
	// different request gets there first after the Redis projection is lost.
	type competingResult struct {
		code pb.ReserveCode
		err  error
	}
	competing := make(chan competingResult, 8)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			workerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			resp, err := s.ReserveWithReplaySignal(workerCtx, &pb.ReserveRequest{RequestId: fmt.Sprintf("%s-c%d", id, i), TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 200})
			if err != nil || resp == nil {
				if err == nil {
					err = fmt.Errorf("nil competing reserve response")
				}
				competing <- competingResult{err: err}
				return
			}
			competing <- competingResult{code: resp.Code}
		}(i)
	}
	close(start)
	workers.Wait()
	close(competing)
	counts := map[pb.ReserveCode]int{}
	for got := range competing {
		if got.err != nil {
			t.Fatal(got.err)
		}
		counts[got.code]++
	}
	if counts[pb.ReserveCode_RESERVE_OK] != 1 || counts[pb.ReserveCode_RESERVE_INSUFFICIENT] != 7 {
		t.Fatalf("competing results=%v", counts)
	}
	var reserves []store.Bill
	if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Find(&reserves).Error; err != nil || len(reserves) != 2 {
		t.Fatalf("reservations=%d err=%v", len(reserves), err)
	}
	var competitorID string
	for _, b := range reserves {
		if b.RequestID != id {
			if b.PreQuota != 240 || b.Status != store.StatusPending {
				t.Fatalf("competitor bill=%+v", b)
			}
			competitorID = b.RequestID
		}
	}
	if competitorID == "" {
		t.Fatal("missing winning competitor")
	}
	var eventCount int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", competitorID+":reserve").Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("winner events=%d err=%v", eventCount, err)
	}
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("%s-c%d", id, i)
		if key == competitorID {
			continue
		}
		var count int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", key+":reserve").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rejected %s events=%d err=%v", key, count, err)
		}
	}

	repeat, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || repeat == nil || repeat.Code != pb.ReserveCode_RESERVE_DUPLICATE {
		t.Fatalf("retry=%+v %v", repeat, err)
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || int64(bill.ID) != repeat.BillId {
		t.Fatalf("bill=%+v repeat=%+v err=%v", bill, repeat, err)
	}
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("bills=%d err=%v", n, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&e).Error; err != nil || e != 1 {
		t.Fatalf("events=%d err=%v", e, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 160 || ledger != 160 {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("confirmed committed before injected lost ack; Redis killed; one competitor reserved 240 and seven insufficient; initial=%v retry=duplicate bills=%d events=%d balance=%d", initial.err, n, e, live)
}
