package taskbench

import "testing"

func TestDeclaredPairBudgetEstimateRoundsAndKeepsUnknownActualSpend(t *testing.T) {
	l := PairBudgetLimits{MaxCalls: 24, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: 500000, InputMicroCNYPerMillion: 1000000, OutputMicroCNYPerMillion: 4000000}
	e, err := EstimateDeclaredPairBudget(l)
	if err != nil || e.PerAttemptMicroCNY != 16896 || e.AllAttemptsMicroCNY != 405504 || e.FitsBudget != true || e.ActualSpendMicroCNY != nil {
		t.Fatalf("estimate=%+v %v", e, err)
	}
	l.MaxMicroCNY = 1
	e, err = EstimateDeclaredPairBudget(l)
	if err != nil || e.FitsBudget || e.FittingAttempts != 0 {
		t.Fatal("insufficient estimate budget accepted")
	}
	l.InputMicroCNYPerMillion = 0
	if _, err = EstimateDeclaredPairBudget(l); err == nil {
		t.Fatal("unknown price treated as free")
	}
}
