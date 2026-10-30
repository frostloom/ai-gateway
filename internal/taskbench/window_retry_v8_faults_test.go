package taskbench

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type windowV8JournalFault struct {
	windowJournalFile
	stage  string
	failed bool
}

func (f *windowV8JournalFault) Write(raw []byte) (int, error) {
	var e windowAccountingEvent
	if json.Unmarshal(raw, &e) == nil {
		if e.Kind == f.stage || f.stage == "intent2" && e.Kind == "intent" && e.Request != nil && e.Request.NativeAttempt == 2 {
			f.failed = true
		}
	}
	return f.windowJournalFile.Write(raw)
}
func (f *windowV8JournalFault) Sync() error {
	if f.failed {
		return fmt.Errorf("synthetic v8 sync fault")
	}
	return f.windowJournalFile.Sync()
}

func TestWindowV8RetryDurabilityStopsAtEachBoundary(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		for _, stage := range []string{"request1", "descriptor1", "unknown", "extra", "extra_funding", "intent2", "request2", "response2", "settle"} {
			t.Run(provider+"/"+stage, func(t *testing.T) {
				c, r := fixtureWindowV8Campaign(t, provider, false)
				if hasString([]string{"unknown", "extra_funding", "intent2", "settle"}, stage) {
					c.ledger.file = &windowV8JournalFault{windowJournalFile: c.ledger.file, stage: stage}
				}
				if stage == "extra" {
					c.openFile = func(path string) (windowJournalFile, error) {
						f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
						if err != nil {
							return nil, err
						}
						return &failingWindowFile{windowJournalFile: f, syncFailure: true}, nil
					}
				}
				r.driver.permit.payload.fileWrap = func(f *os.File) windowPayloadFile {
					bad := (stage == "request1" && filepath.Base(f.Name()) == "request-000001.json") || (stage == "descriptor1" && filepath.Base(f.Name()) == "response-000001.json") || (stage == "request2" && filepath.Base(f.Name()) == "request-000002.json") || (stage == "response2" && filepath.Base(f.Name()) == "response-000002.json")
					return &failingWindowFile{windowJournalFile: f, syncFailure: bad}
				}
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					response := windowTransportResponse(provider)
					defer response.Body.Close()
					io.Copy(w, response.Body)
				}))
				defer server.Close()
				var dials atomic.Int64
				tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					if dials.Add(1) == 1 {
						client, peer := net.Pipe()
						go func() { defer peer.Close(); windowTLSProtocolReadClientHello(bufio.NewReader(peer)) }()
						return client, nil
					}
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				}}
				defer tr.CloseIdleConnections()
				up := &windowActualNoRedirectTransport{tr}
				r.driver.permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				r.phase.parent = ctx
				transport, err := newWindowProviderTransport(r.driver, provider, up)
				if err != nil {
					t.Fatal(err)
				}
				request := windowTransportRequest(t, provider).WithContext(context.WithValue(ctx, windowOwnedPhaseKey{}, r.phase))
				if _, err = transport.RoundTrip(request); err == nil {
					t.Fatal("durability failure returned success")
				}
				state := c.ledger.Snapshot()
				unknown := 1
				extras := 1
				wantDials := int64(1)
				if hasString([]string{"request1", "descriptor1", "unknown"}, stage) {
					unknown = 0
					extras = 0
				}
				if stage == "request1" {
					wantDials = 0
				}
				if stage == "response2" || stage == "settle" {
					wantDials = 2
				}
				if len(state.UnknownIntents) != unknown || len(c.extras) != extras || dials.Load() != wantDials || state.ModelKnown+state.JEVKnown != 0 {
					t.Fatalf("wrong retained transition at %s: dials=%d extras=%d state=%+v", stage, dials.Load(), len(c.extras), state)
				}
				if unknown == 1 && state.ModelUnknownHeld+state.JEVUnknownHeld == 0 {
					t.Fatal("first maximum liability erased")
				}
				if extras == 1 && c.modelExtras+c.jevExtras != 1 {
					t.Fatal("extra quota reclaimed")
				}
				before := dials.Load()
				if _, err = transport.RoundTrip(windowTransportRequest(t, provider).WithContext(request.Context())); err == nil || dials.Load() != before {
					t.Fatal("another send after durability fault")
				}
				if strings.Contains(err.Error(), "synthetic-v8-secret") {
					t.Fatal("error leak")
				}
			})
		}
	}
}
