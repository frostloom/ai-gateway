package taskbench

type DeclaredPairBudgetEstimate struct {
	PerAttemptMicroCNY  int64  `json:"per_attempt_reserved_micro_cny"`
	AllAttemptsMicroCNY int64  `json:"all_attempts_reserved_micro_cny"`
	FittingAttempts     int    `json:"attempts_fitting_declared_budget"`
	FitsBudget          bool   `json:"all_attempts_fit_declared_budget"`
	ActualSpendMicroCNY *int64 `json:"actual_spend_micro_cny"`
}

// EstimateDeclaredPairBudget performs integer arithmetic only, no providers.
// Supplied tariffs/envelopes remain operator declarations, not invoice evidence.
func EstimateDeclaredPairBudget(l PairBudgetLimits) (DeclaredPairBudgetEstimate, error) {
	var e DeclaredPairBudgetEstimate
	if err := l.validate(); err != nil {
		return e, err
	}
	e.PerAttemptMicroCNY = l.cost(l.InputTokenCeiling, int64(l.MaxOutputTokens))
	e.AllAttemptsMicroCNY = e.PerAttemptMicroCNY * int64(l.MaxCalls)
	e.FittingAttempts = int(l.MaxMicroCNY / e.PerAttemptMicroCNY)
	if e.FittingAttempts > l.MaxCalls {
		e.FittingAttempts = l.MaxCalls
	}
	e.FitsBudget = e.AllAttemptsMicroCNY <= l.MaxMicroCNY
	return e, nil
}
