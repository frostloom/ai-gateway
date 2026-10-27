package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWindowOwnedDriverNilPermitRejectsWithoutConstruction(t *testing.T) {
	if _, e := newWindowOwnedDriver(nil, ConversationSuite{}, fullWindowSlot{}, "model", nil, nil, "", "", nil, nil); e == nil {
		t.Fatal("nil permit")
	}
	var d *windowOwnedDriver
	if _, e := d.windowNextRequest(context.Background(), "model"); e == nil {
		t.Fatal("nil driver permission")
	}
}

func TestWindowOwnedDriverActivePhaseCapability(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, e := startWindowCampaign(p, b)
	if e != nil {
		t.Fatal(e)
	}
	defer c.closeWindowCampaign()
	ledger, e := c.openWindowCampaignLedger()
	if e != nil {
		t.Fatal(e)
	}
	env := &PairedFixtureEnvironment{}
	permit := &windowAcquisitionPermit{campaign: c, ledger: ledger, executionSHA: c.prepared.executionSHA, windowIndex: c.index, environment: env}
	phase := &windowOwnedPhase{nonce: "synthetic-live-phase"}
	d := &windowOwnedDriver{permit: permit, ledger: ledger, slot: p.scope.Slots[0], arm: "model", environment: env, phase: phase, setupDone: true}
	phase.driver = d
	permit.active = d
	ctx := context.WithValue(context.Background(), windowOwnedPhaseKey{}, phase)
	for i := 1; i <= 5; i++ {
		identity, e := d.windowNextRequest(ctx, "model")
		if e != nil || identity.Attempt != i || identity.Turn != 1 || identity.CaseKey != d.slot.CaseKey || identity.Arm != "model" {
			t.Fatalf("active request %d %+v %v", i, identity, e)
		}
	}
	if _, e := d.windowNextRequest(ctx, "model"); e == nil || !permit.stopped {
		t.Fatal("sixth attempt admitted")
	}
}

type closedWindowOwnedRoundTripper func(*http.Request) (*http.Response, error)

