package taskbench

import "fmt"

const (
	windowV10BaselineSHA      = "2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47"
	windowV10OriginalScopeSHA = "e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9"
	windowV10ScheduleSHA      = "95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf"
	windowV10SuffixWindowsSHA = "e383d21f0a3981e998ab1f44d2bd1eb886072d04320a727dc2c0fea5bb1e24d2"
	windowV10SuffixCapsSHA    = "7e67ca0b51e7c1ef087cfa490ebbfbd7eda0d30f91e205e0d50272845116701e"
)

type windowCompletionCountsV10 struct {
	Pairs            int `json:"pairs"`
	Arms             int `json:"arms"`
	DefinedUserTurns int `json:"defined_user_turns"`
}

type windowCompletionPartitionV10 struct {
	FirstOrdinal        int `json:"first_ordinal"`
	LastOrdinal         int `json:"last_ordinal"`
	FirstDeclaredWindow int `json:"first_declared_window"`
	WindowCount         int `json:"window_count"`
	Pairs               int `json:"pairs"`
	Arms                int `json:"arms"`
	DefinedUserTurns    int `json:"defined_user_turns"`
	MaxModelCalls       int `json:"max_model_calls"`
	MaxJEVCalls         int `json:"max_jev_calls"`
}

type windowCompletionOverheadV10 struct {
	WindowID         string `json:"window_id"`
	FirstOrdinal     int    `json:"first_ordinal"`
	LastOrdinal      int    `json:"last_ordinal"`
	Pairs            int    `json:"pairs"`
	Arms             int    `json:"arms"`
	DefinedUserTurns int    `json:"defined_user_turns"`
	NativeCalls      int    `json:"native_calls"`
	ModelCalls       int    `json:"model_calls"`
	JEVCalls         int    `json:"jev_calls"`
	ScoresHeld       bool   `json:"scores_held"`
}

// This is a fixed composition declaration, not a historical proof, a human
// approval, or an execution capability. The stopped R5 attempt remains overhead.
// Boundaries is the original study's separate denominator; no suffix boundary
// partition has been established by this declaration.
type windowCompletionDeclarationV10 struct {
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
	StoppedOverhead          windowCompletionOverheadV10  `json:"stopped_overhead"`
	AllAttempt               windowCompletionCountsV10    `json:"all_attempt"`
	HistoricalGlobal         windowV8Amounts              `json:"historical_global"`
	OriginalParent           windowV8Amounts              `json:"original_parent"`
	PriorModelExtras         int                          `json:"prior_model_extras"`
	PriorJEVExtras           int                          `json:"prior_jev_extras"`
	CohortMaxModelExtras     int                          `json:"cohort_max_model_extras"`
	CohortMaxJEVExtras       int                          `json:"cohort_max_jev_extras"`
}

func expectedWindowCompletionDeclarationV10() windowCompletionDeclarationV10 {
	ids := make([]string, 55)
	for i := range ids {
		ids[i] = fmt.Sprintf("window-%03d", i+1)
	}
	return windowCompletionDeclarationV10{
		Schema: "full-window-completion-declaration.v1", HumanReviewedExactScope: false,
		OriginalManifestSHA256: "aa9a664d767c080648213e3d98c63c340d2207afea9121d104bbb2775fdc7666",
		OriginalScopeSHA256:    windowV10OriginalScopeSHA, OriginalSourceCommit: "4ddd37ad5ba44db630bee61ddfebba5402e09d45",
		OriginalSourcesSHA256: "47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00",
		OriginalCapsSHA256:    windowV9CapsSHA, OriginalScheduleSHA256: windowV10ScheduleSHA,
		SuffixWindowsSHA256: windowV10SuffixWindowsSHA, SuffixCapsSHA256: windowV10SuffixCapsSHA,
		PrefixSummarySHA256:  "42be06b9399b999bb83e939c0722e0e2d0c952608af529ed923c29046f00206d",
		PrefixDeliverySHA256: "3ed36a445a5e8be86773d707992974c690dba41f63e2967c2066c3948612ef06",
		PrefixWindowIDs:      ids,
		Prefix:               windowCompletionPartitionV10{1, 385, 1, 55, 385, 770, 1268, 6340, 634},
		Execution:            windowCompletionPartitionV10{386, 3660, 56, 456, 3275, 6550, 10504, 52520, 5252},
		Primary:              windowCompletionCountsV10{3660, 7320, 11772}, Boundaries: 1851,
		PrefixScoreCount: 2300, PrefixPositiveScoreHoles: 10,
		StoppedOverhead:  windowCompletionOverheadV10{"window-056", 386, 390, 5, 10, 22, 23, 18, 5, true},
		AllAttempt:       windowCompletionCountsV10{3665, 7330, 11794},
		HistoricalGlobal: windowV8Amounts{200000000, 4000000}, OriginalParent: windowV8Amounts{180000000, 2500000},
		PriorModelExtras: 0, PriorJEVExtras: 0, CohortMaxModelExtras: 20, CohortMaxJEVExtras: 20,
	}
}
