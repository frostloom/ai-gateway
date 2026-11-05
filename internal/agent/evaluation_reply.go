package agent

// EvaluationSanitizeReply projects retained raw assistant text through the same
// pure sanitizer used by production HTTP responses. It grants no execution or
// chat authority and leaves the supplied evidence unchanged.
func EvaluationSanitizeReply(reply string) string {
	return sanitizeReply(reply)
}