func (f closedWindowOwnedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
func fixtureWindowOwnedPermit(t *testing.T) (*windowAcquisitionPermit, ConversationSuite, func(ConversationCase) http.RoundTripper, *int) {
	t.Helper()
	source, profiles, bindings, _, _, limits, root := inputFixtureWindowCampaign(t)
	bindings.PolicySHA256 = collectionPolicySHA()
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{10, 1}
	}
	scope, e := prepareFullWindowScope(source, profiles, 100, caps, bindings)
	if e != nil {
		t.Fatal(e)
	}
	sha, _, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		t.Fatal(e)
	}
	limits.Model.MaxInput = 10000
	limits.Model.MaxRequestBytes = 1 << 20
	limits.JEV.MaxInput = 10000
	limits.JEV.MaxRequestBytes = 1 << 20
	execution, e := executionSHAWindowCampaign(sha, root, limits)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := prepareWindowCampaign(root, root, scope, source, profiles, bindings, sha, execution, limits)
	if e != nil {
		t.Fatal(e)
	}
	_, baseline := fixtureWindowCampaign(t)
	campaign, e := startWindowCampaign(prepared, baseline)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := campaign.closeWindowCampaign(); e != nil {
			t.Log("stopped synthetic ledger close", e)
		}
	})
	ledger, e := campaign.openWindowCampaignLedger()
	if e != nil {
		t.Fatal(e)
	}
	env, e := OpenPairedFixtureEnvironment(context.Background(), PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := env.Close(); e != nil {
			t.Error(e)
		}
	})
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	t.Cleanup(receiver.Close)
	observer, e := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-owned-synthetic", SecretKey: "sk-owned-synthetic", HashKey: strings.Repeat("h", 32), Release: bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := observer.Shutdown(context.Background()); e != nil {
			t.Log("stopped synthetic observer shutdown", e)
		}
	})
	permit := &windowAcquisitionPermit{campaign: campaign, ledger: ledger, executionSHA: execution, windowIndex: 1, environment: env, observer: observer, observerIdentity: observer.EvaluationIdentity()}
	calls := new(int)
	factory := func(c ConversationCase) http.RoundTripper {
		local := &fullLocalModel{c: c}
		return closedWindowOwnedRoundTripper(func(r *http.Request) (*http.Response, error) {
			*calls++
			if r.URL.String() != "https://api.deepseek.com/anthropic/v1/messages" {
				return nil, fmt.Errorf("unexpected guarded URL")
			}
			raw, e := io.ReadAll(r.Body)
			if e != nil {
				return nil, e
			}
			var obj map[string]any
			if json.Unmarshal(raw, &obj) != nil {
				return nil, fmt.Errorf("invalid closed request")
			}
			obj["model"] = "local-full-customer"
			raw, _ = json.Marshal(obj)
			copy := r.Clone(r.Context())
			u, _ := url.Parse("http://127.0.0.1/v1/messages")
			copy.URL = u
			copy.Body = io.NopCloser(bytes.NewReader(raw))
			response, e := local.RoundTrip(copy)
			if e != nil {
				return nil, e
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			var parsed map[string]any
			json.Unmarshal(body, &parsed)
			parsed["model"] = "deepseek-flash"
			body, _ = json.Marshal(parsed)
			response.Body = io.NopCloser(bytes.NewReader(body))
			return response, nil
		})
	}
	return permit, source, factory, calls
}
func TestWindowOwnedDriverActualHandlerReadAndSealedProof(t *testing.T) {
	permit, source, _, calls := fixtureWindowOwnedPermit(t)
	slot := permit.campaign.prepared.scope.Slots[0]
	primary, _, e := fullCustomerSelection(source)
	if e != nil {
		t.Fatal(e)
	}
	c := primary.Cases[0]
	noJEV := closedWindowOwnedRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Error("model arm called JEV")
		return nil, fmt.Errorf("closed")
	})
	d, e := newWindowOwnedDriver(permit, source, slot, slot.ArmOrder[0], permit.environment, permit.observer, "synthetic-model-key", "synthetic-jev-key", closedReadWindowOwned(calls), noJEV)
	if e != nil {
		t.Fatal(e)
	}
	if *calls != 0 {
		t.Fatal("provider called outside Step")
	}
	ctx, run := permit.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "closed-owned", CaseKey: c.Key(), Arm: d.arm, EvidenceKind: "local_stub"})
	record := recordCollectionConversation(ctx, c, d)
	run.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass})
	if record.Disposition != "continue_recorded" {
		t.Fatalf("actual recording halted: %+v", record)
	}
	if record.Result.Status != "failed" || record.Result.StructuralPass || record.Result.Success || len(record.Issues) == 0 {
		t.Fatal("safe wrong read quality negative was promoted")
	}
	if *calls != 2 || permit.ledger.Snapshot().ModelAttempts != 2 {
		t.Fatalf("actual Handler attempts %d %+v", *calls, permit.ledger.Snapshot())
	}
	session := d.session
	if n, _ := permit.environment.rdb.Exists(context.Background(), "agent:session:"+session).Result(); n != 1 {
		t.Fatal("fixture session missing before owned cleanup")
	}
	proof, e := d.finishWindowOwnedArm(context.Background(), record)
	if e != nil {
		t.Fatal(e)
	}
	if validateWindowOwnedArmProof(permit, proof) != nil || proof.recordingSHA == "" || permit.nextArm != 1 || permit.active != nil || record.Result.Success {
		t.Fatal("invalid sealed proof or upgraded task success")
	}
	if n, _ := permit.environment.rdb.Exists(context.Background(), "agent:session:"+session).Result(); n != 0 {
		t.Fatal("owned session not cleaned")
	}
	if permit.environment.closed {
		t.Fatal("arm closed whole environment")
	}
	proof.recording.Result.Observations[0].Turn.Response.Reply += "tampered"
	if validateWindowOwnedArmProof(permit, proof) == nil {
		t.Fatal("mutated proof admitted")
	}
	if _, e := d.windowNextRequest(ctx, "model"); e == nil || *calls != 2 {
		t.Fatal("finished driver retained permission")
	}
}

