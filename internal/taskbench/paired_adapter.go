package taskbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

type PairModelSettings struct{ BaseURL, APIKey, Model, Protocol string }
type AgentPairOptions struct {
	Observer                *observability.Observer // required for recorded_run; caller owns flush/shutdown
	AllowExecution          bool                    // false is always side-effect-free
	RatesVerified           bool                    // recorded_run requires operator verification for this channel
	StopOnStructuralFailure bool                    // opt-in evaluator stop; legacy defaults remain unchanged
	StopOnObserverFailure   bool                    // opt-in per-arm flush; repeated scope stops before another arm
	StopOnFactFailure       bool                    // opt-in deterministic mismatch stop after durable first arm
	StopOnJEVFailure        bool                    // reviewed full-reservation policy forbids model fallback after a JEV failure
	AllowJEVCalls           bool
	MaxJEVCalls             int
	DefinitionsDir          string
	Config                  PairRunConfig
	Environment             *PairedFixtureEnvironment
	Budget                  *PairBudgetTransport
	Model                   PairModelSettings
	JEVFactory              func() jev.Evaluator // explicit and trusted; no credential discovery
}

type disabledPairTransport struct{}

func (disabledPairTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("preflight cannot call providers")
}

// PrepareAgentPairConfig computes actual hashes without DB/provider calls.
func PrepareAgentPairConfig(s ConversationSuite, p PairedProfiles, m PairModelSettings, evidence, revision, jevModel string) (PairRunConfig, error) {
	var cfg PairRunConfig
	u, err := url.Parse(m.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return cfg, fmt.Errorf("explicit provider endpoint required")
	}
	client, err := agent.NewEvaluationLLMClient(m.BaseURL, m.APIKey, m.Model, m.Protocol, &http.Client{Transport: disabledPairTransport{}}, 128)
	if err != nil {
		return cfg, err
	}
	h := agent.NewHandler(nil, agent.NewToolbox(nil), client, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := h.EvaluationRuntimeMetadata()
	profiles := map[string]string{}
	for _, c := range s.Cases {
		if _, ok := p.archive.Accounts[c.Scenario.Profile]; !ok {
			return cfg, fmt.Errorf("profile not pinned")
		}
		profiles[c.Scenario.Profile] = p.SHA256
	}
	a := ConversationComparisonConfig{RoutingMode: "model", EvidenceKind: evidence, DefinitionSHA256: s.DefinitionSHA256, ManifestSHA256: s.ManifestSHA256, ProfileSHA256: profiles, Model: m.Model, ProviderProtocol: m.Protocol, PromptSHA256: r.PromptSHA256, ToolSchemaSHA256: r.ToolsSHA256, OutputBudget: 128, ExecutionRevision: revision}
	b := a
	a.ThinkingMode = r.ThinkingMode
	b.ThinkingMode = r.ThinkingMode
	b.RoutingMode = "jev_selective"
	cfg = PairRunConfig{Schema: "paired-run.v1", Suite: s, Model: a, Selective: b, ProviderEndpointSHA256: r.EndpointSHA256, JEVModel: jevModel}
	return cfg, validatePairRun(cfg)
}

// RunAgentPair uses the real handler and an owned fixture environment. Default
// invocation cannot create a journal, construct dependencies or call providers.
func RunAgentPair(ctx context.Context, path string, o AgentPairOptions) (runErr error) {
	return runAgentPair(ctx, path, o, false)
}

func runAgentPair(ctx context.Context, path string, o AgentPairOptions, fullCustomer bool) (runErr error) {
	if !o.AllowExecution {
		return fmt.Errorf("agent pair execution disabled")
	}
	if (fullCustomer && (o.Config.SelectionScope != FullCustomerPrimaryScope || !comparisonDigest(o.Config.SelectionSHA256))) || (!fullCustomer && (o.Config.SelectionScope != "" || o.Config.SelectionSHA256 != "")) {
		return fmt.Errorf("pair source selection differs from entry point")
	}
	if fullCustomer && (o.Observer == nil || !o.StopOnStructuralFailure || !o.StopOnObserverFailure || !o.StopOnFactFailure || !o.StopOnJEVFailure) {
		return fmt.Errorf("full selection requires observer and all stop guards")
	}
	if o.Config.Model.EvidenceKind == "recorded_run" && o.Observer == nil {
		return fmt.Errorf("recorded run requires an explicit Langfuse observer")
	}
	if o.Environment == nil || o.Environment.closed || o.Budget == nil || o.JEVFactory == nil || !o.AllowJEVCalls || o.MaxJEVCalls < 1 || o.MaxJEVCalls > 256 {
		return fmt.Errorf("owned fixtures, budget and bounded explicit JEV authorization required")
	}
	if o.Config.Model.EvidenceKind == "recorded_run" && (!o.Config.AllowPaid || !o.RatesVerified || o.Model.APIKey == "" || o.Config.JEVModel == "") {
		return fmt.Errorf("recorded run price/key/version authorization missing")
	}
	if o.Config.Model.EvidenceKind == "local_stub" {
		u, err := url.Parse(o.Model.BaseURL)
		if err != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			return fmt.Errorf("local_stub provider must be literal loopback")
		}
	}
	want, err := PrepareAgentPairConfig(o.Config.Suite, o.Environment.profiles, o.Model, o.Config.Model.EvidenceKind, o.Config.Model.ExecutionRevision, o.Config.JEVModel)
	if err != nil {
		return err
	}
	want.AllowPaid = o.Config.AllowPaid
	var frozenSuite ConversationSuite
	if fullCustomer {
		source, loadErr := LoadConversations(o.DefinitionsDir)
		if loadErr != nil {
			return loadErr
		}
		var digest string
		frozenSuite, digest, err = fullCustomerSelection(source)
		if err != nil {
			return err
		}
		want.SelectionScope, want.SelectionSHA256 = FullCustomerPrimaryScope, digest
	}
	if err := validatePairRun(o.Config); err != nil {
		return err
	}
	want.ArmOrder = o.Config.ArmOrder
	want.PairID = o.Config.PairID
	want.BlockID = o.Config.BlockID
	if !reflect.DeepEqual(want, o.Config) {
		return fmt.Errorf("agent configuration differs before provider calls")
	}
	if o.Budget.contract != PairRunContract(o.Config) || o.Budget.limits.MaxOutputTokens != 128 {
		return fmt.Errorf("agent budget contract differs")
	}
	if !fullCustomer {
		source, loadErr := LoadConversations(o.DefinitionsDir)
		if loadErr != nil {
			return loadErr
		}
		plan, prepareErr := PrepareSmallPair(source)
		if prepareErr != nil {
			return prepareErr
		}
		frozenSuite = plan.Suite
	}
	frozen := map[string]ConversationCase{}
	for _, c := range frozenSuite.Cases {
		frozen[c.Key()] = c
	}
	if o.Config.Suite.DefinitionSHA256 != frozenSuite.DefinitionSHA256 || o.Config.Suite.ManifestSHA256 != frozenSuite.ManifestSHA256 {
		return fmt.Errorf("source digest differs")
	}
	for _, c := range o.Config.Suite.Cases {
		if !reflect.DeepEqual(c, frozen[c.Key()]) {
			if !fullCustomer {
				return fmt.Errorf("case differs from frozen pilot")
			}
			return fmt.Errorf("case differs from frozen selection")
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	token := "paired-" + randomID()
	server := httptest.NewServer(admin.Handler(o.Environment.st, log, token))
	defer server.Close()
	guard, err := openPairJEVGuard(path+".jev.jsonl", PairRunContract(o.Config), o.MaxJEVCalls, o.Config.JEVModel, o.JEVFactory)
	if err != nil {
		return err
	}
	o.Budget.setCompanion(guard.Snapshot, o.StopOnJEVFailure)
	defer func() { o.Budget.setCompanion(nil, false); runErr = errors.Join(runErr, guard.Close()) }()
	factory := func(_ context.Context, arm string, _ ConversationCase) (ConversationDriver, error) {
		oracle := &pairedStoreOracle{env: o.Environment}
		llm, err := agent.NewEvaluationLLMClient(o.Model.BaseURL, o.Model.APIKey, o.Model.Model, o.Model.Protocol, &http.Client{Transport: o.Budget, Timeout: 90 * time.Second}, 128)
		if err != nil {
			return nil, err
		}
		h := agent.NewHandler(nil, agent.NewToolbox(agent.NewAdminClientWithToken(server.URL, token)), llm, o.Environment.st, o.Environment.rdb, log)
		h.SetObserver(o.Observer)
		if err = h.SetRoutingMode(arm); err != nil {
			return nil, err
		}
		h.SetAdminAccess(token, 0)
		if arm == "jev_selective" {
			h.SetJev(guard)
		}
		wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Agent-Internal", token)
			r.Header.Set("X-Agent-Tenant", fmt.Sprint(oracle.f.TenantID))
			h.ServeHTTP(w, r)
		})
		return NewHandlerConversationDriver(wrapper, oracle)
	}
	err = runPreparedPair(ctx, path, o.Config, o.Budget, factory, o.Observer, o.StopOnStructuralFailure, o.StopOnObserverFailure, o.StopOnFactFailure)
	if o.Observer == nil {
		return err
	}
	flushErr := o.Observer.ForceFlush(ctx)
	stats := o.Observer.Stats()
	if flushErr != nil || stats.Dropped != 0 || stats.Failed != 0 {
		return errors.Join(err, fmt.Errorf("langfuse export stopped the run: dropped=%d failed=%d: %v", stats.Dropped, stats.Failed, flushErr))
	}
	return err
}
