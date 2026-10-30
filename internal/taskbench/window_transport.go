package taskbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	capability  windowProviderCapability
	ledger      *windowAccounting
	provider    string
	upstream    http.RoundTripper
	payload     *windowPayloadStore
	diagnostics *windowFailureRecorder
	v8Base      *http.Transport
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
	var diagnostics *windowFailureRecorder
	if sink, ok := capability.(windowFailureDiagnosticCapability); ok {
		diagnostics = sink.windowFailureDiagnostics()
	}
	var v8Base *http.Transport
	if l.contract.Schema == windowAccountingV3 {
		owned, ok := upstream.(*windowActualNoRedirectTransport)
		if !ok || owned == nil || owned.base == nil {
			return nil, fmt.Errorf("v8 concrete owned transport required")
		}
		v8Base = owned.base
	}
	return &windowProviderTransport{capability: capability, ledger: l, provider: provider, upstream: upstream, payload: payload, diagnostics: diagnostics, v8Base: v8Base}, nil
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
	v8 := t.ledger.contract.Schema == windowAccountingV3
	phase, _ := req.Context().Value(windowOwnedPhaseKey{}).(*windowOwnedPhase)
	if v8 && t.guardWindowV8(req, identity, phase) != nil {
		return reject()
	}
	ticket, e := t.ledger.Begin(identity)
	if e != nil {
		return nil, fmt.Errorf("window request admission stopped")
	}
	var nativeOp *observability.Operation
	var nativeSpan trace.Span
	endNative := func(code string) {
		if nativeOp != nil {
			nativeSpan.SetAttributes(attribute.String("langfuse.observation.metadata.native_outcome", code))
			var err error
			if code != "settled" {
				err = fmt.Errorf("%s", code)
			}
			nativeOp.End(err)
			nativeOp = nil
		}
	}
	defer func() { endNative("stopped") }()
	evidence := windowFailureTransportEvidence{Sequence: ticket.sequence, Provider: t.provider, RequestPersistence: "not_attempted", DescriptorPersistence: "not_attempted", LedgerPersistence: "unknown"}
	fail := func(code string) (*http.Response, error) {
		endNative(code)
		t.ledger.mu.Lock()
		eventsBefore := t.ledger.events
		t.ledger.mu.Unlock()
		ledgerErr := t.ledger.Fail(ticket, code)
		if t.diagnostics != nil {
			evidence.Code = code
			// Fail returns a halt error even when the journal append succeeds.
			// A failed/changed append must not be mistaken for that expected error.
			t.ledger.mu.Lock()
			verified := ledgerErr != nil && !t.ledger.poisoned && !t.ledger.closed && t.ledger.events == eventsBefore+1 && t.ledger.state.Halt == code && t.ledger.validTicket(ticket)
			t.ledger.mu.Unlock()
			evidence.LedgerPersistence = "failed"
			if verified {
				evidence.LedgerPersistence = "verified"
			}
			t.diagnostics.mu.Lock()
			t.diagnostics.evidence.Transport = append(t.diagnostics.evidence.Transport, evidence)
			t.diagnostics.mu.Unlock()
		}
		return nil, fmt.Errorf("window provider stopped")
	}
	if t.payload != nil {
		evidence.RequestPersistence = "failed"
		if t.payload.writeWindowRequest(ticket.sequence, identity, raw) != nil {
			return fail("protocol_failure")
		}
		evidence.RequestPersistence = "verified"
	}
	writeResponse := func(status int, body []byte, complete bool, code string, failure *windowTransportFailure) error {
		// Native observations never copy provider content. Captured-byte digests and
		// lengths join to the exact private packet; partial captures remain explicit.
		if v8 && nativeSpan != nil {
			nativeSpan.SetAttributes(attribute.Int("langfuse.observation.metadata.response_status", status), attribute.Int("langfuse.observation.metadata.response_bytes", len(body)), attribute.Bool("langfuse.observation.metadata.response_complete", complete), attribute.Bool("langfuse.observation.metadata.response_available", body != nil || complete))
			if body != nil {
				nativeSpan.SetAttributes(attribute.String("langfuse.observation.metadata.response_capture_sha256", conversationHash(body)))
			}
		}

		// Keep the original response classification even if its descriptor cannot
		// be persisted. The ledger's protocol_failure is a separate outcome.
		evidence.ResponseStage = code
		if status >= 100 && status <= 599 {
			evidence.ResponseStatus = status
		}
		if failure != nil {
			evidence.Cause, evidence.Phase = failure.Cause, failure.Phase
		}
		if t.payload == nil {
			return nil
		}
		err := t.payload.writeWindowResponseWithFailure(ticket.sequence, identity, status, body, complete, code, failure)
		evidence.DescriptorPersistence = "failed"
		if err == nil {
			evidence.DescriptorPersistence = "verified"
			if t.diagnostics != nil {
				// Bind the failure descriptor to the retained request bytes too.
				// This diagnostic verification never upgrades a completion proof.
				if _, _, checkErr := verifyWindowPayloadStore(t.payload); checkErr != nil {
					evidence.RequestPersistence = "unknown"
					evidence.DescriptorPersistence = "failed"
					return checkErr
				}
			}
		}
		return err
	}
	var witness *windowTLSRetryWitness
