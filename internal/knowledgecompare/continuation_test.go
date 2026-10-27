package knowledgecompare

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completionFixture(t *testing.T) (Contract, string, string, string) {
	t.Helper()
	d := t.TempDir()
	c := testContract()
	c.MaxMicroCNY = 20000000
	s, e := LoadSuite("../..")
	if e != nil {
		t.Fatal(e)
	}
	c.SuiteSHA = s.SHA
	parent := filepath.Join(d, "parent.json")
	cap, e := Acquire(parent, c, auth(c))
	if e != nil {
		t.Fatal(e)
	}
	journal := filepath.Join(d, "old.jsonl")
	g, e := NewGuard(journal, cap, roundFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(goodNative))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	resp, e := call(g, 1)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	g.Close()
	prior := Result{Schema: "knowledge-answer100-recording.v1", Status: "stopped", EvidenceKind: "real_provider", Contract: c, Suite: s, Cleanup: true, ActualUserTurns: 1, Tasks: []Task{{CaseID: "K001", Arm: "a", BusinessUnchanged: true, OwnedCleanup: true, Turns: []Turn{{Index: 1, Question: s.Cases[0].Question, HTTPStatus: 200}}}}, Events: g.Events(), Budget: g.State()}
	p := filepath.Join(d, "prior.json")
	b, _ := json.Marshal(prior)
	os.WriteFile(p, b, 0600)
	pb, _ := os.ReadFile(parent)
	jb, _ := os.ReadFile(journal)
	c.PriorRecordingSHA = knowledgepilot.SHA(b)
	c.PriorJournalSHA = knowledgepilot.SHA(jb)
	c.ParentClaimSHA = knowledgepilot.SHA(pb)
	return c, parent, p, journal
}
func TestCompletionInheritsCostsAndBlocksCompletedSlot(t *testing.T) {
	c, p, r, j := completionFixture(t)
	claim := filepath.Join(t.TempDir(), "continue.json")
	cap, e := AcquireCompletion(claim, c, auth(c), p, r, j)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	g, e := NewGuard(filepath.Join(t.TempDir(), "events"), cap, roundFunc(func(req *http.Request) (*http.Response, error) { calls++; return nil, io.EOF }))
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	if g.State().Calls != 1 || g.State().KnownUpper != 360 {
		t.Fatal("parent spent reset")
	}
	if _, e = call(g, 1); e == nil || calls != 0 {
		t.Fatal("completed slot resent")
	}
	if _, e = AcquireCompletion(claim, c, auth(c), p, r, j); e == nil {
		t.Fatal("completion reissued")
	}
}
func TestCompletionRejectsTamperedPriorBeforeClaim(t *testing.T) {
	c, p, r, j := completionFixture(t)
	os.WriteFile(j, []byte("tampered"), 0600)
	path := filepath.Join(t.TempDir(), "claim")
	if _, e := AcquireCompletion(path, c, auth(c), p, r, j); e == nil {
		t.Fatal("tamper accepted")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("invalid evidence consumed claim")
	}
}
func TestCompletionCanRunOnlyNewSlotAndKeepsAggregateCap(t *testing.T) {
	c, p, r, j := completionFixture(t)
	cap, e := AcquireCompletion(filepath.Join(t.TempDir(), "claim"), c, auth(c), p, r, j)
	if e != nil {
		t.Fatal(e)
	}
	g, e := NewGuard(filepath.Join(t.TempDir(), "events"), cap, roundFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(goodNative))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	req, _ := http.NewRequestWithContext(WithTurn(context.Background(), "K001-b", 1), "POST", ProviderURL, strings.NewReader(wire))
	resp, e := g.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if g.State().Calls != 2 || g.State().KnownUpper != 720 {
		t.Fatal("aggregate budget/calls not inherited")
	}
}

