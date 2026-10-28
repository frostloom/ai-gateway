package taskbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// No production type currently implements this private capability. The owned
// driver must later supply live identity and ledger ownership; data is no issuer.
type windowProviderCapability interface {
	windowAccountingLedger() *windowAccounting
	windowNextRequest(context.Context, string) (windowRequestIdentity, error)
}
type windowProviderPayloadCapability interface {
	windowPayloadEvidence() (*windowPayloadStore, bool)
}
type windowProviderTransport struct {
	capability windowProviderCapability
	ledger     *windowAccounting
	provider   string
	upstream   http.RoundTripper
	payload    *windowPayloadStore
}

func windowTransportNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func newWindowProviderTransport(capability windowProviderCapability, provider string, upstream http.RoundTripper) (*windowProviderTransport, error) {
	if windowTransportNil(capability) || reflect.ValueOf(capability).Kind() != reflect.Ptr || windowTransportNil(upstream) {
		return nil, fmt.Errorf("live window capability and explicit transport required")
	}
	l := capability.windowAccountingLedger()
	if l == nil {
		return nil, fmt.Errorf("missing owned window ledger")
	}
	_, model, e := windowProviderEndpoint(provider)
	if e != nil {
		return nil, e
	}
	p, e := l.contract.provider(provider)
	if e != nil || p.Model != model {
		return nil, fmt.Errorf("provider differs from window contract")
	}
	state := l.Snapshot()
	if state.Closed || state.Halt != "" {
		return nil, fmt.Errorf("window stopped")
	}
	var payload *windowPayloadStore
	if evidence, ok := capability.(windowProviderPayloadCapability); ok {
		var required bool
		payload, required = evidence.windowPayloadEvidence()
		if required && payload == nil || payload != nil && (payload.contract != l.contract || required && payload.kind != "recorded_run") {
			return nil, fmt.Errorf("window payload evidence binding rejected")
		}
	}
	return &windowProviderTransport{capability: capability, ledger: l, provider: provider, upstream: upstream, payload: payload}, nil
}

func windowTransportStop(l *windowAccounting, code string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.halt(code)
}

func (t *windowProviderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	requestClosed := false
	closeRequest := func() error {
		if requestClosed || req == nil || req.Body == nil {
			return nil
		}
		requestClosed = true
		return req.Body.Close()
	}
	defer closeRequest()
	if t == nil || t.ledger == nil || t.capability == nil || t.upstream == nil {
		return nil, fmt.Errorf("missing live window transport")
	}
	reject := func() (*http.Response, error) {
		_ = closeRequest()
		_ = windowTransportStop(t.ledger, "invalid_request")
		return nil, fmt.Errorf("window request rejected")
	}
	if req == nil || req.URL == nil || req.Body == nil || req.RequestURI != "" || len(req.TransferEncoding) != 0 || req.Context().Err() != nil {
		return reject()
	}
	if t.capability.windowAccountingLedger() != t.ledger {
		return reject()
	}
	if req.Host != "" && req.Host != req.URL.Host {
		return reject()
	}
	contentTypes := 0
	for name, values := range req.Header {
		if strings.EqualFold(name, "Idempotency-Key") || strings.EqualFold(name, "X-Idempotency-Key") || strings.EqualFold(name, "Content-Encoding") {
			return reject()
		}
		if strings.EqualFold(name, "Content-Type") {
			contentTypes++
			if len(values) != 1 {
				return reject()
			}
			media, _, e := mime.ParseMediaType(values[0])
			if e != nil || media != "application/json" {
				return reject()
			}
		}
	}
	if contentTypes != 1 {
		return reject()
	}
	p, _ := t.ledger.contract.provider(t.provider)
	raw, readErr := io.ReadAll(io.LimitReader(req.Body, int64(p.MaxRequestBytes)+1))
	closeErr := closeRequest()
	if readErr != nil || closeErr != nil || len(raw) > p.MaxRequestBytes {
		return reject()
	}
	identity, e := t.capability.windowNextRequest(req.Context(), t.provider)
	if e != nil || t.capability.windowAccountingLedger() != t.ledger {
		return reject()
	}
	identity.BodySHA256 = conversationHash(raw)
	identity.Bytes = len(raw)
	if validateWindowPOST(t.provider, req.Method, req.URL.String(), raw, p, identity) != nil {
		return reject()
	}
	ticket, e := t.ledger.Begin(identity)
	if e != nil {
		return nil, fmt.Errorf("window request admission stopped")
	}
	fail := func(code string) (*http.Response, error) {
		_ = t.ledger.Fail(ticket, code)
		return nil, fmt.Errorf("window provider stopped")
	}
	if t.payload != nil && t.payload.writeWindowRequest(ticket.sequence, identity, raw) != nil {
		return fail("protocol_failure")
	}
	if req.Context().Err() != nil {
		if t.payload != nil {
			_ = t.payload.writeWindowResponseWithFailure(ticket.sequence, identity, 0, nil, false, "round_trip", &windowTransportFailure{Cause: jev.ClassifyTransportFailureCause(req.Context().Err()), Phase: "before_send"})
		}
		return fail("upstream_failure")
	}
	frozen := req.Clone(req.Context())
	frozen.Body = io.NopCloser(bytes.NewReader(raw))
	frozen.GetBody = nil
	frozen.ContentLength = int64(len(raw))
	var probe *windowTransportProbe
	if t.payload != nil {
		var ctx context.Context
		ctx, probe = newWindowTransportProbe(frozen.Context())
		frozen = frozen.WithContext(ctx)
	}
	response, upErr := t.upstream.RoundTrip(frozen)
	if upErr != nil || response == nil || response.Body == nil {
		status := 0
		if response != nil && response.Body != nil {
			if response.StatusCode >= 100 && response.StatusCode <= 599 {
				status = response.StatusCode
			}
			_ = response.Body.Close()
		}
		if t.payload != nil {
			_ = t.payload.writeWindowResponseWithFailure(ticket.sequence, identity, status, nil, false, "round_trip", probe.snapshot(upErr, ""))
		}
		return fail("upstream_failure")
	}
	body, bodyErr := io.ReadAll(io.LimitReader(response.Body, windowMaxResponseBytes+1))
	bodyCloseErr := response.Body.Close()
	complete := bodyErr == nil && len(body) <= windowMaxResponseBytes
	code := "complete"
	if bodyErr != nil {
		code = "response_read"
	} else if len(body) > windowMaxResponseBytes {
		code = "response_bound"
	} else if bodyCloseErr != nil {
		code = "response_close"
	} else if response.StatusCode != http.StatusOK {
		code = "http_status"
	}
	var failure *windowTransportFailure
	if bodyErr != nil {
		failure = probe.snapshot(bodyErr, "response_read")
	} else if bodyCloseErr != nil && code == "response_close" {
		failure = probe.snapshot(bodyCloseErr, "response_close")
	}
	if t.payload != nil && t.payload.writeWindowResponseWithFailure(ticket.sequence, identity, response.StatusCode, body, complete, code, failure) != nil {
		return fail("protocol_failure")
	}
	if !complete || bodyCloseErr != nil || response.StatusCode != http.StatusOK {
		return fail("protocol_failure")
	}
	usage, e := parseWindowResponseUsage(t.provider, body, p)
	if e != nil {
		var envelope *windowUsageEnvelopeError
		if errors.As(e, &envelope) {
			return fail("usage_out_of_bounds")
		}
		return fail("protocol_failure")
	}
	if e = t.ledger.Settle(ticket, conversationHash(body), usage); e != nil {
		return nil, fmt.Errorf("window usage settlement stopped")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}
