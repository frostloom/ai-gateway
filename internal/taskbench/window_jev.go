package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// newWindowJEV owns construction of the fixed client and guarded transport.
// No production capability issuer exists. An arbitrary Evaluator cannot be
// injected here because that would not prove the ledger guard was installed.
func newWindowJEV(cap windowProviderCapability, apiKey string, upstream http.RoundTripper) (jev.Evaluator, error) {
	transport, err := newWindowProviderTransport(cap, "jev", upstream)
	if err != nil {
		return nil, fmt.Errorf("window JEV setup rejected")
	}
	endpoint, model, _ := windowProviderEndpoint("jev")
	client, err := jev.NewEvaluationClient(endpoint, apiKey, model, transport)
	if err != nil {
		return nil, fmt.Errorf("window JEV setup rejected")
	}
	return &windowJEVEvaluator{client: client, ledger: transport.ledger, model: model}, nil
}

type windowJEVEvaluator struct {
	client *jev.Client
	ledger *windowAccounting
	model  string
}

func (e *windowJEVEvaluator) Evaluate(ctx context.Context, state any, questions map[string]jev.Question) (*jev.Result, error) {
	if e == nil || e.ledger == nil {
		return nil, fmt.Errorf("window JEV evaluation stopped")
	}
	stop := func() (*jev.Result, error) {
		_ = windowTransportStop(e.ledger, "protocol_failure")
		return nil, fmt.Errorf("window JEV evaluation stopped")
	}
	if ctx == nil || e.client == nil {
		return stop()
	}
	// Freeze the supplied questions once. Both the owned client's serialized
	// request and validation use this copy, not the caller's mutable map.
	raw, err := json.Marshal(questions)
	if err != nil {
		return stop()
	}
	specs, err := windowJSONObject(raw)
	if err != nil || len(specs) == 0 {
		return stop()
	}
	var frozen map[string]jev.Question
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&frozen) != nil {
		return stop()
	}
	for name, value := range specs {
		q, ok := value.(map[string]any)
		if name == "" || !ok {
			return stop()
		}
		switch q["type"] {
		case "noul":
		case "choice":
			criteria, ok := q["criteria"].(map[string]any)
			if !ok || len(criteria) == 0 {
				return stop()
			}
		case "score":
			criteria, ok := q["criteria"].([]any)
			if !ok || len(criteria) == 0 {
				return stop()
			}
		default:
			return stop()
		}
	}
	result, err := e.client.Evaluate(ctx, state, frozen)
	if err != nil || result == nil {
		return stop()
	}
	if !windowJEVAnswersValid(result, e.model, specs) {
		return stop()
	}
	return result, nil
}

func windowJEVAnswersValid(result *jev.Result, model string, specs map[string]any) bool {
	if result == nil || result.Model != model || result.Raw == "" {
		return false
	}
	root, err := windowJSONObject([]byte(result.Raw))
	if err != nil || root["model"] != model {
		return false
	}
	answers, ok := root["answers"].(map[string]any)
	if !ok || len(answers) != len(specs) || len(result.Answers) != len(specs) {
		return false
	}
	for name, value := range specs {
		q, ok := value.(map[string]any)
		if !ok {
			return false
		}
		a, ok := answers[name].(map[string]any)
		if !ok || a["type"] != q["type"] {
			return false
		}
		if _, ok := result.Answers[name]; !ok {
			return false
		}
		switch q["type"] {
		case "noul":
			if windowOnlyKeys(a, "type", "noul") != nil || !windowJEVNumberIn(a["noul"], 0, 1) {
				return false
			}
		case "choice":
			if windowOnlyKeys(a, "type", "choice", "confidence", "probabilities") != nil || !windowJEVNumberIn(a["confidence"], 0, 1) {
				return false
			}
			criteria, ok := q["criteria"].(map[string]any)
			if !ok {
				return false
			}
			choice, ok := a["choice"].(string)
			if !ok {
				return false
			}
			if _, ok := criteria[choice]; !ok {
				return false
			}
			if !windowJEVProbabilities(a["probabilities"], func(key string) bool { _, ok := criteria[key]; return ok }) {
				return false
			}
		case "score":
			if windowOnlyKeys(a, "type", "score", "confidence", "legend", "probabilities") != nil || !windowJEVNumberIn(a["confidence"], 0, 1) {
				return false
			}
			criteria, ok := q["criteria"].([]any)
			if !ok || len(criteria) == 0 || !windowJEVNumberIn(a["score"], 0, float64(len(criteria)-1)) {
				return false
			}
			index := func(key string) (int, bool) {
				n, err := strconv.Atoi(key)
				return n, err == nil && n >= 0 && n < len(criteria) && strconv.Itoa(n) == key
			}
			if !windowJEVProbabilities(a["probabilities"], func(key string) bool { _, ok := index(key); return ok }) {
				return false
			}
			legend, ok := a["legend"].(map[string]any)
			if !ok {
				return false
			}
			for key, value := range legend {
				n, ok := index(key)
				if !ok || !reflect.DeepEqual(value, criteria[n]) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func windowJEVNumberIn(value any, min, max float64) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	n, err := number.Float64()
	return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= min && n <= max
}
func windowJEVProbabilities(value any, allowed func(string) bool) bool {
	probabilities, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, value := range probabilities {
		if !allowed(key) || !windowJEVNumberIn(value, 0, 1) {
			return false
		}
	}
	return true
}
