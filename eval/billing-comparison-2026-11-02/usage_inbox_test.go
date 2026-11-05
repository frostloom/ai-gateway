package comparison

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type inboxUsage struct {
	RequestID        string
	TenantID         uint64
	BillID           int64
	ProviderID       int64
	PromptTokens     int64
	CompletionTokens int64
	State            string
}

var errInboxConflict = errors.New("usage inbox conflict")

func inboxDBWithLostEOF(t *testing.T) *gorm.DB { return recoveryDBWithCommitError(t, io.EOF) }

func putUsageInbox(ctx context.Context, db *gorm.DB, x inboxUsage) error {
	if x.RequestID == "" || x.TenantID == 0 || x.BillID <= 0 || x.ProviderID <= 0 || x.PromptTokens < 0 || x.CompletionTokens < 0 {
		return errors.New("invalid inbox input")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var bill struct {
			ID             int64
			TenantID       uint64
			RequestID      string
			ExecutionState string
		}
		if err := tx.Table("bills").Select("id,tenant_id,request_id,execution_state").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND request_id=? AND phase='reserve'", x.BillID, x.TenantID, x.RequestID).Take(&bill).Error; err != nil {
			return err
		}
		if bill.ExecutionState != "forwarding" {
			return errors.New("not forwarding")
		}
		var prior inboxUsage
		err := tx.Table("eval_usage_inbox").Where("request_id=?", x.RequestID).Take(&prior).Error
		if err == nil {
			if prior.TenantID != x.TenantID || prior.BillID != x.BillID || prior.ProviderID != x.ProviderID || prior.PromptTokens != x.PromptTokens || prior.CompletionTokens != x.CompletionTokens {
				return errInboxConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		x.State = "pending"
		return tx.Table("eval_usage_inbox").Create(&x).Error
	})
}
func replayUsageInbox(ctx context.Context, db *gorm.DB, s *store.Store, id string) error {
	var x inboxUsage
	if err := db.WithContext(ctx).Table("eval_usage_inbox").Where("request_id=?", id).Take(&x).Error; err != nil {
		return err
	}
	if x.State == "done" {
		return nil
	}
	if x.State != "pending" {
		return errors.New("invalid inbox state")
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: x.RequestID, BillId: x.BillID, ProviderId: x.ProviderID, PromptTokens: x.PromptTokens, CompletionTokens: x.CompletionTokens}); err != nil {
		return err
	}
	resp, err := s.Settle(ctx, &pb.SettleRequest{RequestId: x.RequestID, BillId: x.BillID})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Settled {
		return errors.New("settle not confirmed")
	}
	return db.WithContext(ctx).Table("eval_usage_inbox").Where("request_id=? AND state='pending'", id).Update("state", "done").Error
}

