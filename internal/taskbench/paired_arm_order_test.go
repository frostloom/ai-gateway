package taskbench

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/jev"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExplicitArmOrderLocalFirstFailure(t *testing.T) {
	for _, kind := range []string{"provider", "jev", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			o := localCampaignOptions(t)
			deps, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer deps.close()
			source, err := LoadConversations(o.DefinitionsDir)
			if err != nil {
				t.Fatal(err)
			}
			small, err := PrepareSmallPair(source)
			if err != nil {
				t.Fatal(err)
			}
			suite := small.Suite
			for _, c := range small.Suite.Cases {
				if c.Key() == "balance-020/explicit_scope_followup" {
					suite.Cases = []ConversationCase{c}
				}
			}
			if len(suite.Cases) != 1 {
				t.Fatal("fixed case not found")
			}
			model := PairModelSettings{BaseURL: "http://127.0.0.1/local-rehearsal", APIKey: "synthetic-only", Model: "local-rehearsal", Protocol: "anthropic"}
			cfg, err := PrepareAgentPairConfig(suite, o.Environment.profiles, model, "local_stub", "test-local-rehearsal", "")
			if err != nil {
				t.Fatal(err)
			}
			cfg.ArmOrder = []string{"jev_selective", "model"}
			if kind == "invalid" {
				cfg.ArmOrder = []string{}
			}
			dir := t.TempDir()
			limits := pairedLimits()
			limits.MaxCalls = 12
			limits.MaxOutputTokens = 128
			limits.MaxRequestBytes = 16384
			limits.InputTokenCeiling = 16384
			limits.MaxMicroCNY = 1000000
			calls := 0
			up := deps.model(suite.Cases[0])
			transport := pairedRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				response, e := up.RoundTrip(r)
				if e == nil && kind == "provider" {
					response.StatusCode = 503
				}
				return response, e
			})
			budget, err := OpenPairBudget(filepath.Join(dir, "budget"), PairRunContract(cfg), limits, transport)
			if err != nil {
				t.Fatal(err)
			}
			defer budget.Close()
			judges := 0
			path := filepath.Join(dir, "run.jsonl")
			err = RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, StopOnStructuralFailure: true, StopOnJEVFailure: true, AllowJEVCalls: true, MaxJEVCalls: 1, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: deps.observer, JEVFactory: func() jev.Evaluator {
				judges++
				if kind == "jev" {
					return jev.NewFake()
				}
				return deps.judge(suite.Cases[0])
			}})
			if err == nil {
				t.Fatal("failure accepted")
			}
			if kind == "invalid" {
				if calls != 0 || judges != 0 {
					t.Fatal("invalid config invoked dependency")
				}
				if _, e := os.Stat(path); !os.IsNotExist(e) {
					t.Fatal("invalid created journal")
				}
				if _, e := os.Stat(path + ".jev.jsonl"); !os.IsNotExist(e) {
					t.Fatal("invalid created JEV guard")
				}
				return
			}
			if judges != 1 || (kind == "jev" && calls != 0) || (kind == "provider" && calls != 1) {
				t.Fatalf("failure continued calls%d judges%d", calls, judges)
			}
			report, e := ReportPreparedPair(path)
			if e != nil || report.FinishedArms != 1 || report.UnrunArms != 1 {
				t.Fatal("first failure not durable", e)
			}
			j, e := loadPairJournal(path)
			if e != nil || j.finished[suite.Cases[0].Key()+":jev_selective"].Result == nil {
				t.Fatal("wrong arm ran", e)
			}
		})
	}
}

