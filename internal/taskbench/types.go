// Package taskbench measures complete billing workflows against a live agent.
// It is a small frozen regression suite, not an estimate of general model accuracy.
package taskbench

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"net/http"
	"time"
)

type Case struct {
	ID                   string `json:"id"`
	Category             string `json:"category"`
	Message              string `json:"message"`
	Followup             string `json:"followup,omitempty"`
	MissingSlot          string `json:"missing_slot,omitempty"`
	Tool                 string `json:"tool,omitempty"`
	AmountMoney          int64  `json:"amount_money,omitempty"`
	Plan                 string `json:"plan,omitempty"`
	InitialSubscription  bool   `json:"initial_subscription"`
	InitiallyCancelled   bool   `json:"initially_cancelled,omitempty"`
	Cancel               bool   `json:"cancel"`
	Read                 bool   `json:"read"`
	DuplicateConfirm     bool   `json:"duplicate_confirm,omitempty"`
	OrderRefund          bool   `json:"order_refund,omitempty"`
	RequireBalanceAnswer bool   `json:"require_balance_answer,omitempty"`
}
type Metadata struct {
	Version         string `json:"version"`
	JevMode         string `json:"jev_mode"`
	Model           string `json:"model"`
	SourceGit       string `json:"source_git"`
	BinarySHA256    string `json:"binary_sha256"`
	EvaluatorSHA256 string `json:"evaluator_binary_sha256"`
}
type Fixture struct {
	TenantID   uint64     `json:"tenant_id"`
	TenantName string     `json:"tenant_name"`
	APIKey     string     `json:"-"`
	Standard   store.Plan `json:"standard"`
	Flagship   store.Plan `json:"flagship"`
	OrderNo    string     `json:"order_no"`
}
type Pending struct {
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args"`
	TenantID uint64         `json:"tenant_id"`
}
type Snapshot struct {
	BalanceCent   int64                 `json:"balance_cent"`
	LedgerBalance int64                 `json:"ledger_balance"`
	Consistent    bool                  `json:"consistent"`
	Orders        []store.RechargeOrder `json:"orders"`
	Subscriptions []store.Subscription  `json:"subscriptions"`
	Topups        []store.Topup         `json:"topups"`
	Audits        []store.AgentAuditLog `json:"audits"`
}
type Backend interface {
	Setup(context.Context, string, Case) (Fixture, error)
	Snapshot(context.Context, Fixture, string) (Snapshot, error)
	Pending(context.Context, string) (Pending, error)
}
type Options struct {
	BaseURL    string
	Metadata   Metadata
	Cases      []Case
	Backend    Backend
	Client     *http.Client
	OnProgress func(done, total int)
}
type Trace struct {
	Tool      string `json:"tool"`
	Args      string `json:"args"`
	Confirmed bool   `json:"confirmed"`
	Result    string `json:"result"`
}
type Response struct {
	SessionID   string  `json:"session_id"`
	Reply       string  `json:"reply"`
	Trace       []Trace `json:"trace"`
	Pending     bool    `json:"pending_confirm"`
	PendingTool string  `json:"pending_tool"`
	Preview     string  `json:"pending_preview"`
	Telemetry   *struct {
		LLMCalls   int   `json:"llm_calls"`
		JevCalls   int   `json:"jev_calls"`
		JevEnabled *bool `json:"jev_enabled"`
	} `json:"telemetry,omitempty"`
}
type Turn struct {
	Message   string   `json:"message"`
	Stage     string   `json:"stage"`
	Status    int      `json:"status"`
	LatencyMS float64  `json:"latency_ms"`
	RawBody   string   `json:"raw_body"`
	Response  Response `json:"response"`
	Pending   *Pending `json:"pending,omitempty"`
	Snapshot  Snapshot `json:"snapshot"`
	Error     string   `json:"error,omitempty"`
}
type Result struct {
	Case                   Case     `json:"case"`
	Fixture                Fixture  `json:"fixture"`
	Initial                Snapshot `json:"initial"`
	Final                  Snapshot `json:"final"`
	Turns                  []Turn   `json:"turns"`
	Errors                 []string `json:"errors"`
	Success                bool     `json:"success"`
	SetupError             bool     `json:"setup_error"`
	ClarificationOffered   bool     `json:"clarification_offered"`
	RedundantClarification bool     `json:"redundant_clarification"`
	PreviewObserved        bool     `json:"preview_observed"`
	PreviewCorrect         bool     `json:"preview_correct"`
	PrematureWrite         bool     `json:"premature_write"`
	CancelNoWriteObserved  bool     `json:"cancel_no_write_observed"`
	DurationMS             float64  `json:"duration_ms"`
	TimeToPreviewMS        *float64 `json:"time_to_preview_ms,omitempty"`
}
type Latencies struct {
	Count     int     `json:"count"`
	AverageMS float64 `json:"average_ms"`
	P50MS     float64 `json:"p50_ms"`
	P95MS     float64 `json:"p95_ms"`
}
type CategoryMetrics struct {
	Total          int     `json:"total"`
	Completed      int     `json:"completed"`
	CompletionRate float64 `json:"completion_rate"`
}
type Metrics struct {
	CategoryMetrics
	ByCategory               map[string]CategoryMetrics `json:"by_category"`
	FailedHTTP               int                        `json:"failed_http"`
	FixtureFailures          int                        `json:"fixture_failures"`
	ClarificationRequired    int                        `json:"clarification_required"`
	ClarificationOffered     int                        `json:"clarification_offered"`
	ClarificationCoverage    float64                    `json:"clarification_coverage"`
	ClarificationCompleted   int                        `json:"clarification_completed"`
	RedundantClarifications  int                        `json:"redundant_clarifications"`
	PreviewObserved          int                        `json:"preview_observed"`
	PreviewCorrect           int                        `json:"preview_correct"`
	PreviewParameterAccuracy float64                    `json:"preview_parameter_accuracy"`
	PrematureWrites          int                        `json:"premature_writes"`
	CancelCases              int                        `json:"cancel_cases"`
	CancelNoWrite            int                        `json:"cancel_no_write"`
	CancelNoWriteObserved    int                        `json:"cancel_no_write_observed"`
	DuplicateConfirmCases    int                        `json:"duplicate_confirm_cases"`
	DuplicateConfirmPassed   int                        `json:"duplicate_confirm_passed"`
	LLMCalls                 int                        `json:"llm_calls"`
	TelemetryTurns           int                        `json:"telemetry_turns"`
	AllTasks                 Latencies                  `json:"all_task_latency"`
	SuccessfulTasks          Latencies                  `json:"successful_task_latency"`
	TimeToPreview            Latencies                  `json:"time_to_preview"`
}
type Report struct {
	Schema        string    `json:"schema"`
	RunID         string    `json:"run_id"`
	StartedAt     time.Time `json:"started_at"`
	Metadata      Metadata  `json:"metadata"`
	DatasetSHA256 string    `json:"dataset_sha256"`
	Limitations   []string  `json:"limitations"`
	Results       []Result  `json:"results"`
	Metrics       Metrics   `json:"metrics"`
}