func TestUsageInboxChild(t *testing.T) {
	if os.Getenv("USAGE_INBOX_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if os.Getenv("USAGE_INBOX_STAGE") == "reported" || os.Getenv("USAGE_INBOX_STAGE") == "settled" {
		var x inboxUsage
		if err := db.Table("eval_usage_inbox").Where("request_id=?", os.Getenv("USAGE_INBOX_ID")).Take(&x).Error; err != nil {
			t.Fatal(err)
		}
		s := store.NewStore(db, redis)
		if err := s.ReportUsage(context.Background(), &pb.ReportUsageRequest{RequestId: x.RequestID, BillId: x.BillID, ProviderId: x.ProviderID, PromptTokens: x.PromptTokens, CompletionTokens: x.CompletionTokens}); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("USAGE_INBOX_STAGE") == "settled" {
			resp, err := s.Settle(context.Background(), &pb.SettleRequest{RequestId: x.RequestID, BillId: x.BillID})
			if err != nil || resp == nil || !resp.Settled {
				t.Fatal(resp, err)
			}
			fmt.Println("SETTLE_COMMITTED")
			time.Sleep(time.Minute)
			return
		}
		fmt.Println("USAGE_COMMITTED")
		time.Sleep(time.Minute)
		return
	}
	x := inboxUsage{RequestID: os.Getenv("USAGE_INBOX_ID"), TenantID: mustUint(os.Getenv("USAGE_INBOX_TENANT")), BillID: int64(mustUint(os.Getenv("USAGE_INBOX_BILL"))), ProviderID: int64(mustUint(os.Getenv("USAGE_INBOX_PROVIDER"))), PromptTokens: 40, CompletionTokens: 2}
	if err := putUsageInbox(context.Background(), db, x); err != nil {
		t.Fatal(err)
	}
	fmt.Println("INBOX_COMMITTED")
	time.Sleep(time.Minute)
}
func mustUint(v string) uint64 {
	var n uint64
	if _, err := fmt.Sscan(v, &n); err != nil {
		panic(err)
	}
	return n
}
func TestUsageInboxSurvivesWorkerDeath(t *testing.T) {
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
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate absent")
	}
	id := fmt.Sprintf("usage-inbox-%d", time.Now().UnixNano())
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
	provider := store.Provider{Name: id, BaseURL: "http://127.0.0.1", Models: "[]", Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("synthetic-owner"))
	bill, err := candidate.ReserveWithExecutionIntent(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, ProviderId: int64(provider.ID), Model: id, EstPromptTokens: 84}, hex.EncodeToString(digest[:]), 30*time.Second)
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	if err := db.Exec("UPDATE bills SET execution_state='forwarding' WHERE id=?", bill.BillId).Error; err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEST_INBOX_ACK_UNKNOWN") == "1" {
		dir := t.TempDir()
		bridge := &inboxFailureBridge{db: inboxDBWithLostEOF(t), tenant: tenant.ID, localDir: dir}
		req := &pb.ReportUsageRequest{RequestId: id, BillId: bill.BillId, ProviderId: int64(provider.ID), PromptTokens: 40, CompletionTokens: 2}
		_, err := bridge.ReportUsage(ctx, req)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected lost commit acknowledgement: %v", err)
		}
		if bridge.journalErr != nil {
			t.Fatal(bridge.journalErr)
		}
		x, err := readLocalUsage(dir, id)
		if err != nil {
			t.Fatal(err)
		}
		var remote inboxUsage
		if err := db.Table("eval_usage_inbox").Where("request_id=?", id).Take(&remote).Error; err != nil {
			t.Fatal(err)
		}
		if x.RequestID != remote.RequestID || x.TenantID != remote.TenantID || x.BillID != remote.BillID || x.ProviderID != remote.ProviderID || x.PromptTokens != remote.PromptTokens || x.CompletionTokens != remote.CompletionTokens {
			t.Fatalf("local=%+v remote=%+v", x, remote)
		}
		if err := putUsageInbox(ctx, db, x); err != nil {
			t.Fatal(err)
		}
		conflict := x
		conflict.CompletionTokens++
		if err := putUsageInbox(ctx, db, conflict); !errors.Is(err, errInboxConflict) {
			t.Fatalf("replay conflict=%v", err)
		}
		var count int64
		if err := db.Table("eval_usage_inbox").Where("request_id=?", id).Count(&count).Error; err != nil || count != 1 {
			t.Fatal(count, err)
		}
		t.Log("committed inbox but client saw EOF; equal local record matches remote; conflict rejected")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestUsageInboxChild$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "USAGE_INBOX_CHILD=1", "USAGE_INBOX_ID="+id, fmt.Sprintf("USAGE_INBOX_TENANT=%d", tenant.ID), fmt.Sprintf("USAGE_INBOX_BILL=%d", bill.BillId), fmt.Sprintf("USAGE_INBOX_PROVIDER=%d", provider.ID))
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
	if err != nil || strings.TrimSpace(line) != "INBOX_COMMITTED" {
		t.Fatalf("child marker=%q err=%v", line, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	x := inboxUsage{RequestID: id, TenantID: tenant.ID, BillID: bill.BillId, ProviderID: int64(provider.ID), PromptTokens: 40, CompletionTokens: 2}
	if err := putUsageInbox(ctx, db, x); err != nil {
		t.Fatal(err)
	}
	conflict := x
	conflict.CompletionTokens++
	if err := putUsageInbox(ctx, db, conflict); !errors.Is(err, errInboxConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if os.Getenv("TEST_LOCAL_RECOVERY_SCAN") == "1" {
		dir := t.TempDir()
		if err := saveLocalUsage(dir, x); err != nil {
			t.Fatal(err)
		}
		secondID := id + "-conflict"
		second, err := candidate.ReserveWithExecutionIntent(ctx, &pb.ReserveRequest{RequestId: secondID, TenantId: int64(tenant.ID), ApiKeyId: 1, ProviderId: int64(provider.ID), Model: id, EstPromptTokens: 84}, hex.EncodeToString(digest[:]), 30*time.Second)
		if err != nil || second.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(second, err)
		}
		if err := db.Exec("UPDATE bills SET execution_state='forwarding' WHERE id=?", second.BillId).Error; err != nil {
			t.Fatal(err)
		}
		remote := inboxUsage{RequestID: secondID, TenantID: tenant.ID, BillID: second.BillId, ProviderID: int64(provider.ID), PromptTokens: 40, CompletionTokens: 2}
		if err := putUsageInbox(ctx, db, remote); err != nil {
			t.Fatal(err)
		}
		local := remote
		local.CompletionTokens = 3
		if err := saveLocalUsage(dir, local); err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(dir, strings.Repeat("0", 64)+".json")
		if err := os.WriteFile(bad, []byte("not-json"), 0600); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("TEST_LOCAL_SCAN_CONCURRENT") == "1" {
			var group sync.WaitGroup
			type scanOutcome struct {
				result localScanResult
				err    error
			}
			outcomes := make(chan scanOutcome, 4)
			start := make(chan struct{})
			for i := 0; i < 4; i++ {
				group.Add(1)
				go func() {
					defer group.Done()
					<-start
					result, err := scanLocalUsage(ctx, db, s, dir)
					outcomes <- scanOutcome{result, err}
				}()
			}
			close(start)
			group.Wait()
			close(outcomes)
			for o := range outcomes {
				if o.err != nil || o.result.Succeeded != 1 || o.result.Failed != 2 {
					t.Fatalf("parallel scan=%+v err=%v", o.result, o.err)
				}
			}
			t.Log("four scanners each observe one successful idempotent item; count is not unique transitions")
		}
		for i := 0; i < 2; i++ {
			result, err := scanLocalUsage(ctx, db, s, dir)
			if err != nil || result.Succeeded != 1 || result.Failed != 2 {
				t.Fatalf("scan %d result=%+v err=%v", i, result, err)
			}
		}
		var goodBill, badBill store.Bill
		if err := db.Where("id=?", bill.BillId).Take(&goodBill).Error; err != nil || goodBill.Status != store.StatusSettled {
			t.Fatal(goodBill.Status, err)
		}
		if err := db.Where("id=?", second.BillId).Take(&badBill).Error; err != nil || badBill.Status != store.StatusPending || badBill.UsageReportedAt != nil {
			t.Fatal(badBill.Status, err)
		}
		var events int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle", secondID + ":reserve"}).Count(&events).Error; err != nil || events != 4 {
			t.Fatal(events, err)
		}
		var settleRows int64
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settleRows).Error; err != nil || settleRows != 1 {
			t.Fatalf("settle rows=%d err=%v", settleRows, err)
		}
		var forbidden int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{secondID + ":usage", secondID + ":settle", secondID + ":reverse"}).Count(&forbidden).Error; err != nil || forbidden != 0 {
			t.Fatal(forbidden, err)
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 358 || ledger != live {
			t.Fatal(live, ledger, err)
		}
		if _, err := os.Stat(bad); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(localUsagePath(dir, secondID)); err != nil {
			t.Fatal(err)
		}
		t.Log("valid recovered; corrupt and conflicting records retained; repeat caused no duplicate; balance=358")
		return
	}
	if os.Getenv("TEST_LOCAL_CONFLICT_GUARD") == "1" {
		dir := t.TempDir()
		bridge := &inboxFailureBridge{db: db, tenant: tenant.ID, localDir: dir}
		_, err := bridge.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, BillId: bill.BillId, ProviderId: int64(provider.ID), PromptTokens: 40, CompletionTokens: 3})
		if !errors.Is(err, errInboxConflict) {
			t.Fatalf("expected inbox conflict, got %v", err)
		}
		if _, err := readLocalUsage(dir, id); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("conflict must not be journaled: %v", err)
		}
		t.Log("permanent usage conflict rejected without local journal")
	}
	second := exec.Command(os.Args[0], "-test.run=^TestUsageInboxChild$", "-test.timeout=30s")
	second.Env = append(os.Environ(), "USAGE_INBOX_CHILD=1", "USAGE_INBOX_STAGE=reported", "USAGE_INBOX_ID="+id)
	secondOut, err := second.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	second.Stderr = os.Stderr
	if err = second.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if second.ProcessState == nil {
			_ = second.Process.Kill()
			_ = second.Wait()
		}
	})
	line, err = bufio.NewReader(secondOut).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "USAGE_COMMITTED" {
		t.Fatalf("usage marker=%q err=%v", line, err)
	}
	if err = second.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = second.Wait()
	third := exec.Command(os.Args[0], "-test.run=^TestUsageInboxChild$", "-test.timeout=30s")
	third.Env = append(os.Environ(), "USAGE_INBOX_CHILD=1", "USAGE_INBOX_STAGE=settled", "USAGE_INBOX_ID="+id)
	thirdOut, err := third.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	third.Stderr = os.Stderr
	if err = third.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if third.ProcessState == nil {
			_ = third.Process.Kill()
			_ = third.Wait()
		}
	})
	line, err = bufio.NewReader(thirdOut).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "SETTLE_COMMITTED" {
		t.Fatalf("settle marker=%q err=%v", line, err)
	}
	if err = third.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = third.Wait()
	var workers sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); errs <- replayUsageInbox(ctx, db, s, id) }()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := replayUsageInbox(ctx, db, s, id); err != nil {
		t.Fatal(err)
	}
	var row store.Bill
	if err := db.Where("id=?", bill.BillId).Take(&row).Error; err != nil || row.Status != store.StatusSettled || row.PromptTokens == nil || *row.PromptTokens != 40 || row.CompletionTokens == nil || *row.CompletionTokens != 2 {
		t.Fatalf("bill=%+v err=%v", row, err)
	}
	var inbox inboxUsage
	if err := db.Table("eval_usage_inbox").Where("request_id=?", id).Take(&inbox).Error; err != nil || inbox.State != "done" {
		t.Fatal(inbox, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatal(events, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 458 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("child_killed_after_inbox_commit=true replay_done=true conflict_rejected=true duplicate_replay_safe=true events=%d balance=%d ledger=%d", events, live, ledger)
}