func TestCompletionChainRetainsAllClosedSegments(t *testing.T) {
	c, p, r, j := completionFixture(t)
	d := t.TempDir()
	child := filepath.Join(d, "child.json")
	cap, e := AcquireCompletion(child, c, auth(c), p, r, j)
	if e != nil {
		t.Fatal(e)
	}
	journal := filepath.Join(d, "second.jsonl")
	g, e := NewGuard(journal, cap, roundFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(goodNative))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequestWithContext(WithTurn(context.Background(), "K001-b", 1), "POST", ProviderURL, strings.NewReader(wire))
	resp, e := g.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	g.externalStop("observation_flush")
	g.Close()
	s, _ := LoadSuite("../..")
	prior := Result{Schema: "knowledge-answer100-recording.v1", Status: "stopped", EvidenceKind: "real_provider", Contract: c, Suite: s, Cleanup: true, PriorTasks: 1, PriorUserTurns: 1, ActualUserTurns: 1, Tasks: []Task{{CaseID: "K001", Arm: "b", BusinessUnchanged: true, OwnedCleanup: true, Turns: []Turn{{Index: 1, Question: s.Cases[0].Question, HTTPStatus: 200}}}}, Events: g.Events(), Budget: g.State()}
	rb, _ := json.Marshal(prior)
	rp := filepath.Join(d, "second.json")
	os.WriteFile(rp, rb, 0600)
	jb, _ := os.ReadFile(journal)
	pb, _ := os.ReadFile(child)
	next := c
	next.PriorRecordingSHA = knowledgepilot.SHA(rb)
	next.PriorJournalSHA = knowledgepilot.SHA(jb)
	next.ParentClaimSHA = knowledgepilot.SHA(pb)
	cp, e := AcquireCompletion(filepath.Join(d, "third-claim"), next, auth(next), child, rp, journal)
	if e != nil {
		t.Fatal(e)
	}
	if cp.inherited.Calls != 2 || cp.inherited.KnownUpper != 720 || len(cp.skip) != 2 || cp.priorTurns != 2 {
		t.Fatal("ancestor accounting or coverage reset")
	}
}

func TestCompletionRequiresVerifiedCleanupWhenOriginalCloseFailed(t *testing.T) {
	c, p, r, j := completionFixture(t)
	b, _ := os.ReadFile(r)
	var old Result
	json.Unmarshal(b, &old)
	old.Cleanup = false
	b, _ = json.Marshal(old)
	os.WriteFile(r, b, 0600)
	c.PriorRecordingSHA = knowledgepilot.SHA(b)
	dir := t.TempDir()
	claim := filepath.Join(dir, "claim")
	if _, e := AcquireCompletion(claim, c, auth(c), p, r, j); e == nil {
		t.Fatal("unverified cleanup accepted")
	}
	proof := []byte(`{"schema":"knowledge-cleanup-readback.v1","prior_recording_sha256":"` + c.PriorRecordingSHA + `","remaining_pair_schemas":0,"checked_at":"2026-10-27T18:30:00+08:00"}`)
	pp := filepath.Join(dir, "proof")
	os.WriteFile(pp, proof, 0600)
	c.CleanupProofSHA = knowledgepilot.SHA(proof)
	if _, e := AcquireCompletion(claim, c, auth(c), p, r, j, pp); e != nil {
		t.Fatal("separate cleanup proof rejected", e)
	}
}