// Deliberately performs a safe own-account read for the first adversarial source
// case. The original reject goal remains a retained quality negative; this tests
// real toolbox/oracle read execution without relabeling the task as successful.
func closedReadWindowOwned(calls *int) http.RoundTripper {
	return closedWindowOwnedRoundTripper(func(r *http.Request) (*http.Response, error) {
		*calls++
		if r.URL.String() != "https://api.deepseek.com/anthropic/v1/messages" {
			return nil, fmt.Errorf("unexpected closed read endpoint")
		}
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			return nil, e
		}
		var request struct {
			Messages []struct {
				Content []struct{ Type, Content string }
			}
		}
		if json.Unmarshal(raw, &request) != nil {
			return nil, fmt.Errorf("bad closed request")
		}
		var content []any
		stop := "tool_use"
		if *calls == 1 {
			content = []any{map[string]any{"type": "tool_use", "id": "owned-read", "name": "get_balance", "input": map[string]any{}}}
		} else {
			stop = "end_turn"
			text := ""
			for _, message := range request.Messages {
				for _, block := range message.Content {
					if block.Type == "tool_result" {
						text = block.Content
					}
				}
			}
			if text == "" {
				return nil, fmt.Errorf("missing actual tool result")
			}
			content = []any{map[string]any{"type": "text", "text": text}}
		}
		body, _ := json.Marshal(map[string]any{"model": "deepseek-flash", "content": content, "stop_reason": stop, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
}

func closedJEVWindowOwned(calls *int) http.RoundTripper {
	return closedWindowOwnedRoundTripper(func(r *http.Request) (*http.Response, error) {
		*calls++
		if r.URL.String() != "https://api.typesafe.ai/v1/systemone" {
			return nil, fmt.Errorf("unexpected closed JEV endpoint")
		}
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			return nil, e
		}
		var request struct {
			Questions map[string]map[string]any `json:"questions"`
		}
		if json.Unmarshal(raw, &request) != nil {
			return nil, fmt.Errorf("bad JEV request")
		}
		answers := map[string]any{}
		for name, q := range request.Questions {
			switch q["type"] {
			case "noul":
				answers[name] = map[string]any{"type": "noul", "noul": .99}
			case "choice":
				choice := "get_balance"
				confidence := .5
				if name == "route_shape" {
					choice = "complex"
					confidence = .99
				}
				answers[name] = map[string]any{"type": "choice", "choice": choice, "confidence": confidence, "probabilities": map[string]any{}}
			case "score":
				criteria := q["criteria"].([]any)
				answers[name] = map[string]any{"type": "score", "score": .1, "confidence": .99, "legend": map[string]any{"0": criteria[0]}, "probabilities": map[string]any{}}
			default:
				return nil, fmt.Errorf("unknown closed question")
			}
		}
		body, _ := json.Marshal(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
}
func TestWindowOwnedDriverActualSelectiveUsesBothGuards(t *testing.T) {
	permit, source, factory, _ := fixtureWindowOwnedPermit(t)
	slot := permit.campaign.prepared.scope.Slots[0]
	primary, _, e := fullCustomerSelection(source)
	if e != nil {
		t.Fatal(e)
	}
	c := primary.Cases[0]
	jevCalls := 0
	var lastProof *windowOwnedArmProof
	for _, arm := range slot.ArmOrder {
		d, e := newWindowOwnedDriver(permit, source, slot, arm, permit.environment, permit.observer, "synthetic-model", "synthetic-jev", factory(c), closedJEVWindowOwned(&jevCalls))
		if e != nil {
			t.Fatal(e)
		}
		ctx, run := permit.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "closed-pair", CaseKey: c.Key(), Arm: arm, EvidenceKind: "local_stub"})
		record := recordCollectionConversation(ctx, c, d)
		run.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass})
		if record.Disposition != "continue_recorded" {
			t.Fatalf("arm halted %+v", record)
		}
		proof, e := d.finishWindowOwnedArm(context.Background(), record)
		lastProof = proof
		if e != nil || validateWindowOwnedArmProof(permit, proof) != nil {
			t.Fatal("no owned pair proof", e)
		}
	}
	if permit.nextArm != 2 || jevCalls != 1 || permit.ledger.Snapshot().JEVAttempts != 1 {
		t.Fatalf("selective bypassed actual guard: cursor%d JEV%d %+v", permit.nextArm, jevCalls, permit.ledger.Snapshot())
	}
	permit.executionSHA = "changed-after-owned-proof"
	if e := validateWindowOwnedArmProof(permit, lastProof); e == nil {
		t.Fatal("owned proof accepted changed current execution identity")
	}

}
func fixtureWindowOwnedPhase(t *testing.T) (*windowOwnedDriver, context.Context) {
	t.Helper()
	p, b := fixtureWindowCampaign(t)
	c, e := startWindowCampaign(p, b)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.closeWindowCampaign() })
	ledger, e := c.openWindowCampaignLedger()
	if e != nil {
		t.Fatal(e)
	}
	env := &PairedFixtureEnvironment{}
	permit := &windowAcquisitionPermit{campaign: c, ledger: ledger, executionSHA: c.prepared.executionSHA, windowIndex: c.index, environment: env}
	phase := &windowOwnedPhase{nonce: "synthetic-live-phase"}
	d := &windowOwnedDriver{permit: permit, ledger: ledger, slot: p.scope.Slots[0], arm: "model", environment: env, phase: phase, setupDone: true}
	phase.driver = d
	permit.active = d
	return d, context.WithValue(context.Background(), windowOwnedPhaseKey{}, phase)
}
func TestWindowOwnedDriverForeignPhaseAndOrderHaveZeroUpstream(t *testing.T) {
	for _, name := range []string{"foreign ctx", "foreign nonce", "stale phase", "cancelled", "wrong arm", "wrong slot", "foreign ledger", "execution", "window", "foreign driver", "closed env", "jev in model", "contract", "cursor"} {
		t.Run(name, func(t *testing.T) {
			d, ctx := fixtureWindowOwnedPhase(t)
			calls := 0
			transport, e := newWindowProviderTransport(d, "model", closedWindowOwnedRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, fmt.Errorf("must not reach upstream")
			}))
			if e != nil {
				t.Fatal(e)
			}
			provider := "model"
			switch name {
			case "foreign ctx":
				ctx = context.Background()
			case "foreign nonce":
				ctx = context.WithValue(ctx, windowOwnedPhaseKey{}, &windowOwnedPhase{nonce: d.phase.nonce})
			case "stale phase":
				d.phase = nil
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "wrong arm":
				d.arm = "jev_selective"
			case "wrong slot":
				d.slot.Ordinal++
			case "foreign ledger":
				d.permit.ledger = &windowAccounting{}
			case "execution":
				d.permit.executionSHA = "changed"
			case "window":
				d.permit.windowIndex++
			case "foreign driver":
				d.permit.active = &windowOwnedDriver{}
			case "closed env":
				d.environment.closed = true
			case "jev in model":
				provider = "jev"
			case "contract":
				d.ledger.contract.Model.InputRate++
			case "cursor":
				d.permit.nextArm = 1
				d.arm = "jev_selective"
			}
			if name == "jev in model" {
				if _, e := d.windowNextRequest(ctx, provider); e == nil {
					t.Fatal("model arm got JEV permission")
				}
			} else {
				body := `{"model":"deepseek-flash","max_tokens":128,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":[{"type":"text","text":"closed"}]}]}`
				request, _ := http.NewRequestWithContext(ctx, "POST", "https://api.deepseek.com/anthropic/v1/messages", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				if _, e := transport.RoundTrip(request); e == nil {
					t.Fatal("foreign request admitted")
				}
			}
			if calls != 0 {
				t.Fatal("foreign permission reached upstream")
			}
		})
	}
}

