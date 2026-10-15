package observability

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Observer struct {
	cfg       Config
	provider  *sdktrace.TracerProvider
	tracer    trace.Tracer
	processor *boundedProcessor
	registry  *prometheus.Registry
}
type Turn struct {
	observer *Observer
	root     trace.Span
	mu       sync.RWMutex
	common   []attribute.KeyValue
}
type turnKey struct{}
type Operation struct {
	span     trace.Span
	observer *Observer
}
type Usage struct {
	Protocol                                           string
	Input, Output, CacheRead, CacheMiss, CacheCreation *int64
}
type Summary struct {
	Pending                                                         bool
	Stage                                                           string
	LLMCalls, JevCalls, JevErrors, LLMUsageMissing, JevUsageMissing int
	FastPath                                                        bool
	FastReason, Intent                                              string
	IntentConfidence                                                float64
}

func New(ctx context.Context, c Config) (*Observer, error) {
	if !c.Enabled {
		return nil, nil
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if c.QueueSize == 0 {
		c.QueueSize = 256
	}
	if c.ExportTimeout == 0 {
		c.ExportTimeout = 2 * time.Second
	}
	auth := base64.StdEncoding.EncodeToString([]byte(c.PublicKey + ":" + c.SecretKey))
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(c.Endpoint), otlptracehttp.WithHeaders(map[string]string{"Authorization": "Basic " + auth, "x-langfuse-ingestion-version": "4"}), otlptracehttp.WithTimeout(c.ExportTimeout), otlptracehttp.WithHTTPClient(&http.Client{Timeout: c.ExportTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}), otlptracehttp.WithMaxRequestSize(1<<20), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
	if err != nil {
		return nil, err
	}
	processor := newProcessor(exp, c.QueueSize, c.ExportTimeout)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.SampleRate))), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "ai-gateway-agent"))), sdktrace.WithSpanLimits(sdktrace.SpanLimits{AttributeCountLimit: 48, AttributeValueLengthLimit: 4096, EventCountLimit: 0, LinkCountLimit: 0, AttributePerEventCountLimit: 0, AttributePerLinkCountLimit: 0}))
	o := &Observer{cfg: c, provider: provider, tracer: provider.Tracer("ai-gateway/langfuse"), processor: processor, registry: prometheus.NewRegistry()}
	for name, read := range map[string]func() float64{"accepted": func() float64 { return float64(processor.counts.accepted.Load()) }, "dropped": func() float64 { return float64(processor.counts.dropped.Load()) }, "exported": func() float64 { return float64(processor.counts.exported.Load()) }, "failed": func() float64 { return float64(processor.counts.failed.Load()) }, "batches": func() float64 { return float64(processor.counts.batches.Load()) }, "turns": func() float64 { return float64(processor.counts.turns.Load()) }, "sampled_turns": func() float64 { return float64(processor.counts.sampledTurns.Load()) }} {
		o.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "agent_langfuse_" + name + "_total", Help: "Local telemetry counter: " + name}, read))
	}
	o.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "agent_langfuse_queue_depth", Help: "Spans awaiting export"}, func() float64 { return float64(len(processor.queue)) }))
	return o, nil
}
func (o *Observer) Gatherer() prometheus.Gatherer {
	if o == nil {
		return nil
	}
	return o.registry
}
func (o *Observer) Stats() Stats {
	if o == nil {
		return Stats{}
	}
	return o.processor.stats()
}
func (o *Observer) Shutdown(ctx context.Context) error {
	if o == nil {
		return nil
	}
	return o.provider.Shutdown(ctx)
}
func (o *Observer) ForceFlush(ctx context.Context) error {
	if o == nil {
		return nil
	}
	return o.provider.ForceFlush(ctx)
}
func (o *Observer) alias(domain, value string) string {
	h := hmac.New(sha256.New, []byte(o.cfg.HashKey))
	h.Write([]byte(domain + "\x00" + value))
	return hex.EncodeToString(h.Sum(nil))
}
func meta(key, value string) attribute.KeyValue {
	return attribute.String("langfuse.observation.metadata."+key, value)
}
func (o *Observer) StartTurn(ctx context.Context, routing string) (context.Context, *Turn) {
	if o == nil {
		return ctx, nil
	}
	common := []attribute.KeyValue{attribute.String("langfuse.version", "agent-observability.v1"), attribute.String("langfuse.release", o.cfg.Release), attribute.String("langfuse.environment", o.cfg.Environment), attribute.StringSlice("langfuse.trace.tags", []string{o.cfg.Environment}), meta("routing_mode", routing)}
	opts := []trace.SpanStartOption{trace.WithNewRoot()}
	if evaluation, ok := ctx.Value(evaluationKey{}).(*EvaluationRun); ok {
		common = append(common, evaluation.common...)
		common = append(common, meta("turn_index", strconv.FormatInt(evaluation.turns.Add(1), 10)))
		opts = nil
	}
	opts = append(opts, trace.WithAttributes(append(common, attribute.String("langfuse.observation.type", "agent"))...))
	ctx, span := o.tracer.Start(ctx, "agent.turn", opts...)
	turn := &Turn{observer: o, root: span, common: common}
	o.processor.counts.turns.Add(1)
	if span.IsRecording() {
		o.processor.counts.sampledTurns.Add(1)
	}
	return context.WithValue(ctx, turnKey{}, turn), turn
}
func (t *Turn) BindIdentity(tenant uint64, sid string) {
	if t == nil {
		return
	}
	attrs := []attribute.KeyValue{attribute.String("langfuse.user.id", t.observer.alias("tenant", strconv.FormatUint(tenant, 10))), attribute.String("langfuse.session.id", t.observer.alias("session", strconv.FormatUint(tenant, 10)+":"+sid))}
	t.mu.Lock()
	t.common = append(t.common, attrs...)
	t.mu.Unlock()
	t.root.SetAttributes(attrs...)
}
func Start(ctx context.Context, name, kind string) (context.Context, *Operation) {
	t, _ := ctx.Value(turnKey{}).(*Turn)
	if t == nil {
		return ctx, nil
	}
	t.mu.RLock()
	common := append([]attribute.KeyValue(nil), t.common...)
	t.mu.RUnlock()
	ctx, span := t.observer.tracer.Start(ctx, name, trace.WithAttributes(append(common, attribute.String("langfuse.observation.type", kind))...))
	return ctx, &Operation{span: span, observer: t.observer}
}
func (o *Operation) Model(name string) {
	if o != nil {
		o.span.SetAttributes(attribute.String("langfuse.observation.model.name", name))
	}
}

