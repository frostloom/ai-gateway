package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordAutoRoute 为一次网关 auto 分流决策写一条根 span（name=auto.route，
// tag auto-route）。state 是发给 JEV 判定的对话状态（走既有裁剪/脱敏），fields
// 是有界决策元数据（模型、分数、档位、兜底/降级原因等）。nil 观察者是 no-op。
func (o *Observer) RecordAutoRoute(ctx context.Context, state any, fields map[string]string) {
	if o == nil || !o.cfg.Enabled {
		return
	}
	_, span := o.tracer.Start(ctx, "auto.route", trace.WithNewRoot(), trace.WithAttributes(
		attribute.String("langfuse.version", "agent-observability.v1"),
		attribute.String("langfuse.release", o.cfg.Release),
		attribute.String("langfuse.environment", o.cfg.Environment),
		attribute.StringSlice("langfuse.trace.tags", []string{o.cfg.Environment, "auto-route"}),
	))
	o.content(span, "input", state)
	for key, value := range fields {
		span.SetAttributes(meta(key, clip(redact(value), 3500)))
	}
	span.End()
}
