package comparison

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
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

func TestReserveAckCrashOldRedisSnapshotMultiprocess(t *testing.T) {
	name, addr := os.Getenv("COMPARISON_REDIS_CONTAINER"), os.Getenv("COMPARISON_REDIS_ADDR")
	if os.Getenv("TEST_ACK_OLD_REDIS_SNAPSHOT") != "1" || !regexp.MustCompile(`^auth-cache-test-redis-[0-9]+-[0-9]+$`).MatchString(name) {
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
	id := fmt.Sprintf("ack-redis-multiprocess-%d", time.Now().UnixNano())
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
	// Only this disposable Redis instance is configured; retain a snapshot
	// containing the pre-reservation projection, then kill it after commit.
	if err := r.ConfigSet(ctx, "save", "").Err(); err != nil {
		t.Fatal(err)
	}
	conf, err := r.ConfigGet(ctx, "appendonly").Result()
	if err != nil || conf["appendonly"] != "no" {
		t.Fatalf("unexpected AOF configuration=%v err=%v", conf, err)
	}
	if err := r.Save(ctx).Err(); err != nil {
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
	stale, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || stale != 500 {
		t.Fatalf("expected old snapshot=500, got=%d err=%v", stale, err)
	}
	committedLedger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || committedLedger != 400 {
		t.Fatalf("committed ledger=%d err=%v", committedLedger, err)
	}
	publicBalance, err := s.GetBalance(ctx, tenant.ID)
	if err != nil || publicBalance != 500 {
		t.Fatalf("expected public stale projection=500, got=%d err=%v", publicBalance, err)
	}
	// The committed 100-cent reservation must remain accounted for even if a
	// different request gets there first after the Redis projection is lost.
	workerCtx, cancelWorkers := context.WithTimeout(ctx, 45*time.Second)
	defer cancelWorkers()
	type process struct {
		cmd    *exec.Cmd
		in     io.WriteCloser
		reader *bufio.Reader
		waited bool
	}
	workers := make([]*process, 0, 8)
	defer func() {
		cancelWorkers()
		for _, p := range workers {
			if !p.waited {
				_ = p.cmd.Process.Kill()
				_ = p.cmd.Wait()
			}
		}
	}()
	for i := 0; i < 8; i++ {
		cmd := exec.CommandContext(workerCtx, os.Args[0], "-test.run=^TestReserveAckCrashCompetitorChild$", "-test.v")
		cmd.Env = append(os.Environ(), "TEST_RESERVE_COMPETITOR_CHILD=1", fmt.Sprintf("TEST_COMPETITOR_TENANT=%d", tenant.ID), "TEST_COMPETITOR_MODEL="+id, fmt.Sprintf("TEST_COMPETITOR_ID=%s-c%d", id, i))
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		p := &process{cmd: cmd, in: input, reader: bufio.NewReader(output)}
		workers = append(workers, p)
		for {
			line, err := p.reader.ReadString('\n')
			if err != nil {
				t.Fatalf("worker %d before ready: %q %v", i, line, err)
			}
			if strings.TrimSpace(line) == "CHILD_READY" {
				break
			}
		}
	}
	for _, p := range workers {
		if _, err := io.WriteString(p.in, "go\n"); err != nil {
			t.Fatal(err)
		}
		_ = p.in.Close()
	}
	counts := map[string]int{}
	for i, p := range workers {
		output, readErr := io.ReadAll(p.reader)
		waitErr := p.cmd.Wait()
		p.waited = true
		if readErr != nil || waitErr != nil {
			t.Fatalf("worker %d output=%s read=%v wait=%v", i, output, readErr, waitErr)
		}
		if strings.Contains(string(output), "CHILD_RESULT:RESERVE_OK") {
			counts["ok"]++
		} else if strings.Contains(string(output), "CHILD_RESULT:RESERVE_INSUFFICIENT") {
			counts["insufficient"]++
		} else {
			t.Fatalf("worker %d unexpected output=%s", i, output)
		}
	}
	if counts["ok"] != 1 || counts["insufficient"] != 7 {
		t.Fatalf("process results=%v", counts)
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
	t.Logf("confirmed committed before injected lost ack; Redis restarted from old snapshot=500, public balance=500 while ledger=400; one of eight independent workers reserved 240 and seven insufficient; initial=%v retry=duplicate bills=%d events=%d balance=%d", initial.err, n, e, live)
}
