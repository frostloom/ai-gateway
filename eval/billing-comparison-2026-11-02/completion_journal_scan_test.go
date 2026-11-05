package comparison

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type completionScanStats struct {
	Scanned, Succeeded, Failed int
	Last                       string
}

// Test-only one-shot scan; no claims, scheduling, cleanup, or durable cursor.
func scanCompletionJournals(ctx context.Context, db *gorm.DB, s *store.Store, dir string, limit int) (completionScanStats, error) {
	return scanCompletionJournalsAfter(ctx, db, s, dir, "", limit)
}

func scanCompletionJournalsAfter(ctx context.Context, db *gorm.DB, s *store.Store, dir, after string, limit int) (completionScanStats, error) {
	var stats completionScanStats
	if limit <= 0 || limit > 128 {
		return stats, errors.New("invalid limit")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return stats, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Name() <= after {
			continue
		}
		if stats.Scanned >= limit {
			break
		}
		stats.Scanned++
		stats.Last = entry.Name()
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
			stats.Failed++
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 65536 {
			stats.Failed++
			continue
		}
		var x completionJournalRecord
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&x); err != nil {
			stats.Failed++
			continue
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF || validateCompletionJournal(x) != nil || filepath.Base(localUsagePath(dir, x.Usage.RequestID)) != entry.Name() {
			stats.Failed++
			continue
		}
		if err := replayCompletionJournal(ctx, db, s, dir, x.Usage.RequestID); err != nil {
			stats.Failed++
			continue
		}
		stats.Succeeded++
	}
	return stats, nil
}

func TestCompletionJournalRecoveryProcessChild(t *testing.T) {
	if os.Getenv("COMPLETION_RECOVERY_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	dir := os.Getenv("COMPLETION_RECOVERY_DIR")
	stats, err := scanCompletionJournals(ctx, db, s, dir, 128)
	if os.Getenv("TEST_RECOVERY_SCAN_PAGED") == "1" {
		if err != nil || stats.Scanned != 128 || stats.Succeeded != 0 || stats.Failed != 128 || stats.Last == "" {
			t.Fatalf("first page=%+v err=%v", stats, err)
		}
		stats, err = scanCompletionJournalsAfter(ctx, db, s, dir, stats.Last, 128)
		if err != nil || stats.Scanned != 1 || stats.Succeeded != 1 || stats.Failed != 0 {
			t.Fatalf("second page=%+v err=%v", stats, err)
		}
		stats.Failed = 128
	} else if err != nil || stats.Scanned != 2 || stats.Succeeded != 1 || stats.Failed != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	n, err := experimentalRecoverCompletedAttempts(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("RECOVERY_SCAN success=%d failed=%d attempt_recovered=%d\n", stats.Succeeded, stats.Failed, n)
}

// PASS characterizes starvation: fixed first page never reaches a later good record.
func TestCompletionJournalFixedPageCanStarveGoodRecord(t *testing.T) {
	dir := t.TempDir()
	id := ""
	for i := 0; i < 1000; i++ {
		candidate := fmt.Sprintf("valid-starvation-%d", i)
		name := filepath.Base(localUsagePath(dir, candidate))
		if name[0] != '0' {
			id = candidate
			break
		}
	}
	if id == "" {
		t.Fatal("could not construct ordered test input")
	}
	x := completionJournalRecord{Usage: inboxUsage{RequestID: id, TenantID: 1, BillID: 2, ProviderID: 3, PromptTokens: 1}, AttemptID: strings.Repeat("a", 32)}
	if err := saveCompletionJournal(dir, x); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		name := fmt.Sprintf("%064x.json", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{invalid"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		stats, err := scanCompletionJournals(context.Background(), nil, nil, dir, 128)
		if err != nil || stats.Scanned != 128 || stats.Failed != 128 || stats.Succeeded != 0 {
			t.Fatalf("scan=%+v err=%v", stats, err)
		}
	}
	t.Log("two fixed-prefix scans repeatedly processed 128 malformed records and did not reach later valid record")
}
