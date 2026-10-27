package taskbench

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func windowClaimFixture(t *testing.T) fullWindowClaimDeclaration {
	t.Helper()
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{100, 10}
	}
	scope, e := prepareFullWindowScope(s, p, 100, caps, b)
	if e != nil {
		t.Fatal(e)
	}
	h, tot, e := validateFullWindowScope(scope, s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	return fullWindowClaimDeclaration{ScopeSHA256: h, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: b.PolicySHA256, Windows: scope.Windows, ModelTotal: tot.ModelCap, JEVTotal: tot.JEVCap}
}

func TestWindowClaimExclusiveAndPermanent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	d := windowClaimFixture(t)
	r, e := createFirstFullWindowClaim(root, d)
	if e != nil {
		t.Fatal(e)
	}
	if r.ModelPermanent != 29061220 || r.JEVPermanent != 223114 || r.Index != 1 || r.PreviousSHA256 != "" || !comparisonDigest(r.SHA256) {
		t.Fatal(r)
	}
	before, _ := os.ReadFile(filepath.Join(root, "claim-000001.json"))
	if _, e = createFirstFullWindowClaim(root, d); e == nil {
		t.Fatal("same scope reclaimed")
	}
	d.ScopeSHA256 = strings.Repeat("f", 64)
	if _, e = createFirstFullWindowClaim(root, d); e == nil {
		t.Fatal("renamed scope reset budget or bypassed pending predecessor")
	}
	after, _ := os.ReadFile(filepath.Join(root, "claim-000001.json"))
	if string(after) != string(before) {
		t.Fatal("permanent claim changed")
	}
}

func TestWindowClaimRejectsOverflowingAttemptTotals(t *testing.T) {
	d := windowClaimFixture(t)
	for i := 0; i < 8; i++ {
		d.Windows[i].MaxJEVCalls += 1 << 61
		d.Windows[i].MaxModelCalls += 1 << 62
	}
	root := filepath.Join(t.TempDir(), "registry")
	if _, e := createFirstFullWindowClaim(root, d); e == nil {
		t.Fatal("wrapped attempt declaration accepted")
	}
	if _, e := os.Stat(root); !os.IsNotExist(e) {
		t.Fatal("invalid declaration persisted")
	}
}

func TestWindowClaimConcurrentCallsUseOneGlobalSlot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	d := windowClaimFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := createFirstFullWindowClaim(root, d); results <- e }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes %d", successes)
	}
}

func TestWindowClaimConcurrentProcessesUseOneGlobalSlot(t *testing.T) {
	d := windowClaimFixture(t)
	if root := os.Getenv("AI_GATEWAY_WINDOW_CLAIM_HELPER"); root != "" {
		if _, e := createFirstFullWindowClaim(root, d); e != nil {
			os.Exit(23)
		}
		os.Exit(0)
	}
	root := filepath.Join(t.TempDir(), "registry")
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestWindowClaimConcurrentProcessesUseOneGlobalSlot$")
			cmd.Env = append(os.Environ(), "AI_GATEWAY_WINDOW_CLAIM_HELPER="+root)
			results <- cmd.Run()
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
			continue
		}
		exit, ok := e.(*exec.ExitError)
		if !ok || exit.ExitCode() != 23 {
			t.Fatal("unexpected child failure", e)
		}
	}
	if successes != 1 {
		t.Fatalf("process successes %d", successes)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 2 {
		t.Fatal("unexpected registry evidence", entries, e)
	}
}

func TestWindowClaimRejectsInvalidBeforePersistence(t *testing.T) {
	for _, name := range []string{"model-over", "jev-over", "zero", "identity", "order", "sum"} {
		t.Run(name, func(t *testing.T) {
			d := windowClaimFixture(t)
			switch name {
			case "model-over":
				d.ModelTotal = 70938881
			case "jev-over":
				d.JEVTotal = 776897
			case "zero":
				d.Windows[0].ModelCap = 0
			case "identity":
				d.PolicySHA256 = ""
			case "order":
				d.Windows[0].ID = "window-002"
			case "sum":
				d.ModelTotal++
			}
			root := filepath.Join(t.TempDir(), "registry")
			if _, e := createFirstFullWindowClaim(root, d); e == nil {
				t.Fatal("invalid declaration accepted")
			}
			if _, e := os.Stat(root); !os.IsNotExist(e) {
				t.Fatal("invalid declaration persisted")
			}
		})
	}
}

func TestWindowClaimIncompleteAndTamperedEvidenceBlocksExpansion(t *testing.T) {
	for _, name := range []string{"partial", "orphan", "altered", "symlink"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "registry")
			d := windowClaimFixture(t)
			if _, e := createFirstFullWindowClaim(root, d); e != nil {
				t.Fatal(e)
			}
			file := filepath.Join(root, "claim-000001.json")
			switch name {
			case "partial":
				os.WriteFile(file, []byte("{"), 0600)
			case "orphan":
				os.WriteFile(filepath.Join(root, "unknown.json"), []byte("{}"), 0600)
			case "altered":
				raw, _ := os.ReadFile(file)
				var v map[string]any
				json.Unmarshal(raw, &v)
				v["permanent_model_micro_cny"] = float64(0)
				raw, _ = json.Marshal(v)
				os.WriteFile(file, raw, 0600)
			case "symlink":
				os.Rename(file, file+".saved")
				os.Symlink(file+".saved", file)
			}
			if _, e := createFirstFullWindowClaim(root, d); e == nil {
				t.Fatal("untrusted predecessor accepted")
			}
		})
	}
}
