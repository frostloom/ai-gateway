package taskbench

// The study and original delivered prefix are unchanged. V13 attempts are
// retained as supplemental DATA, never substituted for the new complete suffix.
type windowCompletionDeclarationV14 windowCompletionDeclarationV13

func expectedWindowCompletionDeclarationV14() windowCompletionDeclarationV14 {
	c := windowCompletionDeclarationV14(expectedWindowCompletionDeclarationV13())
	c.Schema = "full-window-completion-declaration.v14"
	c.HistoricalBaselineSHA256 = windowV14BaselineSHA
	c.HistoricalClaims = 177
	c.HistoricalModelAttempts = 7237
	c.HistoricalJEVAttempts = 1869
	c.ExcludedSources = append(append([]string(nil), c.ExcludedSources...), "V13-normal-prefix", "V13-stopped-window-021")
	return c
}
