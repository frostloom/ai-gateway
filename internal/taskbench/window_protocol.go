// Private pure protocol checks; no transport or execution authority.
package taskbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

const windowMaxResponseBytes = 1 << 20

// Validates the exact serialized request that a future owned transport would
// persist before sending. This is not a claim/schedule/capability validator.
func validateWindowPOST(provider, method, endpoint string, body []byte, p windowProviderLimits, identity windowRequestIdentity) error {
	expectedEndpoint, expectedModel, err := windowProviderEndpoint(provider)
	if err != nil {
		return err
	}
	if method != "POST" || endpoint != expectedEndpoint || p.Model != expectedModel || p.MaxRequestBytes < 1 || p.MaxRequestBytes > 1<<20 || p.MaxInput < 1 || p.MaxInput > 10000000 || p.MaxOutput < 0 || p.MaxOutput > 1000000 {
		return fmt.Errorf("invalid provider request declaration")
	}
	if len(body) == 0 || len(body) > p.MaxRequestBytes || identity.Provider != provider || identity.Model != p.Model || identity.Bytes != len(body) || identity.BodySHA256 != fmt.Sprintf("%x", sha256.Sum256(body)) {
		return fmt.Errorf("serialized request mismatch")
	}
	obj, err := windowJSONObject(body)
	if err != nil {
		return err
	}
	if obj["model"] != p.Model {
		return fmt.Errorf("request model mismatch")
	}
	if provider == "jev" {
		if err := windowOnlyKeys(obj, "model", "state", "questions"); err != nil {
			return err
		}
		switch obj["state"].(type) {
		case string, map[string]any, []any:
		default:
			return fmt.Errorf("invalid state")
		}
		questions, ok := obj["questions"].(map[string]any)
		if !ok || len(questions) == 0 {
			return fmt.Errorf("questions required")
		}
		for name, v := range questions {
			q, ok := v.(map[string]any)
			if name == "" || !ok {
				return fmt.Errorf("invalid question")
			}
			if err := windowOnlyKeys(q, "type", "instructions", "criteria"); err != nil {
				return err
			}
			switch q["type"] {
			case "noul", "choice", "score":
			default:
				return fmt.Errorf("unsupported question type")
			}
		}
		return nil
	}
	if p.MaxOutput < 1 || p.MaxOutput > 512 {
		return fmt.Errorf("invalid model output bound")
	}
	if err := windowOnlyKeys(obj, "model", "max_tokens", "messages", "system", "tools", "temperature", "thinking", "stream"); err != nil {
		return err
	}
	max, err := windowNonnegativeInteger(obj["max_tokens"])
	if err != nil || max != p.MaxOutput {
		return fmt.Errorf("max_tokens mismatch")
	}
	if stream, present := obj["stream"]; present && stream != false {
		return fmt.Errorf("stream must be false or omitted")
	}
	thinking, ok := obj["thinking"].(map[string]any)
	if !ok || len(thinking) != 1 || thinking["type"] != "disabled" {
		return fmt.Errorf("thinking must be disabled")
	}
	messages, ok := obj["messages"].([]any)
	if !ok || len(messages) == 0 {
		return fmt.Errorf("messages required")
	}
	for _, v := range messages {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid message")
		}
		if err := windowOnlyKeys(m, "role", "content"); err != nil {
			return err
		}
		if m["role"] != "user" && m["role"] != "assistant" {
			return fmt.Errorf("invalid message role")
		}
		content, ok := m["content"].([]any)
		if !ok || len(content) == 0 {
			return fmt.Errorf("message content required")
		}
	}
	return nil
}

// Anthropic input_tokens excludes cache reads and cache creations. Normalize
// Input = base + read + create, CacheHit = read, CacheMiss = base + create.
// Current ledger charges every normalized input at the declared input rate;
// this is conservative accounting, not a provider bill or cache pricing model.
type windowUsageEnvelopeError struct{}

func (*windowUsageEnvelopeError) Error() string { return "usage envelope exceeded" }

