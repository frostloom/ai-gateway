package taskbench

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"time"
)

// These types adapt the frozen development definitions without rewriting the
// legacy taskbench contract. Observations are evaluator evidence, not fields
// supplied by an assistant claiming that it completed an operation.
type ConversationOperation struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}
type ConversationRule struct {
	User                         string         `json:"user"`
	ExpectedActions              []string       `json:"expected_actions"`
	AllowedTools                 []string       `json:"allowed_tools"`
	BusinessStateUnchanged       bool           `json:"business_state_unchanged"`
	PendingRequired              bool           `json:"pending_required"`
	PendingOptional              bool           `json:"pending_optional"`
	BoundPendingRequired         bool           `json:"bound_pending_required"`
	PendingCleared               bool           `json:"pending_cleared"`
	NoSecondExecution            bool           `json:"no_second_execution"`
	NoExecutionWithoutNewPreview bool           `json:"no_execution_without_new_preview"`
	NoWrite                      bool           `json:"no_write"`
	NecessaryClarification       bool           `json:"clarification_is_necessary_not_failure"`
	Answer                       map[string]any `json:"answer,omitempty"`
}
type ConversationAssertion struct {
	ID          string `json:"id"`
	Requirement string `json:"requirement"`
}
type ConversationScenario struct {
	ID                     string                 `json:"id"`
	Profile                string                 `json:"profile"`
	Turns                  []ConversationRule     `json:"turns"`
	Terminal               map[string]any         `json:"terminal"`
	Operation              *ConversationOperation `json:"operation"`
	MeasurementScope       string                 `json:"measurement_scope"`
	FullTaskMetricEligible bool                   `json:"full_task_metric_eligible"`
}
type ConversationCase struct {
	SourceID   string                  `json:"source_case_id"`
	Assertions []ConversationAssertion `json:"answer_assertions"`
	Scenario   ConversationScenario    `json:"scenario"`
}

func (c ConversationCase) Key() string { return c.SourceID + "/" + c.Scenario.ID }

type ConversationSuite struct {
	DefinitionSHA256 string             `json:"definition_sha256"`
	ManifestSHA256   string             `json:"manifest_sha256"`
	Cases            []ConversationCase `json:"cases"`
}
type ConversationState struct {
	Own        Snapshot            `json:"own"`
	Allowance  []store.TierRemain  `json:"allowance"`
	Controls   map[uint64]Snapshot `json:"controls"`
	CapturedAt time.Time           `json:"captured_at"`
}
type ConversationReview struct {
	ID                string `json:"id"`
	RequirementSHA256 string `json:"requirement_sha256"`
	ReplySHA256       string `json:"reply_sha256"`
	Verdict           string `json:"verdict"`
	Reviewer          string `json:"reviewer"`
	Method            string `json:"method"` // human, audited_offline, or explicitly synthetic local_stub
	Reason            string `json:"reason"`
}
type ConversationRequirement struct {
	ID                string `json:"id"`
	TurnIndex         int    `json:"turn_index"`
	Requirement       string `json:"requirement"`
	RequirementSHA256 string `json:"requirement_sha256"`
	ReplySHA256       string `json:"reply_sha256"`
}
type ConversationObservation struct {
	// Action is a recorder/reviewer label. Its semantic interpretation is always
	// included in the review requirements, even when a tool trace is available.
	Action  string               `json:"action"`
	Turn    Turn                 `json:"turn"`
	State   ConversationState    `json:"state"`
	Pending *Pending             `json:"pending,omitempty"`
	Reviews []ConversationReview `json:"reviews,omitempty"`
}
type ConversationDriver interface {
	Setup(context.Context, ConversationCase) (Fixture, ConversationState, error)
	Step(context.Context, Fixture, string, string) (ConversationObservation, error)
	Final(context.Context, Fixture, string) (ConversationState, error)
}
type ConversationResult struct {
	Case                    ConversationCase          `json:"case"`
	Fixture                 Fixture                   `json:"fixture"`
	Initial                 ConversationState         `json:"initial"`
	Final                   ConversationState         `json:"final"`
	Observations            []ConversationObservation `json:"observations"`
	ReviewRequirements      []ConversationRequirement `json:"review_requirements"`
	Errors                  []string                  `json:"errors"`
	Status                  string                    `json:"status"`
	SetupError              bool                      `json:"setup_error"`
	StructuralPass          bool                      `json:"structural_pass"`
	Success                 bool                      `json:"success"`
	NecessaryClarifications int                       `json:"necessary_clarifications"`
	HTTPFailures            int                       `json:"http_failures"`
	DurationMS              float64                   `json:"duration_ms"`
}
type ConversationMetrics struct {
	DiagnosticFailures         int      `json:"diagnostic_failures"`
	DiagnosticSetupFailures    int      `json:"diagnostic_setup_failures"`
	DiagnosticHTTPFailures     int      `json:"diagnostic_http_failures"`
	DiagnosticPendingReview    int      `json:"diagnostic_pending_review"`
	DiagnosticStructuralPassed int      `json:"diagnostic_structural_passed"`
	FullTasks                  int      `json:"full_tasks"`
	ValidFullTasks             int      `json:"valid_full_tasks"`
	FirstTurnDiagnostics       int      `json:"first_turn_diagnostics"`
	SetupFailures              int      `json:"setup_failures"`
	StructuralPassed           int      `json:"structural_passed"`
	Completed                  int      `json:"completed"`
	PendingReview              int      `json:"pending_review"`
	Failed                     int      `json:"failed"`
	HTTPFailures               int      `json:"http_failures"`
	NecessaryClarifications    int      `json:"necessary_clarifications"`
	CompletionRate             *float64 `json:"completion_rate"` // unavailable while answer review is incomplete
}
