package taskbench

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowV8HandoffOneShotAndLateCallback(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "one_shot", true: "late_callback"}[late], func(t *testing.T) {
			ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
			tr := httptrace.ContextClientTrace(ctx)
			tlsWitnessEOF(tr)
			w.seal(nil, io.EOF)
			var stopped atomic.Int64
			if err := w.handoffWindowV8(func() { stopped.Add(1) }); err != nil {
				t.Fatalf("live handoff rejected: %v", err)
			}
			if late {
				tr.GotFirstResponseByte()
			}
			var calls int
			send := func() (*http.Response, error) { calls++; return &http.Response{StatusCode: 200}, nil }
			_, err := w.dispatchWindowV8(send)
			if late {
				if err == nil || calls != 0 || stopped.Load() != 1 {
					t.Fatalf("late callback escaped %v %d %d", err, calls, stopped.Load())
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("dispatch %v %d", err, calls)
			}
			if _, err = w.dispatchWindowV8(send); err == nil || calls != 1 {
				t.Fatal("second handoff dispatch")
			}
		})
	}
}

func TestWindowV8SameRoundTripTLSRetry(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			for _, mode := range []string{"direct", "connect"} {
				t.Run(mode, func(t *testing.T) {
					c, r := fixtureWindowV8Campaign(t, provider, false)
					var received atomic.Int64
					server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						received.Add(1)
						raw, err := io.ReadAll(req.Body)
						if err != nil {
							t.Error(err)
						}
						want := windowTransportRequest(t, provider)
						wantRaw, _ := io.ReadAll(want.Body)
						if !bytes.Equal(raw, wantRaw) {
							t.Error("native2 body differs")
						}
						resp := windowTransportResponse(provider)
						io.Copy(w, resp.Body)
					}))
					defer server.Close()
					var dials atomic.Int64
					transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
						if dials.Add(1) == 1 {
							client, peer := net.Pipe()
							go func() {
								defer peer.Close()
								reader := bufio.NewReader(peer)
								if mode == "connect" {
									connect, err := http.ReadRequest(reader)
									if err != nil || connect.Method != "CONNECT" {
										return
									}
									if _, err = io.WriteString(peer, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
										return
									}
								}
								windowTLSProtocolReadClientHello(reader)
							}()
							return client, nil
						}
						return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
					}}
					defer transport.CloseIdleConnections()
					if mode == "connect" {
						proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
							if req.Method != "CONNECT" {
								w.WriteHeader(400)
								return
							}
							conn, _, err := w.(http.Hijacker).Hijack()
							if err != nil {
								return
							}
							defer conn.Close()
							target, err := net.Dial("tcp", server.Listener.Addr().String())
							if err != nil {
								return
							}
							defer target.Close()
							if _, err = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
								return
							}
							go io.Copy(target, conn)
							io.Copy(conn, target)
						}))
						defer proxy.Close()
						proxyURL, _ := url.Parse(proxy.URL)
						transport.Proxy = http.ProxyURL(proxyURL)
						originalDial := transport.DialContext
						transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
							if dials.Load() == 0 {
								return originalDial(ctx, network, address)
							}
							dials.Add(1)
							return (&net.Dialer{}).DialContext(ctx, network, proxyURL.Host)
						}
					}
					up := &windowActualNoRedirectTransport{transport}
					r.driver.permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
					parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					r.phase.parent = parent
					req := windowTransportRequest(t, provider).WithContext(context.WithValue(parent, windowOwnedPhaseKey{}, r.phase))
					tr, err := newWindowProviderTransport(r.driver, provider, up)
					if err != nil {
						t.Fatal(err)
					}
					resp, err := tr.RoundTrip(req)
					if err != nil {
						t.Fatalf("controlled same-RoundTrip retry failed: %v state=%+v", err, c.ledger.Snapshot())
					}
					resp.Body.Close()
					state := c.ledger.Snapshot()
					if received.Load() != 1 || dials.Load() != 2 || len(state.UnknownIntents) != 1 || state.Pending != nil || state.RetryRequired != nil || state.Halt != "" || len(c.extras) != 1 {
						t.Fatalf("logical/native separation: received=%d dials=%d state=%+v extras=%d", received.Load(), dials.Load(), state, len(c.extras))
					}
					summary := c.snapshotWindowV8Runtime(state, c.ledger.contract.WindowID)
					if summary == nil || summary.NativeUnknown != 1 || summary.NativeSettlements != 1 || summary.NativePending != 0 || summary.LogicalModelCalls+summary.LogicalJEVCalls != 1 || summary.NativeModelIntents+summary.NativeJEVIntents != 2 || summary.ModelRetryIntents+summary.JEVRetryIntents != 1 || summary.UnknownHeld.Model+summary.UnknownHeld.JEV == 0 || summary.ExtraPermanent.Model+summary.ExtraPermanent.JEV == 0 {
						t.Fatalf("v8 runtime summary absent or conflated: %+v", summary)
					}
					if r.phase.model+r.phase.jev != 1 {
						t.Fatal("logical callback repeated")
					}
					manifest, _, err := verifyWindowPayloadStore(r.driver.permit.payload)
					if err != nil || len(manifest.Packets) != 2 || manifest.Packets[0].Identity.NativeAttempt != 1 || manifest.Packets[1].Identity.NativeAttempt != 2 || manifest.Schema != "full-window-payload.v2" {
						t.Fatalf("native payloads %v %+v", err, manifest)
					}
					if err = validateWindowAcquisitionPayloadLinkage(c.ledger, c.ledgerDir, "local_stub", manifest); err != nil {
						t.Fatalf("unknown/settled native union rejected: %v", err)
					}
					c.mu.Lock()
					linkErr := c.validateWindowV8ExtrasLocked(state)
					c.mu.Unlock()
					if linkErr != nil {
						t.Fatal(linkErr)
					}
					for _, mutation := range []string{"legacy_schema", "unknown_http200", "missing_failure", "failure_hash", "native2_body", "missing_packet"} {
						t.Run(mutation, func(t *testing.T) {
							raw, _ := json.Marshal(manifest)
							var broken windowPayloadManifest
							json.Unmarshal(raw, &broken)
							switch mutation {
							case "legacy_schema":
								broken.Schema = "full-window-payload.v1"
							case "unknown_http200":
								broken.Packets[0].Response.Status = 200
							case "missing_failure":
								broken.Packets[0].Response = nil
							case "failure_hash":
								broken.Packets[0].FailureDescriptorSHA256 = windowV8BaselineSHA
							case "native2_body":
								broken.Packets[1].Identity.BodySHA256 = windowV8BaselineSHA
							case "missing_packet":
								broken.Packets = broken.Packets[:1]
							}
							if validateWindowAcquisitionPayloadLinkage(c.ledger, c.ledgerDir, "local_stub", broken) == nil {
								t.Fatal("unlinked payload accepted")
							}
						})
					}
					exportWindowV8ControlledBundle(t, c, r.driver.permit.payload, manifest, summary, provider+"-"+mode)
				})
			}
		})
	}
}

