package comparison

import (
	"bufio"
	"bytes"
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
	"strconv"
	"strings"
	"sync"
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

func TestCompletionRecoveryProcessChild(t *testing.T) {
	if os.Getenv("RECOVERY_PROCESS_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, _ := isolatedDB(t)
	fmt.Println("RECOVERY_READY")
	input := bufio.NewReader(os.Stdin)
	if _, err := input.ReadByte(); err != nil {
		t.Fatal(err)
	}
	n, err := experimentalRecoverCompletedAttemptsHook(context.Background(), db, func(count int) error {
		if count != 1 {
			return fmt.Errorf("expected one candidate, got %d", count)
		}
		fmt.Println("RECOVERY_SELECTED")
		_, err := input.ReadByte()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("RECOVERED=%d\n", n)
}

func TestCompletionRecoveryCommitDeathChild(t *testing.T) {
	if os.Getenv("RECOVERY_COMMIT_DEATH_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, _ := isolatedDB(t)
	n, err := experimentalRecoverCompletedAttempts(context.Background(), db)
	if err != nil || n != 1 {
		t.Fatalf("recovered=%d err=%v", n, err)
	}
	fmt.Println("RECOVERY_COMMITTED_BEFORE_RETURN")
	time.Sleep(time.Minute)
}

func killRecoveryAfterCommit(t *testing.T) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCompletionRecoveryCommitDeathChild$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "RECOVERY_COMMIT_DEATH_CHILD=1")
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "RECOVERY_COMMITTED_BEFORE_RETURN" {
		t.Fatalf("commit marker=%q err=%v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child unexpectedly exited normally")
	}
	t.Log("killed recovery process after committed transaction before return")
}

func recoverInProcesses(t *testing.T) int {
	t.Helper()
	type child struct {
		cmd    *exec.Cmd
		input  io.WriteCloser
		output *bufio.Reader
		stderr *bytes.Buffer
	}
	children := make([]child, 0, 4)
	t.Cleanup(func() {
		for _, c := range children {
			_ = c.input.Close()
			if c.cmd.ProcessState == nil {
				_ = c.cmd.Process.Kill()
				_ = c.cmd.Wait()
			}
		}
	})
	for i := 0; i < 4; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCompletionRecoveryProcessChild$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "RECOVERY_PROCESS_CHILD=1")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		c := child{cmd, input, bufio.NewReader(output), &stderr}
		children = append(children, c)
		line, err := c.output.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "RECOVERY_READY" {
			t.Fatalf("child readiness %q %v %s", line, err, c.stderr.String())
		}
	}
	for _, c := range children {
		if _, err := c.input.Write([]byte{'x'}); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range children {
		line, err := c.output.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "RECOVERY_SELECTED" {
			t.Fatalf("child selection %q %v %s", line, err, c.stderr.String())
		}
	}
	for _, c := range children {
		if _, err := c.input.Write([]byte{'x'}); err != nil {
			t.Fatal(err)
		}
		_ = c.input.Close()
	}
	total := 0
	for _, c := range children {
		line, err := c.output.ReadString('\n')
		if err != nil {
			t.Fatalf("child result %q %v %s", line, err, c.stderr.String())
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "RECOVERED=")))
		if err != nil || !strings.HasPrefix(line, "RECOVERED=") {
			t.Fatalf("invalid child result %q %v", line, err)
		}
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("child failed: %v %s", err, c.stderr.String())
		}
		total += n
	}
	return total
}

