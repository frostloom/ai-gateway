// Package knowledgeeval evaluates already recorded paired runs; it never calls providers.
package knowledgeeval

type Config struct {
	Model         string `json:"model"`
	Protocol      string `json:"protocol"`
	RoutingMode   string `json:"routing_mode"`
	ProfileSHA    string `json:"profile_sha256"`
	Revision      string `json:"revision"`
	SuiteSHA      string `json:"suite_sha256"`
	PromptSHA     string `json:"prompt_sha256"`
	ToolsSHA      string `json:"tools_sha256"`
	TariffSHA     string `json:"tariff_sha256"`
	CorpusVersion string `json:"corpus_version"`
	OutputBudget  int    `json:"output_budget"`
	ThinkingMode  string `json:"thinking_mode"`
}
type Case struct {
	ID              string   `json:"id"`
	Category        string   `json:"category"`
	Question        string   `json:"question"`
	EvidenceIDs     []string `json:"evidence_ids"`
	RequiredFacts   []string `json:"required_facts"`
	ForbiddenClaims []string `json:"forbidden_claims"`
	ExpectedTools   []string `json:"expected_tools"`
}
type Price struct {
	Input         *int64 `json:"input_micro_per_million"`
	Output        *int64 `json:"output_micro_per_million"`
	CacheRead     *int64 `json:"cache_read_micro_per_million"`
	CacheCreation *int64 `json:"cache_creation_micro_per_million"`
	Verified      bool   `json:"verified"`
}

// Input is normalized UNcached billable input. Cache fields must be explicit even at zero.
// Raw provider usage and its normalization provenance belong in the source journal.
type Attempt struct {
	Outcome       string `json:"outcome"`
	Currency      string `json:"currency"`
	Input         *int64 `json:"uncached_input"`
	Output        *int64 `json:"output"`
	CacheRead     *int64 `json:"cache_read"`
	CacheCreation *int64 `json:"cache_creation"`
	Price         *Price `json:"price"`
}
type Review struct {
	Correct            bool   `json:"correct"`
	Claims             int    `json:"claims"`
	Unsupported        int    `json:"unsupported"`
	Citations          int    `json:"citations"`
	SupportedCitations int    `json:"supported_citations"`
	CriticalErrors     int    `json:"critical_errors"`
	Reference          string `json:"independent_review_reference"`
}
type Task struct {
	CaseID           string    `json:"case_id"`
	Review           *Review   `json:"review"`
	RetrievedIDs     []string  `json:"retrieved_ids"`
	ActualTools      []string  `json:"actual_tools"`
	DurationMS       *float64  `json:"duration_ms"`
	AttemptsComplete bool      `json:"attempts_complete"`
	Attempts         []Attempt `json:"attempts"`
}
type Arm struct {
	Config Config `json:"config"`
	Tasks  []Task `json:"tasks"`
}
type ComparisonInput struct {
	Schema       string `json:"schema"`
	EvidenceKind string `json:"evidence_kind"`
	Clock        string `json:"clock"`
	Suite        []Case `json:"suite"`
	A            Arm    `json:"a"`
	B            Arm    `json:"b"`
}
type Category struct {
	N        int `json:"n"`
	Reviewed int `json:"reviewed"`
	Correct  int `json:"correct"`
}
type ArmReport struct {
	FailedAttempts  int    `json:"failed_attempts"`
	Timeouts        int    `json:"timeouts"`
	UnknownOutcomes int    `json:"unknown_outcomes"`
	CacheRead       *int64 `json:"cache_read"`
	CacheCreation   *int64 `json:"cache_creation"`

	Correct             int                 `json:"correct"`
	Reviewed            int                 `json:"reviewed"`
	Categories          map[string]Category `json:"categories"`
	CompletionRate      *float64            `json:"completion_rate"`
	UnsupportedRate     *float64            `json:"unsupported_rate"`
	CitationPrecision   *float64            `json:"citation_precision"`
	CitationCoverage    *float64            `json:"citation_coverage"`
	RecallAt3           *float64            `json:"recall_at_3"`
	CriticalErrors      int                 `json:"critical_errors"`
	TotalMicro          map[string]*int64   `json:"configured_total_micro"`
	CostPerCorrectMicro map[string]*float64 `json:"cost_per_correct_micro"`
	CommonPassMicro     map[string]*int64   `json:"common_pass_micro"`
	Attempts            int                 `json:"attempts"`
	Input               *int64              `json:"uncached_input"`
	Output              *int64              `json:"output"`
	P50MS               *float64            `json:"p50_ms"`
	P95MS               *float64            `json:"p95_ms"`
	LatencyN            int                 `json:"latency_n"`
	Gates               map[string]string   `json:"gates"`
}
type Report struct {
	KnowledgeChangePP  *float64            `json:"knowledge_change_pp"`
	TotalCostChangePct map[string]*float64 `json:"total_cost_change_pct"`
	UnitCostChangePct  map[string]*float64 `json:"cost_per_correct_change_pct"`
	P95ChangePct       *float64            `json:"p95_change_pct"`
	CostReductionGate  string              `json:"cost_reduction_gate"`

	Schema              string      `json:"schema"`
	EvidenceKind        string      `json:"evidence_kind"`
	Clock               string      `json:"clock"`
	EndToEnd            bool        `json:"end_to_end"`
	QualityComparable   bool        `json:"quality_comparable"`
	EffectClaimsAllowed bool        `json:"effect_claims_allowed"`
	A                   ArmReport   `json:"a"`
	B                   ArmReport   `json:"b"`
	CandidateWins       int         `json:"candidate_wins"`
	CandidateLosses     int         `json:"candidate_losses"`
	Ties                int         `json:"ties"`
	DiscordantWin95     *[2]float64 `json:"discordant_win_95"`
	Limitations         []string    `json:"limitations"`
}