func TestWindowOwnedDriverWrongPreviewHaltsWithOriginalPolicyEvidence(t *testing.T) {
	permit, source, _, _ := fixtureWindowOwnedPermit(t)
	slot := permit.campaign.prepared.scope.Slots[0]
	suite, _, e := fullCustomerSelection(source)
	if e != nil {
		t.Fatal(e)
	}
	c := suite.Cases[0]
	calls := 0
	wrongPreview := closedWindowOwnedRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"model":"deepseek-flash","content":[{"type":"tool_use","id":"wrong-preview","name":"recharge","input":{"amount_money":100}}],"stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":8}}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	d, e := newWindowOwnedDriver(permit, source, slot, "model", permit.environment, permit.observer, "synthetic-model", "synthetic-jev", wrongPreview, closedJEVWindowOwned(new(int)))
	if e != nil {
		t.Fatal(e)
	}
	ctx, run := permit.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "closed-unsafe-preview", CaseKey: c.Key(), Arm: "model", EvidenceKind: "local_stub"})
	record := recordCollectionConversation(ctx, c, d)
	run.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass})
	if record.Disposition != "halt" || calls != 1 || !permit.stopped {
		t.Fatal("wrong preview did not globally stop")
	}
	if e := verifyCollectionRecording(record); e != nil {
		t.Fatal("driver replaced origin-typed policy evidence", e)
	}
	if _, e := d.finishWindowOwnedArm(context.Background(), record); e == nil {
		t.Fatal("wrong preview minted proof")
	}
	if d.retained == nil {
		t.Fatal("final halt recording not retained before cleanup")
	}
	if permit.nextArm != 0 {
		t.Fatal("wrong preview advanced arm")
	}
	if n, _ := permit.environment.rdb.Exists(context.Background(), "agent:session:"+d.session).Result(); n != 0 {
		t.Fatal("own stopped session not cleaned")
	}
}

