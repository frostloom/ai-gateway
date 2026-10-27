package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func WindowJEVQuestions() map[string]jev.Question {
	return map[string]jev.Question{
		"legit":  {Type: jev.QNoul},
		"intent": {Type: jev.QChoice, Criteria: map[string]string{"read": "read account", "other": "other"}},
		"risk":   {Type: jev.QScore, Criteria: []string{"safe", "review", "block"}},
	}
}

const WindowJEVAnswers = `{"legit":{"type":"noul","noul":0},"intent":{"type":"choice","choice":"read","probabilities":{"read":0.2},"confidence":0},"risk":{"type":"score","legend":{"0":"safe"},"probabilities":{"0":0.3},"score":0,"confidence":0.01}}`

func WindowJEVBody(answers string) string {
	return `{"model":"jev-1.13.0","answers":` + answers + `,"usage":{"input_tokens":2,"output_tokens":1}}`
}
func WindowJEVResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}
func WindowJEVRejectFallback(t *testing.T, cap *windowTransportTestCapability) {
	t.Helper()
	sends := 0
	// Construct the model transport before the failed evaluation in tests where
	// needed: an already-created transport must also refuse after ledger halt.
	tr := &windowProviderTransport{capability: cap, ledger: cap.ledger, provider: "model", upstream: windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { sends++; return windowTransportResponse("model"), nil })}
	if _, e := tr.RoundTrip(windowTransportRequest(t, "model")); e == nil || sends != 0 {
		t.Fatal("model fallback escaped", e, sends)
	}
}
func TestWindowJEVValidZeroAndLowConfidenceRemainNormal(t *testing.T) {
	for _, answers := range []string{WindowJEVAnswers, strings.Replace(WindowJEVAnswers, `"confidence":0}`, `"confidence":0.1}`, 1), strings.Replace(WindowJEVAnswers, `"score":0,`, `"score":1.25,`, 1)} {
		cap, _ := windowTransportFixture(t)
		calls := 0
		client, e := newWindowJEV(cap, "local-only-test-key", windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return WindowJEVResponse(WindowJEVBody(answers), 200), nil
		}))
		if e != nil {
			t.Fatal(e)
		}
		result, e := client.Evaluate(context.Background(), "state", WindowJEVQuestions())
		if e != nil || result == nil || calls != 1 {
			t.Fatal(e, result, calls)
		}
		if result.Answers["legit"].Noul != 0 {
			t.Fatal("negative result rewritten")
		}
		s := cap.ledger.Snapshot()
		if s.Halt != "" || s.Pending != nil || s.JEVHeld != 0 || s.JEVKnown != 6 {
			t.Fatal(s)
		}
	}
}
func TestWindowJEVInvalidAnswerStopsBeforeModelFallback(t *testing.T) {
	cases := []string{
		strings.Replace(WindowJEVAnswers, `,"probabilities":{"read":0.2}`, ``, 1), strings.Replace(WindowJEVAnswers, `,"legend":{"0":"safe"}`, ``, 1), strings.Replace(WindowJEVAnswers, `,"probabilities":{"0":0.3}`, ``, 1), strings.Replace(WindowJEVAnswers, `,"confidence":0.01`, ``, 1), strings.Replace(WindowJEVAnswers, `"choice":"read",`, ``, 1), strings.Replace(WindowJEVAnswers, `"probabilities":{"0":0.3}`, `"probabilities":{"01":0.3}`, 1), strings.Replace(WindowJEVAnswers, `"probabilities":{"read":0.2}`, `"probabilities":[]`, 1), strings.Replace(WindowJEVAnswers, `"confidence":0}`, `"confidence":-0.1}`, 1), `{}`, strings.Replace(WindowJEVAnswers, `"legit":{"type":"noul","noul":0},`, ``, 1), strings.Replace(WindowJEVAnswers, `"noul":0`, `"noul":null`, 1), strings.Replace(WindowJEVAnswers, `,"noul":0`, ``, 1), strings.Replace(WindowJEVAnswers, `"noul":0`, `"noul":"secret-body"`, 1), strings.Replace(WindowJEVAnswers, `"noul":0`, `"noul":-0.1`, 1), strings.Replace(WindowJEVAnswers, `"noul":0`, `"noul":1.1`, 1), strings.Replace(WindowJEVAnswers, `"type":"noul"`, `"type":"choice"`, 1), strings.Replace(WindowJEVAnswers, `"choice":"read"`, `"choice":"absent"`, 1), strings.Replace(WindowJEVAnswers, `,"confidence":0}`, `}`, 1), strings.Replace(WindowJEVAnswers, `"confidence":0}`, `"confidence":null}`, 1), strings.Replace(WindowJEVAnswers, `"confidence":0}`, `"confidence":1.1}`, 1), strings.Replace(WindowJEVAnswers, `"score":0`, `"score":2.1`, 1), strings.Replace(WindowJEVAnswers, `"score":0`, `"score":-1`, 1), strings.Replace(WindowJEVAnswers, `"score":0`, `"score":"bad"`, 1), strings.Replace(WindowJEVAnswers, `"score":0`, `"score":null`, 1), strings.Replace(WindowJEVAnswers, `"probabilities":{"read":0.2}`, `"probabilities":{"wrong":0.2}`, 1), strings.Replace(WindowJEVAnswers, `"probabilities":{"read":0.2}`, `"probabilities":{"read":1.1}`, 1), strings.Replace(WindowJEVAnswers, `"legend":{"0":"safe"}`, `"legend":{"3":"bad"}`, 1), strings.Replace(WindowJEVAnswers, `"legend":{"0":"safe"}`, `"legend":{"0":"wrong"}`, 1), strings.Replace(WindowJEVAnswers, `"probabilities":{"0":0.3}`, `"probabilities":{"0":"bad"}`, 1), strings.Replace(WindowJEVAnswers, `"probabilities":{"0":0.3}`, `"probabilities":null`, 1), strings.Replace(WindowJEVAnswers, `"legit":`, `"extra":{"type":"noul","noul":1},"legit":`, 1),
	}
	for i, answers := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			cap, dir := windowTransportFixture(t)
			calls := 0
			client, e := newWindowJEV(cap, "local-only-test-key", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return WindowJEVResponse(WindowJEVBody(answers), 200), nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			r, e := client.Evaluate(context.Background(), "private-state", WindowJEVQuestions())
			if e == nil || r != nil || calls != 1 {
				t.Fatal("bad answer accepted", e, r, calls)
			}
			if strings.Contains(e.Error(), "secret-body") || strings.Contains(e.Error(), "private-state") || strings.Contains(e.Error(), "local-only-test-key") {
				t.Fatal("error leaked provider data", e)
			}
			s := cap.ledger.Snapshot()
			if s.Halt != "protocol_failure" || s.Pending != nil || s.JEVHeld != 0 || s.JEVKnown != 6 {
				t.Fatal("known usage lost or wrong halt", s)
			}
			audit, auditErr := readWindowAccounting(dir)
			if auditErr != nil || audit.State.Halt != "protocol_failure" || audit.State.JEVKnown != 6 || audit.State.JEVHeld != 0 {
				t.Fatal("protocol halt or known usage not durable before fallback", audit, auditErr)
			}
			WindowJEVRejectFallback(t, cap)
		})
	}
}
func TestWindowJEVUnknownUsageOrHTTPRetainsHold(t *testing.T) {
	for _, tc := range []struct {
		body           string
		status         int
		transportError bool
	}{{WindowJEVBody(WindowJEVAnswers), 503, false}, {strings.Replace(WindowJEVBody(WindowJEVAnswers), `"input_tokens":2`, `"input_tokens":null`, 1), 200, false}, {`secret-body`, 200, true}} {
		cap, _ := windowTransportFixture(t)
		client, e := newWindowJEV(cap, "local-only-test-key", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
			if tc.transportError {
				return nil, fmt.Errorf("secret-body local-only-test-key")
			}
			return WindowJEVResponse(tc.body, tc.status), nil
		}))
		if e != nil {
			t.Fatal(e)
		}
		if r, e := client.Evaluate(context.Background(), "state", WindowJEVQuestions()); e == nil || r != nil || strings.Contains(e.Error(), "secret-body") || strings.Contains(e.Error(), "local-only-test-key") {
			t.Fatal(r, e)
		}
		s := cap.ledger.Snapshot()
		if s.Halt == "" || s.Pending == nil || s.JEVHeld != 2200 || s.JEVKnown != 0 {
			t.Fatal(s)
		}
		WindowJEVRejectFallback(t, cap)
	}
}
func TestWindowJEVFactoryRequiresOwnedGuard(t *testing.T) {
	if _, e := newWindowJEV(nil, "key", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected send"); return nil, nil })); e == nil {
		t.Fatal("missing capability accepted")
	}
	cap, _ := windowTransportFixture(t)
	if _, e := newWindowJEV(cap, "", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected send"); return nil, nil })); e == nil {
		t.Fatal("empty key accepted")
	}
}

