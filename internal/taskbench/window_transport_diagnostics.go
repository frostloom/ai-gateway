package taskbench

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// These are observations, not proof of delivery, processing or a bill. No
// callback arguments, addresses, headers or raw errors are retained.
type windowTransportFailure struct {
	Cause            string `json:"cause"`
	Phase            string `json:"phase"`
	ElapsedMS        int64  `json:"elapsed_ms"`
	TraceObserved    bool   `json:"trace_observed"`
	ConnectionReused bool   `json:"connection_reused"`
	RequestWritten   bool   `json:"request_written"`
	FirstByte        bool   `json:"first_byte"`
}

func (d windowTransportFailure) valid() bool {
	return d.ElapsedMS >= 0 && hasString([]string{"unknown", "canceled", "timeout", "tls_certificate", "dns", "connection_refused", "connection_reset", "broken_pipe", "unexpected_eof", "eof"}, d.Cause) && windowTransportPhaseRank(d.Phase) >= 0 && (d.TraceObserved || !(d.ConnectionReused || d.RequestWritten || d.FirstByte)) && (d.Phase != "before_send" || !d.TraceObserved)
}

func windowTransportPhaseRank(phase string) int {
	switch phase {
	case "before_send", "round_trip":
		return 0
	case "get_connection":
		return 1
	case "connect":
		return 2
	case "connected":
		return 3
	case "tls_handshake":
		return 4
	case "tls_ready":
		return 5
	case "connection_ready":
		return 6
	case "write_request":
		return 7
	case "await_response":
		return 8
	case "read_response":
		return 9
	case "response_read":
		return 10
	case "response_close":
		return 11
	default:
		return -1
	}
}

type windowTransportProbe struct {
	mu      sync.Mutex
	started time.Time
	state   windowTransportFailure
}

// Parallel dial callbacks can arrive late. Preserve the furthest observed
// phase instead of allowing a losing connection to rewind a written request.
func (p *windowTransportProbe) markLocked(phase string) {
	p.state.TraceObserved = true
	if windowTransportPhaseRank(phase) >= windowTransportPhaseRank(p.state.Phase) {
		p.state.Phase = phase
	}
}

func newWindowTransportProbe(ctx context.Context) (context.Context, *windowTransportProbe) {
	p := &windowTransportProbe{started: time.Now(), state: windowTransportFailure{Phase: "round_trip"}}
	mark := func(phase string) { p.mu.Lock(); p.markLocked(phase); p.mu.Unlock() }
	trace := &httptrace.ClientTrace{
		GetConn:      func(string) { mark("get_connection") },
		ConnectStart: func(string, string) { mark("connect") },
		ConnectDone: func(_, _ string, err error) {
			if err == nil {
				mark("connected")
			} else {
				mark("connect")
			}
		},
		TLSHandshakeStart: func() { mark("tls_handshake") },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				mark("tls_ready")
			} else {
				mark("tls_handshake")
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			p.mu.Lock()
			if windowTransportPhaseRank(p.state.Phase) <= windowTransportPhaseRank("connection_ready") {
				p.state.ConnectionReused = info.Reused
			}
			p.markLocked("connection_ready")
			p.mu.Unlock()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			p.mu.Lock()
			p.markLocked("write_request")
			if info.Err == nil {
				p.markLocked("await_response")
				p.state.RequestWritten = true
			}
			p.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			p.mu.Lock()
			p.markLocked("read_response")
			p.state.FirstByte = true
			p.mu.Unlock()
		},
	}
	return httptrace.WithClientTrace(ctx, trace), p
}

func (p *windowTransportProbe) snapshot(err error, phase string) *windowTransportFailure {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	d := p.state
	p.mu.Unlock()
	d.Cause = jev.ClassifyTransportFailureCause(err)
	d.ElapsedMS = time.Since(p.started).Milliseconds()
	if phase != "" {
		d.Phase = phase
	}
	return &d
}