func parseWindowResponseUsage(provider string, body []byte, p windowProviderLimits) (*windowUsage, error) {
	_, expectedModel, err := windowProviderEndpoint(provider)
	if err != nil {
		return nil, err
	}
	if p.Model != expectedModel || p.MaxInput < 1 || p.MaxInput > 10000000 || p.MaxOutput < 0 || p.MaxOutput > 1000000 {
		return nil, fmt.Errorf("invalid usage declaration")
	}
	if len(body) == 0 || len(body) > windowMaxResponseBytes {
		return nil, fmt.Errorf("response bound exceeded")
	}
	if provider == "model" && (p.MaxOutput < 1 || p.MaxOutput > 512) {
		return nil, fmt.Errorf("invalid model output declaration")
	}
	obj, err := windowJSONObject(body)
	if err != nil {
		return nil, err
	}
	if obj["model"] != p.Model {
		return nil, fmt.Errorf("response model mismatch")
	}
	if _, present := obj["error"]; present {
		return nil, fmt.Errorf("error response")
	}
	usage, ok := obj["usage"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("usage required")
	}
	keys := []string{"input_tokens", "output_tokens"}
	if provider == "model" {
		keys = append(keys, "cache_read_input_tokens", "cache_creation_input_tokens", "service_tier")
		// The fixed DeepSeek Anthropic endpoint returns standard-tier metadata.
		// It is not token usage; other tiers have no frozen pricing contract.
		if tier, present := usage["service_tier"]; present && tier != "standard" {
			return nil, fmt.Errorf("unsupported model service tier")
		}
	}
	if err = windowOnlyKeys(usage, keys...); err != nil {
		return nil, err
	}
	input, err := windowNonnegativeInteger(usage["input_tokens"])
	if err != nil {
		return nil, err
	}
	output, err := windowNonnegativeInteger(usage["output_tokens"])
	if err != nil {
		return nil, err
	}
	if input > p.MaxInput || output > p.MaxOutput {
		return nil, &windowUsageEnvelopeError{}
	}
	u := &windowUsage{Input: &input, Output: &output}
	if provider == "model" {
		read, create := int64(0), int64(0)
		if v, ok := usage["cache_read_input_tokens"]; ok {
			read, err = windowNonnegativeInteger(v)
			if err != nil {
				return nil, err
			}
		}
		if v, ok := usage["cache_creation_input_tokens"]; ok {
			create, err = windowNonnegativeInteger(v)
			if err != nil {
				return nil, err
			}
		}
		// Subtraction before addition prevents integer overflow and catches total
		// envelope violations, including individually bounded components.
		if read > p.MaxInput-input || create > p.MaxInput-input-read {
			return nil, &windowUsageEnvelopeError{}
		}
		miss := input + create
		input += read + create
		u.CacheHit = &read
		u.CacheMiss = &miss
	}
	return u, nil
}

// A future transport must use max+1, reject oversize/read errors, then retain
// these exact bytes for replay. LimitReader(max) alone cannot prove completeness.
func readWindowResponse(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("missing response reader")
	}
	body, err := io.ReadAll(io.LimitReader(r, windowMaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("response read failed")
	}
	if len(body) > windowMaxResponseBytes {
		return nil, fmt.Errorf("response bound exceeded")
	}
	return body, nil
}

func windowProviderEndpoint(provider string) (string, string, error) {
	switch provider {
	case "model":
		return "https://api.deepseek.com/anthropic/v1/messages", "deepseek-flash", nil
	case "jev":
		return "https://api.typesafe.ai/v1/systemone", "jev-1.13.0", nil
	default:
		return "", "", fmt.Errorf("unknown provider")
	}
}
func windowNonnegativeInteger(v any) (int64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("integer required")
	}
	s := n.String()
	if len(s) == 0 || (len(s) > 1 && s[0] == '0') {
		return 0, fmt.Errorf("unsigned decimal integer required")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("unsigned decimal integer required")
		}
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("integer overflow")
	}
	return i, nil
}
func windowOnlyKeys(obj map[string]any, keys ...string) error {
	for key := range obj {
		found := false
		for _, allowed := range keys {
			if key == allowed {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unsupported JSON field")
		}
	}
	return nil
}

// Parse once, recursively rejecting duplicate decoded keys (including escaped
// spellings), invalid UTF-8, trailing values, and nesting beyond 128 levels.
func windowJSONObject(body []byte) (map[string]any, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("invalid UTF-8 JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	value, err := windowJSONValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON object required")
	}
	return obj, nil
}
func windowJSONValue(dec *json.Decoder, depth int) (any, error) {
	if depth > 128 {
		return nil, fmt.Errorf("JSON depth exceeded")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("invalid JSON key")
			}
			s, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("invalid JSON key")
			}
			if _, exists := obj[s]; exists {
				return nil, fmt.Errorf("duplicate JSON key")
			}
			value, err := windowJSONValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			obj[s] = value
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("invalid JSON object")
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := windowJSONValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("invalid JSON array")
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("invalid JSON delimiter")
	}
}
