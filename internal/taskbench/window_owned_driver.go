package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/observability"
	"go.opentelemetry.io/otel/trace"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"time"
)

// A trusted collector issues these only from a private higher authorization.
// No public entry currently issues that acquisition authorization.
type windowAcquisitionPermit struct {
	captureFailureEvidence bool

	acquisition      *windowAcquisitionRun
	mu               sync.Mutex
	campaign         *windowCampaign
	ledger           *windowAccounting
	executionSHA     string
	windowIndex      int
	environment      *PairedFixtureEnvironment
	observer         *observability.Observer
	observerIdentity observability.EvaluationIdentity
	evidenceKind     string
	payload          *windowPayloadStore
	proofSHA         []string
	nextArm          int
	active           *windowOwnedDriver
	proofs           []*windowOwnedArmProof
	stopped          bool
}
type windowOwnedPhaseKey struct{}

func (d *windowOwnedDriver) windowPayloadEvidence() (*windowPayloadStore, bool) {
	if d == nil || d.permit == nil {
		return nil, true
	}
	return d.permit.payload, d.permit.evidenceKind == "recorded_run"
}

type windowOwnedPhase struct {
	driver     *windowOwnedDriver
	nonce      string
	model, jev int
}
type windowOwnedDriver struct {
	failure                 windowFailureRecorder
	failureStepInputSession string
	failureSessionConflict  bool

	mu                  sync.Mutex
	permit              *windowAcquisitionPermit
	ledger              *windowAccounting
	slot                fullWindowSlot
	arm                 string
	environment         *PairedFixtureEnvironment
	observer            *observability.Observer
	phase               *windowOwnedPhase
	setupDone           bool
	nextTurn            int
	base                *HandlerConversationDriver
	oracle              *pairedStoreOracle
	server              *httptest.Server
	caseDefinition      ConversationCase
	fixture             Fixture
	initial, final      ConversationState
	observations        []ConversationObservation
	session             string
	finalDone, finished bool
	runtime             *agent.RuntimeMetadata
	observerStart       observability.Stats
	retained            *collectionRecording
	qualityCandidate    *qualityPreviewSidecar
	qualityRetainedSHA  string
	qualityAcquisition  *windowAcquisitionRun
	policySHA           string
	qualityRoot         trace.Span
}

type windowOwnedArmProof struct {
	quality      *windowQualityOwnedEnd
	qualitySHA   string
	permit       *windowAcquisitionPermit
	driver       *windowOwnedDriver
	slot         fullWindowSlot
	arm          string
	recording    collectionRecording
	recordingSHA string
}

