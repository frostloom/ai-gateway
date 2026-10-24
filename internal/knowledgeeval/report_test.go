package knowledgeeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func n(v int64) *int64      { return &v }
func ms(v float64) *float64 { return &v }
func fixture() ComparisonInput {
	cfg := Config{Model: "test", Protocol: "openai", RoutingMode: "model", ProfileSHA: strings.Repeat("a", 64), Revision: strings.Repeat("b", 40), SuiteSHA: strings.Repeat("c", 64), PromptSHA: strings.Repeat("d", 64), ToolsSHA: strings.Repeat("e", 64), TariffSHA: strings.Repeat("f", 64), OutputBudget: 128, ThinkingMode: "disabled"}
	x := ComparisonInput{Schema: "knowledge-comparison.v1", EvidenceKind: "local_stub", Clock: "client_request_to_response", A: Arm{Config: cfg}, B: Arm{Config: cfg}}
	x.B.Config.CorpusVersion = strings.Repeat("1", 64)
	x.B.Config.PromptSHA = strings.Repeat("2", 64)
	x.B.Config.ToolsSHA = strings.Repeat("3", 64)
	for i := 0; i < 72; i++ {
		category := "knowledge"
		if i >= 36 {
			category = []string{"unknown", "dynamic", "safety"}[(i-36)/12]
		}
		id := fmt.Sprintf("case-%02d", i)
		c := Case{ID: id, Question: "question " + id, Category: category, RequiredEvidence: []Evidence{{ChunkID: "KB-001:1", SHA256: strings.Repeat("2", 64), CorpusVersion: strings.Repeat("1", 64)}}}
		if category == "dynamic" {
			c.ExpectedTools = []string{"get_balance"}
		}
		x.Suite = append(x.Suite, c)
		task := Task{ActualTools: []string{"get_balance"}, AttemptsComplete: true, CaseID: id, Review: &Review{Correct: true, Claims: 1, Citations: 1, SupportedCitations: 1, Reference: "independent-review"}, DurationMS: ms(10), Attempts: []Attempt{{Outcome: "success", Currency: "CNY", Input: n(1000), Output: n(0), CacheRead: n(0), CacheCreation: n(0), Price: &Price{Input: n(1000000), Output: n(0), CacheRead: n(0), CacheCreation: n(0), Verified: true}}}}
		x.A.Tasks = append(x.A.Tasks, task)
		copy := task
		copy.Retrieved = []Evidence{{ChunkID: "KB-001:1", SHA256: strings.Repeat("2", 64), CorpusVersion: strings.Repeat("1", 64)}}
		v := *task.Review
		copy.Review = &v
		copy.Attempts = append([]Attempt(nil), task.Attempts...)
		x.B.Tasks = append(x.B.Tasks, copy)
	}
	b, _ := json.Marshal(x.Suite)
	sum := sha256.Sum256(b)
	x.A.Config.SuiteSHA = hex.EncodeToString(sum[:])
	x.B.Config.SuiteSHA = x.A.Config.SuiteSHA
	return x
}
func TestUnknownUsageIsNotZero(t *testing.T) {
	x := fixture()
	x.B.Tasks[0].Attempts[0].Input = nil
	r, e := BuildReport(x)
	if e != nil {
		t.Fatal(e)
	}
	if r.B.TotalMicro["CNY"] != nil || r.EffectClaimsAllowed {
		t.Fatal("unknown priced as zero")
	}
}
func TestFailedAttemptsCountInCost(t *testing.T) {
	x := fixture()
	x.B.Tasks[0].Review.Correct = false
	a := x.B.Tasks[0].Attempts[0]
	a.Input = n(500)
	x.B.Tasks[0].Attempts = append(x.B.Tasks[0].Attempts, a)
	r, e := BuildReport(x)
	if e != nil {
		t.Fatal(e)
	}
	if *r.B.TotalMicro["CNY"] != 72500 || r.B.Correct != 71 || r.B.CostPerCorrectMicro["CNY"] == nil {
		t.Fatalf("failure cost omitted: %+v", r.B)
	}
}
func TestZeroCorrectHasNoUnitCost(t *testing.T) {
	x := fixture()
	for i := range x.B.Tasks {
		x.B.Tasks[i].Review.Correct = false
	}
	r, e := BuildReport(x)
	if e != nil || r.B.CostPerCorrectMicro["CNY"] != nil {
		t.Fatal("invalid zero denominator")
	}
}
func TestCurrencySeparation(t *testing.T) {
	x := fixture()
	x.B.Tasks[0].Attempts[0].Currency = "USD"
	r, e := BuildReport(x)
	if e != nil || *r.B.TotalMicro["USD"] != 1000 || *r.B.TotalMicro["CNY"] != 71000 {
		t.Fatal("currency mixed")
	}
}
func TestKnowledgeComparisonRejectsConfounds(t *testing.T) {
	for _, f := range []func(*ComparisonInput){func(x *ComparisonInput) { x.B.Config.RoutingMode = "jev_selective" }, func(x *ComparisonInput) { x.B.Config.Model = "other" }, func(x *ComparisonInput) { x.B.Config.OutputBudget = 256 }, func(x *ComparisonInput) { x.B.Config.Revision = strings.Repeat("0", 40) }, func(x *ComparisonInput) { x.B.Config.ProfileSHA = strings.Repeat("0", 64) }, func(x *ComparisonInput) { x.B.Tasks[0].CaseID = "unmatched" }} {
		x := fixture()
		f(&x)
		if _, e := BuildReport(x); e == nil {
			t.Fatal("accepted confounded/unpaired comparison")
		}
	}
}
func TestCategoryDenominators(t *testing.T) {
	r, e := BuildReport(fixture())
	if e != nil {
		t.Fatal(e)
	}
	if r.B.Categories["knowledge"].N != 36 || r.B.Categories["safety"].N != 12 || r.EffectClaimsAllowed {
		t.Fatal("wrong denominators or stub claims allowed")
	}
}
func TestUnreviewedBlocksQualityClaims(t *testing.T) {
	x := fixture()
	x.EvidenceKind = "recorded_run"
	x.B.Tasks[0].Review = nil
	r, e := BuildReport(x)
	if e != nil || r.B.CompletionRate != nil || r.EffectClaimsAllowed {
		t.Fatal("unreviewed treated as success")
	}
}
func TestClockBoundaries(t *testing.T) {
	x := fixture()
	x.Clock = "runner"
	r, e := BuildReport(x)
	if e != nil || r.EndToEnd {
		t.Fatal("runner called E2E")
	}
}
func TestInvalidScoresAndUsage(t *testing.T) {
	x := fixture()
	x.B.Tasks[0].Review.Unsupported = 2
	if _, e := BuildReport(x); e == nil {
		t.Fatal("invalid review accepted")
	}
	x = fixture()
	x.B.Tasks[0].Attempts[0].Input = n(-1)
	if _, e := BuildReport(x); e == nil {
		t.Fatal("negative usage accepted")
	}
}
func TestExactPairInterval(t *testing.T) {
	l, u := exactInterval(0, 10)
	if l != 0 || u < .30 || u > .31 {
		t.Fatalf("bad Clopper Pearson interval: %g %g", l, u)
	}
}