func TestWindowOwnedDriverConstructorRejectsSourceOrderAndConfiguration(t *testing.T) {
	for _, name := range []string{"source", "order", "key", "observer", "foreign env"} {
		t.Run(name, func(t *testing.T) {
			p, source, factory, calls := fixtureWindowOwnedPermit(t)
			slot := p.campaign.prepared.scope.Slots[0]
			suite, _, e := fullCustomerSelection(source)
			if e != nil {
				t.Fatal(e)
			}
			key := "synthetic-model"
			switch name {
			case "source":
				source.Cases[0].Scenario.Turns[0].User += " forged"
			case "order":
				slot.Ordinal++
			case "key":
				key = ""
			case "observer":
				p.observer = &observability.Observer{}
				p.observerIdentity = observability.EvaluationIdentity{}
			case "foreign env":
				p.environment = &PairedFixtureEnvironment{}
			}
			if _, e := newWindowOwnedDriver(p, source, slot, "model", p.environment, p.observer, key, "synthetic-jev", factory(suite.Cases[0]), closedJEVWindowOwned(new(int))); e == nil {
				t.Fatal("invalid construction accepted")
			}
			if *calls != 0 || p.active != nil || !p.stopped {
				t.Fatal("invalid construction escaped early guard")
			}
		})
	}
}
func TestWindowOwnedDriverCoherentReplayCannotMintActualProof(t *testing.T) {
	p, source, factory, _ := fixtureWindowOwnedPermit(t)
	slot := p.campaign.prepared.scope.Slots[0]
	suite, _, _ := fullCustomerSelection(source)
	c := suite.Cases[0]
	d, e := newWindowOwnedDriver(p, source, slot, "model", p.environment, p.observer, "synthetic-model", "synthetic-jev", factory(c), closedJEVWindowOwned(new(int)))
	if e != nil {
		t.Fatal(e)
	}
	ctx, run := p.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "closed-material", CaseKey: c.Key(), Arm: "model", EvidenceKind: "local_stub"})
	actual := recordCollectionConversation(ctx, c, d)
	run.End(observability.EvaluationResult{Status: actual.Result.Status, StructuralPass: actual.Result.StructuralPass})
	if actual.Disposition != "continue_recorded" {
		t.Fatal("fixture recording stopped")
	}
	mutated, e := cloneWindowOwned(actual.Result)
	if e != nil {
		t.Fatal(e)
	}
	mutated.Fixture.APIKey = actual.Result.Fixture.APIKey
	mutated.Observations[0].Turn.Response.Reply += "coherently forged reply"
	replay := &replayConversationDriver{episode: ConversationEpisode{Key: c.Key(), Fixture: mutated.Fixture, Initial: mutated.Initial, Final: mutated.Final, Steps: mutated.Observations}}
	forged := recordCollectionConversation(context.Background(), c, replay)
	if e := verifyCollectionRecording(forged); e != nil {
		t.Fatal("test replay not coherent", e)
	}
	if _, e := d.finishWindowOwnedArm(context.Background(), forged); e == nil {
		t.Fatal("coherent JSON/replay minted actual owned proof")
	}
	if p.nextArm != 0 || len(p.proofs) != 0 || !p.stopped {
		t.Fatal("forged recording advanced")
	}
}
func TestWindowOwnedDriverJEVFailurePreventsModelFallback(t *testing.T) {
	p, source, factory, calls := fixtureWindowOwnedPermit(t)
	slot := p.campaign.prepared.scope.Slots[0]
	suite, _, _ := fullCustomerSelection(source)
	c := suite.Cases[0]
	jevCalls := 0
	for _, arm := range slot.ArmOrder {
		judge := closedJEVWindowOwned(&jevCalls)
		if arm == "jev_selective" {
			judge = closedWindowOwnedRoundTripper(func(r *http.Request) (*http.Response, error) {
				jevCalls++
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"model":"jev-1.13.0","answers":{}}`)), Request: r}, nil
			})
		}
		d, e := newWindowOwnedDriver(p, source, slot, arm, p.environment, p.observer, "synthetic-model", "synthetic-jev", factory(c), judge)
		if e != nil {
			t.Fatal(e)
		}
		before := *calls
		ctx, run := p.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "closed-jev-failure", CaseKey: c.Key(), Arm: arm, EvidenceKind: "local_stub"})
		record := recordCollectionConversation(ctx, c, d)
		run.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass})
		proof, e := d.finishWindowOwnedArm(context.Background(), record)
		if arm == "model" {
			if e != nil || proof == nil {
				t.Fatal("model fixture failed", e)
			}
		} else {
			if e == nil || proof != nil || *calls != before || !p.stopped || p.nextArm != 1 {
				t.Fatal("JEV failure reached model fallback")
			}
		}
	}
	if jevCalls != 1 || p.ledger.Snapshot().JEVHeld != 1 {
		t.Fatal("unknown JEV hold not retained")
	}
}

func TestWindowOwnedDriverContextFrameIsLiveDetachedData(t *testing.T) {
	d, ctx := fixtureWindowOwnedPhase(t)
	source, _, _ := windowScopeInputs(t)
	primary, _, e := fullCustomerSelection(source)
	if e != nil {
		t.Fatal(e)
	}
	d.caseDefinition = primary.Cases[0]
	d.phase.driver = d
	got, e := windowOwnedContextFrame(ctx)
	if e != nil || !reflect.DeepEqual(got, d.caseDefinition) {
		t.Fatal("live frame missing", e)
	}
	original := d.caseDefinition.Scenario.Turns[0].User
	got.Scenario.Turns[0].User = "mutated closed capture case"
	if d.caseDefinition.Scenario.Turns[0].User != original {
		t.Fatal("capture frame mutated actual source")
	}
	d.phase = nil
	if _, e := windowOwnedContextFrame(ctx); e == nil {
		t.Fatal("stale frame returned source")
	}
	if _, e := windowOwnedContextFrame(context.Background()); e == nil {
		t.Fatal("foreign frame returned source")
	}
}

func TestWindowOwnedDriverDurableOwnershipBeforeSend(t *testing.T) {
	for _, mode := range []string{"root_replace", "lock_replace", "claim_edit", "ledger_dir_replace", "journal_unlink", "journal_replace", "journal_append"} {
		t.Run(mode, func(t *testing.T) {
			prepared, baseline := fixtureWindowCampaign(t)
			campaign, err := startWindowCampaign(prepared, baseline)
			if err != nil {
				t.Fatal(err)
			}
			defer campaign.closeWindowCampaign()
			ledger, err := campaign.openWindowCampaignLedger()
			if err != nil {
				t.Fatal(err)
			}
			env := &PairedFixtureEnvironment{}
			permit := &windowAcquisitionPermit{campaign: campaign, ledger: ledger, executionSHA: prepared.executionSHA, windowIndex: campaign.index, environment: env}
			phase := &windowOwnedPhase{nonce: "synthetic-owned-phase"}
			driver := &windowOwnedDriver{permit: permit, ledger: ledger, slot: prepared.scope.Slots[0], arm: "model", environment: env, phase: phase, setupDone: true}
			phase.driver = driver
			permit.active = driver
			calls := 0
			transport, err := newWindowProviderTransport(driver, "model", closedWindowOwnedRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return windowTransportResponse("model"), nil }))
			if err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(campaign.ledgerDir, "accounting.jsonl")
			switch mode {
			case "root_replace":
				if err = os.Rename(campaign.root, campaign.root+"-moved"); err == nil {
					err = os.Mkdir(campaign.root, 0700)
				}
			case "lock_replace":
				target := filepath.Join(campaign.root, ".lock")
				if err = os.Rename(target, target+"-moved"); err == nil {
					err = os.WriteFile(target, nil, 0600)
				}
			case "claim_edit":
				err = os.WriteFile(filepath.Join(campaign.root, "claim-000001.json"), []byte("{}"), 0600)
			case "ledger_dir_replace":
				if err = os.Rename(campaign.ledgerDir, campaign.ledgerDir+"-moved"); err == nil {
					err = os.Mkdir(campaign.ledgerDir, 0700)
				}
			case "journal_unlink":
				err = os.Remove(journal)
			case "journal_replace":
				raw, e := os.ReadFile(journal)
				if e != nil {
					t.Fatal(e)
				}
				if err = os.Rename(journal, journal+"-moved"); err == nil {
					err = os.WriteFile(journal, raw, 0600)
				}
			case "journal_append":
				var f *os.File
				f, err = os.OpenFile(journal, os.O_WRONLY|os.O_APPEND, 0600)
				if err == nil {
					_, err = f.WriteString(" ")
					f.Close()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			req := windowTransportRequest(t, "model").WithContext(context.WithValue(context.Background(), windowOwnedPhaseKey{}, phase))
			response, err := transport.RoundTrip(req)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if err == nil || calls != 0 || !permit.stopped || !campaign.poisoned {
				t.Fatalf("changed durable owner sent request: calls=%d stopped=%v poisoned=%v error=%v", calls, permit.stopped, campaign.poisoned, err)
			}
		})
	}
}
