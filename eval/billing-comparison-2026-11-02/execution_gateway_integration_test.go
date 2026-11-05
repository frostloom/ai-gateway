package comparison

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql/driver"
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
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Test-only bridge: reserves and acquires a conservative send permission
// before returning to the real handler. It is not a production billing RPC.
type gatewayExecutionBridge struct {
	store         *store.Store
	atomicReserve experimentalAtomicReserve
	conn          *grpc.ClientConn
	ownerHash     string
}

type executionBillingAPI interface {
	Reserve(context.Context, *pb.ReserveRequest) (*pb.ReserveResponse, error)
	ReportUsage(context.Context, *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error)
	Settle(context.Context, *pb.SettleRequest) (*pb.SettleResponse, error)
	Reverse(context.Context, *pb.ReverseRequest) (*pb.ReverseResponse, error)
}

// Injects a failure before writing usage. Capture is process memory only.
type failedUsageBridge struct {
	executionBillingAPI
	observed *pb.ReportUsageRequest
}

type inboxFailureBridge struct {
	executionBillingAPI
	db         *gorm.DB
	tenant     uint64
	localDir   string
	journalErr error
}

// Test-only fail-closed classification: unfamiliar errors are not replayed.
func inboxConnectionFailure(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysqldriver.ErrInvalidConn) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func TestInboxConnectionFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"wrapped_connection", fmt.Errorf("outer: %w", driver.ErrBadConn), true},
		{"deadline", context.DeadlineExceeded, true},
		{"socket", &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}, true},
		{"conflict", errInboxConflict, false},
		{"cancelled", context.Canceled, false},
		{"not_found", gorm.ErrRecordNotFound, false},
		{"mysql_denied", &mysqldriver.MySQLError{Number: 1045, Message: "access denied"}, false},
		{"unknown", errors.New("unknown"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inboxConnectionFailure(tc.err); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func (b *inboxFailureBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	x := inboxUsage{RequestID: req.RequestId, TenantID: b.tenant, BillID: req.BillId, ProviderID: req.ProviderId, PromptTokens: req.PromptTokens, CompletionTokens: req.CompletionTokens}
	if err := putUsageInbox(ctx, b.db, x); err != nil {
		if b.localDir != "" && inboxConnectionFailure(err) {
			b.journalErr = saveLocalUsage(b.localDir, x)
		}
		return nil, err
	}
	return nil, status.Error(codes.Unavailable, "injected usage unavailable")
}

func (b *failedUsageBridge) ReportUsage(_ context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	b.observed = &pb.ReportUsageRequest{RequestId: req.RequestId, BillId: req.BillId, ProviderId: req.ProviderId, PromptTokens: req.PromptTokens, CompletionTokens: req.CompletionTokens}
	return nil, status.Error(codes.Unavailable, "injected usage unavailable")
}

func (b *gatewayExecutionBridge) Reserve(ctx context.Context, req *pb.ReserveRequest) (*pb.ReserveResponse, error) {
	resp, err := b.atomicReserve.ReserveWithExecutionIntent(ctx, req, b.ownerHash, 30*time.Second)
	if err != nil || resp.Code != pb.ReserveCode_RESERVE_OK {
		return resp, err
	}
	var permit wrapperspb.Int64Value
	if err := b.conn.Invoke(ctx, "/eval.execution.v1.Execution/Transition", wrapperspb.String(req.RequestId+"|forwarding|synthetic-owner"), &permit); err != nil {
		return nil, err
	}
	if permit.Value != 1 {
		return nil, status.Error(codes.FailedPrecondition, "no forwarding permission")
	}
	return resp, nil
}
func (b *gatewayExecutionBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	if err := b.store.ReportUsage(ctx, req); err != nil {
		return nil, err
	}
	return &pb.ReportUsageResponse{Accepted: true}, nil
}
func (b *gatewayExecutionBridge) Settle(ctx context.Context, req *pb.SettleRequest) (*pb.SettleResponse, error) {
	return b.store.Settle(ctx, req)
}
func (b *gatewayExecutionBridge) Reverse(ctx context.Context, req *pb.ReverseRequest) (*pb.ReverseResponse, error) {
	return b.store.Reverse(ctx, req)
}

type gatewayExecutionRouter struct {
	url        string
	providerID uint64
}

func (r gatewayExecutionRouter) Route(context.Context, string, ...uint64) (*gatewayclient.RouteResult, error) {
	return &gatewayclient.RouteResult{ProviderID: r.providerID, BaseURL: r.url}, nil
}
func (r gatewayExecutionRouter) Report(context.Context, uint64, bool) {}

func TestRealGatewayWithExecutionBridge(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental billing and gateway overlays")
	}
	gin.SetMode(gin.TestMode)
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate missing")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	server.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	owner := sha256.Sum256([]byte("synthetic-owner"))
	bridge := &gatewayExecutionBridge{store: s, atomicReserve: candidate, conn: conn, ownerHash: hex.EncodeToString(owner[:])}
	var billing executionBillingAPI = bridge
	var attemptDB *gorm.DB
	if os.Getenv("TEST_ATTEMPT_GUARD") == "1" {
		attemptDB = setupAttemptDB(t)
		if err := attemptDB.Exec("CREATE TABLE IF NOT EXISTS eval_attempt_request (tenant_id BIGINT UNSIGNED NOT NULL,request_id VARBINARY(128) NOT NULL,fingerprint VARBINARY(64) NOT NULL,PRIMARY KEY(tenant_id,request_id)) ENGINE=InnoDB").Error; err != nil {
			t.Fatal(err)
		}
		billing = &guardedAttemptBridge{gatewayExecutionBridge: bridge, db: attemptDB}
	}
	modes := []string{"complete", "truncated", "stream_complete", "stream_partial", "stream_empty"}
	if os.Getenv("TEST_LOST_KNOWN_USAGE") == "1" {
		modes = []string{"usage_failed", "stream_usage_failed"}
	}
	if os.Getenv("TEST_HANDLER_INBOX") == "1" {
		if err := db.Exec(`CREATE TABLE IF NOT EXISTS eval_usage_inbox (request_id VARBINARY(128) PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,bill_id BIGINT NOT NULL,provider_id BIGINT NOT NULL,prompt_tokens BIGINT NOT NULL,completion_tokens BIGINT NOT NULL,state VARCHAR(16) NOT NULL) ENGINE=InnoDB`).Error; err != nil {
			t.Fatal(err)
		}
		modes = []string{"usage_failed", "stream_usage_failed"}
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("gw-execution-%s-%d", mode, time.Now().UnixNano())
			model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			tenant := store.Tenant{Name: id, InitialQuota: 1000}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				calls.Add(1)
				if mode == "stream_complete" || mode == "stream_usage_failed" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
					return
				}
				if mode == "stream_partial" || mode == "stream_empty" {
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("Content-Length", "200")
					w.WriteHeader(200)
					if mode == "stream_partial" {
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
					} else {
						_, _ = io.WriteString(w, "data: {\"choices\":[")
					}
					return
				}
				if mode == "truncated" {
					w.Header().Set("Content-Length", "200")
					w.WriteHeader(200)
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"partial`)
					return
				}
				_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`)
			}))
			defer up.Close()
			provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]", Weight: 1}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			stream := strings.HasPrefix(mode, "stream_")
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":%t}`, id, stream)))
			c.Set("tenant_id", int64(tenant.ID))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", id+"-request")
			requestBilling := billing
			var failed *failedUsageBridge
			var localBridge *inboxFailureBridge
			if os.Getenv("TEST_LOST_KNOWN_USAGE") == "1" {
				failed = &failedUsageBridge{executionBillingAPI: billing}
				requestBilling = failed
			}
			if os.Getenv("TEST_HANDLER_INBOX") == "1" {
				localBridge = &inboxFailureBridge{executionBillingAPI: billing, db: db, tenant: tenant.ID}
				if os.Getenv("TEST_HANDLER_LOCAL_JOURNAL") == "1" {
					localBridge.localDir = t.TempDir()
					if os.Getenv("TEST_HANDLER_LOCAL_FAIL") == "1" {
						localBridge.localDir = filepath.Join(localBridge.localDir, "missing-directory")
					}
				}
				requestBilling = localBridge
			}
			if os.Getenv("TEST_HANDLER_INBOX_WRITE_FAIL") == "1" {
				if err := db.Callback().Create().Before("gorm:create").Register("test:inbox-write-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "eval_usage_inbox" {
						tx.AddError(io.ErrUnexpectedEOF)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Callback().Create().Remove("test:inbox-write-failure") })
			}
			var dbKillSucceeded atomic.Bool
			var mysqlPortBefore string
			if os.Getenv("TEST_HANDLER_INBOX_DB_KILL") == "1" {
				container := os.Getenv("COMPARISON_OWNED_MYSQL_CONTAINER")
				if !strings.HasPrefix(container, "auth-cache-test-mysql-") || strings.ContainsAny(container, "/ ") {
					t.Fatal("isolated MySQL container required")
				}
				port, err := exec.Command("docker", "port", container, "3306/tcp").Output()
				if err != nil {
					t.Fatal(err)
				}
				mysqlPortBefore = strings.TrimSpace(string(port))
				var killed atomic.Bool
				if err := db.Callback().Create().Before("gorm:create").Register("test:inbox-db-kill", func(tx *gorm.DB) {
					if tx.Statement.Table == "eval_usage_inbox" && killed.CompareAndSwap(false, true) {
						if output, err := exec.Command("docker", "kill", container).CombinedOutput(); err != nil {
							tx.AddError(fmt.Errorf("kill isolated mysql: %v %s", err, output))
						} else {
							dbKillSucceeded.Store(true)
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Callback().Create().Remove("test:inbox-db-kill") })
				t.Cleanup(func() {
					if killed.Load() {
						_ = exec.Command("docker", "start", container).Run()
					}
				})
			}
			conservative := os.Getenv("TEST_GATEWAY_CONSERVATIVE") == "1"
			handler.Chat(requestBilling, gatewayExecutionRouter{url: up.URL, providerID: provider.ID}, slog.New(slog.DiscardHandler), time.Second, conservative)(c)
			if os.Getenv("TEST_HANDLER_INBOX_WRITE_FAIL") == "1" {
				db.Callback().Create().Remove("test:inbox-write-failure")
			}
			if os.Getenv("TEST_HANDLER_INBOX_DB_KILL") == "1" {
				db.Callback().Create().Remove("test:inbox-db-kill")
				if !dbKillSucceeded.Load() {
					t.Fatal("did not kill isolated MySQL")
				}
				container := os.Getenv("COMPARISON_OWNED_MYSQL_CONTAINER")
				if output, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
					t.Fatalf("restart mysql: %v %s", err, output)
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
					t.Fatal("isolated mysql failed to restart")
				}
				port, err := exec.Command("docker", "port", container, "3306/tcp").Output()
				if err != nil || strings.TrimSpace(string(port)) != mysqlPortBefore {
					t.Fatalf("mysql port changed before=%q after=%q err=%v", mysqlPortBefore, strings.TrimSpace(string(port)), err)
				}
			}
			expected := 200
			if mode == "truncated" || mode == "stream_empty" {
				expected = 502
			}
			if recorder.Code != expected || calls.Load() != 1 {
				t.Fatal(recorder.Code, calls.Load(), recorder.Body.String())
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id+"-request", store.PhaseReserve).First(&bill).Error; err != nil {
				t.Fatal(err)
			}
			if attemptDB != nil {
				var attempts []attemptRow
				wantState := "sending"
				if os.Getenv("TEST_ATTEMPT_FINISH") == "1" && (mode == "complete" || mode == "stream_complete") {
					wantState = "completed"
				}
				if err := attemptDB.Table("eval_route_attempt").Where("tenant_id=? AND request_id=?", tenant.ID, id+"-request").Find(&attempts).Error; err != nil || len(attempts) != 1 || attempts[0].State != wantState {
					t.Fatal(attempts, err)
				}
				var attemptEvents int64
				wantEvents := int64(0)
				if wantState == "completed" {
					wantEvents = 1
				}
				if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attempts[0].AttemptID).Count(&attemptEvents).Error; err != nil || attemptEvents != wantEvents {
					t.Fatalf("attempt events=%d want=%d err=%v", attemptEvents, wantEvents, err)
				}
			}
			settledMode := mode == "complete" || mode == "stream_complete" || (mode == "stream_partial" && !conservative)
			if settledMode {
				if bill.Status != store.StatusSettled || bill.UsageReportedAt == nil {
					t.Fatal(bill.Status, bill.UsageReportedAt)
				}
				if mode == "stream_partial" && (bill.CompletionTokens == nil || *bill.CompletionTokens <= 0 || !strings.Contains(recorder.Body.String(), "hello")) {
					t.Fatalf("partial stream usage=%v body=%q", bill.CompletionTokens, recorder.Body.String())
				}
				if mode == "stream_complete" && (bill.CompletionTokens == nil || *bill.CompletionTokens != 2) {
					t.Fatalf("complete stream usage=%v", bill.CompletionTokens)
				}
			} else {
				if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
					t.Fatal(bill.Status, bill.UsageReportedAt)
				}
				if err := db.Model(&store.Bill{}).Where("id=?", bill.ID).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = db.Model(&store.Bill{}).Where("id=?", bill.ID).UpdateColumn("created_at", time.Now()).Error
				})
				if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id=?", bill.ID).Error; err != nil {
					t.Fatal(err)
				}
				runner := reconciler.New(db, &stateSweepBridge{db: db, conn: conn, s: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
				stats, err := runner.SweepOnce(ctx)
				if err != nil || stats.Scanned != 1 || stats.Reversed != 0 {
					t.Fatal(stats, err)
				}
				if err := db.Where("id=?", bill.ID).First(&bill).Error; err != nil || bill.Status != store.StatusPending {
					t.Fatal(bill.Status, err)
				}
				if failed != nil {
					if failed.observed == nil || failed.observed.PromptTokens != 40 {
						t.Fatalf("captured usage=%v", failed.observed)
					}
					if err := s.ReportUsage(ctx, failed.observed); err != nil {
						t.Fatal(err)
					}
					settle, err := s.Settle(ctx, &pb.SettleRequest{RequestId: failed.observed.RequestId, BillId: failed.observed.BillId})
					if err != nil || !settle.Settled {
						t.Fatal(settle, err)
					}
					if err := db.Where("id=?", bill.ID).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
						t.Fatal(bill.Status, err)
					}
					settledMode = true
					t.Logf("mode=%s HTTP=200 usage_unavailable=true sweep_reverse=0 pending_preserved=true memory_replay_settled=true", mode)
				}
				if os.Getenv("TEST_HANDLER_INBOX") == "1" {
					if os.Getenv("TEST_HANDLER_INBOX_WRITE_FAIL") == "1" || os.Getenv("TEST_HANDLER_INBOX_DB_KILL") == "1" {
						var count int64
						if err := db.Table("eval_usage_inbox").Where("request_id=?", id+"-request").Count(&count).Error; err != nil || count != 0 {
							t.Fatal(count, err)
						}
						if os.Getenv("TEST_HANDLER_LOCAL_JOURNAL") == "1" && os.Getenv("TEST_HANDLER_LOCAL_FAIL") == "1" {
							if localBridge == nil || localBridge.journalErr == nil {
								t.Fatal("expected local journal failure")
							}
							t.Logf("mode=%s inbox_and_local_failed=true pending_preserved=true", mode)
						} else if os.Getenv("TEST_HANDLER_LOCAL_JOURNAL") == "1" {
							if localBridge == nil {
								t.Fatal("local journal bridge missing")
							}
							if localBridge.journalErr != nil {
								t.Fatalf("journal write failed: %v", localBridge.journalErr)
							}
							x, err := readLocalUsage(localBridge.localDir, id+"-request")
							if err != nil || x.PromptTokens != 40 {
								t.Fatalf("local usage=%+v err=%v", x, err)
							}
							if err := putUsageInbox(ctx, db, x); err != nil {
								t.Fatal(err)
							}
							if err := replayUsageInbox(ctx, db, s, x.RequestID); err != nil {
								t.Fatal(err)
							}
							if err := db.Where("id=?", bill.ID).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
								t.Fatal(bill.Status, err)
							}
							settledMode = true
							t.Logf("mode=%s mysql_crashed=%t local_journal=true replay_settled=true", mode, os.Getenv("TEST_HANDLER_INBOX_DB_KILL") == "1")
						} else {
							t.Logf("mode=%s HTTP=200 inbox_write_failed=true inbox_rows=0 pending_preserved=true", mode)
						}
					} else {
						var inbox inboxUsage
						if err := db.Table("eval_usage_inbox").Where("request_id=?", id+"-request").Take(&inbox).Error; err != nil || inbox.State != "pending" || inbox.PromptTokens != 40 {
							t.Fatalf("missing durable usage: %+v err=%v", inbox, err)
						}
						if os.Getenv("TEST_HANDLER_INBOX_KILL") == "1" {
							worker := exec.Command(os.Args[0], "-test.run=^TestUsageInboxChild$", "-test.timeout=30s")
							worker.Env = append(os.Environ(), "USAGE_INBOX_CHILD=1", "USAGE_INBOX_STAGE=reported", "USAGE_INBOX_ID="+inbox.RequestID)
							pipe, err := worker.StdoutPipe()
							if err != nil {
								t.Fatal(err)
							}
							worker.Stderr = os.Stderr
							if err = worker.Start(); err != nil {
								t.Fatal(err)
							}
							t.Cleanup(func() {
								if worker.ProcessState == nil {
									_ = worker.Process.Kill()
									_ = worker.Wait()
								}
							})
							line, err := bufio.NewReader(pipe).ReadString('\n')
							if err != nil || strings.TrimSpace(line) != "USAGE_COMMITTED" {
								t.Fatalf("worker marker=%q err=%v", line, err)
							}
							if err := worker.Process.Kill(); err != nil {
								t.Fatal(err)
							}
							_ = worker.Wait()
							t.Log("replay worker killed after usage commit before settlement")
						}
						if err := replayUsageInbox(ctx, db, s, id+"-request"); err != nil {
							t.Fatal(err)
						}
						if err := db.Where("id=?", bill.ID).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
							t.Fatal(bill.Status, err)
						}
						settledMode = true
						t.Logf("mode=%s HTTP=200 durable_inbox=true sweep_reverse=0 replay_settled=true", mode)
					}
				}
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != ledger {
				t.Fatal(live, ledger, err)
			}
			keys := []string{id + "-request:reserve", id + "-request:usage", id + "-request:settle", id + "-request:reverse"}
			expectedEvents := int64(1)
			if settledMode {
				expectedEvents = 3
			}
			var events int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", keys).Count(&events).Error; err != nil || events != expectedEvents {
				t.Fatal(events, err)
			}
			for i, key := range keys {
				want := int64(0)
				if i == 0 || (settledMode && (i == 1 || i == 2)) {
					want = 1
				}
				var count int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", key).Count(&count).Error; err != nil || count != want {
					t.Fatalf("event %q count=%d want=%d err=%v", key, count, want, err)
				}
			}
			t.Logf("mode=%s status=%d calls=%d bill=%s balance=%d ledger=%d expected_events=%d", mode, recorder.Code, calls.Load(), bill.Status, live, ledger, events)
		})
	}
}
