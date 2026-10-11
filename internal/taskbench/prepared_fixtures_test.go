package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// This backend/server is an oracle-driven double, not a language model or JEV.
// Its passing score proves runner/scorer plumbing, never natural-language quality.
type preparedReadBackend struct {
	current Case
	state   Snapshot
	tenant  uint64
}

func (b *preparedReadBackend) Setup(_ context.Context, _ string, c Case) (Fixture, error) {
	b.current = c
	b.tenant++
	f := Fixture{TenantID: b.tenant, APIKey: "offline-only-credential", Standard: store.Plan{ID: 1, Name: "月度·标准", PriceMoney: 20}, Flagship: store.Plan{ID: 2, Name: "月度·旗舰", PriceMoney: 50}}
	b.state = Snapshot{BalanceCent: 500000, LedgerBalance: 500000, Consistent: true}
	if c.InitialSubscription {
		b.state.Subscriptions = []store.Subscription{{TenantID: b.tenant, PlanID: f.Standard.ID, Status: store.SubActive}}
	}
	return f, nil
}
func (b *preparedReadBackend) Snapshot(context.Context, Fixture, string) (Snapshot, error) {
	return b.state, nil
}
func (b *preparedReadBackend) Pending(context.Context, string) (Pending, error) {
	return Pending{}, fmt.Errorf("read fixture must never require confirmation")
}

func TestPreparedReadFixturesThroughOfflineRunner(t *testing.T) {
	raw, err := os.ReadFile("../../eval/task-suites/2026-10-11-read-fixtures-dev-v1/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 214 {
		t.Fatalf("prepared cases=%d want214", len(cases))
	}
	for _, mode := range []string{"model", "jev_selective"} {
		for _, failure := range []string{"none", "wrong_answer", "http_failure"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				backend := &preparedReadBackend{}
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Header.Get("Authorization") != "Bearer offline-only-credential" {
						t.Error("missing isolated fixture authentication")
					}
					reply := "当前现金余额5000元。"
					if backend.current.Tool == "get_my_subscription" {
						if backend.current.InitialSubscription {
							reply = "当前订阅月度·标准套餐。"
						} else {
							reply = "没有生效中的订阅。"
						}
					}
					result := reply
					if calls == 1 && failure == "wrong_answer" {
						reply = "现金余额999元。"
					}
					if calls == 1 && failure == "http_failure" {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					json.NewEncoder(w).Encode(map[string]any{
						"session_id": fmt.Sprintf("offline-%d", backend.tenant), "reply": reply,
						"trace": []Trace{{Tool: backend.current.Tool, Result: result}},
						"telemetry": map[string]any{"jev_enabled": mode == "jev_selective", "llm_calls": 0, "jev_calls": 0,
							"runtime": map[string]any{"model": "offline-double", "routing_mode": mode}},
					})
				}))
				defer server.Close()
				jevMode := "off"
				if mode == "jev_selective" {
					jevMode = "on"
				}
				report := Run(context.Background(), Options{BaseURL: server.URL, Backend: backend, Cases: cases, Metadata: Metadata{Model: "offline-double", JevMode: jevMode, EvaluationContractSHA256: "synthetic-test-contract", FixtureDefinitionSHA256: "synthetic-test-fixture"}})
				completed := len(cases)
				if failure != "none" {
					completed--
				}
				if report.Metrics.ValidTasks != len(cases) || report.Metrics.Completed != completed || calls != len(cases) || len(report.ConfigurationErrors) != 0 {
					t.Fatalf("failure=%s metrics=%+v calls=%d config=%v first=%+v", failure, report.Metrics, calls, report.ConfigurationErrors, report.Results[0])
				}
				wantHTTP := 0
				if failure == "http_failure" {
					wantHTTP = 1
				}
				if report.Metrics.FailedHTTP != wantHTTP || report.Metrics.LLMCalls != 0 {
					t.Fatalf("unexpected error/call accounting: %+v", report.Metrics)
				}
			})
		}
	}
}