func exportWindowV8ControlledBundle(t *testing.T, c *windowCampaign, payload *windowPayloadStore, manifest windowPayloadManifest, summary *windowV8RuntimeSummary, name string) {
	root := os.Getenv("WINDOW_V8_EXPORT_FIXTURES")
	if root == "" {
		return
	}
	files := map[string]string{}
	for name := range payload.files {
		raw, err := os.ReadFile(filepath.Join(payload.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	ledger, err := os.ReadFile(filepath.Join(c.ledgerDir, "accounting.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	extraFiles := map[string]string{}
	for _, extra := range c.extras {
		name := fmt.Sprintf("extra-%06d.json", extra.Index)
		raw, err := os.ReadFile(filepath.Join(c.root, name))
		if err != nil {
			t.Fatal(err)
		}
		extraFiles[name] = string(raw)
	}
	bundle := map[string]any{"evidence_kind": "synthetic_controlled_fixture_not_actual_proof", "payload_manifest": manifest, "runtime_summary": summary, "extra_claims": c.extras, "ledger_utf8": string(ledger), "payload_files_utf8": files, "authorization": json.RawMessage(c.prepared.v8Authority.raw), "authorization_utf8": string(c.prepared.v8Authority.raw), "extra_claim_files_utf8": extraFiles, "public_key_hex": c.prepared.v8Authority.manifest.BuildAttestationPublicKey}
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "native-"+name+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWindowV8SecondEOFRetainsBothLiabilities(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, r := fixtureWindowV8Campaign(t, provider, false)
			var dials atomic.Int64
			tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				client, peer := net.Pipe()
				go func() { defer peer.Close(); windowTLSProtocolReadClientHello(bufio.NewReader(peer)) }()
				return client, nil
			}}
			defer tr.CloseIdleConnections()
			up := &windowActualNoRedirectTransport{tr}
			r.driver.permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
			parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.phase.parent = parent
			req := windowTransportRequest(t, provider).WithContext(context.WithValue(parent, windowOwnedPhaseKey{}, r.phase))
			transport, err := newWindowProviderTransport(r.driver, provider, up)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = transport.RoundTrip(req); err == nil {
				t.Fatal("second EOF returned success")
			}
			state := c.ledger.Snapshot()
			if dials.Load() != 2 || len(c.extras) != 1 || len(state.UnknownIntents) != 1 || state.Pending == nil || state.Pending.Request.NativeAttempt != 2 || state.Halt != "upstream_failure" || state.RetryRequired == nil || state.ModelUnknownHeld+state.JEVUnknownHeld == 0 || state.ModelHeld+state.JEVHeld == 0 {
				t.Fatalf("liabilities lost: dials=%d state=%+v", dials.Load(), state)
			}
			before := dials.Load()
			if _, err = transport.RoundTrip(windowTransportRequest(t, provider)); err == nil || dials.Load() != before {
				t.Fatal("third/cross logical escaped")
			}
			if r.phase.model+r.phase.jev != 1 {
				t.Fatal("logical tool/handler request repeated")
			}
		})
	}
}

func TestWindowV8ChangedControlledTransportRefusesBeforeNativeIntent(t *testing.T) {
	c, r := fixtureWindowV8Campaign(t, "model", false)
	up := &windowActualNoRedirectTransport{&http.Transport{}}
	r.driver.permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.phase.parent = parent
	transport, err := newWindowProviderTransport(r.driver, "model", up)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	up.base = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, fmt.Errorf("synthetic changed transport; no network")
	}}
	req := windowTransportRequest(t, "model").WithContext(context.WithValue(parent, windowOwnedPhaseKey{}, r.phase))
	if _, err = transport.RoundTrip(req); err == nil || calls != 0 || c.ledger.Snapshot().ModelAttempts != 0 {
		t.Fatal("changed owned concrete transport escaped", err, calls, c.ledger.Snapshot())
	}
}

