package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

func beginJevObservation(ctx context.Context, name string) (context.Context, *observability.Operation) {
	return observability.Start(ctx, name, "evaluator")
}
func endJevObservation(op *observability.Operation, user string, r *jev.Result, err error) {
	if op != nil && strings.TrimSpace(user) != "" {
		op.Input(map[string]any{"user_message": user})
	}
	if op != nil && r != nil {
		answers := make(map[string]map[string]any, len(r.Answers))
		for name, answer := range r.Answers {
			answers[name] = map[string]any{
				"type":       answer.Type,
				"choice":     answer.Choice,
				"confidence": answer.Conf(),
				"noul":       answer.Noul,
				"score":      answer.Score,
			}
		}
		if raw, marshalErr := json.Marshal(map[string]any{"model": r.Model, "answers": answers}); marshalErr == nil {
			op.Output(string(raw))
		}
	}
	usage := observability.Usage{Protocol: "jev"}
	if r != nil {
		var raw struct {
			Usage struct {
				Input  *int64 `json:"input_tokens"`
				Output *int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(r.Raw), &raw) == nil {
			usage.Input = raw.Usage.Input
			usage.Output = raw.Usage.Output
		}
	}
	op.Usage(usage)
	op.End(err)
}
