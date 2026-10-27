package knowledgecompare

import (
	"context"
	"encoding/json"
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
