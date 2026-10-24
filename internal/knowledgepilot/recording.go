package knowledgepilot

import (
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"reflect"
)

type RecordedTask struct {
	CaseID            string `json:"case_id"`
	Arm               string `json:"arm"`
	Identity          string `json:"identity"`
	Tenant            uint64 `json:"tenant_id"`
	Session           string `json:"session"`
	Reply             string `json:"reply"`
	HTTPStatus        int    `json:"http_status"`
	PendingBefore     bool   `json:"pending_before"`
	PendingAfter      bool   `json:"pending_after"`
	Before, After     json.RawMessage
	Trace             json.RawMessage         `json:"trace"`
	Telemetry         agent.TurnTelemetry     `json:"telemetry"`
	Runtime           agent.RuntimeMetadata   `json:"runtime"`
	DurationMS        float64                 `json:"duration_ms"`
	Searches          [][]json.RawMessage     `json:"searches"`
	Attempts          []knowledgeeval.Attempt `json:"attempts"`
	StateOK           bool                    `json:"state_ok"`
	BusinessUnchanged bool                    `json:"business_unchanged"`
}

func (t RecordedTask) FirstEvidence() []knowledgeeval.Evidence {
	var out []knowledgeeval.Evidence
	if len(t.Searches) == 0 {
		return out
	}
	for _, b := range t.Searches[0] {
		var e knowledgeeval.Evidence
		if json.Unmarshal(b, &e) == nil {
			out = append(out, e)
		}
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

type Readback struct {
	TaskID       string `json:"task_id"`
	TraceID      string `json:"trace_id"`
	Verified     bool   `json:"verified"`
	Observations int    `json:"observations"`
	ReceiptSHA   string `json:"receipt_sha256"`
}
type Recording struct {
	Suite        []knowledgeeval.Case `json:"suite"`
	EvidenceKind string               `json:"evidence_kind"`
	Scope        Scope                `json:"scope"`
	Complete     bool                 `json:"complete"`
	Tasks        []RecordedTask       `json:"tasks"`
	Events       []Event              `json:"events"`
	Readback     []Readback           `json:"readback"`
}
type ScoredTask struct {
	TaskID   string               `json:"task_id"`
	ReplySHA string               `json:"reply_sha256"`
	Review   knowledgeeval.Review `json:"review"`
}

func Comparison(r Recording, reviews []ScoredTask) (knowledgeeval.ComparisonInput, error) {
	var out knowledgeeval.ComparisonInput
	if !r.Complete || len(r.Tasks) != 144 || len(reviews) != 144 || len(r.Readback) != 144 {
		return out, fmt.Errorf("complete paired tasks, independent reviews and readback required")
	}
	// The current collector can only produce synthetic loopback evidence. A JSON
	// relabel cannot turn it into a paid recorded_run.
	if r.EvidenceKind != "local_stub" {
		return out, fmt.Errorf("real execution evidence not supported in this build")
	}
	if err := validateAttemptJournal(r); err != nil {
		return out, err
	}
	rb := map[string]Readback{}
	for _, b := range r.Readback {
		if !b.Verified || b.TraceID == "" || b.Observations < 1 || !digest(b.ReceiptSHA, 32) || rb[b.TaskID].TaskID != "" {
			return out, fmt.Errorf("incomplete or duplicate trace readback")
		}
		rb[b.TaskID] = b
	}
	scores := map[string]ScoredTask{}
	for _, s := range reviews {
		if !digest(s.ReplySHA, 32) || s.Review.Reference == "" || scores[s.TaskID].TaskID != "" {
			return out, fmt.Errorf("unbound or duplicate review")
		}
		scores[s.TaskID] = s
	}
	for _, t := range r.Tasks {
		sc, ok := scores[t.Identity]
		b, obs := rb[t.Identity]
		if !ok || !obs || sc.ReplySHA != SHA([]byte(t.Reply)) || b.TraceID != t.Telemetry.LangfuseTraceID || !t.BusinessUnchanged || t.HTTPStatus != 200 || t.Runtime.RoutingMode != "model" || t.Telemetry.JevCalls != 0 {
			return out, fmt.Errorf("task evidence incomplete or inconsistent")
		}
	}
	suiteRaw, _ := json.Marshal(r.Suite)
	if len(r.Suite) != 72 || SHA(suiteRaw) != r.Scope.SuiteSHA {
		return out, fmt.Errorf("frozen suite differs")
	}
	out = knowledgeeval.ComparisonInput{Schema: "knowledge-comparison.v1", EvidenceKind: "local_stub", Clock: "runner_request", Suite: r.Suite}
	by := map[string]RecordedTask{}
	for _, t := range r.Tasks {
		if by[t.Identity].Identity != "" {
			return out, fmt.Errorf("duplicate task")
		}
		by[t.Identity] = t
	}
	config := func(m agent.RuntimeMetadata) knowledgeeval.Config {
		return knowledgeeval.Config{Model: m.Model, Protocol: m.Protocol, RoutingMode: "model", ProfileSHA: r.Scope.ProfileSHA, Revision: r.Scope.Revision, SuiteSHA: r.Scope.SuiteSHA, PromptSHA: m.PromptSHA256, ToolsSHA: m.ToolsSHA256, TariffSHA: SHA([]byte("unverified-local-tariffs")), CorpusVersion: m.CorpusVersion, OutputBudget: m.MaxOutputTokens, ThinkingMode: "unspecified_local"}
	}
	for _, c := range r.Suite {
		for _, arm := range []string{"a", "b"} {
			t, ok := by[c.ID+"-"+arm]
			if !ok || t.CaseID != c.ID || t.Arm != arm {
				return out, fmt.Errorf("missing paired task")
			}
			review := scores[t.Identity].Review
			if review.Correct && !t.StateOK {
				return out, fmt.Errorf("review contradicts required state gate")
			}
			task := knowledgeeval.Task{CaseID: c.ID, Review: &review, Retrieved: t.FirstEvidence(), DurationMS: &t.DurationMS, AttemptsComplete: true, Attempts: t.Attempts}
			var trace []struct {
				Tool    string `json:"tool"`
				Outcome string `json:"outcome"`
			}
			if json.Unmarshal(t.Trace, &trace) != nil {
				return out, fmt.Errorf("invalid trace")
			}
			for _, x := range trace {
				if x.Outcome == "success" {
					task.ActualTools = append(task.ActualTools, x.Tool)
				}
			}
			dst := &out.A
			if arm == "b" {
				dst = &out.B
			}
			cfg := config(t.Runtime)
			if len(dst.Tasks) == 0 {
				dst.Config = cfg
			} else if !reflect.DeepEqual(dst.Config, cfg) {
				return out, fmt.Errorf("runtime changed within arm")
			}
			dst.Tasks = append(dst.Tasks, task)
		}
	}
	if _, err := knowledgeeval.BuildReport(out); err != nil {
		return out, err
	}
	return out, nil
}
func UnchangedBusiness(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

func validateAttemptJournal(r Recording) error {
	pending := map[int]Event{}
	done := map[string][]knowledgeeval.Attempt{}
	last := 0
	for _, ev := range r.Events {
		switch ev.Kind {
		case "reserved":
			if ev.Sequence != last+1 || ev.TaskID == "" || !json.Valid(ev.Request) || SHA(ev.Request) != ev.RequestSHA {
				return fmt.Errorf("invalid durable reservation")
			}
			last = ev.Sequence
			pending[ev.Sequence] = ev
		case "completed":
			p, ok := pending[ev.Sequence]
			if !ok || p.TaskID != ev.TaskID || p.RequestSHA != ev.RequestSHA || ev.Attempt == nil || ev.Status != 200 {
				return fmt.Errorf("unpaired completion")
			}
			normalized, e := NormalizeOpenAI(ev.Response)
			if e != nil || !reflect.DeepEqual(normalized, *ev.Attempt) {
				return fmt.Errorf("usage normalization differs")
			}
			done[ev.TaskID] = append(done[ev.TaskID], *ev.Attempt)
			delete(pending, ev.Sequence)
		default:
			return fmt.Errorf("failed or unknown journal event prevents complete comparison")
		}
	}
	if len(pending) != 0 {
		return fmt.Errorf("interrupted reservation")
	}
	seen := map[string]bool{}
	for _, t := range r.Tasks {
		if seen[t.Identity] || !json.Valid(t.Before) || !json.Valid(t.After) || t.Tenant == 0 || t.Session == "" {
			return fmt.Errorf("invalid task identity/state")
		}
		seen[t.Identity] = true
		attempts := done[t.Identity]
		if len(attempts) != len(t.Attempts) || len(attempts) != t.Telemetry.LLMCalls || len(attempts) > 5 {
			return fmt.Errorf("attempt journal coverage differs")
		}
		if len(attempts) > 0 && !reflect.DeepEqual(attempts, t.Attempts) {
			return fmt.Errorf("recorded attempts differ")
		}
		delete(done, t.Identity)
		if (t.Arm == "a" && t.Runtime.KnowledgeEnabled) || (t.Arm == "b" && (!t.Runtime.KnowledgeEnabled || t.Runtime.CorpusVersion != r.Scope.CorpusVersion)) {
			return fmt.Errorf("knowledge intervention identity differs")
		}
	}
	if len(done) != 0 {
		return fmt.Errorf("orphan task attempts")
	}
	return nil
}