func TestWindowV8UnqualifiedProviderFailuresNeverRetry(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		for _, mode := range []string{"http_status", "body_read", "certificate", "canceled", "foreign_roundtripper"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				c, r := fixtureWindowV8Campaign(t, provider, false)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if mode == "http_status" {
						w.WriteHeader(503)
						io.WriteString(w, "synthetic failure")
						return
					}
					if mode == "body_read" {
						w.Header().Set("Content-Length", "10000")
						io.WriteString(w, "{")
						return
					}
					response := windowTransportResponse(provider)
					defer response.Body.Close()
					io.Copy(w, response.Body)
				}))
				server.Config.ErrorLog = log.New(io.Discard, "", 0)
				server.StartTLS()
				defer server.Close()
				var calls atomic.Int64
				base := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: mode != "certificate"}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					calls.Add(1)
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				}}
				defer base.CloseIdleConnections()
				up := &windowActualNoRedirectTransport{base}
				r.driver.permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
				parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				r.phase.parent = parent
				var upstream http.RoundTripper = up
				if mode == "foreign_roundtripper" {
					upstream = windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, io.EOF })
				}
				transport, err := newWindowProviderTransport(r.driver, provider, upstream)
				if mode == "foreign_roundtripper" {
					if err == nil {
						t.Fatal("arbitrary RoundTripper accepted for V8")
					}
					if calls.Load() != 0 {
						t.Fatal("foreign call")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode == "canceled" {
					cancel()
				}
				req := windowTransportRequest(t, provider).WithContext(context.WithValue(parent, windowOwnedPhaseKey{}, r.phase))
				if _, err = transport.RoundTrip(req); err == nil {
					t.Fatal("provider failure returned success")
				}
				state := c.ledger.Snapshot()
				want := int64(1)
				if mode == "canceled" {
					want = 0
				}
				if calls.Load() != want || len(state.UnknownIntents) != 0 || len(c.extras) != 0 || state.Halt == "" {
					t.Fatal("unqualified failure retried", calls.Load(), state)
				}
			})
		}
	}
}
func TestWindowV8HandoffCancellationAndReentrantCallbacks(t *testing.T) {
	for _, mode := range []string{"before_handoff", "after_handoff", "reentrant_late"} {
		t.Run(mode, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, w := newWindowTLSRetryWitness(parent, parent)
			tr := httptrace.ContextClientTrace(ctx)
			tlsWitnessEOF(tr)
			w.seal(nil, io.EOF)
			var stopped atomic.Int64
			if mode == "before_handoff" {
				cancel()
				if w.handoffWindowV8(func() { stopped.Add(1) }) == nil {
					t.Fatal("canceled handoff")
				}
				return
			}
			if err := w.handoffWindowV8(func() { stopped.Add(1) }); err != nil {
				t.Fatal(err)
			}
			if mode == "after_handoff" {
				cancel()
			}
			calls := 0
			_, err := w.dispatchWindowV8(func() (*http.Response, error) {
				calls++
				tr.GotFirstResponseByte()
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})
			if err == nil {
				t.Fatal("invalidated handoff returned success")
			}
			want := 0
			if mode == "reentrant_late" {
				want = 1
				if stopped.Load() != 1 {
					t.Fatal("late callback failed to stop")
				}
			}
			if calls != want {
				t.Fatal("canceled/reentrant dispatch", calls, want)
			}
		})
	}
}