// BasePrompt links evaluation generations to the immutable base template only.
// Dynamic hints and conversation history remain part of the application input.
func (o *Operation) BasePrompt(hash string) {
	if o == nil || o.observer == nil || o.observer.cfg.Environment != "evaluation" {
		return
	}
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != hash {
		return
	}
	o.span.SetAttributes(
		attribute.String("langfuse.observation.prompt.name", "customer-service/system/"+hash),
		attribute.Int("langfuse.observation.prompt.version", 1),
		meta("prompt_scope", "base_system_only"),
		meta("base_prompt_sha256", hash),
	)
}
func (o *Operation) Usage(u Usage) {
	if o == nil {
		return
	}
	details := map[string]int64{}
	set := func(key string, value *int64) {
		if value != nil && *value >= 0 {
			details[key] = *value
		}
	}
	set("input", u.Input)
	set("output", u.Output)
	complete := u.Input != nil && u.Output != nil && *u.Input >= 0 && *u.Output >= 0
	if u.Protocol == "openai" && u.CacheRead != nil && *u.CacheRead >= 0 {
		if u.Input != nil && *u.CacheRead <= *u.Input {
			details["input"] = *u.Input - *u.CacheRead
			details["cache_read_input_tokens"] = *u.CacheRead
		} else {
			complete = false
		}
	} else {
		set("cache_read_input_tokens", u.CacheRead)
	}
	set("cache_creation_input_tokens", u.CacheCreation)
	o.span.SetAttributes(meta("usage_complete", strconv.FormatBool(complete)), meta("usage_protocol", u.Protocol))
	if u.CacheMiss != nil {
		o.span.SetAttributes(meta("provider_cache_miss_tokens", strconv.FormatInt(*u.CacheMiss, 10)))
	}
	if len(details) > 0 {
		raw, _ := json.Marshal(details)
		o.span.SetAttributes(attribute.String("langfuse.observation.usage_details", string(raw)))
	}
}
func (o *Operation) End(err error) {
	if o == nil {
		return
	}
	if err != nil {
		o.span.SetStatus(codes.Error, "operation failed")
		if o.observer != nil && o.observer.cfg.CaptureContent {
			o.span.SetAttributes(meta("error_summary", redact(err.Error())))
		}
		o.span.SetAttributes(attribute.String("langfuse.observation.level", "ERROR"))
	}
	o.span.End()
}
func (t *Turn) End(status int, s Summary) {
	if t == nil {
		return
	}
	t.root.SetAttributes(meta("http_status", strconv.Itoa(status)), meta("stage", s.Stage), meta("pending_confirm", strconv.FormatBool(s.Pending)), meta("llm_calls", strconv.Itoa(s.LLMCalls)), meta("jev_calls", strconv.Itoa(s.JevCalls)), meta("jev_errors", strconv.Itoa(s.JevErrors)), meta("llm_usage_missing", strconv.Itoa(s.LLMUsageMissing)), meta("jev_usage_missing", strconv.Itoa(s.JevUsageMissing)), meta("jev_fast_path", strconv.FormatBool(s.FastPath)), meta("jev_fast_path_reason", s.FastReason), meta("jev_intent", s.Intent), meta("jev_intent_confidence", strconv.FormatFloat(s.IntentConfidence, 'f', -1, 64)))
	if status >= 400 {
		t.root.SetStatus(codes.Error, "request failed")
	}
	t.root.End()
}

func (o *Operation) Phase(phase string) {
	if o != nil {
		o.span.SetAttributes(meta("phase", phase))
	}
}