dispatchNative:
	if req.Context().Err() != nil {
		writeResponse(0, nil, false, "round_trip", &windowTransportFailure{Cause: jev.ClassifyTransportFailureCause(req.Context().Err()), Phase: "before_send"})
		return fail("upstream_failure")
	}
	frozen := req.Clone(req.Context())
	frozen.Body = io.NopCloser(bytes.NewReader(raw))
	frozen.GetBody = nil
	frozen.ContentLength = int64(len(raw))
	if v8 {
		ctx, op := observability.Start(frozen.Context(), "window.native", "span")
		nativeOp = op
		frozen = frozen.WithContext(ctx)
		nativeSpan = trace.SpanFromContext(ctx)
		if d, ok := t.capability.(*windowOwnedDriver); ok && d.observer != nil && (nativeOp == nil || !nativeSpan.IsRecording()) {
			return fail("protocol_failure")
		}
		nativeID := windowV8DeclarationSHA(struct {
			Claim    string
			Sequence int
			Request  windowRequestIdentity
		}{t.ledger.contract.ClaimSHA256, ticket.sequence, identity})
		nativeSpan.SetAttributes(attribute.String("langfuse.observation.metadata.native_schema", "full-window-native.v8"), attribute.String("langfuse.observation.metadata.native_id", nativeID), attribute.Int("langfuse.observation.metadata.native_sequence", ticket.sequence), attribute.Int("langfuse.observation.metadata.native_attempt", identity.NativeAttempt), attribute.String("langfuse.observation.metadata.provider", identity.Provider), attribute.String("langfuse.observation.metadata.body_sha256", identity.BodySHA256), attribute.Int("langfuse.observation.metadata.request_bytes", identity.Bytes), attribute.String("langfuse.observation.metadata.native_content_mode", "metadata_only"), attribute.String("langfuse.observation.metadata.claim_sha256", t.ledger.contract.ClaimSHA256), attribute.String("langfuse.observation.metadata.case_key", identity.CaseKey), attribute.Int("langfuse.observation.metadata.pass", identity.Pass), attribute.String("langfuse.observation.metadata.arm", identity.Arm), attribute.Int("langfuse.observation.metadata.turn", identity.Turn), attribute.Int("langfuse.observation.metadata.logical_attempt", identity.Attempt))
	}
	var probe *windowTransportProbe
	if t.payload != nil || t.diagnostics != nil {
		var ctx context.Context
		ctx, probe = newWindowTransportProbe(frozen.Context())
		frozen = frozen.WithContext(ctx)
	}
	if v8 && identity.NativeAttempt == 1 {
		ctx, w := newWindowTLSRetryWitness(frozen.Context(), phase.parent)
		witness = w
		frozen = frozen.WithContext(ctx)
	}
	var response *http.Response
	var upErr error
	if v8 && identity.NativeAttempt == 2 {
		response, upErr = witness.dispatchWindowV8(func() (*http.Response, error) {
			if t.guardWindowV8(req, identity, phase) != nil {
				return nil, fmt.Errorf("v8 pre-dispatch owner changed")
			}
			return t.upstream.RoundTrip(frozen)
		})
	} else {
		if v8 && t.guardWindowV8(req, identity, phase) != nil {
			return fail("upstream_failure")
		}
		response, upErr = t.upstream.RoundTrip(frozen)
	}
	if v8 && identity.NativeAttempt == 1 {
		witness.seal(response, upErr)
	}
	if upErr != nil || response == nil || response.Body == nil {
		status := 0
		if response != nil && response.Body != nil {
			if response.StatusCode >= 100 && response.StatusCode <= 599 {
				status = response.StatusCode
			}
			_ = response.Body.Close()
		}
		if v8 && identity.NativeAttempt == 1 && response == nil && witness.eligible() {
			// Persist the complete native1 failure before ending its active ticket.
			if writeResponse(status, nil, false, "round_trip", probe.snapshot(upErr, "")) != nil {
				return fail("protocol_failure")
			}
			descriptor, err := windowV8FailureDescriptor(t.payload, ticket.sequence, identity)
			if err != nil {
				return fail("protocol_failure")
			}
			d := t.capability.(*windowOwnedDriver)
			if witness.handoffWindowV8(func() { _ = d.stopWindowOwned() }) != nil {
				return fail("upstream_failure")
			}
			nativeSpan.SetAttributes(attribute.String("langfuse.observation.metadata.failure_descriptor_sha256", descriptor))
			endNative("tls_eof_unknown")
			if err = t.ledger.EndUnknown(ticket, descriptor); err != nil {
				return t.stopWindowV8Transition()
			}
			if witness.checkWindowV8() != nil || t.guardWindowV8(req, identity, phase) != nil {
				return t.stopWindowV8Transition()
			}
			receipt, err := d.permit.campaign.claimWindowExtra(windowOwnedRetryContext{driver: d, phase: phase, sequence: ticket.sequence, request: identity, descriptorSHA: descriptor, requestContext: req.Context()})
			if err != nil || receipt.recordFunding() != nil {
				return t.stopWindowV8Transition()
			}
			if witness.checkWindowV8() != nil || t.guardWindowV8(req, identity, phase) != nil {
				return t.stopWindowV8Transition()
			}
			identity.NativeAttempt = 2
			nextTicket, err := t.ledger.Begin(identity)
			if err != nil {
				return t.stopWindowV8Transition()
			}
			ticket = nextTicket
			evidence = windowFailureTransportEvidence{Sequence: ticket.sequence, Provider: t.provider, RequestPersistence: "failed", DescriptorPersistence: "not_attempted", LedgerPersistence: "unknown"}
			if t.payload.writeWindowRequest(ticket.sequence, identity, raw) != nil {
				return fail("protocol_failure")
			}
			evidence.RequestPersistence = "verified"
			goto dispatchNative
		}
		writeResponse(status, nil, false, "round_trip", probe.snapshot(upErr, ""))
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
	if writeResponse(response.StatusCode, body, complete, code, failure) != nil {
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
	if v8 && (t.guardWindowV8(req, identity, phase) != nil || identity.NativeAttempt == 2 && witness.checkWindowV8() != nil) {
		return fail("upstream_failure")
	}
	if e = t.ledger.Settle(ticket, conversationHash(body), usage); e != nil {
		return nil, fmt.Errorf("window usage settlement stopped")
	}
	endNative("settled")
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}
