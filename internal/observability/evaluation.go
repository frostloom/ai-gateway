package observability

import (
	"context"
	"strconv"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Evaluation contains only public frozen case identifiers, never credentials.
type Evaluation struct{ RunID, CaseKey, Arm, EvidenceKind string }
type EvaluationResult struct {
	Status                                string
	StructuralPass                        bool
	NecessaryClarifications, HTTPFailures int
}
type EvaluationRun struct {
	root   trace.Span
	common []attribute.KeyValue
	turns  atomic.Int64
}
type evaluationKey struct{}

// StartEvaluation groups one complete task arm. Each request is a child span;
// unrelated production requests still start new roots, ignoring remote parents.
func (o *Observer) StartEvaluation(ctx context.Context, e Evaluation) (context.Context, *EvaluationRun) {
	if o == nil {
		return ctx, nil
	}
	common := []attribute.KeyValue{
		attribute.String("langfuse.environment", o.cfg.Environment), attribute.String("langfuse.release", o.cfg.Release),
		attribute.StringSlice("langfuse.trace.tags", []string{o.cfg.Environment, e.EvidenceKind, e.Arm}),
		meta("run_id", e.RunID), meta("case_key", e.CaseKey), meta("arm", e.Arm), meta("evidence_kind", e.EvidenceKind),
	}
	ctx, span := o.tracer.Start(ctx, "evaluation.task", trace.WithNewRoot(), trace.WithAttributes(append(common, attribute.String("langfuse.observation.type", "span"))...))
	run := &EvaluationRun{root: span, common: common}
	return context.WithValue(ctx, evaluationKey{}, run), run
}
func (r *EvaluationRun) TraceID() string {
	if r == nil || !r.root.SpanContext().IsSampled() {
		return ""
	}
	return r.root.SpanContext().TraceID().String()
}
func (t *Turn) TraceID() string {
	if t == nil || !t.root.SpanContext().IsSampled() {
		return ""
	}
	return t.root.SpanContext().TraceID().String()
}
func (r *EvaluationRun) End(v EvaluationResult) {
	if r == nil {
		return
	}
	success := "unknown"
	if v.Status == "passed" {
		success = "true"
	}
	if v.Status == "failed" || v.Status == "setup_failed" {
		success = "false"
		r.root.SetStatus(codes.Error, "task failed")
	}
	r.root.SetAttributes(meta("status", v.Status), meta("structural_pass", strconv.FormatBool(v.StructuralPass)), meta("task_success", success), meta("necessary_clarifications", strconv.Itoa(v.NecessaryClarifications)), meta("http_failures", strconv.Itoa(v.HTTPFailures)))
	r.root.End()
}
