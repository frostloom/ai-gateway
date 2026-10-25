package taskbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepeatedDiagnosticPublicTemplateStaysUnready(t *testing.T) {
	dir := filepath.Join("..", "..", "eval", "paired-pilots", "2026-10-25-repeated-diagnostic-entry")
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	auth, plan, billing, evidence := read("authorization.example.json"), read("plan.json"), read("billing.example.json"), read("evidence.txt")
	pilot, err := CheckAuthorizedRepeatedDiagnostic(auth, plan, billing, evidence)
	if err != nil || pilot.Ready() || pilot.HumanAuthorized() || !pilot.IsRepeatedDiagnostic() {
		t.Fatalf("false template became executable: %v ready=%v human=%v diag=%v blockers=%v", err, pilot.Ready(), pilot.HumanAuthorized(), pilot.IsRepeatedDiagnostic(), pilot.Blockers())
	}
	if pilot.ExecutionRevision() != "0000000000000000000000000000000000000000" || pilot.RepeatedDiagnosticBinding() != v5RepeatedDiagnosticBinding() || pilot.ExecutionRevision() == pilot.RepeatedDiagnosticBinding().ObserverRevision {
		t.Fatal("false template froze an execution revision or dropped the observer binding")
	}
	var budget map[string]any
	if json.Unmarshal(read("budget-allocation.json"), &budget) != nil {
		t.Fatal("budget")
	}
	for key, want := range map[string]float64{
		"currently_reserved_micro_cny": 12300288, "currently_reserved_micro_usd": 94080,
		"new_declared_micro_cny": 8380416, "new_declared_micro_usd": 64512,
		"cumulative_if_claimed_micro_cny": 20680704, "cumulative_if_claimed_micro_usd": 158592,
		"unallocated_if_claimed_micro_cny": 79319296, "unallocated_if_claimed_micro_usd": 841408,
	} {
		if budget[key] != want {
			t.Fatal("reservation integer drift", key, budget[key])
		}
	}
	if budget["new_claim_exists"] != false || budget["actual_reservation_changed"] != false || budget["pairs_17_to_24_resumed"] != false {
		t.Fatal("budget flags authorize a run")
	}
	sum := sha256.Sum256(read("prior-repeated-claim.json"))
	if hex.EncodeToString(sum[:]) != V5RepeatedClaimSHA {
		t.Fatal("public seventh predecessor is not the consumed claim")
	}
	claim := filepath.Join("..", "..", ".tmp", "paired-live-retests", V5RepeatedDiagnosticID+".json")
	if _, err = os.Stat(claim); !os.IsNotExist(err) {
		t.Fatal("checking the false template created a claim", err)
	}
}
