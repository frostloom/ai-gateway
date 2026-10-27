package taskbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func sealedWindowCampaignFixture(t *testing.T) *windowCampaign {
	t.Helper()
	root := t.TempDir()
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	contract := windowTestContract()
	contract.Model.MaxAttempts = 120
	contract.ModelCap = 10000
	ledgerDir := filepath.Join(root, "ledger-000001")
	ledger, err := newWindowAccounting(ledgerDir, contract)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		request := windowTestRequest("model")
		request.CaseKey = fmt.Sprintf("case-%03d/answer_complete", i)
		ticket, err := ledger.Begin(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := ledger.Settle(ticket, request.BodySHA256, &windowUsage{Input: windowInt(1), Output: windowInt(1)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	audit, err := readWindowAccounting(ledgerDir)
	if err != nil || !audit.State.Closed || audit.State.Halt != "" || audit.State.Pending != nil {
		t.Fatal("fixture ledger was not fully replayed and sealed", err)
	}
	return &windowCampaign{root: root, rootInfo: info, lock: lock, files: map[string]string{}, ledgerAudits: map[string]string{"ledger-000001": audit.JournalSHA256}}
}

func TestWindowCampaignSealedInventoryAllocationBound(t *testing.T) {
	campaign := sealedWindowCampaignFixture(t)
	if err := campaign.inventoryWindowCampaign(); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(5, func() {
		if err := campaign.inventoryWindowCampaign(); err != nil {
			panic(err)
		}
	})
	if allocations > 128 {
		t.Fatalf("immutable sealed inventory allocates %.0f objects; want at most 128", allocations)
	}
}

func TestWindowCampaignSealedInventoryRejectsChangedEvidence(t *testing.T) {
	for _, mutation := range []string{"append", "edit", "truncate", "missing", "orphan_file", "orphan_dir", "symlink_file", "symlink_dir"} {
		t.Run(mutation, func(t *testing.T) {
			campaign := sealedWindowCampaignFixture(t)
			dir := filepath.Join(campaign.root, "ledger-000001")
			file := filepath.Join(dir, "accounting.jsonl")
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "append":
				err = os.WriteFile(file, append(raw, '\n'), 0600)
			case "edit":
				raw[0] = '['
				err = os.WriteFile(file, raw, 0600)
			case "truncate":
				err = os.WriteFile(file, raw[:len(raw)-1], 0600)
			case "missing":
				err = os.Remove(file)
			case "orphan_file":
				err = os.WriteFile(filepath.Join(dir, "orphan"), nil, 0600)
			case "orphan_dir":
				err = os.Mkdir(filepath.Join(dir, "orphan"), 0700)
			case "symlink_file":
				if err = os.Rename(file, filepath.Join(t.TempDir(), "original")); err == nil {
					// Even a same-byte target must remain a regular named file.
					target := filepath.Join(t.TempDir(), "target")
					if err = os.WriteFile(target, raw, 0600); err == nil {
						err = os.Symlink(target, file)
					}
				}
			case "symlink_dir":
				target := filepath.Join(t.TempDir(), "original")
				if err = os.Rename(dir, target); err == nil {
					err = os.Symlink(target, dir)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := campaign.inventoryWindowCampaign(); err == nil {
				t.Fatal("changed sealed evidence accepted")
			}
		})
	}
}
