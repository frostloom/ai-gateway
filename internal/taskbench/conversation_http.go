package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

// ConversationOracle reads authoritative server state, including absent pending
// as nil. It must never construct state from the assistant's HTTP response.
type ConversationOracle interface {
	Setup(context.Context, ConversationCase) (Fixture, ConversationState, error)
	State(context.Context, Fixture, string) (ConversationState, error)
	Pending(context.Context, Fixture, string) (*Pending, error)
}
type HandlerConversationDriver struct {
	handler http.Handler
	oracle  ConversationOracle
}

// NewHandlerConversationDriver invokes an explicitly supplied handler in-process.
// It has no remote URL, environment configuration, retries or model client.
// A supplied production handler can have external dependencies: callers must
// explicitly replace them with local doubles before using this for free tests.
func NewHandlerConversationDriver(h http.Handler, o ConversationOracle) (*HandlerConversationDriver, error) {
	if h == nil || o == nil {
		return nil, fmt.Errorf("handler and authoritative oracle required")
	}
	return &HandlerConversationDriver{handler: h, oracle: o}, nil
}
func (d *HandlerConversationDriver) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	return d.oracle.Setup(ctx, c)
}
func (d *HandlerConversationDriver) Final(ctx context.Context, f Fixture, s string) (ConversationState, error) {
	return d.oracle.State(ctx, f, s)
}
func (d *HandlerConversationDriver) Step(ctx context.Context, f Fixture, s, message string) (ConversationObservation, error) {
	obs := ConversationObservation{Action: "unreviewed", Turn: Turn{Message: message}}
	raw, err := json.Marshal(map[string]string{"session_id": s, "message": message})
	if err != nil {
		return obs, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://local.invalid/chat", bytes.NewReader(raw))
	if err != nil {
		return obs, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.APIKey)
	rec := httptest.NewRecorder()
	d.handler.ServeHTTP(rec, req)
	obs.Turn.Status = rec.Code
	obs.Turn.RawBody = rec.Body.String()
	var envelope map[string]json.RawMessage
	if json.Unmarshal(rec.Body.Bytes(), &envelope) == nil {
		obs.Turn.TelemetryRaw = envelope["telemetry"]
		obs.Turn.TraceRaw = envelope["trace"]
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &obs.Turn.Response); err != nil {
		obs.Turn.Error = "invalid response JSON"
	}
	if rec.Code != http.StatusOK {
		obs.Turn.Error = fmt.Sprintf("HTTP %d: %s", rec.Code, obs.Turn.Response.HTTPError)
	}
	session := obs.Turn.Response.SessionID
	if session == "" {
		session = s
	}
	obs.State, err = d.oracle.State(ctx, f, session)
	if err != nil {
		return obs, fmt.Errorf("state oracle: %w", err)
	}
	obs.Pending, err = d.oracle.Pending(ctx, f, session)
	if err != nil {
		return obs, fmt.Errorf("pending oracle: %w", err)
	}
	obs.Turn.Snapshot = obs.State.Own
	obs.Turn.Pending = obs.Pending
	// These are trace-derived recording labels, not semantic verdicts. No-tool
	// clarify/cancel/refuse/respond labels need independent review.
	if obs.Turn.Status == 200 && obs.Turn.Error == "" {
		for _, tr := range obs.Turn.Response.Trace {
			if tr.Phase == "execute" && isConversationWrite(tr.Tool) {
				obs.Action = "execute"
				break
			}
			if tr.Phase == "preview" && isConversationWrite(tr.Tool) {
				obs.Action = "preview"
			} else if tr.Phase == "read" && obs.Action == "unreviewed" {
				obs.Action = "read"
			}
		}
	}
	return obs, nil
}
