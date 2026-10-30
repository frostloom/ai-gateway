package taskbench

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Test-only ownership and synthetic signing. It never calls readiness, a real
// claim constructor, fixtures/DB, an owner loop or any supplier endpoint.
func fixtureWindowV9Native(t *testing.T, provider string) (*windowCampaign, windowOwnedRetryContext) {
	t.Helper()
	a, key := fixtureWindowV9Authority(t)
	parent := filepath.Dir(a.authorizationPath)
	root := filepath.Join(parent, "synthetic-native-v9")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	scope := a.manifest.Scope
	raw, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	sha := conversationHash(raw)
	limits := windowV4ExpectedLimits()
	execution, err := executionSHAWindowCampaign(sha, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	a.manifest.ScopeSHA256, a.manifest.ExecutionSHA256 = sha, execution
	a.manifest.CampaignRoot = root
	a.binding.ScopeSHA256, a.binding.ExecutionSHA256 = sha, execution
	canonical, err := json.Marshal(a.binding)
	if err != nil {
		t.Fatal(err)
	}
	a.raw, err = json.Marshal(windowV9ExecutionAuthorization{a.binding, hex.EncodeToString(ed25519.Sign(key, canonical))})
	if err != nil {
		t.Fatal(err)
	}
	a.authorizationSHA = conversationHash(a.raw)
	if err = os.WriteFile(a.authorizationPath, a.raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.revalidate(); err != nil {
		t.Fatal("synthetic V9 re-signing setup", err)
	}
	contract := contractWindowCampaign(scope, sha, scope.Windows[0], limits)
	c := &windowCampaign{root: root, rootInfo: info, lock: lock, prepared: windowCampaignPrepared{root: root, scopeSHA: sha, executionSHA: execution, scope: scope, limits: limits, contracts: []windowAccountingContract{contract}, v8Authority: a.nativeView()}, baseline: windowV9CandidateBaseline(expectedStoppedV8WindowBaseline()), index: 1, claimSHA: sha, model: 52654504 + scope.Windows[0].ModelCap, jev: 408701 + scope.Windows[0].JEVCap, files: map[string]string{}, ledgerAudits: map[string]string{}}
	c.ledgerDir = filepath.Join(root, "ledger")
	c.ledger, err = newWindowAccounting(c.ledgerDir, contract)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.ledger.Close() })
	env := &PairedFixtureEnvironment{}
	permit := &windowAcquisitionPermit{campaign: c, ledger: c.ledger, executionSHA: execution, windowIndex: 1, environment: env, evidenceKind: "local_stub"}
	slot := scope.Slots[0]
	d := &windowOwnedDriver{permit: permit, ledger: c.ledger, environment: env, slot: slot, arm: slot.ArmOrder[0], setupDone: true}
	if provider == "jev" {
		d.arm = "jev_selective"
		permit.nextArm = 1
		permit.proofs = make([]*windowOwnedArmProof, 1)
		permit.proofSHA = []string{"synthetic-prior-arm-placeholder"}
	}
	phase := &windowOwnedPhase{driver: d, nonce: "synthetic-v9-native-phase", parent: context.Background()}
	d.phase = phase
	permit.active = d
	payload, err := newWindowPayloadStore(filepath.Join(parent, "synthetic-native-payload"), root, contract, "local_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { payload.Close() })
	permit.payload = payload
	return c, windowOwnedRetryContext{driver: d, phase: phase}
}

func checkWindowV9NativeBase(t *testing.T, c *windowCampaign) {
	t.Helper()
	w := c.prepared.scope.Windows[0]
	if c.prepared.scope.Schema != "full-window-scope.v9" || w.LastOrdinal-w.FirstOrdinal+1 > 12 || c.prepared.scope.RetryDeclaration.MaxModelExtras != 20 || c.prepared.scope.RetryDeclaration.MaxJEVExtras != 20 || c.baseline.PermanentModel != 52654504 || c.baseline.PermanentJEV != 408701 || c.baseline.TotalModel != 180000000 || c.baseline.TotalJEV != 2500000 || c.ledger.contract.Schema != windowAccountingV3 {
		t.Fatal("V9 native fixture did not retain typed46/caps/native bounds")
	}
}

// Real http.Transport, TLS handshake/EOF and HTTP Handler on local sockets.
// DialContext always uses the explicit loopback listener, never supplier DNS.
func runWindowV9NativeLoopback(t *testing.T, provider, mode string) (*windowCampaign, windowOwnedRetryContext, *windowProviderTransport, context.Context, int64, int64, error) {
	t.Helper()
	c, r := fixtureWindowV9Native(t, provider)
	checkWindowV9NativeBase(t, c)
	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	r.phase.parent = parent
	var received, dials atomic.Int64
	want := windowTransportRequest(t, provider)
	wantBody, err := io.ReadAll(want.Body)
	if err != nil {
		t.Fatal(err)
	}
	want.Body.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received.Add(1)
		body, e := io.ReadAll(req.Body)
		if e != nil {
			t.Error(e)
		}
		if req.Method != "POST" || !bytes.Equal(body, wantBody) || req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != "" {
			t.Error("native HTTP method/body/headers changed")
		}
		if mode == "written-eof" {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			conn.Close()
			return
		}
		if mode == "missing-usage" {
			fmt.Fprint(w, `{"model":"`+map[string]string{"model": "deepseek-flash", "jev": "jev-1.13.0"}[provider]+`"}`)
			return
		}
		response := windowTransportResponse(provider)
		defer response.Body.Close()
		io.Copy(w, response.Body)
	}))
	t.Cleanup(server.Close)
	base := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if dials.Add(1) == 1 && (mode == "eligible-eof" || mode == "canceled-handshake") {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				windowTLSProtocolReadClientHello(bufio.NewReader(peer))
				if mode == "canceled-handshake" {
					cancel()
				}
			}()
			return client, nil
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	t.Cleanup(base.CloseIdleConnections)
	up := &windowActualNoRedirectTransport{base}
	r.driver.permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
	tr, err := newWindowProviderTransport(r.driver, provider, up)
	if err != nil {
		t.Fatal("V9 production transport constructor", err)
	}
	req := windowTransportRequest(t, provider).WithContext(context.WithValue(parent, windowOwnedPhaseKey{}, r.phase))
	resp, callErr := tr.RoundTrip(req)
	if resp != nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
	}
	return c, r, tr, parent, received.Load(), dials.Load(), callErr
}

