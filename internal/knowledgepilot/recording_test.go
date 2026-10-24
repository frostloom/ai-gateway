package knowledgepilot

import (
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"testing"
)

func completeLocalRecording(t *testing.T) (Recording, []ScoredTask) {
	t.Helper()
	_, s := template(t)
	in, e := LoadInputs("../..", s)
	if e != nil {
		t.Fatal(e)
	}
	r := Recording{EvidenceKind: "local_stub", Scope: s, Suite: in.Cases, Complete: true}
	var reviews []ScoredTask
	a, e := NormalizeOpenAI([]byte(goodResponse))
	if e != nil {
		t.Fatal(e)
	}
	for i, x := range BuildOrder(in.Cases) {
		m := s.A
		if x.Arm == "b" {
			m = s.B
		}
		trace := fmt.Sprintf("%032x", i+1)
		task := RecordedTask{CaseID: x.CaseID, Arm: x.Arm, Identity: x.Identity, Tenant: uint64(i + 1), Session: x.Identity, Reply: "SYNTHETIC", HTTPStatus: 200, Trace: json.RawMessage(`[]`), Telemetry: agent.TurnTelemetry{LangfuseTraceID: trace, LLMCalls: 1}, Runtime: m, DurationMS: 10, Attempts: []knowledgeeval.Attempt{a}, StateOK: true, BusinessUnchanged: true, Before: json.RawMessage(`{"balance":25}`), After: json.RawMessage(`{"balance":25}`)}
		r.Tasks = append(r.Tasks, task)
		r.Readback = append(r.Readback, Readback{TaskID: x.Identity, TraceID: trace, Verified: true, Observations: 1, ReceiptSHA: SHA([]byte("synthetic-receipt-" + x.Identity))})
		r.Events = append(r.Events, Event{Kind: "reserved", TaskID: x.Identity, Sequence: i + 1, RequestSHA: SHA([]byte(requestBody)), Request: json.RawMessage(requestBody)}, Event{Kind: "completed", Status: 200, TaskID: x.Identity, Sequence: i + 1, RequestSHA: SHA([]byte(requestBody)), Response: json.RawMessage(goodResponse), Attempt: &a})
		reviews = append(reviews, ScoredTask{TaskID: x.Identity, ReplySHA: SHA([]byte(task.Reply)), Review: knowledgeeval.Review{Correct: false, Claims: 1, Reference: "synthetic independent rubric fixture"}})
	}
	return r, reviews
}
func TestRecordingRejectsPartialMissingReviewOrEvidence(t *testing.T) {
	r, rv := completeLocalRecording(t)
	if _, e := Comparison(r, rv); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"partial", "review", "journal", "reply", "state"} {
		r, rv := completeLocalRecording(t)
		switch kind {
		case "partial":
			r.Complete = false
		case "review":
			rv = rv[:143]
		case "journal":
			r.Events = nil
		case "reply":
			r.Tasks[0].Reply = "changed"
		case "state":
			r.Tasks[0].StateOK = false
			rv[0].Review.Correct = true
		}
		if _, e := Comparison(r, rv); e == nil {
			t.Fatal("accepted missing " + kind)
		}
	}
}
func TestRecordingRequiresObserverReadback(t *testing.T) {
	for _, kind := range []string{"missing", "unverified", "mismatch"} {
		r, rv := completeLocalRecording(t)
		switch kind {
		case "missing":
			r.Readback = nil
		case "unverified":
			r.Readback[0].Verified = false
		case "mismatch":
			r.Readback[0].TraceID = "different"
		}
		if _, e := Comparison(r, rv); e == nil {
			t.Fatal("accepted bad readback " + kind)
		}
	}
}
func TestRecordingRecallUsesFirstRetrieval(t *testing.T) {
	r, rv := completeLocalRecording(t)
	b, _ := json.Marshal(r.Suite[0].RequiredEvidence[0])
	r.Tasks[1].Searches = [][]json.RawMessage{{}, {b}}
	x, e := Comparison(r, rv)
	if e != nil {
		t.Fatal(e)
	}
	if len(x.B.Tasks[0].Retrieved) != 0 {
		t.Fatal("later retrieval inflated first recall")
	}
	report, e := knowledgeeval.BuildReport(x)
	if e != nil || report.EffectClaimsAllowed || report.EndToEnd {
		t.Fatal("local arithmetic claimed real benefit", e)
	}
}