func TestExplicitArmOrderLegacyBytesAndBudget(t *testing.T) {
	cfg := pairTestConfig()
	raw, _ := json.Marshal(cfg)
	forward := explicitTestOrder(t, cfg, []string{"model", "jev_selective"})
	explicit, _ := json.Marshal(forward)
	old := strings.TrimSuffix(string(explicit), `,"arm_order":["model","jev_selective"]}`) + "}"
	if old != string(raw) || strings.Contains(string(raw), "arm_order") || PairRunContract(cfg) != conversationHash(raw) {
		t.Fatal("omitted order changed historical serialization")
	}
	cfg.Suite.Cases = append(cfg.Suite.Cases, cfg.Suite.Cases[0])
	order := pairOrder(cfg)
	if order[0].Arm != "model" || order[2].Arm != "jev_selective" {
		t.Fatal("legacy parity changed")
	}
	cfg = pairTestConfig()
	dir := t.TempDir()
	limits := pairedLimits()
	limits.MaxOutputTokens = cfg.Model.OutputBudget
	budget, err := OpenPairBudget(filepath.Join(dir, "budget"), PairRunContract(cfg), limits, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer budget.Close()
	calls := 0
	err = RunPreparedPair(context.Background(), filepath.Join(dir, "pair"), forward, budget, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		calls++
		return rechargeDriver(), nil
	})
	if err == nil || calls != 0 {
		t.Fatal("explicit order reused historical budget")
	}
	if _, err := os.Stat(filepath.Join(dir, "pair")); !os.IsNotExist(err) {
		t.Fatal("mismatched budget created journal")
	}
}

func explicitTestOrder(t *testing.T, cfg PairRunConfig, order []string) PairRunConfig {
	t.Helper()
	raw, _ := json.Marshal(cfg)
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	fields["arm_order"], _ = json.Marshal(order)
	raw, _ = json.Marshal(fields)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestExplicitArmOrderExecutionAndBinding(t *testing.T) {
	for _, order := range [][]string{{"model", "jev_selective"}, {"jev_selective", "model"}} {
		t.Run(order[0], func(t *testing.T) {
			cfg := explicitTestOrder(t, pairTestConfig(), order)
			path := filepath.Join(t.TempDir(), "pair")
			var started []string
			factory := func(_ context.Context, arm string, _ ConversationCase) (ConversationDriver, error) {
				started = append(started, arm)
				return rechargeDriver(), nil
			}
			if err := RunPreparedPair(context.Background(), path, cfg, nil, factory); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(started, order) {
				t.Fatalf("actual factory order %v want %v", started, order)
			}
			raw, _ := os.ReadFile(path)
			var saved []string
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var e pairRunEvent
				json.Unmarshal([]byte(line), &e)
				if e.Kind == "start" {
					saved = append(saved, e.Arm)
				}
			}
			if !reflect.DeepEqual(saved, order) {
				t.Fatal("journal order differs", saved)
			}
			legacy := pairTestConfig()
			if PairRunContract(cfg) == PairRunContract(legacy) {
				t.Fatal("explicit order not bound")
			}
			if err := RunPreparedPair(context.Background(), path, legacy, nil, factory); err == nil || len(started) != 2 {
				t.Fatal("changed order resumed")
			}
			opposite := explicitTestOrder(t, legacy, []string{order[1], order[0]})
			if err := RunPreparedPair(context.Background(), path, opposite, nil, factory); err == nil || len(started) != 2 {
				t.Fatal("opposite explicit order resumed")
			}
		})
	}
}

func TestExplicitArmOrderInvalidBeforeFiles(t *testing.T) {
	for _, order := range [][]string{{}, {"model"}, {"model", "model"}, {"jev_selective", "unknown"}, {"model", "jev_selective", "model"}} {
		cfg := explicitTestOrder(t, pairTestConfig(), order)
		p := filepath.Join(t.TempDir(), "pair")
		calls := 0
		err := RunPreparedPair(context.Background(), p, cfg, nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
			calls++
			return rechargeDriver(), nil
		})
		if err == nil || calls != 0 {
			t.Fatalf("invalid %v executed", order)
		}
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Fatal("invalid order created journal")
		}
	}
}

func TestExplicitArmOrderReverseCleanupStops(t *testing.T) {
	cfg := explicitTestOrder(t, pairTestConfig(), []string{"jev_selective", "model"})
	p := filepath.Join(t.TempDir(), "pair")
	var arms []string
	err := runPreparedPair(context.Background(), p, cfg, nil, func(_ context.Context, arm string, _ ConversationCase) (ConversationDriver, error) {
		arms = append(arms, arm)
		return &pairFaultDriver{ConversationDriver: rechargeDriver(), stage: "cleanup"}, nil
	}, nil, true)
	if err == nil || !reflect.DeepEqual(arms, []string{"jev_selective"}) {
		t.Fatalf("reverse failure continued %v %v", arms, err)
	}
	report, e := ReportPreparedPair(p)
	if e != nil || report.FinishedArms != 1 || report.UnrunArms != 1 {
		t.Fatal("failed reverse prefix lost", e)
	}
}