func TestMissingTariffNotZero(t *testing.T) {
	b, _ := json.Marshal(fixture())
	var raw map[string]any
	json.Unmarshal(b, &raw)
	price := raw["b"].(map[string]any)["tasks"].([]any)[0].(map[string]any)["attempts"].([]any)[0].(map[string]any)["price"].(map[string]any)
	delete(price, "output_micro_per_million")
	b, _ = json.Marshal(raw)
	var x ComparisonInput
	json.Unmarshal(b, &x)
	r, e := BuildReport(x)
	if e != nil || r.B.TotalMicro["CNY"] != nil {
		t.Fatal("missing tariff treated as zero")
	}
}
func TestDynamicRequiresObservedTool(t *testing.T) {
	x := fixture()
	x.Suite[48].ExpectedTools = []string{"get_balance"}
	x.B.Tasks[48].ActualTools = nil
	r, e := BuildReport(x)
	if e != nil {
		t.Fatal(e)
	}
	if r.B.Gates["dynamic"] != "fail" {
		t.Fatal("review alone bypassed real-time tool check")
	}
}
func TestContradictoryReviewRejected(t *testing.T) {
	x := fixture()
	x.B.Tasks[0].Review.CriticalErrors = 1
	if _, e := BuildReport(x); e == nil {
		t.Fatal("critical error declared correct")
	}
}
func TestAlteredGoldRejected(t *testing.T) {
	x := fixture()
	x.Suite[0].RequiredFacts = []string{"altered gold"}
	if _, e := BuildReport(x); e == nil {
		t.Fatal("suite hash not bound to gold")
	}
}
func TestDeltasAndCostGate(t *testing.T) {
	x := fixture()
	r, e := BuildReport(x)
	if e != nil {
		t.Fatal(e)
	}
	if r.TotalCostChangePct["CNY"] == nil || *r.TotalCostChangePct["CNY"] != 0 || r.KnowledgeChangePP == nil || *r.KnowledgeChangePP != 0 || r.CostReductionGate == "pass" {
		t.Fatal("wrong optimization deltas")
	}
}
func TestFrozenSuiteIdentity(t *testing.T) {
	raw, e := os.ReadFile("../../eval/knowledge/acceptance-v1/questions.json")
	if e != nil {
		t.Fatal(e)
	}
	var suite struct {
		Schema string `json:"schema"`
		Cases  []Case `json:"cases"`
	}
	if json.Unmarshal(raw, &suite) != nil || len(suite.Cases) != 72 {
		t.Fatal("invalid frozen suite")
	}
	var m struct {
		SuiteSHA string            `json:"suite_canonical_sha256"`
		Files    map[string]string `json:"files"`
	}
	raw, e = os.ReadFile("../../eval/knowledge/acceptance-v1/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(raw, &m)
	canonical, _ := json.Marshal(suite.Cases)
	h := sha256.Sum256(canonical)
	if hex.EncodeToString(h[:]) != m.SuiteSHA {
		t.Fatal("canonical suite mismatch")
	}
	for f, want := range m.Files {
		b, e := os.ReadFile("../../eval/knowledge/acceptance-v1/" + f)
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != want {
			t.Fatalf("changed frozen file %s", f)
		}
	}
}
func TestAttemptOutcomeAndCacheCounts(t *testing.T) {
	x := fixture()
	x.B.Tasks[0].Attempts[0].Outcome = "timeout"
	x.B.Tasks[0].Attempts[0].CacheRead = n(25)
	r, e := BuildReport(x)
	if e != nil || r.B.FailedAttempts != 1 || r.B.Timeouts != 1 || r.B.CacheRead == nil || *r.B.CacheRead != 25 {
		t.Fatal("lost failure/cache metrics")
	}
}
func TestMissingAndNullReviewCannotPass(t *testing.T) {
	for _, field := range []string{"correct", "claims", "unsupported", "citations", "supported_citations", "critical_errors", "independent_review_reference"} {
		for _, null := range []bool{false, true} {
			b, _ := json.Marshal(fixture())
			var raw map[string]any
			json.Unmarshal(b, &raw)
			review := raw["b"].(map[string]any)["tasks"].([]any)[0].(map[string]any)["review"].(map[string]any)
			if null {
				review[field] = nil
			} else {
				delete(review, field)
			}
			b, _ = json.Marshal(raw)
			var x ComparisonInput
			e := json.Unmarshal(b, &x)
			if e != nil {
				continue
			}
			r, e := BuildReport(x)
			if e == nil && (r.QualityComparable || r.CostReductionGate == "pass") {
				t.Fatalf("missing/null %s admitted", field)
			}
		}
	}
}
func TestNonexistentChunkCannotRecall(t *testing.T) {
	x := fixture()
	for i, c := range x.Suite {
		if c.Category == "knowledge" {
			x.B.Tasks[i].Retrieved = []Evidence{{ChunkID: "KB-001:999", SHA256: strings.Repeat("2", 64), CorpusVersion: strings.Repeat("1", 64)}}
		}
	}
	r, e := BuildReport(x)
	if e != nil {
		t.Fatal(e)
	}
	if r.B.RecallAt3 != nil && *r.B.RecallAt3 > 0 {
		t.Fatal("nonexistent chunk counted as complete evidence")
	}
}
func TestZeroKnowledgeCitationCoverageBlocksCostGate(t *testing.T) {
	x := fixture()
	x.EvidenceKind = "recorded_run"
	for i, c := range x.Suite {
		if c.Category == "knowledge" {
			x.B.Tasks[i].Review.Citations = 0
			x.B.Tasks[i].Review.SupportedCitations = 0
		}
		x.B.Tasks[i].Attempts[0].Input = n(500)
	}
	r, e := BuildReport(x)
	if e != nil {
		t.Fatal(e)
	}
	if r.CostReductionGate == "pass" {
		t.Fatal("zero source coverage passed cost gate")
	}
}
func TestWrongEvidenceHashOrVersionCannotRecall(t *testing.T) {
	for _, field := range []string{"sha", "version"} {
		x := fixture()
		for i, c := range x.Suite {
			if c.Category == "knowledge" {
				if field == "sha" {
					x.B.Tasks[i].Retrieved[0].SHA256 = strings.Repeat("f", 64)
				} else {
					x.B.Tasks[i].Retrieved[0].CorpusVersion = strings.Repeat("f", 64)
				}
			}
		}
		r, e := BuildReport(x)
		if e != nil {
			t.Fatal(e)
		}
		if r.B.RecallAt3 != nil && *r.B.RecallAt3 != 0 {
			t.Fatal("wrong digest/version credited")
		}
	}
}
