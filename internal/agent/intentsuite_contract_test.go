package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestContractIntentSuiteLoadsFrozen1220(t *testing.T) {
	path := filepath.Join("..", "..", "eval", "contracts", "2026-10-09-customer-service-v1", "cases.json")
	normal, adv, digest, err := LoadContractIntentSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(normal) != 1200 || len(adv) != 20 {
		t.Fatalf("counts=%d/%d", len(normal), len(adv))
	}
	raw, _ := os.ReadFile(path)
	if digest != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatal("hash differs from actual cases bytes")
	}
	changed := 0
	for _, sample := range append(normal, adv...) {
		saved, _ := json.Marshal(sample)
		var fields map[string]any
		json.Unmarshal(saved, &fields)
		original, ok := fields["original"].(map[string]any)
		if !ok || original["id"] != sample.ID || fields["original_text"] != original["text"] || fields["contract_case_id"] != "cs-contract-v1:"+sample.ID {
			t.Fatalf("source evidence lost: %s", saved)
		}
		if sample.Text != original["text"] {
			changed++
		}
		if sample.AskBack != (fields["expected_action"] == "clarify") {
			t.Fatalf("clarification mapping wrong: %s", saved)
		}
	}
	if changed != 19 {
		t.Fatalf("normalized=%d want19", changed)
	}
}
func TestContractIntentSuiteRejectsInvalidEvidence(t *testing.T) {
	path := filepath.Join("..", "..", "eval", "contracts", "2026-10-09-customer-service-v1", "cases.json")
	raw, _ := os.ReadFile(path)
	var all []map[string]any
	json.Unmarshal(raw, &all)
	for _, kind := range []string{"duplicate-source", "missing-original", "empty-text", "wrong-original-id", "wrong-schema", "wrong-status", "unproven-normalization"} {
		t.Run(kind, func(t *testing.T) {
			copyRaw, _ := json.Marshal(all[:1])
			var rows []map[string]any
			json.Unmarshal(copyRaw, &rows)
			switch kind {
			case "duplicate-source":
				rows = append(rows, rows[0])
			case "missing-original":
				delete(rows[0], "original")
			case "empty-text":
				rows[0]["text"] = " "
			case "wrong-original-id":
				rows[0]["original"].(map[string]any)["id"] = "wrong"
			case "wrong-status":
				rows[0]["annotation_status"] = "independent_gold"
			case "unproven-normalization":
				rows[0]["text"] = "changed without proof"
			}
			data, _ := json.Marshal(rows)
			dir := t.TempDir()
			casePath := filepath.Join(dir, "cases.json")
			os.WriteFile(casePath, data, 0600)
			schema := 1
			if kind == "wrong-schema" {
				schema = 2
			}
			manifest, _ := json.Marshal(map[string]any{"schema_version": schema, "version": "customer-service-contract-v1", "count": len(rows), "annotation_status": "ai_posthoc_not_independent_gold", "artifacts": map[string]string{"cases.json": fmt.Sprintf("%x", sha256.Sum256(data))}})
			os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600)
			if _, _, _, err := LoadContractIntentSuite(casePath); err == nil {
				t.Fatal("accepted " + kind)
			}
		})
	}
}
