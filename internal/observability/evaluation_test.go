package observability

import (
	"bytes"
	"context"
	"testing"
)

func TestEvaluationCorrelatesTurnsWithoutCallingReviewPendingFailure(t *testing.T) {
	server, capture := receiver(t)
	defer server.Close()
	o, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, run := o.StartEvaluation(context.Background(), Evaluation{RunID: "batch-contract", CaseKey: "balance-020/explicit_scope_followup", Arm: "jev_selective", EvidenceKind: "local_stub"})
	for i := 0; i < 2; i++ {
		turnCtx, turn := o.StartTurn(ctx, "jev_selective")
		_, operation := Start(turnCtx, "llm.attempt", "generation")
		operation.End(nil)
		if turn.TraceID() != run.TraceID() || turn.TraceID() == "" {
			t.Fatal("turn not linked to task trace")
		}
		turn.End(200, Summary{Pending: i == 0})
	}
	run.End(EvaluationResult{Status: "needs_review", StructuralPass: true, NecessaryClarifications: 1})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.spans) != 5 {
		t.Fatalf("spans=%d", len(capture.spans))
	}
	for _, span := range capture.spans {
		a := attrs(span)
		if a["langfuse.observation.metadata.case_key"] != "balance-020/explicit_scope_followup" || a["langfuse.observation.metadata.evidence_kind"] != "local_stub" || a["langfuse.observation.metadata.arm"] != "jev_selective" {
			t.Fatal("missing evaluation provenance", a)
		}
		if !bytes.Equal(span.TraceId, capture.spans[0].TraceId) {
			t.Fatal("task trace split")
		}
		if span.Name == "evaluation.task" {
			if a["langfuse.observation.metadata.status"] != "needs_review" || a["langfuse.observation.metadata.necessary_clarifications"] != "1" || a["langfuse.observation.metadata.task_success"] != "unknown" || span.Status.GetCode() != 0 {
				t.Fatalf("pending review mislabeled: %+v", span)
			}
		}
		if span.Name == "agent.turn" && a["langfuse.observation.metadata.turn_index"] != "1" && a["langfuse.observation.metadata.turn_index"] != "2" {
			t.Fatal("missing turn index")
		}
	}
}

func TestDisabledEvaluationIsNoOp(t *testing.T) {
	var o *Observer
	ctx, run := o.StartEvaluation(context.Background(), Evaluation{})
	_, turn := o.StartTurn(ctx, "model")
	if run.TraceID() != "" || turn.TraceID() != "" {
		t.Fatal("disabled trace claimed")
	}
	run.End(EvaluationResult{})
}
