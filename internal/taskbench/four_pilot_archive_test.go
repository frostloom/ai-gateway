package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The real attempt is immutable evidence, including its failures. Reading it
// must never execute providers or promote the independent review into raw pass.
func TestFourPilotArchiveIntegrityAndOfflineReport(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-14-four-task-v1"
	raw, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if conversationHash(raw) != "d48b80f7b26699c985a552e5e952363882cbeade07c71b95636a9137d08281d1" {
		t.Fatal("frozen four-task manifest changed")
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || len(manifest.Files) != 10 {
		t.Fatal("invalid frozen manifest", err)
	}
	for name, hash := range manifest.Files {
		if !filepath.IsLocal(name) {
			t.Fatal("nonlocal archive path", name)
		}
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || conversationHash(data) != hash {
			t.Fatal("frozen artifact changed", name, err)
		}
	}
	report, err := ReportPreparedPair(filepath.Join(base, "attempt-01/run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if report.FinishedArms != 8 || report.PlannedCases != 4 || report.UnrunArms != 0 || report.CompletionRate != nil || report.EffectClaimsAllowed {
		t.Fatal("incorrect real attempt evidence scope")
	}
	counts := map[string]int{}
	for _, row := range report.Rows {
		counts[row.Status]++
	}
	if counts["needs_review"] != 2 || counts["failed"] != 6 {
		t.Fatal("raw failures or pending reviews changed", counts)
	}
}