func newWindowOwnedDriver(permit *windowAcquisitionPermit, source ConversationSuite, slot fullWindowSlot, arm string, env *PairedFixtureEnvironment, observer *observability.Observer, modelKey, jevKey string, modelUpstream, jevUpstream http.RoundTripper) (*windowOwnedDriver, error) {
	if permit == nil {
		return nil, fmt.Errorf("missing acquisition permit")
	}
	permit.mu.Lock()
	defer permit.mu.Unlock()
	fail := func() (*windowOwnedDriver, error) {
		permit.stopWindowOwnedLocked()
		return nil, fmt.Errorf("owned Handler construction rejected")
	}
	if permit.active != nil || permit.stopped || env == nil || env != permit.environment || env.closed || env.admin == nil || env.db == nil || env.st == nil || env.rdb == nil || env.redis == nil || !strings.HasPrefix(env.name, "ai_gateway_pair_") || env.rdb.Options().Addr != env.redis.Addr() || observer == nil || observer != permit.observer || strings.TrimSpace(modelKey) == "" || strings.TrimSpace(jevKey) == "" || windowTransportNil(modelUpstream) || windowTransportNil(jevUpstream) {
		return fail()
	}
	c := permit.campaign
	if c == nil {
		return fail()
	}
	c.mu.Lock()
	scope := c.prepared.scope
	scopeSHA := c.prepared.scopeSHA
	c.mu.Unlock()
	identity := observer.EvaluationIdentity()
	if !identity.Enabled || !identity.CaptureContent || identity.SampleRate != 1 || identity.Environment != "evaluation" || identity.Release == "" || identity.Release == "unknown" || identity.ExportTimeoutMS != windowObserverTimeoutForScope(scope.Schema) || identity.QueueSize < 1 || !comparisonDigest(identity.EndpointSHA256) || !comparisonDigest(identity.ProjectSHA256) || identity != permit.observerIdentity {
		return fail()
	}
	if scope.Bindings.PolicySHA256 != collectionPolicySHA() && scope.Bindings.PolicySHA256 != windowOwnedQualityPolicySHA() {
		return fail()
	}
	digest, _, e := validateFullWindowScope(scope, source, env.profiles, scope.Bindings)
	if e != nil || digest != scopeSHA {
		return fail()
	}
	selected, _, e := fullCustomerSelection(source)
	if e != nil {
		return fail()
	}
	var actual ConversationCase
	found := false
	for _, candidate := range selected.Cases {
		if candidate.Key() == slot.CaseKey {
			actual = candidate
			found = true
			break
		}
	}
	if !found || len(actual.Scenario.Turns) != slot.UserTurns {
		return fail()
	}
	d := &windowOwnedDriver{permit: permit, ledger: permit.ledger, slot: slot, arm: arm, environment: env, observer: observer, observerStart: observer.Stats(), policySHA: scope.Bindings.PolicySHA256}
	permit.active = d
	if permit.guardWindowOwnedLocked(d) != nil {
		permit.active = nil
		return fail()
	}
	d.caseDefinition, e = cloneWindowOwned(actual)
	if e != nil {
		return fail()
	}
	modelTransport, e := newWindowProviderTransport(d, "model", modelUpstream)
	if e != nil {
		return fail()
	}
	llm, e := agent.NewEvaluationLLMClient("https://api.deepseek.com/anthropic", modelKey, "deepseek-flash", "anthropic", &http.Client{Transport: modelTransport, Timeout: 90 * time.Second}, 128)
	if e != nil {
		return fail()
	}
	// All explicit identity/source/cursor/provider/observer checks above precede
	// the local administration listener and any fixture mutation.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	token := "owned-window-" + randomID()
	server := httptest.NewServer(admin.Handler(env.st, log, token))
	d.server = server
	constructionFailed := true
	defer func() {
		if constructionFailed {
			server.Close()
		}
	}()
	h := agent.NewHandler(nil, agent.NewToolbox(agent.NewAdminClientWithToken(server.URL, token)), llm, env.st, env.rdb, log)
	h.SetObserver(observer)
	h.SetAdminAccess(token, 0)
	if e = h.SetRoutingMode(arm); e != nil {
		return fail()
	}
	if arm == "jev_selective" {
		evaluator, e := newWindowJEV(d, jevKey, jevUpstream)
		if e != nil {
			return fail()
		}
		h.SetJev(evaluator)
	}
	d.runtime = h.EvaluationRuntimeMetadata()
	d.oracle = &pairedStoreOracle{env: env}
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Agent-Internal", token)
		r.Header.Set("X-Agent-Tenant", fmt.Sprint(d.oracle.f.TenantID))
		h.ServeHTTP(w, r)
	})
	d.base, e = NewHandlerConversationDriver(wrapper, d.oracle)
	if e != nil {
		return fail()
	}
	constructionFailed = false
	return d, nil
}
func cloneWindowOwned[T any](value T) (T, error) {
	var out T
	raw, e := json.Marshal(value)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	return out, e
}
func (d *windowOwnedDriver) stopWindowOwned() error {
	if d != nil && d.permit != nil {
		d.permit.mu.Lock()
		d.permit.stopWindowOwnedLocked()
		d.permit.mu.Unlock()
	}
	return fmt.Errorf("owned Handler arm stopped")
}
func (d *windowOwnedDriver) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	if d == nil {
		return Fixture{}, ConversationState{}, fmt.Errorf("missing owned driver")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || d.base == nil || d.setupDone || d.finished || !reflect.DeepEqual(c, d.caseDefinition) {
		return Fixture{}, ConversationState{}, d.stopWindowOwned()
	}
	d.permit.mu.Lock()
	guard := d.permit.guardWindowOwnedLocked(d)
	d.permit.mu.Unlock()
	if guard != nil {
		return Fixture{}, ConversationState{}, d.stopWindowOwned()
	}
	if d.windowQualityMode() {
		d.qualityRoot = trace.SpanFromContext(ctx)
		if !d.qualityRoot.SpanContext().IsValid() || !d.qualityRoot.IsRecording() {
			return Fixture{}, ConversationState{}, d.stopWindowOwned()
		}
	}
	f, initial, e := d.base.Setup(ctx, d.caseDefinition)
	if e != nil {
		return f, initial, d.stopWindowOwned()
	}
	d.fixture, e = cloneWindowOwned(f)
	d.fixture.APIKey = f.APIKey
	if e != nil {
		return f, initial, d.stopWindowOwned()
	}
	d.initial, e = cloneWindowOwned(initial)
	if e != nil {
		return f, initial, d.stopWindowOwned()
	}
	d.setupDone = true
	outF, cloneErr := cloneWindowOwned(d.fixture)
	if cloneErr != nil {
		return Fixture{}, ConversationState{}, d.stopWindowOwned()
	}
	outF.APIKey = d.fixture.APIKey
	outState, e := cloneWindowOwned(d.initial)
	if e != nil {
		return Fixture{}, ConversationState{}, d.stopWindowOwned()
	}
	return outF, outState, nil
}
func (d *windowOwnedDriver) Step(ctx context.Context, f Fixture, session, message string) (ConversationObservation, error) {
	if d == nil {
		return ConversationObservation{}, fmt.Errorf("missing owned driver")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || !d.setupDone || d.finished || d.finalDone || d.qualityCandidate != nil || d.nextTurn >= len(d.caseDefinition.Scenario.Turns) || !reflect.DeepEqual(f, d.fixture) || session != d.session {
		return ConversationObservation{}, d.stopWindowOwned()
	}
	if d.windowQualityMode() && (d.qualityRoot == nil || !d.qualityRoot.IsRecording() || !trace.SpanFromContext(ctx).SpanContext().Equal(d.qualityRoot.SpanContext())) {
		return ConversationObservation{}, d.stopWindowOwned()
	}
	expected, e := resolveConversationMessage(d.caseDefinition.Scenario.Turns[d.nextTurn].User, d.fixture)
	if e != nil || message != expected {
		return ConversationObservation{}, d.stopWindowOwned()
	}
	p := d.permit
	p.mu.Lock()
	if p.guardWindowOwnedLocked(d) != nil {
		p.mu.Unlock()
		return ConversationObservation{}, d.stopWindowOwned()
	}
	p.mu.Unlock()
	var before qualityEffectCapture
	var previous json.RawMessage
	if d.windowQualityMode() {
		var err error
		before, err = captureWindowQualityEffects(ctx, d)
		if err != nil {
			return ConversationObservation{}, d.stopWindowOwned()
		}
		previous, _, err = captureWindowQualitySession(ctx, d, true)
		if err != nil {
			return ConversationObservation{}, d.stopWindowOwned()
		}
	}
	p.mu.Lock()
	if p.guardWindowOwnedLocked(d) != nil {
		p.mu.Unlock()
		return ConversationObservation{}, d.stopWindowOwned()
	}
	phase := &windowOwnedPhase{nonce: randomID(), driver: d}
	d.phase = phase
	p.mu.Unlock()
	d.failureStepInputSession = d.session
	obs, stepErr := d.base.Step(context.WithValue(ctx, windowOwnedPhaseKey{}, phase), d.fixture, d.session, message)
	p.mu.Lock()
	d.phase = nil
	p.mu.Unlock()
	stored, e := cloneWindowOwned(obs)
	if e != nil {
		return obs, d.stopWindowOwned()
	}
	d.observations = append(d.observations, stored)
	d.nextTurn++
	if obs.Turn.Response.SessionID != "" {
		if d.session != "" && d.session != obs.Turn.Response.SessionID {
			d.failureSessionConflict = true
			return obs, d.stopWindowOwned()
		}
		d.session = obs.Turn.Response.SessionID
	}
	if stepErr != nil || obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" || d.session == "" || obs.Turn.Response.Telemetry == nil || obs.Turn.Response.Telemetry.LangfuseTraceID == "" || !reflect.DeepEqual(obs.Turn.Response.Telemetry.Runtime, d.runtime) {
		return obs, d.stopWindowOwned()
	}
	state := d.ledger.Snapshot()
	if state.Halt != "" || state.Closed || state.Pending != nil {
		return obs, d.stopWindowOwned()
	}
	// Replay only the observed prefix through the SAME origin-typed policy. A
	// safe incomplete task goal remains a negative; wrong preview/effect/unknown
	// evidence halts before the next scripted confirmation can enter Handler.
	prefix, e := cloneWindowOwned(d.caseDefinition)
	if e != nil {
		return obs, d.stopWindowOwned()
	}
	prefix.Scenario.Turns = prefix.Scenario.Turns[:d.nextTurn]
	replay := &replayConversationDriver{episode: ConversationEpisode{Key: prefix.Key(), Fixture: d.fixture, Initial: d.initial, Final: stored.State, Steps: d.observations}}
	assessment := recordCollectionConversation(ctx, prefix, replay)
	if assessment.Disposition != "continue_recorded" {
		// Revoke the next phase immediately, while returning this actual observed
		// turn without inventing a Step error. The outer core must retain the same
		// origin-typed policy issues that replay derives from this raw observation.
		if d.windowQualityMode() {
			if d.latchWindowQualityCandidate(ctx, before, previous) == nil {
				return obs, nil
			}
		}
		_ = d.stopWindowOwned()
		return obs, nil
	}
	return obs, nil
}
func (d *windowOwnedDriver) Final(ctx context.Context, f Fixture, session string) (ConversationState, error) {
	if d == nil {
		return ConversationState{}, fmt.Errorf("missing owned driver")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.windowFailureDiagnostics() != nil {
		d.permit.mu.Lock()
		stopped := d.permit.stopped
		d.permit.mu.Unlock()
		if stopped {
			return ConversationState{}, d.failureFinalLocked(ctx, f, session)
		}
	}
	if ctx == nil || ctx.Err() != nil || !d.setupDone || d.finished || d.finalDone || !reflect.DeepEqual(f, d.fixture) || session != d.session || d.environment.closed {
		return ConversationState{}, d.stopWindowOwned()
	}
	state, e := d.base.Final(ctx, d.fixture, d.session)
	if e != nil {
		return state, d.stopWindowOwned()
	}
	d.final, e = cloneWindowOwned(state)
	if e != nil {
		return state, d.stopWindowOwned()
	}
	d.finalDone = true
	return state, nil
}
func (d *windowOwnedDriver) windowAccountingLedger() *windowAccounting {
	if d == nil {
		return nil
	}
	return d.ledger
}
func (p *windowAcquisitionPermit) stopWindowOwnedLocked() {
	p.stopped = true
	// A failed ownership check must never write through the suspect permit's
	// arbitrary ledger pointer. Only the campaign's actual live ledger is owned.
	if p.campaign != nil {
		p.campaign.mu.Lock()
		owned := p.campaign.ledger
		p.campaign.mu.Unlock()
		if owned != nil {
			_ = windowTransportStop(owned, "invalid_request")
		}
	}
}

// Caller holds permit.mu. This checks live owner/claim/window on every request.
func (p *windowAcquisitionPermit) guardWindowOwnedLocked(d *windowOwnedDriver) error {
	if p == nil || p.stopped || p.campaign == nil || p.ledger == nil || p.active != d || d == nil || d.ledger != p.ledger || d.environment != p.environment || d.observer != p.observer || p.environment == nil || p.environment.closed {
		return fmt.Errorf("owned acquisition stopped")
	}
	if d.policySHA != "" && (d.policySHA != p.campaign.prepared.scope.Bindings.PolicySHA256 || (d.policySHA != collectionPolicySHA() && d.policySHA != windowOwnedQualityPolicySHA())) {
		return fmt.Errorf("owned acquisition policy changed")
	}
	if c := p.campaign; c != nil && c.prepared.scope.Bindings.PolicySHA256 == windowOwnedQualityPolicySHA() {
		if p.acquisition == nil || p.acquisition.validateWindowQualityFiles() != nil {
			return fmt.Errorf("owned quality acquisition persistence changed")
		}
	}
	if d.observer != nil {
		identity := d.observer.EvaluationIdentity()
		stats := d.observer.Stats()
		if identity != p.observerIdentity || !identity.Enabled || stats.Failed != 0 || stats.Dropped != 0 {
			return fmt.Errorf("owned observer stopped")
		}
	}
	c := p.campaign
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.poisoned || c.ledger != p.ledger || p.executionSHA != c.prepared.executionSHA || p.windowIndex != c.index || c.index < 1 || c.index > len(c.prepared.scope.Windows) || c.index > len(c.prepared.contracts) || p.ledger.contract.ClaimSHA256 != c.claimSHA {
		return fmt.Errorf("owned acquisition identity changed")
	}
	if c.inventoryWindowCampaign() != nil || p.ledger.validateOwnedPath() != nil {
		c.poisoned = true
		return fmt.Errorf("owned acquisition persistence changed")
	}
	expectedContract := c.prepared.contracts[c.index-1]
	expectedContract.ClaimSHA256 = c.claimSHA
	if p.ledger.contract != expectedContract {
		return fmt.Errorf("owned window contract changed")
	}
	if len(p.proofs) != p.nextArm || len(p.proofSHA) != p.nextArm {
		return fmt.Errorf("owned arm registry/cursor differs")
	}
	w := c.prepared.scope.Windows[c.index-1]
	total := 2 * (w.LastOrdinal - w.FirstOrdinal + 1)
	if p.nextArm < 0 || p.nextArm >= total || w.FirstOrdinal < 1 || w.LastOrdinal > len(c.prepared.scope.Slots) {
		return fmt.Errorf("owned arm schedule exhausted")
	}
	slot := c.prepared.scope.Slots[w.FirstOrdinal-1+p.nextArm/2]
	if slot != d.slot || slot.ArmOrder[p.nextArm%2] != d.arm {
		return fmt.Errorf("owned arm order changed")
	}
	state := p.ledger.Snapshot()
	if state.Closed || state.Halt != "" {
		return fmt.Errorf("owned ledger stopped")
	}
	return nil
}
func (d *windowOwnedDriver) windowNextRequest(ctx context.Context, provider string) (windowRequestIdentity, error) {
	if d == nil || d.permit == nil {
		return windowRequestIdentity{}, fmt.Errorf("missing owned phase")
	}
	p := d.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	fail := func() (windowRequestIdentity, error) {
		p.stopWindowOwnedLocked()
		return windowRequestIdentity{}, fmt.Errorf("owned phase permission rejected")
	}
	if ctx == nil || ctx.Err() != nil || d.phase == nil || d.phase.nonce == "" || d.phase.driver != d || ctx.Value(windowOwnedPhaseKey{}) != d.phase || !d.setupDone || d.nextTurn < 0 || d.nextTurn >= d.slot.UserTurns {
		return fail()
	}
	if p.guardWindowOwnedLocked(d) != nil {
		return fail()
	}
	attempt := 0
	model := ""
	switch provider {
	case "model":
		if d.phase.model >= 5 {
			return fail()
		}
		d.phase.model++
		attempt = d.phase.model
		model = "deepseek-flash"
	case "jev":
		if d.arm != "jev_selective" || d.phase.jev >= 1 {
			return fail()
		}
		d.phase.jev++
		attempt = d.phase.jev
		model = "jev-1.13.0"
	default:
		return fail()
	}
	return windowRequestIdentity{Provider: provider, Model: model, CaseKey: d.slot.CaseKey, Pass: d.slot.Pass, Arm: d.arm, Turn: d.nextTurn + 1, Attempt: attempt}, nil
}
func (d *windowOwnedDriver) finishWindowOwnedArm(ctx context.Context, record collectionRecording) (*windowOwnedArmProof, error) {
	if d == nil {
		return nil, fmt.Errorf("missing owned driver")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return nil, d.stopWindowOwned()
	}
	d.finished = true
	if d.server != nil {
		defer d.server.Close()
	}
	fail := func() (*windowOwnedArmProof, error) {
		if d.windowFailureDiagnostics() != nil {
			return nil, d.cleanupFailureLocked(ctx)
		}
		_ = d.cleanupWindowOwnedSession(ctx)
		return nil, d.stopWindowOwned()
	}
	if ctx == nil || ctx.Err() != nil || !d.setupDone || !d.finalDone || verifyCollectionRecording(record) != nil {
		return fail()
	}
	if !sameMaterialWindowOwned(record.Result, d) {
		return fail()
	}
	retained, e := cloneWindowOwned(record)
	if e != nil {
		return fail()
	}
	retained.Result.Fixture.APIKey = d.fixture.APIKey
	d.retained = &retained
	if d.qualityCandidate != nil {
		return d.finishWindowQualityOwned(ctx, retained)
	}
	// Preserve the authentic final record (including a v1 global policy halt)
	// before deleting the owned Redis session; only complete safe arms mint proof.
	if d.nextTurn != len(d.caseDefinition.Scenario.Turns) || record.Disposition != "continue_recorded" || record.Result.SetupError || record.Result.HTTPFailures != 0 {
		return fail()
	}
	raw, e := json.Marshal(retained)
	if e != nil {
		return fail()
	}
	seal := conversationHash(raw)
	if d.windowFailureDiagnostics() != nil {
		d.failure.mu.Lock()
		d.failure.evidence.CleanupAttempted = true
		d.failure.mu.Unlock()
	}
	if e = d.cleanupWindowOwnedSession(ctx); e != nil {
		if d.windowFailureDiagnostics() != nil {
			d.failure.mu.Lock()
			d.failure.evidence.Cleanup = "failed"
			d.failure.mu.Unlock()
			d.captureFailureEvidenceLocked()
		}
		return nil, d.stopWindowOwned()
	}
	if d.windowFailureDiagnostics() != nil {
		d.failure.mu.Lock()
		d.failure.evidence.Cleanup = "verified"
		d.failure.mu.Unlock()
	}
	if e = d.observer.ForceFlush(ctx); e != nil {
		if d.windowFailureDiagnostics() != nil {
			d.captureFailureEvidenceLocked()
		}
		return nil, d.stopWindowOwned()
	}
	stats := d.observer.Stats()
	if d.observer.EvaluationIdentity() != d.permit.observerIdentity || stats.Failed != 0 || stats.Dropped != 0 || stats.QueueDepth != 0 || stats.Turns-d.observerStart.Turns != uint64(d.nextTurn) || stats.SampledTurns-d.observerStart.SampledTurns != uint64(d.nextTurn) || stats.Accepted != stats.Exported {
		return nil, d.stopWindowOwned()
	}
	p := d.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.guardWindowOwnedLocked(d) != nil {
		return nil, fmt.Errorf("owned permit stopped before proof")
	}
	proof := &windowOwnedArmProof{permit: p, driver: d, slot: d.slot, arm: d.arm, recording: retained, recordingSHA: seal}
	p.proofs = append(p.proofs, proof)
	p.proofSHA = append(p.proofSHA, seal)
	p.nextArm++
	p.active = nil
	return proof, nil
}
func (d *windowOwnedDriver) cleanupWindowOwnedSession(ctx context.Context) error {
	if d.session == "" {
		return nil
	}
	if ctx == nil || ctx.Err() != nil || d.environment == nil || d.environment.closed || d.environment != d.permit.environment {
		return fmt.Errorf("owned session cleanup unavailable")
	}
	key := "agent:session:" + d.session
	if e := d.environment.rdb.Del(ctx, key).Err(); e != nil {
		return e
	}
	n, e := d.environment.rdb.Exists(ctx, key).Result()
	if e != nil || n != 0 {
		return fmt.Errorf("owned session cleanup not verified")
	}
	return nil
}
func sameMaterialWindowOwned(record ConversationResult, d *windowOwnedDriver) bool {
	if !reflect.DeepEqual(record.Fixture, d.fixture) {
		return false
	}
	type material struct {
		Case           ConversationCase
		Fixture        Fixture
		Initial, Final ConversationState
		Steps          []ConversationObservation
	}
	normalize := func(steps []ConversationObservation) []ConversationObservation {
		copy, e := cloneWindowOwned(steps)
		if e != nil {
			return nil
		}
		for i := range copy {
			copy[i].Turn.LatencyMS = 0
			copy[i].Turn.Stage = ""
		}
		return copy
	}
	want := material{d.caseDefinition, d.fixture, d.initial, d.final, normalize(d.observations)}
	got := material{record.Case, record.Fixture, record.Initial, record.Final, normalize(record.Observations)}
	a, e := json.Marshal(want)
	if e != nil {
		return false
	}
	b, e := json.Marshal(got)
	return e == nil && string(a) == string(b)
}
func validateWindowOwnedArmProof(permit *windowAcquisitionPermit, proof *windowOwnedArmProof) error {
	if permit == nil || proof == nil {
		return fmt.Errorf("missing owned proof")
	}
	permit.mu.Lock()
	defer permit.mu.Unlock()
	fail := func() error {
		permit.stopWindowOwnedLocked()
		return fmt.Errorf("owned proof identity/evidence/seal differs")
	}
	if permit.stopped || permit.campaign == nil || permit.ledger == nil || proof.permit != permit || proof.driver == nil || proof.driver.permit != permit || proof.driver.ledger != permit.ledger || proof.driver.retained == nil || proof.driver.environment != permit.environment || proof.driver.observer != permit.observer || len(permit.proofs) != permit.nextArm || len(permit.proofSHA) != permit.nextArm || !sameMaterialWindowOwned(proof.recording.Result, proof.driver) {
		return fail()
	}
	c := permit.campaign
	c.mu.Lock()
	validOwner := !c.closed && !c.poisoned && c.ledger == permit.ledger && c.prepared.executionSHA == permit.executionSHA && c.index == permit.windowIndex && c.index > 0 && c.index <= len(c.prepared.scope.Windows) && c.index <= len(c.prepared.contracts)
	var w fullWindowDeclaration
	var slots []fullWindowSlot
	if validOwner {
		expected := c.prepared.contracts[c.index-1]
		expected.ClaimSHA256 = c.claimSHA
		validOwner = permit.ledger.contract == expected
		w = c.prepared.scope.Windows[c.index-1]
		slots = c.prepared.scope.Slots
	}
	c.mu.Unlock()
	if !validOwner {
		return fail()
	}
	state := permit.ledger.Snapshot()
	if state.Halt != "" || state.Pending != nil {
		return fail()
	}
	if permit.observer == nil || permit.observer.EvaluationIdentity() != permit.observerIdentity {
		return fail()
	}
	stats := permit.observer.Stats()
	if stats.Failed != 0 || stats.Dropped != 0 || stats.QueueDepth != 0 || stats.Accepted != stats.Exported {
		return fail()
	}
	for i, known := range permit.proofs {
		if known == proof {
			ordinal := w.FirstOrdinal - 1 + i/2
			if ordinal < 0 || ordinal >= len(slots) || ordinal >= w.LastOrdinal {
				return fail()
			}
			slot := slots[ordinal]
			raw, e := json.Marshal(proof.recording)
			retained, e2 := json.Marshal(proof.driver.retained)
			if e != nil || e2 != nil || conversationHash(raw) != permit.proofSHA[i] || conversationHash(retained) != permit.proofSHA[i] || proof.recordingSHA != permit.proofSHA[i] || proof.slot != slot || proof.slot != proof.driver.slot || proof.arm != slot.ArmOrder[i%2] || proof.arm != proof.driver.arm || proof.recording.Result.Case.Key() != slot.CaseKey {
				return fail()
			}
			if _, err := windowQualityProofEvidence(proof); err != nil {
				return fail()
			}
			return nil
		}
	}
	return fail()
}

// Read-only source data for a closed packet-capture substitute, never authority.
// Step already holds driver.mu during synchronous HTTP, so the phase lifecycle
// is checked with permit.mu (the lock that creates/clears that phase).
func windowOwnedContextFrame(ctx context.Context) (ConversationCase, error) {
	if ctx == nil || ctx.Err() != nil {
		return ConversationCase{}, fmt.Errorf("missing live owned capture frame")
	}
	phase, ok := ctx.Value(windowOwnedPhaseKey{}).(*windowOwnedPhase)
	if !ok || phase == nil || phase.driver == nil || phase.driver.permit == nil {
		return ConversationCase{}, fmt.Errorf("missing live owned capture frame")
	}
	d := phase.driver
	p := d.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	if d.phase != phase || phase.nonce == "" || p.guardWindowOwnedLocked(d) != nil || d.caseDefinition.Key() != d.slot.CaseKey {
		return ConversationCase{}, fmt.Errorf("stale or foreign owned capture frame")
	}
	return cloneWindowOwned(d.caseDefinition)
}
