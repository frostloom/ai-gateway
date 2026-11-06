package taskbench

// The study and original delivered prefix are unchanged. V14 attempts are
// retained as supplemental DATA, never substituted for the new complete suffix.
type windowCompletionDeclarationV15 windowCompletionDeclarationV14

func expectedWindowCompletionDeclarationV15() windowCompletionDeclarationV15 {
	c := windowCompletionDeclarationV15(expectedWindowCompletionDeclarationV14())
	c.Schema = "full-window-completion-declaration.v15"
	c.HistoricalBaselineSHA256 = windowV15BaselineSHA
	c.HistoricalClaims = 178
	c.HistoricalModelAttempts = 7245
	c.HistoricalJEVAttempts = 1871
	c.ExcludedSources = append(append([]string(nil), c.ExcludedSources...), "V14-stopped-window-001")
	return c
}
