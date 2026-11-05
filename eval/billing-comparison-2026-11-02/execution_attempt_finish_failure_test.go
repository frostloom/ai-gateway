package comparison

import (
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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAttemptOutboxFailureAfterUpstreamResponse(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental overlays")
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
	attemptDB := setupAttemptDB(t)
	if err := attemptDB.Exec("CREATE TABLE IF NOT EXISTS eval_attempt_request (tenant_id BIGINT UNSIGNED NOT NULL,request_id VARBINARY(128) NOT NULL,fingerprint VARBINARY(64) NOT NULL,PRIMARY KEY(tenant_id,request_id)) ENGINE=InnoDB").Error; err != nil {
		t.Fatal(err)
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
	bridge := &guardedAttemptBridge{gatewayExecutionBridge: &gatewayExecutionBridge{store: s, atomicReserve: candidate, conn: conn, ownerHash: hex.EncodeToString(owner[:])}, db: attemptDB}
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			id := fmt.Sprintf("attempt-outbox-fail-%t-%d", stream, time.Now().UnixNano())
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
			var injected atomic.Int32
			if err := attemptDB.Callback().Create().Before("gorm:create").Register("test:attempt_outbox_failure", func(tx *gorm.DB) {
				if os.Getenv("TEST_FINISH_NORMAL") != "1" && tx.Statement.Table == "eval_attempt_outbox" && injected.CompareAndSwap(0, 1) {
					tx.AddError(errors.New("synthetic attempt outbox failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { attemptDB.Callback().Create().Remove("test:attempt_outbox_failure") })
			if os.Getenv("TEST_USAGE_FAIL_AFTER_FINISH_FAIL") == "1" {
				if err := db.Callback().Create().Before("gorm:create").Register("test:usage_outbox_failure", func(tx *gorm.DB) {
					if ev, ok := tx.Statement.Dest.(*store.OutboxEvent); ok && ev.EventKey == id+"-request:usage" {
						tx.AddError(errors.New("synthetic usage outbox failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Callback().Create().Remove("test:usage_outbox_failure") })
			}
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				calls.Add(1)
				if stream {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n")
				} else {
					_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`)
				}
			}))
			defer up.Close()
			provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]", Weight: 1}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			var requestBilling executionBillingAPI = bridge
			var autoJournalDir string
			if os.Getenv("TEST_AUTO_COMPLETION_JOURNAL") == "1" {
				autoJournalDir = t.TempDir()
				if os.Getenv("TEST_AUTO_JOURNAL_MISSING_DIR") == "1" {
					autoJournalDir = filepath.Join(autoJournalDir, "missing")
				}
				requestBilling = &autoCompletionJournalBridge{guardedAttemptBridge: bridge, dir: autoJournalDir, tenant: tenant.ID}
			}
			engine := gin.New()
			engine.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set("tenant_id", int64(tenant.ID))
				c.Set("api_key_id", int64(1))
				c.Set("request_id", id+"-request")
			}, handler.Chat(requestBilling, gatewayExecutionRouter{url: up.URL, providerID: provider.ID}, slog.New(slog.DiscardHandler), time.Second))
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":%t}`, id, stream)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			response, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			wanted := 503
			if stream || os.Getenv("TEST_FINISH_NONBLOCKING") == "1" || os.Getenv("TEST_FINISH_NORMAL") == "1" {
				wanted = 200
			}
			wantInjected := int32(1)
			if os.Getenv("TEST_FINISH_NORMAL") == "1" {
				wantInjected = 0
			}
			if resp.StatusCode != wanted || calls.Load() != 1 || injected.Load() != wantInjected || strings.Contains(string(response), "[DONE]") != stream {
				t.Fatalf("status=%d calls=%d injection=%d body=%q", resp.StatusCode, calls.Load(), injected.Load(), string(response))
			}
			var attempt attemptRow
			wantAttempt, wantAttemptEvents := "sending", int64(0)
			if os.Getenv("TEST_FINISH_NORMAL") == "1" {
				wantAttempt, wantAttemptEvents = "completed", 1
			}
			if err := attemptDB.Table("eval_route_attempt").Where("tenant_id=? AND request_id=?", tenant.ID, id+"-request").Take(&attempt).Error; err != nil || attempt.State != wantAttempt {
				t.Fatal(attempt, err)
			}
			if os.Getenv("TEST_FINISH_NORMAL") == "1" && (attempt.Outcome == nil || !*attempt.Outcome) {
				t.Fatalf("unexpected outcome=%v", attempt.Outcome)
			}
			var attemptEvents int64
			if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attempt.AttemptID).Count(&attemptEvents).Error; err != nil || attemptEvents != wantAttemptEvents {
				t.Fatal(attemptEvents, err)
			}
			var bill store.Bill
			wantBill := store.StatusPending
			if os.Getenv("TEST_FINISH_NONBLOCKING") == "1" && os.Getenv("TEST_USAGE_FAIL_AFTER_FINISH_FAIL") != "1" {
				wantBill = store.StatusSettled
			}
			if err := db.Where("request_id=? AND phase=?", id+"-request", store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != wantBill || (bill.UsageReportedAt != nil) != (wantBill == store.StatusSettled) {
				t.Fatal(bill.Status, err)
			}
			for i, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				want := int64(0)
				if i == 0 || (wantBill == store.StatusSettled && (i == 1 || i == 2)) {
					want = 1
				}
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+"-request:"+phase).Count(&n).Error; err != nil || n != want {
					t.Fatal(phase, n, want, err)
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
			if os.Getenv("TEST_USAGE_FAIL_AFTER_FINISH_FAIL") == "1" {
				var evidence string
				if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil || evidence != "" {
					t.Fatalf("unexpected completion evidence=%q err=%v", evidence, err)
				}
				if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != 0 {
					t.Fatalf("recovery without persisted evidence=%d err=%v", n, err)
				}
				if os.Getenv("TEST_AUTO_JOURNAL_MISSING_DIR") == "1" {
					if _, err := readCompletionJournal(autoJournalDir, id+"-request"); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("unexpected local journal: %v", err)
					}
					t.Log("local journal unavailable; bill remains pending without persisted usage")
				}
				t.Log("completion and usage not persisted: recovery cannot infer completion")
				if os.Getenv("TEST_JOURNAL_EVIDENCE_V2") == "1" {
					dir := autoJournalDir
					if dir == "" {
						dir = t.TempDir()
					}
					x := completionJournalRecord{Usage: inboxUsage{RequestID: id + "-request", TenantID: tenant.ID, BillID: int64(bill.ID), ProviderID: int64(provider.ID), PromptTokens: 40}, AttemptID: attempt.AttemptID}
					if os.Getenv("TEST_AUTO_COMPLETION_JOURNAL") == "1" {
						if actual, err := readCompletionJournal(dir, x.Usage.RequestID); err != nil || actual != x {
							t.Fatalf("automatic journal=%+v err=%v", actual, err)
						}
					} else {
						if err := saveCompletionJournal(dir, x); err != nil {
							t.Fatal(err)
						}
					}
					if err := saveCompletionJournal(dir, x); err != nil {
						t.Fatalf("same record: %v", err)
					}
					conflict := x
					conflict.Usage.PromptTokens++
					if err := saveCompletionJournal(dir, conflict); !errors.Is(err, errLocalUsageConflict) {
						t.Fatalf("conflicting record: %v", err)
					}
					wrongDir := t.TempDir()
					wrong := x
					wrong.AttemptID = strings.Repeat("a", 32)
					if err := saveCompletionJournal(wrongDir, wrong); err != nil {
						t.Fatal(err)
					}
					if err := db.Callback().Create().Remove("test:usage_outbox_failure"); err != nil {
						t.Fatal(err)
					}
					if err := replayCompletionJournal(ctx, db, s, wrongDir, wrong.Usage.RequestID); err == nil {
						t.Fatal("wrong attempt ID accepted")
					}
					wrongTenantDir := t.TempDir()
					wrongTenant := x
					wrongTenant.Usage.TenantID++
					if err := saveCompletionJournal(wrongTenantDir, wrongTenant); err != nil {
						t.Fatal(err)
					}
					if err := replayCompletionJournal(ctx, db, s, wrongTenantDir, wrongTenant.Usage.RequestID); err == nil {
						t.Fatal("wrong tenant accepted")
					}
					if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.UsageReportedAt != nil {
						t.Fatalf("invalid replay changed bill=%+v err=%v", bill, err)
					}
					if err := replayCompletionJournal(ctx, db, s, dir, x.Usage.RequestID); err != nil {
						t.Fatal(err)
					}
					if err := replayCompletionJournal(ctx, db, s, dir, x.Usage.RequestID); err != nil {
						t.Fatalf("replay: %v", err)
					}
					if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.UsageReportedAt == nil {
						t.Fatalf("bill=%+v err=%v", bill, err)
					}
					if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil || evidence != attempt.AttemptID {
						t.Fatalf("evidence=%q attempt=%q err=%v", evidence, attempt.AttemptID, err)
					}
					if err := attemptDB.Callback().Create().Remove("test:attempt_outbox_failure"); err != nil {
						t.Fatal(err)
					}
					if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != 1 {
						t.Fatalf("attempt recovery=%d err=%v", n, err)
					}
					if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != 0 {
						t.Fatalf("attempt replay=%d err=%v", n, err)
					}
					if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attempt.AttemptID).Take(&attempt).Error; err != nil || attempt.State != "completed" {
						t.Fatalf("attempt=%+v err=%v", attempt, err)
					}
					if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attempt.AttemptID).Count(&attemptEvents).Error; err != nil || attemptEvents != 1 {
						t.Fatalf("attempt events=%d err=%v", attemptEvents, err)
					}
					live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
					if err != nil {
						t.Fatal(err)
					}
					ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
					if err != nil || live != ledger || live != 960 {
						t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
					}
					t.Log("evidence journal replay: settled once, completion identity retained, one attempt event")
				}
				if os.Getenv("TEST_JOURNAL_EVIDENCE_GAP") == "1" {
					if err := db.Exec(`CREATE TABLE IF NOT EXISTS eval_usage_inbox (request_id VARBINARY(128) PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,bill_id BIGINT NOT NULL,provider_id BIGINT NOT NULL,prompt_tokens BIGINT NOT NULL,completion_tokens BIGINT NOT NULL,state VARCHAR(16) NOT NULL) ENGINE=InnoDB`).Error; err != nil {
						t.Fatal(err)
					}
					dir := t.TempDir()
					x := inboxUsage{RequestID: id + "-request", TenantID: tenant.ID, BillID: int64(bill.ID), ProviderID: int64(provider.ID), PromptTokens: 40, CompletionTokens: 0}
					if err := saveLocalUsage(dir, x); err != nil {
						t.Fatal(err)
					}
					if err := db.Callback().Create().Remove("test:usage_outbox_failure"); err != nil {
						t.Fatal(err)
					}
					result, err := scanLocalUsage(ctx, db, s, dir)
					if err != nil || result.Succeeded != 1 || result.Failed != 0 {
						t.Fatalf("journal replay=%+v err=%v", result, err)
					}
					if err := db.Where("id=?", bill.ID).Take(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.UsageReportedAt == nil {
						t.Fatalf("after journal bill=%+v err=%v", bill, err)
					}
					if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil || evidence != "" {
						t.Fatalf("journal unexpectedly supplied completion evidence=%q err=%v", evidence, err)
					}
					if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != 0 {
						t.Fatalf("unexpected recovery without evidence=%d err=%v", n, err)
					}
					live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
					if err != nil {
						t.Fatal(err)
					}
					ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
					if err != nil || live != ledger || live != 960 {
						t.Fatalf("after journal balance=%d ledger=%d err=%v", live, ledger, err)
					}
					t.Log("journal replay settled usage but omitted completion identity; attempt remains sending")
				}
			}
			if os.Getenv("TEST_VERIFY_COMPLETION_RECOVERY") == "1" {
				if os.Getenv("TEST_COMPLETION_EVIDENCE") != "1" || os.Getenv("TEST_FINISH_NONBLOCKING") != "1" || wantBill != store.StatusSettled {
					t.Fatal("completion evidence and nonblocking failure are required")
				}
				var evidence string
				if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil || evidence != attempt.AttemptID {
					t.Fatalf("evidence=%q attempt=%q err=%v", evidence, attempt.AttemptID, err)
				}
				if err := attemptDB.Callback().Create().Remove("test:attempt_outbox_failure"); err != nil {
					t.Fatal(err)
				}
				recovered, err := experimentalRecoverCompletedAttempts(ctx, db)
				if err != nil || recovered != 1 {
					t.Fatalf("recovery=%d err=%v", recovered, err)
				}
				if n, err := experimentalRecoverCompletedAttempts(ctx, db); err != nil || n != 0 {
					t.Fatalf("recovery replay=%d err=%v", n, err)
				}
				if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attempt.AttemptID).Take(&attempt).Error; err != nil || attempt.State != "completed" || attempt.Outcome == nil || !*attempt.Outcome {
					t.Fatalf("recovered attempt=%+v err=%v", attempt, err)
				}
				if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attempt.AttemptID).Count(&attemptEvents).Error; err != nil || attemptEvents != 1 {
					t.Fatalf("recovered attempt events=%d err=%v", attemptEvents, err)
				}
				t.Log("completion evidence persisted; explicit recovery applied exactly once")
			}
			t.Logf("stream=%v status=%d done_seen=%v attempt=%s attempt_events=%d bill=%s usage=%v balance=%d ledger=%d", stream, resp.StatusCode, strings.Contains(string(response), "[DONE]"), attempt.State, attemptEvents, bill.Status, bill.UsageReportedAt != nil, live, ledger)
		})
	}
}
