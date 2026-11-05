package comparison

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCompletionJournalHandlerChild(t *testing.T) {
	if os.Getenv("COMPLETION_HANDLER_CHILD") != "1" {
		t.Skip("child only")
	}
	gin.SetMode(gin.TestMode)
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate absent")
	}
	conn, err := controlplane.Dial(os.Getenv("COMPLETION_CAP_ADDR"), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Callback().Create().Before("gorm:create").Register("test:child-attempt-outbox-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_attempt_outbox" {
			tx.AddError(errors.New("synthetic attempt outbox failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	owner := sha256.Sum256([]byte("synthetic-owner"))
	base := &guardedAttemptBridge{gatewayExecutionBridge: &gatewayExecutionBridge{store: s, atomicReserve: candidate, conn: conn, ownerHash: hex.EncodeToString(owner[:])}, db: db}
	tenant := mustUint(os.Getenv("COMPLETION_TENANT"))
	bridge := &autoCompletionJournalBridge{guardedAttemptBridge: base, dir: os.Getenv("COMPLETION_DIR"), tenant: tenant}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, os.Getenv("COMPLETION_MODEL"))))
	c.Set("tenant_id", int64(tenant))
	c.Set("api_key_id", int64(1))
	c.Set("request_id", os.Getenv("COMPLETION_REQUEST"))
	handler.Chat(bridge, gatewayExecutionRouter{url: os.Getenv("COMPLETION_UPSTREAM"), providerID: mustUint(os.Getenv("COMPLETION_PROVIDER"))}, slog.New(slog.DiscardHandler), time.Second)(c)
	t.Fatal("handler returned before parent killed it")
}

func TestCompletionJournalHandlerKilledAfterSync(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	attemptDB := setupAttemptDB(t)
	if err := attemptDB.Exec("CREATE TABLE IF NOT EXISTS eval_attempt_request (tenant_id BIGINT UNSIGNED NOT NULL,request_id VARBINARY(128) NOT NULL,fingerprint VARBINARY(64) NOT NULL,PRIMARY KEY(tenant_id,request_id)) ENGINE=InnoDB").Error; err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("journal-auto-kill-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`)
	}))
	defer upstream.Close()
	provider := store.Provider{Name: id, BaseURL: upstream.URL, Models: "[]", Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	grpcServer.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = grpcServer.Serve(listener) }()
	defer grpcServer.Stop()
	dir := t.TempDir()
	request := id + "-req"
	cmd := exec.Command(os.Args[0], "-test.run=^TestCompletionJournalHandlerChild$")
	cmd.Env = append(os.Environ(), "COMPLETION_HANDLER_CHILD=1", "TEST_PAUSE_AFTER_COMPLETION_JOURNAL=1", "TEST_COORDINATED_FINISH=1", "COMPLETION_CAP_ADDR="+listener.Addr().String(), "COMPLETION_DIR="+dir, "COMPLETION_MODEL="+id, "COMPLETION_REQUEST="+request, "COMPLETION_UPSTREAM="+upstream.URL, fmt.Sprintf("COMPLETION_TENANT=%d", tenant.ID), fmt.Sprintf("COMPLETION_PROVIDER=%d", provider.ID))
	wantMarker := "AUTO_COMPLETION_JOURNAL_SYNCED"
	if os.Getenv("TEST_KILL_BEFORE_COMPLETION_JOURNAL") == "1" {
		cmd.Env = append(cmd.Env, "TEST_PAUSE_BEFORE_COMPLETION_JOURNAL=1")
		wantMarker = "AUTO_COMPLETION_JOURNAL_BEFORE_WRITE"
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			if strings.TrimSpace(scan.Text()) == wantMarker {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("child exited before journal sync")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("journal marker timeout")
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	err = cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("wait=%v", err)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("wait=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
	if os.Getenv("TEST_KILL_BEFORE_COMPLETION_JOURNAL") == "1" {
		if _, err := readCompletionJournal(dir, request); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected journal: %v", err)
		}
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", request, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			t.Fatalf("before sweep bill=%+v err=%v", bill, err)
		}
		if err := db.Exec("UPDATE bills SET created_at=DATE_SUB(NOW(),INTERVAL 11 MINUTE), execution_lease_until=DATE_SUB(NOW(),INTERVAL 1 SECOND) WHERE id=?", bill.ID).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Model(&store.Bill{}).Where("id=?", bill.ID).UpdateColumn("created_at", time.Now())
		stats, err := reconciler.New(db, nil, slog.New(slog.DiscardHandler), 10*time.Minute).SweepOnce(ctx)
		if err != nil || stats.Scanned != 1 || stats.Held != 1 || stats.Reversed != 0 {
			t.Fatalf("default sweep=%+v err=%v", stats, err)
		}
		conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		stats, err = reconciler.New(db, &stateSweepBridge{db: db, conn: conn, s: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse()).SweepOnce(ctx)
		if err != nil || stats.Scanned != 1 || stats.Reversed != 0 {
			t.Fatalf("test bridge sweep=%+v err=%v", stats, err)
		}
		for _, action := range []string{"forwarding", "reversing"} {
			var grant wrapperspb.Int64Value
			actor := "synthetic-owner"
			if action == "reversing" {
				actor = "synthetic-recovery"
			}
			if err := conn.Invoke(ctx, "/eval.execution.v1.Execution/Transition", wrapperspb.String(request+"|"+action+"|"+actor), &grant); err != nil || grant.Value != 0 {
				t.Fatalf("action=%s grant=%d err=%v", action, grant.Value, err)
			}
		}
		if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			t.Fatalf("after sweep bill=%+v err=%v", bill, err)
		}
		var a attemptRow
		if err := attemptDB.Table("eval_route_attempt").Where("tenant_id=? AND request_id=?", tenant.ID, request).Take(&a).Error; err != nil || a.State != "sending" {
			t.Fatalf("attempt=%+v err=%v", a, err)
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != ledger || live != 999 {
			t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var n int64
			want := int64(0)
			if phase == "reserve" {
				want = 1
			}
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":"+phase).Count(&n).Error; err != nil || n != want {
				t.Fatalf("phase=%s count=%d err=%v", phase, n, err)
			}
		}
		t.Log("handler child killed before journal; default sweep held pending, test-gated reverse and resend denied; balance=ledger=999")
		return
	}
	x, err := readCompletionJournal(dir, request)
	if err != nil || x.Usage.PromptTokens != 40 || x.Usage.TenantID != tenant.ID || x.Usage.ProviderID != int64(provider.ID) {
		t.Fatalf("journal=%+v err=%v", x, err)
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", request, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil || int64(bill.ID) != x.Usage.BillID {
		t.Fatalf("before replay bill=%+v err=%v", bill, err)
	}
	if os.Getenv("TEST_RECOVERY_SCAN_CHILD") == "1" {
		failed := 1
		if os.Getenv("TEST_RECOVERY_SCAN_PAGED") == "1" {
			failed = 128
			for i := 0; i < failed; i++ {
				name := fmt.Sprintf("%064x.json", i)
				if name >= filepath.Base(localUsagePath(dir, request)) {
					t.Fatal("cannot construct ordered poison records")
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte("{invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		} else if err := os.WriteFile(localUsagePath(dir, "malformed-synthetic"), []byte("{invalid"), 0600); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			recovery := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestCompletionJournalRecoveryProcessChild$")
			recovery.Env = append(os.Environ(), "COMPLETION_RECOVERY_CHILD=1", "COMPLETION_RECOVERY_DIR="+dir)
			output, err := recovery.CombinedOutput()
			cancel()
			want := fmt.Sprintf("RECOVERY_SCAN success=1 failed=%d attempt_recovered=%d", failed, 1-i)
			if err != nil || !strings.Contains(string(output), want) {
				t.Fatalf("recovery run=%d output=%s err=%v", i, output, err)
			}
		}
	} else {
		if err := replayCompletionJournal(ctx, db, s, dir, request); err != nil {
			t.Fatal(err)
		}
		if err := replayCompletionJournal(ctx, db, s, dir, request); err != nil {
			t.Fatal(err)
		}
	}
	var evidence string
	if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil || evidence != x.AttemptID {
		t.Fatalf("evidence=%q expected=%q err=%v", evidence, x.AttemptID, err)
	}
	wantRecovery := 1
	if os.Getenv("TEST_RECOVERY_SCAN_CHILD") == "1" {
		wantRecovery = 0
	}
	if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != wantRecovery {
		t.Fatalf("recover=%d err=%v", n, err)
	}
	if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != 0 {
		t.Fatalf("replay recover=%d err=%v", n, err)
	}
	var a attemptRow
	if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", x.AttemptID).Take(&a).Error; err != nil || a.State != "completed" {
		t.Fatalf("attempt=%+v err=%v", a, err)
	}
	var events int64
	if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", x.AttemptID).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusSettled {
		t.Fatalf("final bill=%+v err=%v", bill, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != live {
		t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		var n int64
		want := int64(0)
		if phase != "reverse" {
			want = 1
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":"+phase).Count(&n).Error; err != nil || n != want {
			t.Fatalf("phase=%s count=%d err=%v", phase, n, err)
		}
	}
	t.Logf("handler child SIGKILL after local journal sync; one upstream call, scan_children=%t, settled and recovered attempt once, balance=ledger=960", os.Getenv("TEST_RECOVERY_SCAN_CHILD") == "1")
}