func TestWindowJEVQuestionCopyPreservesJSONNumbersAndCallerIsolation(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	questions := map[string]jev.Question{"intent": {Type: jev.QChoice, Criteria: map[string]any{"read": map[string]any{"value": json.Number("9007199254740993")}, "other": "other"}}}
	client, e := newWindowJEV(cap, "key", windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "9007199254740993") {
			t.Error("question number rounded before guarded request")
		}
		delete(questions["intent"].Criteria.(map[string]any), "read")
		return WindowJEVResponse(WindowJEVBody(`{"intent":{"type":"choice","choice":"read","confidence":0,"probabilities":{"read":0.2}}}`), 200), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	if r, e := client.Evaluate(context.Background(), "state", questions); e != nil || r == nil {
		t.Fatal("caller mutation changed frozen question contract", r, e)
	}
}
func TestWindowJEVLocalInputErrorsHaltWithoutSend(t *testing.T) {
	for _, kind := range []string{"empty_questions", "bad_criteria", "state_encoding", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			cap, _ := windowTransportFixture(t)
			calls := 0
			client, e := newWindowJEV(cap, "key", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
			if e != nil {
				t.Fatal(e)
			}
			questions := WindowJEVQuestions()
			var state any = "state"
			ctx := context.Background()
			switch kind {
			case "empty_questions":
				questions = nil
			case "bad_criteria":
				questions["intent"] = jev.Question{Type: jev.QChoice}
			case "state_encoding":
				state = make(chan int)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if r, e := client.Evaluate(ctx, state, questions); e == nil || r != nil || calls != 0 {
				t.Fatal(r, e, calls)
			}
			s := cap.ledger.Snapshot()
			if s.Halt != "protocol_failure" || s.JEVAttempts != 0 || s.JEVKnown != 0 || s.JEVHeld != 0 {
				t.Fatal(s)
			}
			WindowJEVRejectFallback(t, cap)
		})
	}
}
