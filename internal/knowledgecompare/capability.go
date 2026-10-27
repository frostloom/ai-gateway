package knowledgecompare

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"os"
	"path/filepath"
	"sync"
)

const CampaignID = "knowledge-answer100-independent-20261027"
const ProviderURL = "https://api.deepseek.com/v1/chat/completions"

type Contract struct {
	CostStudy             string `json:"cost_study,omitempty"`
	CollectSafeFailures   bool   `json:"collect_safe_failures,omitempty"`
	ObservationBestEffort bool   `json:"observation_best_effort,omitempty"`
	CleanupProofSHA       string `json:"cleanup_proof_sha256,omitempty"`
	ObserverTimeoutMS     int    `json:"observer_timeout_ms,omitempty"`
	PriorRecordingSHA     string `json:"prior_recording_sha256,omitempty"`
	PriorJournalSHA       string `json:"prior_journal_sha256,omitempty"`
	ParentClaimSHA        string `json:"parent_claim_sha256,omitempty"`
	Schema                string `json:"schema"`
	ID                    string `json:"id"`
	FundingPool           string `json:"funding_pool"`
	SuiteSHA              string `json:"suite_sha256"`
	Revision              string `json:"revision"`
	CorpusSHA             string `json:"corpus_sha256"`
	MainSHA               string `json:"main_reference_sha256"`
	BinarySHA             string `json:"binary_sha256"`
	PriceSHA              string `json:"price_sha256"`
	MaxMicroCNY           int64  `json:"max_micro_cny"`
	MaxCalls              int    `json:"max_calls"`
	MaxOutput             int    `json:"max_output_tokens"`
	MaxBytes              int    `json:"max_request_bytes"`
	InputCeiling          int64  `json:"input_ceiling"`
	InputRate             int64  `json:"input_micro_per_million"`
	OutputRate            int64  `json:"output_micro_per_million"`
}

func (c Contract) SHA() string      { b, _ := json.Marshal(c); return knowledgepilot.SHA(b) }
func validSHA(s string, n int) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == n }
func (c Contract) Validate() error {
	if c.CostStudy != "" && (c.CostStudy != "knowledge-resident-cost.v1" || c.PriorRecordingSHA == "" || c.MaxMicroCNY != 20000000) {
		return fmt.Errorf("invalid cost study")
	}
	if (c.CollectSafeFailures || c.ObservationBestEffort || c.CleanupProofSHA != "") && c.PriorRecordingSHA == "" {
		return fmt.Errorf("completion policy requires parent")
	}
	if c.CleanupProofSHA != "" && !validSHA(c.CleanupProofSHA, 32) {
		return fmt.Errorf("cleanup proof digest invalid")
	}
	if c.ObserverTimeoutMS != 0 && c.ObserverTimeoutMS != 10000 {
		return fmt.Errorf("observer deadline rejected")
	}
	if c.Schema != "knowledge-answer100.v1" || c.ID != CampaignID || c.FundingPool != "knowledge-independent" || (c.MaxMicroCNY != 5000000 && c.MaxMicroCNY != 20000000) || c.MaxCalls != 1120 || c.MaxOutput != 512 || c.MaxBytes != 32768 || c.InputCeiling != 1048576 || c.InputRate != 2000000 || c.OutputRate != 8000000 {
		return fmt.Errorf("contract bounds differ")
	}
	if c.PriorRecordingSHA != "" || c.PriorJournalSHA != "" || c.ParentClaimSHA != "" {
		for _, h := range []string{c.PriorRecordingSHA, c.PriorJournalSHA, c.ParentClaimSHA} {
			if !validSHA(h, 32) {
				return fmt.Errorf("incomplete continuation digest")
			}
		}
	}
	for _, h := range []string{c.SuiteSHA, c.CorpusSHA, c.MainSHA, c.BinarySHA, c.PriceSHA} {
		if !validSHA(h, 32) {
			return fmt.Errorf("missing bound digest")
		}
	}
	if !validSHA(c.Revision, 20) {
		return fmt.Errorf("missing source revision")
	}
	return nil
}

type Approval struct {
	Schema      string `json:"schema"`
	ContractSHA string `json:"contract_sha256"`
	FundingPool string `json:"funding_pool"`
	Approved    bool   `json:"approved_independent_budget"`
	MaxMicroCNY int64  `json:"max_micro_cny"`
	ConsentSHA  string `json:"consent_sha256"`
}
type Capability struct {
	mu         sync.Mutex
	c          Contract
	used       bool
	skip       map[string]bool
	inherited  GuardState
	priorTurns int
}

func (c *Capability) consume() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		return fmt.Errorf("capability already consumed")
	}
	c.used = true
	return nil
}
func CheckApproval(c Contract, a Approval) error {
	if e := c.Validate(); e != nil {
		return e
	}
	if a.Schema != "knowledge-independent-approval.v1" || !a.Approved || a.ContractSHA != c.SHA() || a.FundingPool != c.FundingPool || a.MaxMicroCNY != c.MaxMicroCNY || !validSHA(a.ConsentSHA, 32) {
		return fmt.Errorf("explicit independent budget approval missing")
	}
	return nil
}
func writeNew(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	n, e := f.Write(b)
	if e != nil {
		return e
	}
	if n != len(b) {
		return fmt.Errorf("short write")
	}
	if e = f.Sync(); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// Caller chooses only the canonical common-Git-root path. This capability can
// never consume the main100+1 pool; its separate approval must be explicit.
func Acquire(path string, c Contract, a Approval) (*Capability, error) {
	if c.PriorRecordingSHA != "" {
		return nil, fmt.Errorf("completion must verify parent evidence")
	}
	if e := CheckApproval(c, a); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(struct {
		Schema   string   `json:"schema"`
		Status   string   `json:"status"`
		Contract Contract `json:"contract"`
		Approval Approval `json:"approval"`
	}{"knowledge-independent-claim.v1", "consumed_before_execution", c, a})
	if e != nil {
		return nil, e
	}
	if e = writeNew(path, append(raw, '\n')); e != nil {
		return nil, e
	}
	return &Capability{c: c}, nil
}
