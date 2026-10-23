package taskbench

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

type handlerOracle struct {
	f       Fixture
	state   ConversationState
	pending *Pending
	err     error
}

func (o *handlerOracle) Setup(context.Context, ConversationCase) (Fixture, ConversationState, error) {
	return o.f, o.state, nil
}
func (o *handlerOracle) State(context.Context, Fixture, string) (ConversationState, error) {
	return o.state, o.err
}
func (o *handlerOracle) Pending(context.Context, Fixture, string) (*Pending, error) {
	return o.pending, nil
}

func TestConversationHandlerCapturesServerEvidence(t *testing.T) {
	f, s := conversationFixture()
	f.APIKey = "test-only-key"
	oracle := &handlerOracle{f: f, state: s, pending: &Pending{Tool: "recharge", TenantID: f.TenantID, Args: map[string]any{"amount_money": float64(100)}}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/chat" || r.Header.Get("Authorization") != "Bearer test-only-key" {
			t.Error("request lost identity/protocol")
		}
		fmt.Fprint(w, `{"session_id":"s","reply":"请确认","pending_confirm":true,"pending_tool":"recharge","trace":[{"tool":"recharge","phase":"preview","outcome":"pending","resolved_args":"{\"amount_money\":100}"}],"telemetry":{"llm_calls":1,"jev_calls":0}}`)
	})
	d, err := NewHandlerConversationDriver(h, oracle)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := d.Step(context.Background(), f, "", "充值100元")
	if err != nil {
		t.Fatal(err)
	}
	if obs.Action != "preview" || obs.Pending == nil || obs.State.Own.BalanceCent != s.Own.BalanceCent || len(obs.Turn.TraceRaw) == 0 || len(obs.Turn.TelemetryRaw) == 0 || obs.Turn.RawBody == "" {
		t.Fatalf("lost evidence: %+v", obs)
	}
}
func TestConversationHandlerDoesNotInferNoToolSemantics(t *testing.T) {
	f, s := conversationFixture()
	d, _ := NewHandlerConversationDriver(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"session_id":"s","reply":"请提供金额"}`)
	}), &handlerOracle{f: f, state: s})
	obs, err := d.Step(context.Background(), f, "", "充值")
	if err != nil {
		t.Fatal(err)
	}
	if obs.Action != "unreviewed" {
		t.Fatalf("invented semantic action: %s", obs.Action)
	}
}

func TestConversationUnreviewedActionIsPendingNotWrong(t *testing.T) {
	d := rechargeDriver()
	d.steps[0].Action = "unreviewed"
	r := RunConversation(context.Background(), rechargeConversation(), d)
	if !r.StructuralPass || r.Status != "needs_review" || r.Success || r.NecessaryClarifications != 0 {
		t.Fatalf("missing annotation treated as incorrect behavior: %+v", r)
	}
	d = rechargeDriver()
	d.steps[1].Action = "unreviewed"
	r = RunConversation(context.Background(), rechargeConversation(), d)
	if r.StructuralPass {
		t.Fatal("trace-bearing action lost structural label")
	}
}
func TestConversationHandlerPreservesFailureAndPendingOracle(t *testing.T) {
	f, s := conversationFixture()
	for _, body := range []string{`{"error":"unavailable","telemetry":{"llm_calls":2,"jev_calls":1}}`, "not-json"} {
		d, _ := NewHandlerConversationDriver(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); fmt.Fprint(w, body) }), &handlerOracle{f: f, state: s})
		obs, err := d.Step(context.Background(), f, "s", "确认")
		if err != nil || obs.Turn.Status != 503 || obs.Turn.RawBody != body || obs.Turn.Error == "" {
			t.Fatalf("failure erased: %+v %v", obs, err)
		}
	}
	oracle := &handlerOracle{f: f, state: s, err: fmt.Errorf("oracle unavailable")}
	d, _ := NewHandlerConversationDriver(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"session_id":"s"}`) }), oracle)
	if _, err := d.Step(context.Background(), f, "s", "查余额"); err == nil {
		t.Fatal("oracle failure accepted")
	}
	if _, err := NewHandlerConversationDriver(nil, oracle); err == nil {
		t.Fatal("nil handler accepted")
	}
	if _, err := NewHandlerConversationDriver(http.NotFoundHandler(), nil); err == nil {
		t.Fatal("nil oracle accepted")
	}
}

func TestConversationRequiredReadCannotRemainUnreviewed(t *testing.T) {
	f, state := conversationFixture()
	for _, allowClarify := range []bool{false, true} {
		actions := []string{"read"}
		if allowClarify {
			actions = append(actions, "clarify")
		}
		c := ConversationCase{SourceID: "balance-required", Scenario: ConversationScenario{ID: "read", Profile: "no_subscription", MeasurementScope: "complete_task", FullTaskMetricEligible: true, Turns: []ConversationRule{{User: "查当前余额", ExpectedActions: actions, AllowedTools: []string{"get_balance"}}}, Terminal: map[string]any{"business_state_unchanged": true}}}
		d := &scriptedConversation{f: f, initial: state, steps: []ConversationObservation{{Action: "unreviewed", State: state, Turn: Turn{Status: 200, Response: Response{SessionID: "read-session", Reply: "当前现金余额 ¥5000.00。"}}}}}
		r := RunConversation(context.Background(), c, d)
		if allowClarify {
			if !r.StructuralPass || r.Status != "needs_review" {
				t.Fatalf("clarification alternatives lost review gate: %+v", r)
			}
		} else if r.StructuralPass {
			t.Fatal("required successful read accepted without trace")
		}
	}
}