// Passing this characterization test means the candidate lacks enough durable
// evidence to infer attempt completion from billing state alone.
func TestSettledUsageCannotIdentifyCompletedStream(t *testing.T) {
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
	id := fmt.Sprintf("attempt-ambiguity-%d", time.Now().UnixNano())
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
	if err := attemptDB.Callback().Create().Before("gorm:create").Register("test:ambiguous_attempt_outbox", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_attempt_outbox" && injected.CompareAndSwap(0, 1) {
			tx.AddError(errors.New("synthetic attempt event failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { attemptDB.Callback().Create().Remove("test:ambiguous_attempt_outbox") })
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 2 {
			w.Header().Set("Content-Length", "200")
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		if n == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
		}
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]", Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("tenant_id", int64(tenant.ID))
		c.Set("api_key_id", int64(1))
		c.Set("request_id", id+"-"+c.GetHeader("X-Test-Request"))
	}, handler.Chat(bridge, gatewayExecutionRouter{url: up.URL, providerID: provider.ID}, slog.New(slog.DiscardHandler), time.Second))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	bodies := make([]string, 0, 2)
	client := &http.Client{Timeout: 10 * time.Second}
	var attemptIDs [2]string
	for i := 0; i < 2; i++ {
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":true}`, id)
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Request", fmt.Sprintf("r%d", i))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode, err)
		}
		bodies = append(bodies, string(data))
	}
	if calls.Load() != 2 || injected.Load() != 1 || !strings.Contains(bodies[0], "[DONE]") || strings.Contains(bodies[1], "[DONE]") {
		t.Fatal(calls.Load(), injected.Load(), bodies)
	}
	for i := 0; i < 2; i++ {
		rid := fmt.Sprintf("%s-r%d", id, i)
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.PromptTokens == nil || *bill.PromptTokens != 1 || bill.CompletionTokens == nil || *bill.CompletionTokens != 2 || bill.ProviderID == nil || *bill.ProviderID != provider.ID {
			t.Fatal(i, bill.Status, bill.PromptTokens, bill.CompletionTokens, err)
		}
		var attempt attemptRow
		if err := attemptDB.Table("eval_route_attempt").Where("tenant_id=? AND request_id=?", tenant.ID, rid).Take(&attempt).Error; err != nil || attempt.State != "sending" {
			t.Fatal(attempt, err)
		}
		attemptIDs[i] = attempt.AttemptID
		if os.Getenv("TEST_COMPLETION_EVIDENCE") == "1" {
			var evidence string
			if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil {
				t.Fatal(err)
			}
			want := ""
			if i == 0 {
				want = attempt.AttemptID
			}
			if evidence != want {
				t.Fatalf("case=%d evidence=%q want=%q", i, evidence, want)
			}
		}
		var n int64
		if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attempt.AttemptID).Count(&n).Error; err != nil || n != 0 {
			t.Fatal(n, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle"} {
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&n).Error; err != nil || n != 1 {
				t.Fatal(phase, n, err)
			}
		}
	}
	if os.Getenv("TEST_COMPLETION_RECOVERY") == "1" {
		if os.Getenv("TEST_COORDINATED_FINISH") == "1" {
			won, err := completeAttemptCoordinated(ctx, attemptDB, tenant.ID, id+"-r0", attemptIDs[0], false)
			if won || !errors.Is(err, errAttemptConflict) {
				t.Fatalf("late failure won=%v err=%v", won, err)
			}
			t.Log("completed stream usage evidence rejects late contradictory failure")
		}
		if os.Getenv("TEST_RECOVERY_STALE") == "1" {
			n, err := experimentalRecoverCompletedAttemptsHook(ctx, attemptDB, func(count int) error {
				if count != 1 {
					return fmt.Errorf("expected one candidate, got %d", count)
				}
				return db.Exec("UPDATE bills SET completion_attempt_id=NULL WHERE request_id=? AND phase=?", id+"-r0", store.PhaseReserve).Error
			})
			if err != nil || n != 0 {
				t.Fatalf("stale evidence recovered=%d err=%v", n, err)
			}
			var row attemptRow
			var events int64
			if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attemptIDs[0]).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attemptIDs[0]).Count(&events).Error; err != nil || row.State != "sending" || events != 0 {
				t.Fatalf("stale evidence state=%s events=%d err=%v", row.State, events, err)
			}
			if err := db.Exec("UPDATE bills SET completion_attempt_id=? WHERE request_id=? AND phase=?", attemptIDs[0], id+"-r0", store.PhaseReserve).Error; err != nil {
				t.Fatal(err)
			}
		}
		// Fault injection: the incomplete request references another request's
		// attempt; a recovery scan must not accept cross-request evidence.
		q := db.Exec("UPDATE bills SET completion_attempt_id=? WHERE request_id=? AND phase=?", attemptIDs[0], id+"-r1", store.PhaseReserve)
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal(q.RowsAffected, q.Error)
		}
		if os.Getenv("TEST_RECOVERY_OUTBOX_FAILURE") == "1" {
			injected := errors.New("injected recovery outbox failure")
			var once atomic.Bool
			name := "test:recovery-outbox-failure"
			if err := attemptDB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == "eval_attempt_outbox" && once.CompareAndSwap(false, true) {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			n, err := experimentalRecoverCompletedAttempts(ctx, attemptDB)
			attemptDB.Callback().Create().Remove(name)
			if n != 0 || !errors.Is(err, injected) || !once.Load() {
				t.Fatalf("failure n=%d err=%v injected=%v", n, err, once.Load())
			}
			var row attemptRow
			var events int64
			if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attemptIDs[0]).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attemptIDs[0]).Count(&events).Error; err != nil || row.State != "sending" || row.Outcome != nil || events != 0 {
				t.Fatalf("rollback state=%s outcome=%v events=%d err=%v", row.State, row.Outcome, events, err)
			}
			t.Log("recovery outbox failure rolled back state and event; retry follows")
		}
		if os.Getenv("TEST_RECOVERY_KILL_CONNECTION") == "1" {
			var once atomic.Bool
			var killErr error
			name := "test:recovery-kill-connection"
			if err := attemptDB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table != "eval_attempt_outbox" || !once.CompareAndSwap(false, true) {
					return
				}
				var connectionID uint64
				if killErr = tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT CONNECTION_ID()").Scan(&connectionID).Error; killErr != nil {
					tx.AddError(killErr)
					return
				}
				killErr = db.Exec(fmt.Sprintf("KILL CONNECTION %d", connectionID)).Error
				if killErr != nil {
					tx.AddError(killErr)
				}
			}); err != nil {
				t.Fatal(err)
			}
			n, err := experimentalRecoverCompletedAttempts(ctx, attemptDB)
			attemptDB.Callback().Create().Remove(name)
			if n != 0 || err == nil || killErr != nil || !once.Load() {
				t.Fatalf("kill n=%d err=%v killErr=%v injected=%v", n, err, killErr, once.Load())
			}
			var row attemptRow
			var events int64
			if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attemptIDs[0]).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attemptIDs[0]).Count(&events).Error; err != nil || row.State != "sending" || row.Outcome != nil || events != 0 {
				t.Fatalf("kill rollback state=%s outcome=%v events=%d err=%v", row.State, row.Outcome, events, err)
			}
			t.Log("MySQL connection killed after update before outbox: rollback verified; retry follows")
		}
		if os.Getenv("TEST_RECOVERY_COMMIT_DEATH") == "1" {
			killRecoveryAfterCommit(t)
		}
		if os.Getenv("TEST_RECOVERY_COMMIT_ACK_LOSS") == "1" {
			if os.Getenv("TEST_RECOVERY_COMMIT_DEATH") == "1" || os.Getenv("TEST_RECOVERY_MULTIPROCESS") == "1" {
				t.Fatal("select one commit scenario")
			}
			n, err := experimentalRecoverCompletedAttempts(ctx, recoveryDBWithLostCommitAck(t))
			if n != 0 || !errors.Is(err, errCommitAckLost) {
				t.Fatalf("commit acknowledgement loss n=%d err=%v", n, err)
			}
			var row attemptRow
			var events int64
			if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attemptIDs[0]).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attemptIDs[0]).Count(&events).Error; err != nil || row.State != "completed" || events != 1 {
				t.Fatalf("committed state=%s events=%d err=%v", row.State, events, err)
			}
			t.Log("driver returned injected error after actual commit; state and event persisted")
		}
		if os.Getenv("TEST_RECOVERY_MULTIPROCESS") == "1" {
			if os.Getenv("TEST_RECOVERY_COMMIT_DEATH") == "1" {
				t.Fatal("select one process scenario")
			}
			if n := recoverInProcesses(t); n != 1 {
				t.Fatalf("process recovery total=%d", n)
			}
			t.Log("four independent processes recovered exactly once")
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		type result struct {
			n   int
			err error
		}
		results := make(chan result, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				n, err := experimentalRecoverCompletedAttempts(ctx, attemptDB)
				results <- result{n, err}
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		total := 0
		for result := range results {
			if result.err != nil {
				t.Error(result.err)
			}
			total += result.n
		}
		wantTotal := 1
		if os.Getenv("TEST_RECOVERY_MULTIPROCESS") == "1" || os.Getenv("TEST_RECOVERY_COMMIT_DEATH") == "1" || os.Getenv("TEST_RECOVERY_COMMIT_ACK_LOSS") == "1" {
			wantTotal = 0
		}
		if total != wantTotal {
			t.Fatalf("unique recovered=%d, want %d", total, wantTotal)
		}
		for i, attemptID := range attemptIDs {
			var attempt attemptRow
			if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", attemptID).Take(&attempt).Error; err != nil {
				t.Fatal(err)
			}
			wantState, wantEvents := "sending", int64(0)
			if i == 0 {
				wantState, wantEvents = "completed", 1
			}
			var events int64
			if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attemptID).Count(&events).Error; err != nil || attempt.State != wantState || events != wantEvents {
				t.Fatal(i, attempt.State, events, err)
			}
		}
		if n, err := experimentalRecoverCompletedAttempts(ctx, attemptDB); err != nil || n != 0 {
			t.Fatal(n, err)
		}
		t.Log("complete=1 cross_request_partial=untouched repeat=0")
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 994 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("completed_done=true partial_done=false both_settled=true both_usage=1+2 before_recovery_both_attempt=sending before_recovery_attempt_events=0 balance=%d ledger=%d", live, ledger)
}
