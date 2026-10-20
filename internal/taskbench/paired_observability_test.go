package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type pairedSpanCapture struct {
	sync.Mutex
	spans []*tracepb.Span
}

// The default sink is a local test server. Real local Langfuse ingestion needs
// explicit opt-in and independent project keys, never model/provider keys.
func pairedTestObserver(t *testing.T) (*observability.Observer, *pairedSpanCapture) {
	t.Helper()
	capture := &pairedSpanCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(raw, &body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		capture.Lock()
		defer capture.Unlock()
		for _, resource := range body.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				capture.spans = append(capture.spans, scope.Spans...)
			}
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(server.Close)
	cfg := observability.Config{CaptureContent: true, Enabled: true, Endpoint: server.URL + "/api/public/otel/v1/traces", PublicKey: "pk-local-test", SecretKey: "sk-local-test", HashKey: strings.Repeat("h", 32), Environment: "evaluation", Release: "local-paired-observation", SampleRate: 1, QueueSize: 256, ExportTimeout: 2 * time.Second}
	if os.Getenv("TASKBENCH_LANGFUSE_SMOKE") == "1" {
		var err error
		cfg, err = observability.ConfigFromEnv(os.Getenv)
		if err != nil {
			t.Fatal("invalid explicit Langfuse configuration")
		}
		endpoint, _ := url.Parse(cfg.Endpoint)
		if !cfg.Enabled || cfg.Environment != "evaluation" || cfg.SampleRate != 1 || endpoint == nil || endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
			t.Fatal("smoke requires enabled local evaluation project and full sampling")
		}
		capture = nil
	}
	observer, err := observability.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return observer, capture
}

func checkPairedObservations(t *testing.T, observer *observability.Observer, capture *pairedSpanCapture, traceIDs []string, reports []SmallPairReport) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := observer.ForceFlush(ctx); err != nil {
		t.Fatal("Langfuse ingestion incomplete", err)
	}
	stats := observer.Stats()
	if stats.Dropped != 0 || stats.Failed != 0 || stats.Exported == 0 {
		t.Fatalf("incomplete observability: %+v", stats)
	}
	if capture != nil {
		capture.Lock()
		defer capture.Unlock()
		tasks, turns, generations := 0, 0, 0
		wantTasks, wantTurns, wantGenerations := 0, 0, 0
		for _, report := range reports {
			wantTasks += report.FinishedArms
			for _, row := range report.Rows {
				wantTurns += row.ObservedTurns
				if row.ModelAttempts == nil {
					t.Fatal("local observation test requires complete upstream attempt evidence")
				}
				wantGenerations += *row.ModelAttempts
			}
		}
		for _, span := range capture.spans {
			attrs := map[string]string{}
			for _, a := range span.Attributes {
				attrs[a.Key] = a.Value.GetStringValue()
			}
			if attrs["langfuse.observation.metadata.case_key"] == "" || attrs["langfuse.observation.metadata.run_id"] == "" {
				t.Fatal("span lost case association")
			}
			switch span.Name {
			case "evaluation.task":
				tasks++
				if attrs["langfuse.observation.metadata.status"] != "needs_review" || attrs["langfuse.observation.metadata.task_success"] != "unknown" {
					t.Fatal("local task incorrectly presented as proven success")
				}
			case "agent.turn":
				turns++
				if attrs["langfuse.observation.input"] == "" || attrs["langfuse.observation.output"] == "" {
					t.Fatal("synthetic question/answer not captured")
				}
			case "llm.attempt":
				generations++
			}
		}
		if tasks != wantTasks || turns != wantTurns || generations != wantGenerations || tasks != len(traceIDs) {
			t.Fatalf("tasks/turns/generations=%d/%d/%d want=%d/%d/%d", tasks, turns, generations, wantTasks, wantTurns, wantGenerations)
		}
		t.Logf("local OTLP coverage: tasks=%d turns=%d generations=%d drops=%d failures=%d", tasks, turns, generations, stats.Dropped, stats.Failed)
	}
	if path := os.Getenv("TASKBENCH_LANGFUSE_SMOKE_RECEIPT"); path != "" && os.Getenv("TASKBENCH_LANGFUSE_SMOKE") == "1" {
		raw, _ := json.MarshalIndent(map[string]any{"evidence_kind": "local_stub", "trace_ids": traceIDs, "reports": reports, "stats": stats, "external_model_calls": 0, "external_jev_calls": 0, "semantic_review": "pending_independent_review"}, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
