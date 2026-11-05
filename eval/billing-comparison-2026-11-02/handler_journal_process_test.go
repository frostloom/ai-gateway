package comparison

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type childJournalBridge struct {
	*gatewayExecutionBridge
	dir       string
	tenant    uint64
	container string
	db        *gorm.DB
}

func TestManualResolutionProcessChild(t *testing.T) {
	if os.Getenv("MANUAL_RESOLUTION_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	var resolution manualUsageResolution
	if err := json.Unmarshal([]byte(os.Getenv("MANUAL_RESOLUTION_INPUT")), &resolution); err != nil {
		t.Fatal(err)
	}
	if err := recordManualUsage(context.Background(), db, resolution); err != nil {
		t.Fatal(err)
	}
	fmt.Println("MANUAL_RESOLUTION_COMMITTED")
	time.Sleep(time.Minute)
}

func (b *childJournalBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	if !strings.HasPrefix(b.container, "auth-cache-test-mysql-") || strings.ContainsAny(b.container, "/ ") {
		return nil, errors.New("isolated container required")
	}
	if out, err := exec.Command("docker", "kill", b.container).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("kill: %v %s", err, out)
	}
	if os.Getenv("TEST_KILL_BEFORE_LOCAL") == "1" {
		fmt.Println("HANDLER_BEFORE_JOURNAL")
		time.Sleep(time.Minute)
		return nil, errors.New("interrupted before usage persistence")
	}
	bridge := &inboxFailureBridge{executionBillingAPI: b.gatewayExecutionBridge, db: b.db, tenant: b.tenant, localDir: b.dir}
	_, err := bridge.ReportUsage(ctx, req)
	if err == nil || bridge.journalErr != nil {
		return nil, fmt.Errorf("usage=%v journal=%v", err, bridge.journalErr)
	}
	if _, readErr := readLocalUsage(b.dir, req.RequestId); readErr != nil {
		return nil, readErr
	}
	fmt.Println("HANDLER_JOURNAL_SYNCED")
	time.Sleep(time.Minute)
	return nil, err
}
func TestHandlerJournalProcessChild(t *testing.T) {
	if os.Getenv("HANDLER_JOURNAL_CHILD") != "1" {
		t.Skip("helper only")
	}
	gin.SetMode(gin.TestMode)
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate absent")
	}
	conn, err := controlplane.Dial(os.Getenv("HANDLER_CAP_ADDR"), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	digest := sha256.Sum256([]byte("synthetic-owner"))
	b := &childJournalBridge{gatewayExecutionBridge: &gatewayExecutionBridge{store: s, atomicReserve: candidate, conn: conn, ownerHash: hex.EncodeToString(digest[:])}, dir: os.Getenv("HANDLER_JOURNAL_DIR"), tenant: mustUint(os.Getenv("HANDLER_TENANT")), container: os.Getenv("COMPARISON_OWNED_MYSQL_CONTAINER"), db: db}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	model := os.Getenv("HANDLER_MODEL")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, model)))
	c.Set("tenant_id", int64(b.tenant))
	c.Set("api_key_id", int64(1))
	c.Set("request_id", os.Getenv("HANDLER_REQUEST"))
	handler.Chat(b, gatewayExecutionRouter{url: os.Getenv("HANDLER_MOCK_URL"), providerID: mustUint(os.Getenv("HANDLER_PROVIDER"))}, slog.New(slog.DiscardHandler), time.Second)(c)
	t.Fatal("handler returned before parent killed child")
}
func TestHandlerJournalSurvivesProcessKill(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("experimental overlay required")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS eval_usage_inbox (request_id VARBINARY(128) PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,bill_id BIGINT NOT NULL,provider_id BIGINT NOT NULL,prompt_tokens BIGINT NOT NULL,completion_tokens BIGINT NOT NULL,state VARCHAR(16) NOT NULL) ENGINE=InnoDB`).Error; err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEST_MANUAL_RESOLUTION") == "1" {
		if err := db.Exec(`CREATE TABLE IF NOT EXISTS eval_manual_usage (request_id VARBINARY(128) PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,bill_id BIGINT NOT NULL,provider_id BIGINT NOT NULL,prompt_tokens BIGINT NOT NULL,completion_tokens BIGINT NOT NULL,evidence_sha VARBINARY(64) NOT NULL,actor VARBINARY(128) NOT NULL) ENGINE=InnoDB`).Error; err != nil {
			t.Fatal(err)
		}
	}
	id := fmt.Sprintf("handler-journal-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":2}}`)
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]", Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	server.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()
	container := os.Getenv("COMPARISON_OWNED_MYSQL_CONTAINER")
	if !strings.HasPrefix(container, "auth-cache-test-mysql-") || strings.ContainsAny(container, "/ ") {
		t.Fatal("isolated container required")
	}
	t.Cleanup(func() { _ = exec.Command("docker", "start", container).Run() })
	dir := t.TempDir()
	request := id + "-request"
	cmd := exec.Command(os.Args[0], "-test.run=^TestHandlerJournalProcessChild$", "-test.timeout=40s")
	cmd.Env = append(os.Environ(), "HANDLER_JOURNAL_CHILD=1", "HANDLER_CAP_ADDR="+lis.Addr().String(), "HANDLER_JOURNAL_DIR="+dir, fmt.Sprintf("HANDLER_TENANT=%d", tenant.ID), "HANDLER_MODEL="+id, "HANDLER_REQUEST="+request, "HANDLER_MOCK_URL="+up.URL, fmt.Sprintf("HANDLER_PROVIDER=%d", provider.ID))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	wantMarker := "HANDLER_JOURNAL_SYNCED"
	if os.Getenv("TEST_KILL_BEFORE_LOCAL") == "1" {
		wantMarker = "HANDLER_BEFORE_JOURNAL"
	}
	if err != nil || strings.TrimSpace(line) != wantMarker {
		t.Fatalf("child marker=%q err=%v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	if output, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
		t.Fatalf("restart=%v %s", err, output)
	}
	ready := false
	for i := 0; i < 90; i++ {
		if exec.Command("docker", "exec", container, "mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-pisolated", "--silent").Run() == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("mysql restart timeout")
	}
	if os.Getenv("TEST_KILL_BEFORE_LOCAL") == "1" {
		if _, err := readLocalUsage(dir, request); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected local usage: %v", err)
		}
		result, err := scanLocalUsage(ctx, db, s, dir)
		if err != nil || result.Succeeded != 0 || result.Failed != 0 {
			t.Fatalf("unexpected recovery=%+v err=%v", result, err)
		}
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", request, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			t.Fatalf("bill=%+v err=%v", bill, err)
		}
		if err := db.Exec("UPDATE bills SET created_at=DATE_SUB(NOW(), INTERVAL 11 MINUTE),execution_lease_until=DATE_SUB(NOW(), INTERVAL 1 SECOND) WHERE id=?", bill.ID).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = db.Model(&store.Bill{}).Where("id=?", bill.ID).UpdateColumn("created_at", time.Now()).Error
		})
		conn, err := controlplane.Dial(lis.Addr().String(), "test-token")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		runner := reconciler.New(db, &stateSweepBridge{db: db, conn: conn, s: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
		stats, err := runner.SweepOnce(ctx)
		if err != nil || stats.Scanned != 1 || stats.Reversed != 0 {
			t.Fatal(stats, err)
		}
		if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			t.Fatal(bill.Status, err)
		}
		for _, call := range []string{request + "|forwarding|synthetic-owner", request + "|reversing|synthetic-recovery"} {
			var grant wrapperspb.Int64Value
			if err := conn.Invoke(ctx, "/eval.execution.v1.Execution/Transition", wrapperspb.String(call), &grant); err != nil || grant.Value != 0 {
				t.Fatalf("stale transition grant=%d err=%v", grant.Value, err)
			}
		}
		var executionState string
		if err := db.Raw("SELECT execution_state FROM bills WHERE id=?", bill.ID).Scan(&executionState).Error; err != nil || executionState != "forwarding" {
			t.Fatal(executionState, err)
		}
		var inboxRows int64
		if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Count(&inboxRows).Error; err != nil || inboxRows != 0 {
			t.Fatal(inboxRows, err)
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 499 || ledger != live {
			t.Fatal(live, ledger, err)
		}
		var events int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{request + ":reserve", request + ":usage", request + ":settle", request + ":reverse"}).Count(&events).Error; err != nil || events != 1 {
			t.Fatal(events, err)
		}
		var reserveEvents int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":reserve").Count(&reserveEvents).Error; err != nil || reserveEvents != 1 {
			t.Fatal(reserveEvents, err)
		}
		t.Log("upstream replied once; handler killed before journal; no usage evidence; expired forwarding remains pending; no automatic refund")
		if os.Getenv("TEST_MANUAL_RESOLUTION") == "1" {
			digest := sha256.Sum256([]byte("synthetic reviewer evidence"))
			resolution := manualUsageResolution{RequestID: request, TenantID: tenant.ID, BillID: int64(bill.ID), ProviderID: int64(provider.ID), PromptTokens: 40, CompletionTokens: 2, EvidenceSHA: hex.EncodeToString(digest[:]), Actor: "synthetic-reviewer"}
			if os.Getenv("TEST_MANUAL_USAGE_RACE") == "1" {
				start := make(chan struct{})
				manualResult := make(chan error, 1)
				usageResult := make(chan error, 1)
				go func() { <-start; manualResult <- recordManualUsage(ctx, db, resolution) }()
				go func() {
					<-start
					usageResult <- s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: request, BillId: int64(bill.ID), ProviderId: int64(provider.ID), PromptTokens: 40, CompletionTokens: 3})
				}()
				close(start)
				manualErr, usageErr := <-manualResult, <-usageResult
				if (manualErr == nil) == (usageErr == nil) {
					t.Fatalf("expected exactly one winner: manual=%v usage=%v", manualErr, usageErr)
				}
				wantBalance := int64(457)
				if manualErr == nil {
					wantBalance = 458
					if status.Code(usageErr) != codes.FailedPrecondition {
						t.Fatal(usageErr)
					}
					if err := replayUsageInbox(ctx, db, s, request); err != nil {
						t.Fatal(err)
					}
				} else {
					settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: request, BillId: int64(bill.ID)})
					if err != nil || !settled.Settled {
						t.Fatal(settled, err)
					}
				}
				live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
				if err != nil {
					t.Fatal(err)
				}
				ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
				if err != nil || live != wantBalance || ledger != live {
					t.Fatal(live, ledger, err)
				}
				var settleRows, events int64
				if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", request, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 1 {
					t.Fatal(settleRows, err)
				}
				if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{request + ":reserve", request + ":usage", request + ":settle"}).Count(&events).Error; err != nil || events != 3 {
					t.Fatal(events, err)
				}
				t.Logf("manual_won=%v usage_won=%v balance=%d", manualErr == nil, usageErr == nil, live)
				return
			}
			if os.Getenv("TEST_MANUAL_USAGE_FIRST") == "1" {
				if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: request, BillId: int64(bill.ID), ProviderId: int64(provider.ID), PromptTokens: 40, CompletionTokens: 3}); err != nil {
					t.Fatal(err)
				}
				if err := recordManualUsage(ctx, db, resolution); err == nil {
					t.Fatal("manual record accepted after conflicting usage")
				}
				var audits, inbox int64
				if err := db.Table("eval_manual_usage").Where("request_id=?", request).Count(&audits).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Count(&inbox).Error; err != nil {
					t.Fatal(err)
				}
				if audits != 0 || inbox != 0 {
					t.Fatal(audits, inbox)
				}
				settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: request, BillId: int64(bill.ID)})
				if err != nil || !settled.Settled {
					t.Fatal(settled, err)
				}
				live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
				if err != nil {
					t.Fatal(err)
				}
				ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
				if err != nil || live != 457 || ledger != live {
					t.Fatal(live, ledger, err)
				}
				t.Log("late normal usage committed first; conflicting manual resolution rejected; balance=457")
				return
			}
			injected := errors.New("synthetic audit write failure")
			if err := db.Callback().Create().Before("gorm:create").Register("test:manual-audit-fail", func(tx *gorm.DB) {
				if tx.Statement.Table == "eval_manual_usage" {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			err := recordManualUsage(ctx, db, resolution)
			db.Callback().Create().Remove("test:manual-audit-fail")
			if !errors.Is(err, injected) {
				t.Fatalf("audit injection=%v", err)
			}
			var beforeAudit, beforeInbox int64
			if err := db.Table("eval_manual_usage").Where("request_id=?", request).Count(&beforeAudit).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Count(&beforeInbox).Error; err != nil {
				t.Fatal(err)
			}
			if beforeAudit != 0 || beforeInbox != 0 {
				t.Fatalf("audit rollback=%d inbox rollback=%d", beforeAudit, beforeInbox)
			}
			wrongTenant := resolution
			wrongTenant.TenantID++
			if err := recordManualUsage(ctx, db, wrongTenant); err == nil {
				t.Fatal("cross-tenant resolution accepted")
			}
			wrongProvider := resolution
			wrongProvider.ProviderID++
			if err := recordManualUsage(ctx, db, wrongProvider); err == nil {
				t.Fatal("wrong provider accepted")
			}
			missingEvidence := resolution
			missingEvidence.EvidenceSHA = ""
			if err := recordManualUsage(ctx, db, missingEvidence); err == nil {
				t.Fatal("missing evidence accepted")
			}
			if os.Getenv("TEST_MANUAL_ACK_UNKNOWN") == "1" {
				err := recordManualUsage(ctx, recoveryDBWithCommitError(t, io.EOF), resolution)
				if !errors.Is(err, io.EOF) {
					t.Fatalf("expected simulated lost commit response: %v", err)
				}
				var auditCount, inboxCount int64
				if err := db.Table("eval_manual_usage").Where("request_id=?", request).Count(&auditCount).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Count(&inboxCount).Error; err != nil {
					t.Fatal(err)
				}
				if auditCount != 1 || inboxCount != 1 {
					t.Fatalf("committed audit=%d inbox=%d", auditCount, inboxCount)
				}
				t.Log("manual transaction committed; driver returned injected EOF; matching audit and inbox persisted")
			}
			if os.Getenv("TEST_MANUAL_KILL_SQL") == "1" {
				var killed atomic.Bool
				var killErr error
				if err := db.Callback().Create().After("gorm:create").Register("test:manual-kill-session", func(tx *gorm.DB) {
					if tx.Statement.Table != "eval_manual_usage" || !killed.CompareAndSwap(false, true) {
						return
					}
					var connectionID uint64
					if killErr = tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT CONNECTION_ID()").Scan(&connectionID).Error; killErr != nil {
						return
					}
					killErr = db.Exec(fmt.Sprintf("KILL CONNECTION %d", connectionID)).Error
				}); err != nil {
					t.Fatal(err)
				}
				err := recordManualUsage(ctx, db, resolution)
				db.Callback().Create().Remove("test:manual-kill-session")
				if err == nil || killErr != nil || !killed.Load() {
					t.Fatalf("manual write err=%v kill=%v happened=%v", err, killErr, killed.Load())
				}
				var auditCount, inboxCount int64
				if err := db.Table("eval_manual_usage").Where("request_id=?", request).Count(&auditCount).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Count(&inboxCount).Error; err != nil {
					t.Fatal(err)
				}
				if auditCount != 0 || inboxCount != 0 {
					t.Fatalf("failed commit audit=%d inbox=%d", auditCount, inboxCount)
				}
				t.Log("MySQL session killed after both writes before commit; audit and inbox rolled back")
			}
			if os.Getenv("TEST_MANUAL_PROCESS_KILL") == "1" {
				encoded, err := json.Marshal(resolution)
				if err != nil {
					t.Fatal(err)
				}
				worker := exec.Command(os.Args[0], "-test.run=^TestManualResolutionProcessChild$", "-test.timeout=30s")
				worker.Env = append(os.Environ(), "MANUAL_RESOLUTION_CHILD=1", "MANUAL_RESOLUTION_INPUT="+string(encoded))
				pipe, err := worker.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				worker.Stderr = os.Stderr
				if err := worker.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if worker.ProcessState == nil {
						_ = worker.Process.Kill()
						_ = worker.Wait()
					}
				})
				line, err := bufio.NewReader(pipe).ReadString('\n')
				if err != nil || strings.TrimSpace(line) != "MANUAL_RESOLUTION_COMMITTED" {
					t.Fatalf("manual child=%q err=%v", line, err)
				}
				if err := worker.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				_ = worker.Wait()
				t.Log("manual worker killed after commit before settlement")
			}
			var wg sync.WaitGroup
			errs := make(chan error, 8)
			start := make(chan struct{})
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); <-start; errs <- recordManualUsage(ctx, db, resolution) }()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			conflict := resolution
			conflict.CompletionTokens++
			if err := recordManualUsage(ctx, db, conflict); !errors.Is(err, errManualConflict) {
				t.Fatalf("conflict=%v", err)
			}
			actorConflict := resolution
			actorConflict.Actor = "other-reviewer"
			if err := recordManualUsage(ctx, db, actorConflict); !errors.Is(err, errManualConflict) {
				t.Fatalf("actor conflict=%v", err)
			}
			var audits, inbox int64
			if err := db.Table("eval_manual_usage").Where("request_id=?", request).Count(&audits).Error; err != nil || audits != 1 {
				t.Fatal(audits, err)
			}
			if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Count(&inbox).Error; err != nil || inbox != 1 {
				t.Fatal(inbox, err)
			}
			if os.Getenv("TEST_MANUAL_LATE_USAGE") == "1" {
				err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: request, BillId: int64(bill.ID), ProviderId: int64(provider.ID), PromptTokens: 40, CompletionTokens: 3})
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("contradictory late usage accepted: %v", err)
				}
				if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.UsageReportedAt != nil {
					t.Fatalf("usage changed before replay: %+v err=%v", bill, err)
				}
				t.Log("late contradictory usage rejected before inbox replay")
			}
			if os.Getenv("TEST_MANUAL_REDIS_CRASH") == "1" {
				container := os.Getenv("COMPARISON_OWNED_REDIS_CONTAINER")
				if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") {
					t.Fatal("isolated Redis container required")
				}
				port, err := exec.Command("docker", "port", container, "6379/tcp").Output()
				if err != nil {
					t.Fatal(err)
				}
				beforePort := strings.TrimSpace(string(port))
				if output, err := exec.Command("docker", "kill", container).CombinedOutput(); err != nil {
					t.Fatalf("kill isolated Redis: %v %s", err, output)
				}
				t.Cleanup(func() { _ = exec.Command("docker", "start", container).Run() })
				attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				replayErr := replayUsageInbox(attemptCtx, db, s, request)
				cancel()
				if replayErr == nil {
					t.Fatal("replay unexpectedly settled while Redis down")
				}
				var afterFailure store.Bill
				if err := db.Where("id=?", bill.ID).Take(&afterFailure).Error; err != nil || afterFailure.Status != store.StatusPending || afterFailure.UsageReportedAt == nil {
					t.Fatalf("bill after Redis loss status=%s usage=%v err=%v", afterFailure.Status, afterFailure.UsageReportedAt, err)
				}
				var stored inboxUsage
				if err := db.Table("eval_usage_inbox").Where("request_id=?", request).Take(&stored).Error; err != nil || stored.State != "pending" {
					t.Fatal(stored, err)
				}
				var settlementCount int64
				if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", request, store.PhaseSettle).Count(&settlementCount).Error; err != nil || settlementCount != 0 {
					t.Fatal(settlementCount, err)
				}
				var usageEvents, reverseEvents int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":usage").Count(&usageEvents).Error; err != nil || usageEvents != 1 {
					t.Fatal(usageEvents, err)
				}
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 0 {
					t.Fatal(reverseEvents, err)
				}
				if output, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
					t.Fatalf("restart isolated Redis: %v %s", err, output)
				}
				ready := false
				for i := 0; i < 30; i++ {
					if exec.Command("docker", "exec", container, "redis-cli", "ping").Run() == nil {
						ready = true
						break
					}
					time.Sleep(time.Second)
				}
				if !ready {
					t.Fatal("Redis restart timeout")
				}
				port, err = exec.Command("docker", "port", container, "6379/tcp").Output()
				if err != nil || strings.TrimSpace(string(port)) != beforePort {
					t.Fatalf("Redis port changed: %q %q %v", beforePort, port, err)
				}
				exists, err := r.Exists(ctx, store.BalanceKey(tenant.ID)).Result()
				if err != nil {
					t.Fatal(err)
				}
				rebuilt, err := s.RebuildBalance(ctx, tenant.ID)
				if err != nil || rebuilt != 499 {
					t.Fatalf("rebuild=%d err=%v", rebuilt, err)
				}
				t.Logf("Redis killed: usage durable and settlement pending; key_exists_after_restart=%d; rebuilt from ledger", exists)
			}
			if err := replayUsageInbox(ctx, db, s, request); err != nil {
				t.Fatal(err)
			}
			if err := replayUsageInbox(ctx, db, s, request); err != nil {
				t.Fatal(err)
			}
			if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.UsageReportedAt == nil {
				t.Fatal(bill.Status, err)
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != 458 || ledger != live {
				t.Fatal(live, ledger, err)
			}
			var finalEvents, settleRows int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{request + ":reserve", request + ":usage", request + ":settle"}).Count(&finalEvents).Error; err != nil || finalEvents != 3 {
				t.Fatal(finalEvents, err)
			}
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", request, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 1 {
				t.Fatal(settleRows, err)
			}
			t.Log("synthetic manual record accepted idempotently; conflict rejected; one audit and settlement; balance=458")
		}
		return
	}
	x, err := readLocalUsage(dir, request)
	if err != nil || x.PromptTokens != 40 || x.CompletionTokens != 2 {
		t.Fatalf("local=%+v err=%v", x, err)
	}
	var before store.Bill
	if err := db.Where("request_id=? AND phase=?", request, store.PhaseReserve).Take(&before).Error; err != nil || before.Status != store.StatusPending || before.UsageReportedAt != nil {
		t.Fatalf("before status=%s usage=%v err=%v", before.Status, before.UsageReportedAt, err)
	}
	result, err := scanLocalUsage(ctx, db, s, dir)
	if err != nil || result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("scan=%+v err=%v", result, err)
	}
	if result, err := scanLocalUsage(ctx, db, s, dir); err != nil || result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("repeat scan=%+v err=%v", result, err)
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", request, store.PhaseReserve).Take(&bill).Error; err != nil || bill.Status != store.StatusSettled {
		t.Fatal(bill.Status, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{request + ":reserve", request + ":usage", request + ":settle"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatal(events, err)
	}
	var reverseEvents, settleRows int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", request+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 0 {
		t.Fatal(reverseEvents, err)
	}
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", request, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 1 {
		t.Fatal(settleRows, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 458 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("handler_killed_after_journal=true mock_calls=1 scanner_success=1 settled=true events=%d balance=%d ledger=%d", events, live, ledger)
}
