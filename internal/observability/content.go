package observability

import (
	"encoding/json"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const contentLimit = 4096

var masks = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:sk|pk)-[a-z0-9_-]{4,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)(?:authorization|x-api-key|api[_ -]?key|password|secret|access[_ -]?token)["']?\s*[:=]\s*["']?(?:bearer\s+)?[^\s,;"}]+`),
	regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`),
	regexp.MustCompile(`\b1[3-9][0-9]{9}\b`),
	regexp.MustCompile(`\b[0-9]{17}[0-9xX]\b`),
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func redact(s string) string {
	truncated := len(s) > 2048
	s = clip(s, 2048)
	for _, re := range masks {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	if truncated {
		s += "[TRUNCATED]"
	}
	return s
}
func (o *Observer) safeValue(value any, depth int) any {
	if depth > 8 {
		return "[DEPTH LIMIT]"
	}
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		count := 0
		for key, item := range v {
			if count >= 32 {
				out["truncated"] = "[TRUNCATED]"
				break
			}
			count++
			normalized := strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					return unicode.ToLower(r)
				}
				return -1
			}, key)
			safeKey := clip(redact(key), 64)
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || strings.Contains(normalized, "apikey") || normalized == "authorization" || strings.Contains(normalized, "cookie") || normalized == "xapikey" {
				out[safeKey] = "[REDACTED]"
				continue
			}
			if normalized == "tenantid" || normalized == "userid" || normalized == "sessionid" || normalized == "orderno" || normalized == "orderid" || normalized == "requestid" {
				raw, _ := json.Marshal(item)
				out[safeKey] = o.alias("content:"+normalized, string(raw))
				continue
			}
			out[safeKey] = o.safeValue(item, depth+1)
		}
		return out
	case []any:
		limit := len(v)
		if limit > 32 {
			limit = 32
		}
		out := make([]any, 0, limit+1)
		for _, item := range v[:limit] {
			out = append(out, o.safeValue(item, depth+1))
		}
		if limit < len(v) {
			out = append(out, "[TRUNCATED]")
		}
		return out
	case string:
		return redact(v)
	default:
		return v
	}
}
func (o *Observer) content(span trace.Span, field string, value any) {
	if o == nil || !o.cfg.CaptureContent {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return
	}
	raw, err = json.Marshal(o.safeValue(decoded, 0))
	if err != nil {
		return
	}
	if len(raw) > contentLimit {
		preview := clip(string(raw), 1500)
		raw, _ = json.Marshal(map[string]any{"truncated": true, "preview": preview})
	}
	span.SetAttributes(attribute.String("langfuse.observation."+field, string(raw)))
}
func (t *Turn) Input(value string) {
	if t != nil {
		t.observer.content(t.root, "input", value)
	}
}
func (t *Turn) Output(value string) {
	if t != nil {
		t.observer.content(t.root, "output", value)
	}
}
func (t *Turn) Error(err error) {
	if t != nil && err != nil && t.observer.cfg.CaptureContent {
		t.root.SetAttributes(meta("error_summary", redact(err.Error())))
	}
}
func (o *Operation) Input(value any) {
	if o != nil {
		o.observer.content(o.span, "input", value)
	}
}
func (o *Operation) Output(value string) {
	if o != nil {
		o.observer.content(o.span, "output", value)
	}
}
