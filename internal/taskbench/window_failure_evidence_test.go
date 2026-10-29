package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/observability"
	"github.com/redis/go-redis/v9"
)

type failureEvidenceOracle struct {
	handlerOracle
	reads int
}

type failureCleanupHook struct {
	d        *windowOwnedDriver
	commands []string
	fail     string
	t        *testing.T
}

func (h *failureCleanupHook) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) { return nil, fmt.Errorf("network forbidden") }
}
func (h *failureCleanupHook) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error { return fmt.Errorf("pipeline forbidden") }
}
func (h *failureCleanupHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		if !h.d.permit.stopped || h.d.phase != nil {
			h.t.Error("cleanup before execution revocation")
		}
		if len(cmd.Args()) != 2 || cmd.Args()[1] != "agent:session:controlled-failed-session" {
			h.t.Error("cleanup escaped owned session")
		}
		h.commands = append(h.commands, cmd.Name())
		if cmd.Name() == h.fail {
			return fmt.Errorf("synthetic secret must not escape")
		}
		switch c := cmd.(type) {
		case *redis.IntCmd:
			if cmd.Name() == "del" {
				c.SetVal(1)
			} else if cmd.Name() == "exists" {
				c.SetVal(0)
			} else {
				return fmt.Errorf("unexpected command")
			}
		default:
			return fmt.Errorf("unexpected command")
		}
		return nil
	}
}
func TestOwnedFailureEvidenceCleanupFailureAndAbsenceRemainDistinct(t *testing.T) {
	for _, fault := range []string{"del", "exists", "none", "foreign_owner"} {
		t.Run(fault, func(t *testing.T) {
			d, _, _ := failureEvidenceDriver(t, true)
			record := recordCollectionConversation(context.Background(), d.caseDefinition, d)
			hook := &failureCleanupHook{d: d, fail: fault, t: t}
			client := redis.NewClient(&redis.Options{Addr: "forbidden.invalid:0", MaxRetries: -1})
			client.AddHook(hook)
			t.Cleanup(func() { _ = client.Close() })
			d.environment.rdb = client
			if fault == "foreign_owner" {
				d.permit.active = &windowOwnedDriver{}
			}
			proof, err := d.finishWindowOwnedArm(context.Background(), record)
			if err == nil || proof != nil || !d.finished || !d.permit.stopped || d.permit.nextArm != 0 {
				t.Fatal("cleanup relaxed failed arm")
			}
			want := "failed"
			if fault == "none" {
				want = "verified"
			}
			if fault == "foreign_owner" {
				want = "unknown"
				if len(hook.commands) != 0 {
					t.Fatal("foreign session touched")
				}
			}
			if d.failure.evidence.Cleanup != want {
				t.Fatalf("cleanup result discarded: %+v", d.failure.evidence)
			}
			if fault != "foreign_owner" {
				count := len(hook.commands)
				hook.fail = "none"
				_ = d.cleanupFailureLocked(context.Background())
				if len(hook.commands) != count || d.failure.evidence.Cleanup != want {
					t.Fatal("cleanup repeated or laundered original failure")
				}
			}
			raw, _ := json.Marshal(d.failure.evidence)
			if strings.Contains(string(raw), "synthetic secret") || strings.Contains(err.Error(), "synthetic secret") {
				t.Fatal("raw cleanup error leaked")
			}
		})
	}
}
func TestOwnedFailureEvidenceObserverIdentityCannotLookComplete(t *testing.T) {
	d, _, _ := failureEvidenceDriver(t, true)
	RunConversation(context.Background(), d.caseDefinition, d)
	// An uninitialized observer cannot supply Stats. Identity must reject it
	// before dereferencing counters or asserting completion.
	d.observer = &observability.Observer{}
	d.permit.observerIdentity.Enabled = true
	e := d.captureFailureEvidenceLocked()
	if e.Observer != "incomplete" {
		t.Fatalf("invalid observer lost: %+v", e)
	}
}

func (o *failureEvidenceOracle) State(ctx context.Context, f Fixture, session string) (ConversationState, error) {
	o.reads++
	return o.handlerOracle.State(ctx, f, session)
}

