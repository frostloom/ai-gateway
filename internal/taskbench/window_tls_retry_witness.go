package taskbench

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"sync"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// windowTLSRetrySnapshot is diagnostic data only. It carries neither an
// eligibility decision nor a capability, and cannot be loaded into a witness.
// Counts do not establish supplier processing, billing or native HTTP/2 counts.
type windowTLSRetrySnapshot struct {
	TLSStarts, TLSDone, TLSFailedEOF, TLSSuccess                   uint64
	GotConn, WroteHeaders, WroteHeaderFields, WroteRequests        uint64
	FirstBytes, ContinueResponses, OtherResponses, IdleConnections uint64
	Wait100Continues, OtherEvents, SealCalls, LateEvents           uint64
	Invalid                                                        bool
}

// windowTLSRetryWitness is isolated preparation with no runtime caller. The
// live object must be checked after sealing; a copied snapshot is no authority.
// It performs no retry, accounting, network operation or capability issuance.
type windowTLSRetryWitness struct {
	mu                                sync.Mutex
	request, parent                   context.Context
	state                             windowTLSRetrySnapshot
	outcomeEOF                        bool
	runtimeStop                       func()
	runtimeHandoff, runtimeDispatched bool
}

// Keep both original contexts. WithClientTrace adds callbacks to the existing
// request context without changing its cancellation, deadline or prior trace.
// Missing contexts reject construction with a nil witness; no context is replaced.
func newWindowTLSRetryWitness(request, parent context.Context) (context.Context, *windowTLSRetryWitness) {
	if request == nil || parent == nil {
		return request, nil
	}
	w := &windowTLSRetryWitness{request: request, parent: parent}
	other := func() { w.event(func(s *windowTLSRetrySnapshot) { s.OtherEvents++ }) }
	tr := &httptrace.ClientTrace{
		TLSHandshakeStart: func() {
			w.event(func(s *windowTLSRetrySnapshot) {
				s.TLSStarts++
				if s.TLSStarts != 1 || s.TLSDone != 0 {
					s.Invalid = true
				}
			})
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			w.event(func(s *windowTLSRetrySnapshot) {
				s.TLSDone++
				if err == nil || state.HandshakeComplete {
					s.TLSSuccess++
				}
				if windowTLSRetryPureEOF(err) {
					s.TLSFailedEOF++
				}
				if s.TLSStarts != 1 || s.TLSDone != 1 {
					s.Invalid = true
				}
			})
		},
		GotConn:              func(httptrace.GotConnInfo) { w.event(func(s *windowTLSRetrySnapshot) { s.GotConn++ }) },
		WroteHeaders:         func() { w.event(func(s *windowTLSRetrySnapshot) { s.WroteHeaders++ }) },
		WroteHeaderField:     func(string, []string) { w.event(func(s *windowTLSRetrySnapshot) { s.WroteHeaderFields++ }) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { w.event(func(s *windowTLSRetrySnapshot) { s.WroteRequests++ }) },
		GotFirstResponseByte: func() { w.event(func(s *windowTLSRetrySnapshot) { s.FirstBytes++ }) },
		Got100Continue:       func() { w.event(func(s *windowTLSRetrySnapshot) { s.ContinueResponses++ }) },
		Got1xxResponse: func(int, textproto.MIMEHeader) error {
			w.event(func(s *windowTLSRetrySnapshot) { s.OtherResponses++ })
			return nil
		},
		PutIdleConn:     func(error) { w.event(func(s *windowTLSRetrySnapshot) { s.IdleConnections++ }) },
		GetConn:         func(string) { other() },
		DNSStart:        func(httptrace.DNSStartInfo) { other() },
		DNSDone:         func(httptrace.DNSDoneInfo) { other() },
		ConnectStart:    func(string, string) { other() },
		ConnectDone:     func(string, string, error) { other() },
		Wait100Continue: func() { w.event(func(s *windowTLSRetrySnapshot) { s.Wait100Continues++ }) },
	}
	return httptrace.WithClientTrace(request, tr), w
}

func (w *windowTLSRetryWitness) event(update func(*windowTLSRetrySnapshot)) {
	w.mu.Lock()
	if w.state.SealCalls != 0 {
		w.state.LateEvents++
		w.state.Invalid = true
	}
	update(&w.state)
	stop := w.runtimeStop
	late := w.state.LateEvents != 0
	w.mu.Unlock()
	if late && stop != nil {
		stop()
	}
}

// Seal only a controlled RoundTrip result, once. Repeated sealing permanently
// invalidates the witness and never overwrites the original result.
func (w *windowTLSRetryWitness) seal(response *http.Response, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.SealCalls++
	if w.state.SealCalls != 1 {
		w.state.Invalid = true
		return
	}
	w.outcomeEOF = response == nil && windowTLSRetryPureEOF(err)
	// Cancellation at seal cannot be undone by any later observation.
	if w.request.Err() != nil || w.parent.Err() != nil {
		w.state.Invalid = true
	}
}

func (w *windowTLSRetryWitness) eligible() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.eligibleWindowLocked()
}
func (w *windowTLSRetryWitness) eligibleWindowLocked() bool {
	s := w.state
	return s.SealCalls == 1 && !s.Invalid && s.LateEvents == 0 && w.outcomeEOF &&
		s.TLSStarts == 1 && s.TLSDone == 1 && s.TLSFailedEOF == 1 && s.TLSSuccess == 0 &&
		s.GotConn == 0 && s.WroteHeaders == 0 && s.WroteHeaderFields == 0 && s.WroteRequests == 0 &&
		s.FirstBytes == 0 && s.ContinueResponses == 0 && s.OtherResponses == 0 && s.IdleConnections == 0 && s.Wait100Continues == 0 &&
		w.request.Err() == nil && w.parent.Err() == nil
}

func (w *windowTLSRetryWitness) snapshot() windowTLSRetrySnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

// errors.Is(EOF) alone also accepts joined errors and arbitrary Is methods.
// Accept only a bounded single unwrap chain ending at the EOF sentinel, then
// apply the existing typed classifier to exclude timeout/certificate wrappers.
// Multi-error trees, cycles and opaque EOF claims are conservatively rejected.
func windowTLSRetryPureEOF(err error) bool {
	original := err
	for depth := 0; err != nil && depth < 64; depth++ {
		if err == io.EOF {
			return jev.ClassifyTransportFailureCause(original) == "eof"
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
