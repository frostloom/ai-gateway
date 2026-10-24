// Package knowledgepilot prepares isolated knowledge experiments. Paid execution
// is intentionally unavailable until all legacy budget writers are coordinated.
package knowledgepilot

import (
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
)

const ScopeID = "knowledge-control-20261024"
const BudgetBlocker = "cross_entry_budget_uncoordinated"

type Limits struct {
	PerTask      int `json:"attempts_per_task"`
	Total        int `json:"attempts_total"`
	Output       int `json:"max_output_tokens"`
	RequestBytes int `json:"max_request_bytes"`
}
type Scope struct {
	Schema           string                `json:"schema"`
	ID               string                `json:"scope_id"`
	Revision         string                `json:"revision"`
	SuiteSHA         string                `json:"suite_sha256"`
	SuiteFileSHA     string                `json:"suite_file_sha256"`
	ProfileSHA       string                `json:"profile_sha256"`
	OrderSHA         string                `json:"order_sha256"`
	CorpusVersion    string                `json:"corpus_version"`
	Endpoint         string                `json:"endpoint"`
	Model            string                `json:"model"`
	Protocol         string                `json:"protocol"`
	A                agent.RuntimeMetadata `json:"a"`
	B                agent.RuntimeMetadata `json:"b"`
	Limits           Limits                `json:"limits"`
	AllowPaid        bool                  `json:"allow_paid"`
	HumanAuthorized  bool                  `json:"human_authorized"`
	ReservedCNYMicro *int64                `json:"reserved_cny_micro"`
}
type CheckResult struct {
	Ready      bool     `json:"ready_for_paid_run"`
	Blockers   []string `json:"blockers"`
	Scope      Scope    `json:"scope"`
	ModelCalls int      `json:"model_calls"`
	JEVCalls   int      `json:"jev_calls"`
	Mode       string   `json:"mode"`
}
type FixtureSpec struct {
	BalanceCents int64  `json:"balance_cents"`
	Pending      string `json:"pending"`
	Subscription string `json:"subscription"`
}
type ScheduledTask struct {
	CaseID   string `json:"case_id"`
	Arm      string `json:"arm"`
	Identity string `json:"identity"`
}
type Inputs struct {
	Cases    []knowledgeeval.Case
	Profiles map[string]FixtureSpec
	Corpus   *knowledge.Corpus
	Scope    Scope
}
