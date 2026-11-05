package comparison

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
)

// Test-only journal record; ownership of the directory is an explicit trust assumption.
type completionJournalRecord struct {
	Usage     inboxUsage
	AttemptID string
}

// autoCompletionJournalBridge is an isolated test substitute; it injects EOF
// before the usage write and retains the handler's actual request locally.
type autoCompletionJournalBridge struct {
	*guardedAttemptBridge
	dir    string
	tenant uint64
}

func (b *autoCompletionJournalBridge) ReportUsage(_ context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	attempt, ok := b.observed.Load(req.RequestId)
	if !ok {
		return nil, errors.New("no observed completion attempt")
	}
	x := completionJournalRecord{Usage: inboxUsage{RequestID: req.RequestId, TenantID: b.tenant, BillID: req.BillId, ProviderID: req.ProviderId, PromptTokens: req.PromptTokens, CompletionTokens: req.CompletionTokens}, AttemptID: attempt.(string)}
	if os.Getenv("TEST_PAUSE_BEFORE_COMPLETION_JOURNAL") == "1" {
		fmt.Println("AUTO_COMPLETION_JOURNAL_BEFORE_WRITE")
		time.Sleep(time.Minute)
	}
	if err := saveCompletionJournal(b.dir, x); err != nil {
		return nil, err
	}
	if os.Getenv("TEST_PAUSE_AFTER_COMPLETION_JOURNAL") == "1" {
		fmt.Println("AUTO_COMPLETION_JOURNAL_SYNCED")
		time.Sleep(time.Minute)
	}
	return nil, io.EOF
}

func validateCompletionJournal(x completionJournalRecord) error {
	raw, err := hex.DecodeString(x.AttemptID)
	u := x.Usage
	if err != nil || len(raw) != 16 || u.RequestID == "" || u.TenantID == 0 || u.BillID <= 0 || u.ProviderID <= 0 || u.PromptTokens < 0 || u.CompletionTokens < 0 || u.State != "" {
		return errors.New("invalid completion journal")
	}
	return nil
}
func readCompletionJournal(dir, id string) (completionJournalRecord, error) {
	var x completionJournalRecord
	f, err := os.Open(localUsagePath(dir, id))
	if err != nil {
		return x, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return x, errors.New("invalid journal file")
	}
	dec := json.NewDecoder(io.LimitReader(f, 65537))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&x); err != nil {
		return x, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return x, errors.New("trailing journal data")
	}
	if err := validateCompletionJournal(x); err != nil {
		return x, err
	}
	if x.Usage.RequestID != id {
		return x, errors.New("journal identity mismatch")
	}
	return x, nil
}
func saveCompletionJournal(dir string, x completionJournalRecord) error {
	if err := validateCompletionJournal(x); err != nil {
		return err
	}
	data, err := json.Marshal(x)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".completion-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(name, localUsagePath(dir, x.Usage.RequestID)); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		prior, e := readCompletionJournal(dir, x.Usage.RequestID)
		if e != nil {
			return e
		}
		if prior != x {
			return errLocalUsageConflict
		}
	}
	return syncLocalDir(dir)
}
func replayCompletionJournal(ctx context.Context, db *gorm.DB, s *store.Store, dir, id string) error {
	x, err := readCompletionJournal(dir, id)
	if err != nil {
		return err
	}
	var bill struct{ ID uint64 }
	q := db.WithContext(ctx).Table("bills").Select("id").Where("id=? AND tenant_id=? AND request_id=? AND phase=?", x.Usage.BillID, x.Usage.TenantID, x.Usage.RequestID, store.PhaseReserve).Take(&bill)
	if q.Error != nil {
		return q.Error
	}
	writer, ok := any(s).(experimentalCompletionUsage)
	if !ok {
		return errors.New("completion interface unavailable")
	}
	req := &pb.ReportUsageRequest{RequestId: x.Usage.RequestID, BillId: x.Usage.BillID, ProviderId: x.Usage.ProviderID, PromptTokens: x.Usage.PromptTokens, CompletionTokens: x.Usage.CompletionTokens}
	if err := writer.ReportUsageWithAttemptCompletion(ctx, req, x.AttemptID); err != nil {
		return err
	}
	resp, err := s.Settle(ctx, &pb.SettleRequest{RequestId: x.Usage.RequestID, BillId: x.Usage.BillID})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Settled {
		return errors.New("settlement not acknowledged")
	}
	return nil
}

func TestCompletionJournalProcessChild(t *testing.T) {
	if os.Getenv("COMPLETION_JOURNAL_CHILD") != "1" {
		t.Skip("child only")
	}
	x := completionJournalRecord{Usage: inboxUsage{RequestID: "synthetic-process", TenantID: 1, BillID: 2, ProviderID: 3, PromptTokens: 40}, AttemptID: strings.Repeat("a", 32)}
	if err := saveCompletionJournal(os.Getenv("COMPLETION_JOURNAL_DIR"), x); err != nil {
		t.Fatal(err)
	}
	fmt.Println("COMPLETION_JOURNAL_SYNCED")
	time.Sleep(time.Minute)
}
func TestCompletionJournalSurvivesProcessKill(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCompletionJournalProcessChild$")
	cmd.Env = append(os.Environ(), "COMPLETION_JOURNAL_CHILD=1", "COMPLETION_JOURNAL_DIR="+dir)
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
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if strings.TrimSpace(s.Text()) == "COMPLETION_JOURNAL_SYNCED" {
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
			t.Fatal("child exited before sync")
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child timeout")
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
	x, err := readCompletionJournal(dir, "synthetic-process")
	if err != nil || x.AttemptID != strings.Repeat("a", 32) || x.Usage.PromptTokens != 40 {
		t.Fatalf("record=%+v err=%v", x, err)
	}
	if err := saveCompletionJournal(dir, x); err != nil {
		t.Fatal(err)
	}
	x.AttemptID = strings.Repeat("b", 32)
	if err := saveCompletionJournal(dir, x); !errors.Is(err, errLocalUsageConflict) {
		t.Fatalf("conflict=%v", err)
	}
	t.Log("synced record survived child SIGKILL; same record idempotent, changed attempt rejected")
}
