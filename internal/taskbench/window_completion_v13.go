package taskbench

// V13 retains the original primary study and delivered prefix. Historical
// attempts are identified separately: their stopped rows confer no normal
// completion, source, observation, shipment or score authority.
type windowCompletionDeclarationV13 struct {
	Schema                   string                       `json:"schema"`
	HumanReviewedExactScope  bool                         `json:"human_reviewed_exact_scope"`
	OriginalManifestSHA256   string                       `json:"original_manifest_sha256"`
	OriginalScopeSHA256      string                       `json:"original_scope_sha256"`
	OriginalSourceCommit     string                       `json:"original_source_commit"`
	OriginalSourcesSHA256    string                       `json:"original_sources_sha256"`
	OriginalCapsSHA256       string                       `json:"original_caps_sha256"`
	OriginalScheduleSHA256   string                       `json:"original_schedule_sha256"`
	SuffixWindowsSHA256      string                       `json:"suffix_windows_sha256"`
	SuffixCapsSHA256         string                       `json:"suffix_caps_sha256"`
	PrefixSummarySHA256      string                       `json:"prefix_summary_sha256"`
	PrefixDeliverySHA256     string                       `json:"prefix_delivery_sha256"`
	PrefixWindowIDs          []string                     `json:"prefix_window_ids"`
	Prefix                   windowCompletionPartitionV10 `json:"prefix"`
	Execution                windowCompletionPartitionV10 `json:"execution"`
	Primary                  windowCompletionCountsV10    `json:"primary"`
	Boundaries               int                          `json:"boundaries"`
	PrefixScoreCount         int                          `json:"prefix_score_count"`
	PrefixPositiveScoreHoles int                          `json:"prefix_positive_score_holes"`
	HistoricalBaselineSHA256 string                       `json:"historical_baseline_sha256"`
	HistoricalClaims         int                          `json:"historical_claims"`
	HistoricalModelAttempts  int                          `json:"historical_model_attempts"`
	HistoricalJEVAttempts    int                          `json:"historical_jev_attempts"`
	ExcludedSources          []string                     `json:"excluded_sources"`
	Global                   windowV8Amounts              `json:"global"`
	PriorModelExtras         int                          `json:"prior_model_extras"`
	PriorJEVExtras           int                          `json:"prior_jev_extras"`
	CohortMaxModelExtras     int                          `json:"cohort_max_model_extras"`
	CohortMaxJEVExtras       int                          `json:"cohort_max_jev_extras"`
}

func expectedWindowCompletionDeclarationV13() windowCompletionDeclarationV13 {
	// Reuse only immutable original R5 identity/partition fields, never V10's
	// historical money or its obsolete all-attempt denominator.
	c := expectedWindowCompletionDeclarationV10()
	return windowCompletionDeclarationV13{
		Schema:                 "full-window-completion-declaration.v13",
		OriginalManifestSHA256: c.OriginalManifestSHA256, OriginalScopeSHA256: c.OriginalScopeSHA256,
		OriginalSourceCommit: c.OriginalSourceCommit, OriginalSourcesSHA256: c.OriginalSourcesSHA256,
		OriginalCapsSHA256: c.OriginalCapsSHA256, OriginalScheduleSHA256: c.OriginalScheduleSHA256,
		SuffixWindowsSHA256: c.SuffixWindowsSHA256, SuffixCapsSHA256: c.SuffixCapsSHA256,
		PrefixSummarySHA256: c.PrefixSummarySHA256, PrefixDeliverySHA256: c.PrefixDeliverySHA256,
		PrefixWindowIDs: append([]string(nil), c.PrefixWindowIDs...), Prefix: c.Prefix, Execution: c.Execution,
		Primary: c.Primary, Boundaries: 1851, PrefixScoreCount: 2300, PrefixPositiveScoreHoles: 10,
		HistoricalBaselineSHA256: windowV13BaselineSHA, HistoricalClaims: 156, HistoricalModelAttempts: 6691, HistoricalJEVAttempts: 1685,
		ExcludedSources: []string{"all-pre-R5-attempts", "R5-stopped-window-056", "V10-supplemental-attempts", "V11-R3-normal-prefix", "V11-R3-stopped-window-050", "V12-Root4-normal-prefix", "V12-Root4-stopped-window-002"},
		Global:          windowV8Amounts{200000000, 4000000}, CohortMaxModelExtras: 20, CohortMaxJEVExtras: 20,
	}
}
