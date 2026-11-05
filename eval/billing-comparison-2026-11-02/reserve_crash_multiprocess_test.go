package comparison

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReserveCrashWorkerChild(t *testing.T) {
	if os.Getenv("TEST_RESERVE_CRASH_CHILD") != "1" {
		t.Skip("child only")
	}
	id := os.Getenv("TEST_RESERVE_CRASH_ID")
	tenant, err := strconv.ParseInt(os.Getenv("TEST_RESERVE_CRASH_TENANT"), 10, 64)
	if err != nil || tenant <= 0 || !strings.HasPrefix(id, "ack-redis-multiprocess-") {
		t.Fatalf("invalid child inputs")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.Callback().Create().Before("gorm:create").Register("eval:kill-reserve", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" {
			fmt.Fprintln(os.Stderr, "CHILD_AFTER_REDIS_BEFORE_INSERT")
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err = store.NewStore(db, r).ReserveWithReplaySignal(context.Background(), &pb.ReserveRequest{RequestId: id, TenantId: tenant, ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	t.Fatalf("child survived: %v", err)
}

func TestReserveWorkerCrashAndMultiprocessCompetition(t *testing.T) {
	if os.Getenv("TEST_RESERVE_PROCESS_CRASH") != "1" {
		t.Skip("isolated process test")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("ack-redis-multiprocess-%d", time.Now().UnixNano())
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
	childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestReserveCrashWorkerChild$")
	child.Env = append(os.Environ(), "TEST_RESERVE_CRASH_CHILD=1", "TEST_RESERVE_CRASH_ID="+id, fmt.Sprintf("TEST_RESERVE_CRASH_TENANT=%d", tenant.ID))
	output, err := child.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("child exit %v output=%s", err, output)
	}
	st, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !st.Signaled() || st.Signal() != syscall.SIGKILL || !strings.Contains(string(output), "CHILD_AFTER_REDIS_BEFORE_INSERT") {
		t.Fatalf("unexpected child exit=%v output=%s", err, output)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 400 {
		t.Fatalf("raw balance=%d err=%v", live, err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != 500 {
		t.Fatalf("ledger=%d err=%v", ledger, err)
	}
	guard, err := r.Exists(ctx, store.ReqGuardKey(id)).Result()
	if err != nil || guard != 1 {
		t.Fatalf("orphan guard=%d err=%v", guard, err)
	}
	var n int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("crash bill=%d err=%v", n, err)
	}
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
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		p := &process{cmd: cmd, in: input, reader: bufio.NewReader(out)}
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
		out, readErr := io.ReadAll(p.reader)
		waitErr := p.cmd.Wait()
		p.waited = true
		if readErr != nil || waitErr != nil {
			t.Fatalf("worker %d output=%s read=%v wait=%v", i, out, readErr, waitErr)
		}
		if strings.Contains(string(out), "CHILD_RESULT:RESERVE_OK") {
			counts["ok"]++
		} else if strings.Contains(string(out), "CHILD_RESULT:RESERVE_INSUFFICIENT") {
			counts["insufficient"]++
		} else {
			t.Fatalf("worker %d output=%s", i, out)
		}
	}
	if counts["ok"] != 2 || counts["insufficient"] != 6 {
		t.Fatalf("worker counts=%v", counts)
	}
	var bills []store.Bill
	if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Find(&bills).Error; err != nil || len(bills) != 2 {
		t.Fatalf("bills=%d err=%v", len(bills), err)
	}
	winners := make(map[string]bool)
	for _, b := range bills {
		winners[b.RequestID] = true
		if b.PreQuota != 240 || b.Status != store.StatusPending {
			t.Fatalf("bill=%+v", b)
		}
		var events int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", b.RequestID+":reserve").Count(&events).Error; err != nil || events != 1 {
			t.Fatalf("event=%d err=%v", events, err)
		}
	}
	for i := 0; i < 8; i++ {
		requestID := fmt.Sprintf("%s-c%d", id, i)
		var eventCount int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", requestID+":reserve").Count(&eventCount).Error; err != nil {
			t.Fatal(err)
		}
		want := int64(0)
		if winners[requestID] {
			want = 1
		}
		if eventCount != want {
			t.Fatalf("request %s event=%d want=%d", requestID, eventCount, want)
		}
	}
	var crashEvent int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&crashEvent).Error; err != nil || crashEvent != 0 {
		t.Fatalf("crashed request event=%d err=%v", crashEvent, err)
	}
	again, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || again == nil || again.Code != pb.ReserveCode_RESERVE_INSUFFICIENT {
		t.Fatalf("original retry=%+v err=%v", again, err)
	}
	if n, err := r.Exists(ctx, store.ReqGuardKey(id)).Result(); err != nil || n != 0 {
		t.Fatalf("orphan guard after retry=%d err=%v", n, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 20 || ledger != 20 {
		t.Fatalf("final=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("SIGKILL after Redis debit before SQL: raw=400 ledger=500; eight processes => two reserve, six insufficient; original retry insufficient; final balance=%d", live)
}