func TestWindowV9NativeNormalLoopback(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, r, _, parent, received, dials, err := runWindowV9NativeLoopback(t, provider, "normal")
			s := c.ledger.Snapshot()
			if err != nil || parent.Err() != nil || received != 1 || dials != 1 || s.ModelAttempts+s.JEVAttempts != 1 || s.Pending != nil || s.Halt != "" || len(s.UnknownIntents) != 0 || len(c.extras) != 0 || r.phase.model+r.phase.jev != 1 {
				t.Fatalf("V9 normal native: recv%d/dial%d state%+v err%v", received, dials, s, err)
			}
			summary := c.snapshotWindowV8Runtime(s, c.ledger.contract.WindowID)
			if summary == nil || summary.NativeSettlements != 1 || summary.HistoricalPermanent != (windowV8Amounts{52654504, 408701}) || summary.ExtraPermanent != (windowV8Amounts{}) {
				t.Fatalf("V9 native summary %+v", summary)
			}
		})
	}
}
func TestWindowV9NativeEligibleEOFOnceLoopback(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, r, _, parent, received, dials, err := runWindowV9NativeLoopback(t, provider, "eligible-eof")
			s := c.ledger.Snapshot()
			if err != nil || parent.Err() != nil || received != 1 || dials != 2 || s.ModelAttempts+s.JEVAttempts != 2 || len(s.UnknownIntents) != 1 || s.Pending != nil || s.RetryRequired != nil || s.Halt != "" || len(c.extras) != 1 || r.phase.model+r.phase.jev != 1 {
				t.Fatalf("V9 one EOF retry: recv%d/dial%d state%+v extras%d err%v", received, dials, s, len(c.extras), err)
			}
			summary := c.snapshotWindowV8Runtime(s, c.ledger.contract.WindowID)
			if summary == nil || summary.NativeUnknown != 1 || summary.NativeSettlements != 1 || summary.UnknownHeld.Model+summary.UnknownHeld.JEV == 0 || summary.ExtraPermanent.Model+summary.ExtraPermanent.JEV == 0 || summary.LogicalModelCalls+summary.LogicalJEVCalls != 1 {
				t.Fatalf("unknown liability lost: %+v", summary)
			}
			manifest, _, err := verifyWindowPayloadStore(r.driver.permit.payload)
			if err != nil || len(manifest.Packets) != 2 {
				t.Fatalf("two native packets: %+v %v", manifest, err)
			}
			first, second := manifest.Packets[0].Identity, manifest.Packets[1].Identity
			if first.NativeAttempt != 1 || second.NativeAttempt != 2 || first.BodySHA256 != second.BodySHA256 || first.Bytes != second.Bytes || first.Attempt != second.Attempt || first.Turn != second.Turn || first.CaseKey != second.CaseKey {
				t.Fatal("retry changed original logical/body binding")
			}
			if err = validateWindowAcquisitionPayloadLinkage(c.ledger, c.ledgerDir, "local_stub", manifest); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestWindowV9NativeWrittenEOFCannotRetry(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, r, tr, _, received, dials, err := runWindowV9NativeLoopback(t, provider, "written-eof")
			s := c.ledger.Snapshot()
			if err == nil || received != 1 || dials != 1 || s.ModelAttempts+s.JEVAttempts != 1 || s.Halt == "" || len(s.UnknownIntents) != 0 || len(c.extras) != 0 {
				t.Fatalf("written EOF retried: recv%d/dial%d state%+v err%v", received, dials, s, err)
			}
			req := windowTransportRequest(t, provider).WithContext(context.WithValue(r.phase.parent, windowOwnedPhaseKey{}, r.phase))
			if resp, e := tr.RoundTrip(req); e == nil || resp != nil {
				t.Fatal("halted logical operation restarted")
			}
		})
	}
}
func TestWindowV9NativeMissingUsageStops(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, _, _, _, received, dials, err := runWindowV9NativeLoopback(t, provider, "missing-usage")
			s := c.ledger.Snapshot()
			if err == nil || received != 1 || dials != 1 || s.Halt == "" || s.Pending == nil || s.ModelHeld+s.JEVHeld == 0 || s.ModelKnown+s.JEVKnown != 0 || len(c.extras) != 0 {
				t.Fatalf("missing usage settled/retried: %+v %v", s, err)
			}
		})
	}
}
func TestWindowV9NativeCancelDuringHandshakeNoSupplement(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, _, _, parent, received, dials, err := runWindowV9NativeLoopback(t, provider, "canceled-handshake")
			s := c.ledger.Snapshot()
			if err == nil || parent.Err() == nil || received != 0 || dials != 1 || s.ModelAttempts+s.JEVAttempts != 1 || s.Halt == "" || len(c.extras) != 0 || len(s.UnknownIntents) != 0 {
				t.Fatalf("canceled request got extra: recv%d/dial%d state%+v err%v", received, dials, s, err)
			}
		})
	}
}