func costBaselineFixture(t *testing.T, fullAccounting bool) (Contract, string, string, string) {
	c, p, r, j := completionFixture(t)
	raw, _ := os.ReadFile(r)
	var old Result
	json.Unmarshal(raw, &old)
	s, _ := LoadSuite("../..")
	old.Tasks = nil
	for _, q := range s.Cases {
		for _, arm := range []string{"a", "b"} {
			task := Task{CaseID: q.ID, Arm: arm, BusinessUnchanged: true, OwnedCleanup: true}
			for i, question := range q.Turns {
				task.Turns = append(task.Turns, Turn{Index: i + 1, Question: question, HTTPStatus: 200})
			}
			old.Tasks = append(old.Tasks, task)
		}
	}
	old.Status = "checkpoint_pending"
	old.Complete = true
	old.AllTasksDisposed = true
	old.ActualUserTurns = 224
	if fullAccounting {
		old.Events = nil
		old.Budget = GuardState{}
		for i := 0; i < 308; i++ {
			input := int64(100)
			hit, miss := int64(40), int64(60)
			if i == 307 {
				input = 600764
				hit = 0
				miss = input
			}
			response := strings.Replace(goodNative, `"prompt_tokens":100`, fmt.Sprintf(`"prompt_tokens":%d`, input), 1)
			response = strings.Replace(response, `"prompt_cache_hit_tokens":40`, fmt.Sprintf(`"prompt_cache_hit_tokens":%d`, hit), 1)
			response = strings.Replace(response, `"prompt_cache_miss_tokens":60`, fmt.Sprintf(`"prompt_cache_miss_tokens":%d`, miss), 1)
			task := old.Tasks[i%200]
			intent := Event{Kind: "intent", Task: task.CaseID + "-" + task.Arm, Turn: 1, Sequence: i + 1, RequestSHA: knowledgepilot.SHA([]byte(wire)), Request: json.RawMessage(wire)}
			usage := Event{Kind: "usage", Task: intent.Task, Turn: 1, Sequence: i + 1, Input: input, Output: 20, CacheHit: hit, CacheMiss: miss, UpperCost: price(input, 20, c), Response: json.RawMessage(response)}
			old.Events = append(old.Events, intent, usage)
			old.Budget.Calls++
			old.Budget.KnownUpper += usage.UpperCost
		}
		var journal []byte
		for _, e := range old.Events {
			b, _ := json.Marshal(e)
			journal = append(journal, append(b, '\n')...)
		}
		os.WriteFile(j, journal, 0600)
		c.PriorJournalSHA = knowledgepilot.SHA(journal)
	}
	raw, _ = json.Marshal(old)
	os.WriteFile(r, raw, 0600)
	c.PriorRecordingSHA = knowledgepilot.SHA(raw)
	return c, p, r, j
}
func TestCostStudyRequiresCompleteBaselineAndKeepsParentSpend(t *testing.T) {
	c, p, r, j := costBaselineFixture(t, true)
	path := filepath.Join(t.TempDir(), "study")
	if _, e := AcquireCompletion(path, c, auth(c), p, r, j); e == nil {
		t.Fatal("completed scope resumed without distinct study")
	}
	c.CostStudy = "knowledge-resident-cost.v1"
	cap, e := AcquireCompletion(path, c, auth(c), p, r, j)
	if e != nil {
		t.Fatal(e)
	}
	if cap.inherited.Calls != 308 || cap.inherited.KnownUpper != 1312208 || len(cap.skip) != 0 || cap.priorTurns != 0 {
		t.Fatal("study reset budget or reused old completion counts")
	}
	if _, e := AcquireCompletion(path, c, auth(c), p, r, j); e == nil {
		t.Fatal("study reissued")
	}
}
func TestCostStudyRejectsUnderaccountedCompleteBaseline(t *testing.T) {
	c, p, r, j := costBaselineFixture(t, false)
	c.CostStudy = "knowledge-resident-cost.v1"
	path := filepath.Join(t.TempDir(), "study")
	if _, e := AcquireCompletion(path, c, auth(c), p, r, j); e == nil {
		t.Fatal("1-call fake complete baseline accepted")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("invalid baseline wrote claim")
	}
}
func TestCostStudyRejectsMissingCallAndFalseCompletion(t *testing.T) {
	for _, kind := range []string{"missing_call", "false_complete"} {
		t.Run(kind, func(t *testing.T) {
			c, p, r, j := costBaselineFixture(t, true)
			b, _ := os.ReadFile(r)
			var old Result
			json.Unmarshal(b, &old)
			if kind == "false_complete" {
				old.Complete = false
			} else {
				last := old.Events[len(old.Events)-1]
				old.Budget.Calls--
				old.Budget.KnownUpper -= last.UpperCost
				old.Events = old.Events[:len(old.Events)-2]
				var journal []byte
				for _, e := range old.Events {
					x, _ := json.Marshal(e)
					journal = append(journal, append(x, '\n')...)
				}
				os.WriteFile(j, journal, 0600)
				c.PriorJournalSHA = knowledgepilot.SHA(journal)
			}
			b, _ = json.Marshal(old)
			os.WriteFile(r, b, 0600)
			c.PriorRecordingSHA = knowledgepilot.SHA(b)
			c.CostStudy = "knowledge-resident-cost.v1"
			if _, e := AcquireCompletion(filepath.Join(t.TempDir(), "claim"), c, auth(c), p, r, j); e == nil {
				t.Fatal("invalid baseline accepted")
			}
		})
	}
}
