package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCompletionEvidenceOwnershipAndAtomicity(t *testing.T) {
	if os.Getenv("TEST_COMPLETION_EVIDENCE") != "1" {
		t.Skip("requires experimental completion overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("reserve intent interface absent")
	}
	writer, ok := any(s).(experimentalCompletionUsage)
	if !ok {
		t.Fatal("completion usage interface absent")
	}
	attemptDB := setupAttemptDB(t)
	id := fmt.Sprintf("completion-atomic-%d", time.Now().UnixNano())
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
	p1 := store.Provider{Name: id + "-1", BaseURL: "http://127.0.0.1", Models: "[]", Weight: 1}
	p2 := store.Provider{Name: id + "-2", BaseURL: "http://127.0.0.1", Models: "[]", Weight: 1}
	if err := db.Create(&p1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&p2).Error; err != nil {
		t.Fatal(err)
	}
	owner := sha256.Sum256([]byte("synthetic-owner"))
	bill, err := candidate.ReserveWithExecutionIntent(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, ProviderId: int64(p1.ID), Model: id, EstPromptTokens: 84}, hex.EncodeToString(owner[:]), 30*time.Second)
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	if err := db.Exec("UPDATE bills SET execution_state='forwarding' WHERE id=? AND execution_state='reserved'", bill.BillId).Error; err != nil {
		t.Fatal(err)
	}
	a1hash := sha256.Sum256([]byte(id + "-a1"))
	a2hash := sha256.Sum256([]byte(id + "-a2"))
	good := hex.EncodeToString(a1hash[:16])
	wrong := hex.EncodeToString(a2hash[:16])
	for _, a := range []attemptRow{{AttemptID: good, TenantID: tenant.ID, RequestID: id, Ordinal: 0, ProviderID: p1.ID, State: "sending"}, {AttemptID: wrong, TenantID: tenant.ID, RequestID: id, Ordinal: 1, ProviderID: p2.ID, State: "sending"}} {
		if err := attemptDB.Table("eval_route_attempt").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	req := &pb.ReportUsageRequest{RequestId: id, BillId: int64(bill.BillId), ProviderId: int64(p1.ID), PromptTokens: 40}
	if os.Getenv("TEST_COMPLETION_BILL_ID") == "1" {
		badID := &pb.ReportUsageRequest{RequestId: req.RequestId, BillId: req.BillId + 1, ProviderId: req.ProviderId, PromptTokens: req.PromptTokens}
		if err := writer.ReportUsageWithAttemptCompletion(ctx, badID, good); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("mismatched bill ID accepted: %v", err)
		}
		var untouched store.Bill
		if err := db.Where("id=?", bill.BillId).Take(&untouched).Error; err != nil || untouched.UsageReportedAt != nil {
			t.Fatalf("unexpected usage after mismatched ID: %+v err=%v", untouched, err)
		}
	}
	if err := writer.ReportUsageWithAttemptCompletion(ctx, req, wrong); status.Code(err) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	if os.Getenv("TEST_COMPLETION_FAILURE_FIRST") == "1" {
		won, err := completeAttemptCoordinated(ctx, attemptDB, tenant.ID, id, good, false)
		if err != nil || !won {
			t.Fatal(won, err)
		}
		if err := writer.ReportUsageWithAttemptCompletion(ctx, req, good); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("usage after failure=%v", err)
		}
		var evidence string
		var usageEvents, failureEvents int64
		if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.BillId).Scan(&evidence).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":usage").Count(&usageEvents).Error; err != nil {
			t.Fatal(err)
		}
		if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=? AND outcome=?", good, false).Count(&failureEvents).Error; err != nil {
			t.Fatal(err)
		}
		if evidence != "" || usageEvents != 0 || failureEvents != 1 {
			t.Fatalf("evidence=%q usage=%d failure=%d", evidence, usageEvents, failureEvents)
		}
		t.Log("failure first: completion evidence rejected; failure event unique")
		return
	}
	if os.Getenv("TEST_COMPLETION_COORDINATED") == "1" {
		var once atomic.Bool
		result := make(chan error, 1)
		started := make(chan struct{})
		if err := db.Callback().Update().Before("gorm:update").Register("test:completion-concurrent-failure", func(tx *gorm.DB) {
			if tx.Statement.Table != "bills" || !once.CompareAndSwap(false, true) {
				return
			}
			go func() {
				close(started)
				_, e := completeAttemptCoordinated(ctx, attemptDB, tenant.ID, id, good, false)
				result <- e
			}()
			<-started
			// Allow an unlocked competing transaction to finish, while also
			// permitting a correctly locked competitor to wait for our commit.
			select {
			case e := <-result:
				result <- e
			case <-time.After(250 * time.Millisecond):
			}
		}); err != nil {
			t.Fatal(err)
		}
		usageErr := writer.ReportUsageWithAttemptCompletion(ctx, req, good)
		db.Callback().Update().Remove("test:completion-concurrent-failure")
		var failureErr error
		select {
		case failureErr = <-result:
		case <-time.After(10 * time.Second):
			t.Fatal("competing transition timed out")
		}
		var row attemptRow
		if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", good).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		var evidence string
		if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.BillId).Scan(&evidence).Error; err != nil {
			t.Fatal(err)
		}
		if usageErr == nil && failureErr == nil || evidence == good && row.Outcome != nil && !*row.Outcome {
			t.Fatalf("contradiction usage=%v failure=%v state=%s evidence=%q", usageErr, failureErr, row.State, evidence)
		}
		if usageErr != nil || !errors.Is(failureErr, errAttemptConflict) || evidence != good || row.State != "sending" {
			t.Fatalf("unexpected completion winner usage=%v failure=%v state=%s evidence=%q", usageErr, failureErr, row.State, evidence)
		}
		t.Log("completion observation wins; failure transition rejected")
		return
	}
	if os.Getenv("TEST_COMPLETION_EVIDENCE_RACE") == "1" {
		var once atomic.Bool
		var competingErr error
		if err := db.Callback().Update().Before("gorm:update").Register("test:attempt-changes-after-check", func(tx *gorm.DB) {
			if tx.Statement.Table != "bills" || !once.CompareAndSwap(false, true) {
				return
			}
			var won bool
			won, competingErr = completeAttempt(ctx, attemptDB, good, false)
			if competingErr == nil && !won {
				competingErr = errors.New("competing failure did not complete")
			}
		}); err != nil {
			t.Fatal(err)
		}
		err := writer.ReportUsageWithAttemptCompletion(ctx, req, good)
		db.Callback().Update().Remove("test:attempt-changes-after-check")
		if err != nil || competingErr != nil || !once.Load() {
			t.Fatalf("usage=%v competing=%v injected=%v", err, competingErr, once.Load())
		}
		var row attemptRow
		if err := attemptDB.Table("eval_route_attempt").Where("attempt_id=?", good).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		var evidence string
		if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.BillId).Scan(&evidence).Error; err != nil {
			t.Fatal(err)
		}
		if row.State != "completed" || row.Outcome == nil || *row.Outcome || evidence != good {
			t.Fatalf("unexpected result: attempt=%+v evidence=%q", row, evidence)
		}
		var usageEvents, attemptEvents int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":usage").Count(&usageEvents).Error; err != nil {
			t.Fatal(err)
		}
		if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=? AND outcome=?", good, false).Count(&attemptEvents).Error; err != nil {
			t.Fatal(err)
		}
		if usageEvents != 1 || attemptEvents != 1 {
			t.Fatalf("usage events=%d failure events=%d", usageEvents, attemptEvents)
		}
		t.Log("reproduced conflicting failure outcome and completion evidence after stale attempt check")
		return
	}
	var injected atomic.Int32
	if err := db.Callback().Create().Before("gorm:create").Register("test:usage_event_failure", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.OutboxEvent); ok && ev.EventKey == id+":usage" && injected.CompareAndSwap(0, 1) {
			tx.AddError(errors.New("synthetic usage outbox failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("test:usage_event_failure") })
	if err := writer.ReportUsageWithAttemptCompletion(ctx, req, good); err == nil || injected.Load() != 1 {
		t.Fatal(err, injected.Load())
	}
	var before store.Bill
	if err := db.Where("id=?", bill.BillId).First(&before).Error; err != nil || before.UsageReportedAt != nil {
		t.Fatal(before.UsageReportedAt, err)
	}
	var evidence string
	if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.BillId).Scan(&evidence).Error; err != nil || evidence != "" {
		t.Fatal(evidence, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":usage").Count(&events).Error; err != nil || events != 0 {
		t.Fatal(events, err)
	}
	if err := writer.ReportUsageWithAttemptCompletion(ctx, req, good); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEST_COMPLETION_REPLAY_IDENTITY") == "1" {
		otherHash := sha256.Sum256([]byte(id + "-same-provider-other-attempt"))
		other := hex.EncodeToString(otherHash[:16])
		if err := attemptDB.Table("eval_route_attempt").Create(&attemptRow{AttemptID: other, TenantID: tenant.ID, RequestID: id, Ordinal: 2, ProviderID: p1.ID, State: "sending"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := writer.ReportUsageWithAttemptCompletion(ctx, req, good); err != nil {
			t.Fatalf("same attempt replay: %v", err)
		}
		if err := writer.ReportUsageWithAttemptCompletion(ctx, req, other); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("different attempt replay must not acknowledge its completion evidence: %v", err)
		}
	}
	var after store.Bill
	if err := db.Where("id=?", bill.BillId).First(&after).Error; err != nil || after.UsageReportedAt == nil {
		t.Fatal(after.UsageReportedAt, err)
	}
	if err := db.Raw("SELECT completion_attempt_id FROM bills WHERE id=?", bill.BillId).Scan(&evidence).Error; err != nil || evidence != good {
		t.Fatal(evidence, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":usage").Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 400 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("wrong_provider=FailedPrecondition injected=%d rollback_usage=false rollback_evidence=false retry_usage=true retry_evidence=true usage_events=%d balance=%d ledger=%d", injected.Load(), events, live, ledger)
}
