package taskbench

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type localCampaignDependencies struct {
	model    func(ConversationCase) http.RoundTripper
	judge    func(ConversationCase) jev.Evaluator
	observer *observability.Observer
	capture  *localCampaignCapture
	close    func() error
}

func newLocalCampaignDependencies(ctx context.Context) (*localCampaignDependencies, error) {
	capture := &localCampaignCapture{}
	server := httptest.NewServer(capture)
	o, err := observability.New(ctx, observability.Config{Enabled: true, CaptureContent: true, Endpoint: server.URL + "/api/public/otel/v1/traces", PublicKey: "pk-local-rehearsal", SecretKey: "sk-local-rehearsal", HashKey: strings.Repeat("h", 32), Environment: "evaluation", Release: "local-campaign-rehearsal", SampleRate: 1, QueueSize: 256, ExportTimeout: 2 * time.Second})
	if err != nil {
		server.Close()
		return nil, err
	}
	d := &localCampaignDependencies{observer: o, capture: capture}
	d.model = func(c ConversationCase) http.RoundTripper { return &localCampaignModel{c: c} }
	d.judge = func(c ConversationCase) jev.Evaluator {
		tool, confidence := "get_balance", .99
		if c.Scenario.Operation != nil {
			tool = c.Scenario.Operation.Tool
		}
		if c.Key() == "balance-020/explicit_scope_followup" {
			confidence = .5
		}
		script := jev.FakeScript{Answers: map[string]jev.Answer{"intent": jev.AnswerChoice(tool, confidence), "legit": jev.AnswerNoul(.99), "risk": jev.AnswerScore(.1, .99, []string{"normal", "caution", "high"}), "route_shape": jev.AnswerChoice("single_read", .99)}}
		return jev.NewFake(script)
	}
	d.close = func() error {
		ctx, cancel := localCampaignCleanupContext()
		defer cancel()
		err := o.Shutdown(ctx)
		server.Close()
		return err
	}
	return d, nil
}

// This transport returns synthetic usage in process. It has no network client,
// redirect handling, external endpoint or credential discovery.
type localCampaignModel struct{ c ConversationCase }

func (m *localCampaignModel) RoundTrip(r *http.Request) (*http.Response, error) {
	defer r.Body.Close()
	var request struct {
		Messages []struct {
			Content []struct{ Type, Text, Content string } `json:"content"`
		} `json:"messages"`
		Max      int                   `json:"max_tokens"`
		Thinking struct{ Type string } `json:"thinking"`
	}
	if r.Method != "POST" || r.URL.Hostname() != "127.0.0.1" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Max != 128 || request.Thinking.Type != "disabled" {
		return nil, fmt.Errorf("unexpected local model request")
	}
	tool := "get_balance"
	args := map[string]any{}
	if m.c.Scenario.Operation != nil {
		tool = m.c.Scenario.Operation.Tool
		args = map[string]any{"amount_money": 100}
	}
	content := []any{map[string]any{"type": "tool_use", "id": "local-call", "name": tool, "input": args}}
	stop := "tool_use"
	if len(request.Messages) > 0 {
		for _, block := range request.Messages[len(request.Messages)-1].Content {
			text := ""
			if block.Type == "tool_result" {
				text = block.Content
			} else if block.Type == "text" {
				switch block.Text {
				case "看看我还能用多少":
					text = "请问您要查询人民币现金余额，还是套餐token额度？"
				case "我要充值":
					text = "请问要充值多少元人民币？"
				case "我要退款":
					text = "请问要退回多少元充值款，或是哪笔充值订单？"
				}
			}
			if text != "" {
				content = []any{map[string]any{"type": "text", "text": text}}
				stop = "end_turn"
			}
		}
	}
	raw, err := json.Marshal(map[string]any{"model": "local-rehearsal", "content": content, "stop_reason": stop, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
}

type localCampaignCapture struct {
	sync.Mutex
	spans  []*tracepb.Span
	failed bool
}

func (c *localCampaignCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	defer r.Body.Close()
	var body collector.ExportTraceServiceRequest
	if err == nil {
		err = proto.Unmarshal(raw, &body)
	}
	c.Lock()
	defer c.Unlock()
	if err != nil || r.Method != "POST" || r.URL.Path != "/api/public/otel/v1/traces" {
		c.failed = true
		w.WriteHeader(400)
		return
	}
	for _, resource := range body.ResourceSpans {
		for _, scope := range resource.ScopeSpans {
			c.spans = append(c.spans, scope.Spans...)
		}
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
}

func (c *localCampaignCapture) check(report SmallPairReport) (LocalCampaignObservations, error) {
	c.Lock()
	defer c.Unlock()
	var counts LocalCampaignObservations
	if c.failed {
		return counts, fmt.Errorf("local OTLP capture failed")
	}
	for _, row := range report.Rows {
		if len(row.LangfuseTraceID) != 32 || row.ModelAttempts == nil {
			return counts, fmt.Errorf("missing local trace/attempts")
		}
		tasks, turns, generations := 0, 0, 0
		for _, span := range c.spans {
			if hex.EncodeToString(span.TraceId) != row.LangfuseTraceID {
				continue
			}
			attrs := map[string]string{}
			for _, attr := range span.Attributes {
				attrs[attr.Key] = attr.Value.GetStringValue()
			}
			if attrs["langfuse.observation.metadata.case_key"] != row.Key || attrs["langfuse.observation.metadata.arm"] != row.Arm || attrs["langfuse.observation.metadata.run_id"] == "" || attrs["langfuse.observation.metadata.evidence_kind"] != "local_stub" {
				return counts, fmt.Errorf("local span association differs")
			}
			switch span.Name {
			case "evaluation.task":
				if attrs["langfuse.observation.metadata.task_success"] != "unknown" {
					return counts, fmt.Errorf("synthetic semantic success claim")
				}
				tasks++
			case "agent.turn":
				turns++
			case "llm.attempt":
				generations++
			}
		}
		if tasks != 1 || turns != row.ObservedTurns || generations != *row.ModelAttempts {
			return counts, fmt.Errorf("local task/turn/generation coverage differs")
		}
		counts.Tasks += tasks
		counts.Turns += turns
		counts.Generations += generations
	}
	return counts, nil
}