// The Handler and oracle are local doubles. The owner, ledger, runner, Step,
// Final, finish and permit guards are real, with no DB or listener.
func failureEvidenceDriver(t *testing.T, enabled bool) (*windowOwnedDriver, *failureEvidenceOracle, *int) {
	t.Helper()
	prepared, baseline := fixtureWindowCampaign(t)
	campaign, err := startWindowCampaign(prepared, baseline)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = campaign.closeWindowCampaign() })
	ledger, err := campaign.openWindowCampaignLedger()
	if err != nil {
		t.Fatal(err)
	}
	env := &PairedFixtureEnvironment{}
	p := &windowAcquisitionPermit{captureFailureEvidence: enabled, campaign: campaign, ledger: ledger, executionSHA: prepared.executionSHA, windowIndex: campaign.index, environment: env}
	f, state := conversationFixture()
	f.APIKey = "synthetic-only"
	oracle := &failureEvidenceOracle{handlerOracle: handlerOracle{f: f, state: state}}
	calls := new(int)
	base, err := NewHandlerConversationDriver(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.Header.Get("Authorization") != "Bearer synthetic-only" {
			t.Error("fixture identity lost")
		}
		w.WriteHeader(500)
		fmt.Fprint(w, `{"session_id":"controlled-failed-session","error":"synthetic failure"}`)
	}), oracle)
	if err != nil {
		t.Fatal(err)
	}
	d := &windowOwnedDriver{permit: p, ledger: ledger, environment: env, base: base, caseDefinition: rechargeConversation(), slot: prepared.scope.Slots[0], arm: prepared.scope.Slots[0].ArmOrder[0]}
	p.active = d
	return d, oracle, calls
}
func TestOwnedFailureEvidenceDoesNotCallPossiblyWritingFinal(t *testing.T) {
	d, oracle, calls := failureEvidenceDriver(t, true)
	result := RunConversation(context.Background(), d.caseDefinition, d)
	if result.Status != "failed" || result.Success || len(result.Observations) != 1 || *calls != 1 || oracle.reads != 1 {
		t.Fatalf("failure changed: %+v calls=%d reads=%d", result, *calls, oracle.reads)
	}
	// Even supplying the matching controlled session must not call State after a
	// stop: production MeOverview can repair the balance projection.
	_, err := d.Final(context.Background(), d.fixture, d.session)
	if err == nil || oracle.reads != 1 {
		t.Fatalf("terminal oracle may write: err=%v reads=%d", err, oracle.reads)
	}
	e := d.failure.evidence
	if e.Final != "unknown" || e.FinalReason != "oracle_may_write" || e.SessionSHA256 != conversationHash([]byte("controlled-failed-session")) {
		t.Fatalf("terminal reason/session lost: %+v", e)
	}
	if _, err = d.Step(context.Background(), d.fixture, d.session, "确认"); err == nil || *calls != 1 || !d.permit.stopped || d.permit.nextArm != 0 {
		t.Fatal("stop was relaxed")
	}
}
func TestOwnedFailureEvidenceCleanupErrorCannotMintProof(t *testing.T) {
	d, _, _ := failureEvidenceDriver(t, true)
	record := recordCollectionConversation(context.Background(), d.caseDefinition, d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proof, err := d.finishWindowOwnedArm(ctx, record)
	if err == nil || proof != nil || d.permit.nextArm != 0 || len(d.permit.proofs) != 0 || !d.permit.stopped {
		t.Fatal("failure minted proof")
	}
	if d.failure.evidence.Cleanup != "unknown" {
		t.Fatalf("unavailable cleanup result discarded: %+v", d.failure.evidence)
	}
	if d.failure.evidence.Observer != "unknown" {
		t.Fatal("missing observer not unknown")
	}
}
func TestOwnedFailureEvidenceDefaultOffPreservesLegacyJSON(t *testing.T) {
	d, _, _ := failureEvidenceDriver(t, false)
	r := RunConversation(context.Background(), d.caseDefinition, d)
	if r.Success || !strings.Contains(strings.Join(r.Errors, " "), "final oracle: owned Handler arm stopped") {
		t.Fatal("legacy failure changed")
	}
	raw, err := json.Marshal(windowAcquisitionRow{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "failure_evidence") || d.failure.evidence.Schema != "" {
		t.Fatal("default diagnostic enabled")
	}
}

type failureEvidenceCapability struct {
	*windowTransportPayloadCapability
	recorder windowFailureRecorder
}

func (c *failureEvidenceCapability) windowFailureDiagnostics() *windowFailureRecorder {
	return &c.recorder
}
func TestOwnedFailureEvidenceRetainsProviderAndPersistenceFailures(t *testing.T) {
	for _, fault := range []string{"none", "descriptor_write", "descriptor_sync", "descriptor_readback", "ledger_sync", "both"} {
		t.Run(fault, func(t *testing.T) {
			c := windowTestContract()
			c.Model = windowProtocolLimits("model")
			c.JEV = windowProtocolLimits("jev")
			c.JEV.MaxInput, c.JEV.InputRate, c.JEV.OutputRate = 64000, 42000, 0
			c.ModelCap = 100000
			c.JEVCap = 100000
			var journal *failingWindowFile
			l, err := newWindowAccountingWithFile(filepath.Join(t.TempDir(), "ledger"), c, func(f *os.File) windowJournalFile { journal = &failingWindowFile{windowJournalFile: f}; return journal })
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			store, err := newWindowPayloadStore(filepath.Join(t.TempDir(), "payload"), filepath.Join(t.TempDir(), "campaign"), c, "recorded_run")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			cap := &failureEvidenceCapability{windowTransportPayloadCapability: &windowTransportPayloadCapability{windowTransportTestCapability: &windowTransportTestCapability{ledger: l, calls: map[string]int{}}, store: store, required: true}}
			calls := 0
			tr, err := newWindowProviderTransport(cap, "jev", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if fault == "ledger_sync" || fault == "both" {
					journal.syncFailure = true
				}
				if fault == "descriptor_write" || fault == "descriptor_sync" || fault == "both" {
					store.fileWrap = func(f *os.File) windowPayloadFile {
						return &failingWindowFile{windowJournalFile: f, writeFailure: fault == "descriptor_write", syncFailure: fault != "descriptor_write"}
					}
				}
				if fault == "descriptor_readback" {
					for name := range store.files {
						if err := os.WriteFile(filepath.Join(store.dir, name), []byte("changed"), 0600); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
				return nil, io.EOF
			}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tr.RoundTrip(windowTransportRequest(t, "jev")); err == nil {
				t.Fatal("failure hidden")
			}
			e := cap.recorder.evidence.Transport
			if len(e) != 1 {
				t.Fatalf("provider/persistence evidence discarded: %+v", e)
			}
			got := e[0]
			if got.Code != "upstream_failure" || got.Provider != "jev" || got.Sequence != 1 || got.Cause != "eof" {
				t.Fatalf("provider cause lost: %+v", got)
			}
			wantDescriptor := "verified"
			if strings.HasPrefix(fault, "descriptor_") || fault == "both" {
				wantDescriptor = "failed"
			}
			wantLedger := "verified"
			if fault == "ledger_sync" || fault == "both" {
				wantLedger = "failed"
			}
			if got.DescriptorPersistence != wantDescriptor || got.LedgerPersistence != wantLedger {
				t.Fatalf("wrong independent persistence outcomes: %+v", got)
			}
			if fault == "descriptor_readback" && got.RequestPersistence != "unknown" {
				t.Fatal("changed payload inventory described as verified")
			}
			state := l.Snapshot()
			if state.Pending == nil || state.JEVHeld != 2688 || state.JEVKnown != 0 {
				t.Fatalf("unknown hold lost: %+v", state)
			}
			if _, err = tr.RoundTrip(windowTransportRequest(t, "jev")); err == nil || calls != 1 {
				t.Fatal("next egress escaped")
			}
		})
	}
}

func TestOwnedFailureEvidenceRejectsForeignTerminalIdentity(t *testing.T) {
	for _, kind := range []string{"fixture", "caller_session", "active_driver", "response_session", "closed_owner"} {
		t.Run(kind, func(t *testing.T) {
			d, o, _ := failureEvidenceDriver(t, true)
			RunConversation(context.Background(), d.caseDefinition, d)
			f, s := d.fixture, d.session
			switch kind {
			case "fixture":
				f.TenantID++
			case "caller_session":
				s = "foreign"
			case "active_driver":
				d.permit.active = &windowOwnedDriver{}
			case "response_session":
				d.observations[0].Turn.Response.SessionID = "foreign"
			case "closed_owner":
				d.permit.campaign.closed = true
				t.Cleanup(func() { d.permit.campaign.closed = false })
			}
			_, err := d.Final(context.Background(), f, s)
			if err == nil || o.reads != 1 || d.failure.evidence.Final != "unknown" || d.failure.evidence.FinalReason != "session_identity_unverified" || d.finalDone {
				t.Fatalf("invalid identity became final: err=%v reads=%d evidence=%+v", err, o.reads, d.failure.evidence)
			}
		})
	}
}

func TestOwnedFailureEvidenceQualityFailureKeepsCleanupFailure(t *testing.T) {
	d, _, _ := failureEvidenceDriver(t, true)
	record := recordCollectionConversation(context.Background(), d.caseDefinition, d)
	hook := &failureCleanupHook{d: d, fail: "del", t: t}
	client := redis.NewClient(&redis.Options{Addr: "forbidden.invalid:0", MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	d.environment.rdb = client
	proof, err := d.finishWindowQualityOwned(context.Background(), record)
	if proof != nil || err == nil || d.permit.nextArm != 0 || !d.permit.stopped || d.failure.evidence.Cleanup != "failed" {
		t.Fatalf("quality failure lost cleanup: %+v %v", d.failure.evidence, err)
	}
}

func TestOwnedFailureEvidenceObserverTurnDeltaFailures(t *testing.T) {
	start := observability.Stats{Turns: 10, SampledTurns: 10}
	for _, tc := range []struct {
		name           string
		turns, sampled uint64
		want           string
	}{
		{"turn_only", 11, 12, "incomplete"}, {"sampled_only", 12, 11, "incomplete"}, {"matching_is_not_delivery_proof", 12, 12, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := windowFailureObserverState(observability.Stats{Turns: tc.turns, SampledTurns: tc.sampled, Accepted: 20, Exported: 20}, start, 2)
			if got != tc.want {
				t.Fatalf("known counter failure lost: got=%s want=%s", got, tc.want)
			}
		})
	}
}
func TestOwnedFailureEvidenceResponseStageSurvivesDescriptorFault(t *testing.T) {
	for _, stage := range []string{"http_status", "response_bound"} {
		for _, fault := range []string{"write", "sync"} {
			t.Run(stage+"/"+fault, func(t *testing.T) {
				cap, _ := windowTransportFixture(t)
				store, err := newWindowPayloadStore(filepath.Join(t.TempDir(), "payload"), filepath.Join(t.TempDir(), "campaign"), cap.ledger.contract, "recorded_run")
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				sink := &failureEvidenceCapability{windowTransportPayloadCapability: &windowTransportPayloadCapability{windowTransportTestCapability: cap, store: store, required: true}}
				status := 503
				body := "synthetic private response"
				if stage == "response_bound" {
					status = 200
					body = strings.Repeat("x", windowMaxResponseBytes+1)
				}
				calls := 0
				tr, err := newWindowProviderTransport(sink, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					store.fileWrap = func(f *os.File) windowPayloadFile {
						return &failingWindowFile{windowJournalFile: f, writeFailure: fault == "write", syncFailure: fault == "sync"}
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tr.RoundTrip(windowTransportRequest(t, "model")); err == nil {
					t.Fatal("failure lost")
				}
				raw, _ := json.Marshal(sink.recorder.evidence.Transport)
				var entries []map[string]any
				_ = json.Unmarshal(raw, &entries)
				if len(entries) != 1 || entries[0]["response_stage"] != stage || entries[0]["response_status"] != float64(status) || entries[0]["descriptor_persistence"] != "failed" || entries[0]["code"] != "protocol_failure" {
					t.Fatalf("provider response stage lost beside persistence failure: %s", raw)
				}
				if strings.Contains(string(raw), "synthetic private response") {
					t.Fatal("response body leaked")
				}
				before := cap.ledger.Snapshot()
				if before.Pending == nil || before.ModelHeld == 0 {
					t.Fatal("hold lost")
				}
				_, err = tr.RoundTrip(windowTransportRequest(t, "model"))
				after := cap.ledger.Snapshot()
				if err == nil || calls != 1 || after.ModelHeld != before.ModelHeld {
					t.Fatal("retry or hold change")
				}
			})
		}
	}
}
func TestOwnedFailureEvidenceForeignActiveArmCannotInheritHold(t *testing.T) {
	d, _, _ := failureEvidenceDriver(t, true)
	if _, _, err := d.Setup(context.Background(), d.caseDefinition); err != nil {
		t.Fatal(err)
	}
	// Another active arm now owns the same window ledger. Its real pending intent
	// must remain held, but must not be attributed to the old driver's row.
	req := windowTestRequest("jev")
	req.Model = d.ledger.contract.JEV.Model
	req.CaseKey = "other-arm/confirm"
	if _, err := d.ledger.Begin(req); err != nil {
		t.Fatal(err)
	}
	pending := d.ledger.Snapshot()
	if pending.JEVHeld <= 0 || pending.Pending == nil {
		t.Fatal("test has no pending hold")
	}
	d.permit.active = &windowOwnedDriver{arm: "jev_selective", ledger: d.ledger, permit: d.permit}
	d.failure.evidence.ModelHeld = 99 // also reject stale evidence from an earlier capture
	e := d.captureFailureEvidenceLocked()
	if e.AccountingSource != "unknown" || !e.PendingUnknown || e.ModelHeld != 0 || e.JEVHeld != 0 {
		t.Fatalf("foreign arm attributed to old row: %+v", e)
	}
	after := d.ledger.Snapshot()
	if after.JEVHeld != pending.JEVHeld || after.Pending == nil || after.Halt != "" {
		t.Fatal("diagnostic changed actual ledger")
	}
}
