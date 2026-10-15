package agent

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

func beginJevObservation(ctx context.Context, name string) (context.Context, *observability.Operation) {
	return observability.Start(ctx, name, "evaluator")
}
func endJevObservation(op *observability.Operation, r *jev.Result, err error) {
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
