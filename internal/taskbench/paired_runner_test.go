package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pairTestConfig() PairRunConfig {
	s, a, b := comparisonInput()
	return PairRunConfig{Schema: "paired-run.v1", Suite: s, Model: a.Config, Selective: b.Config}
}
func TestPairRunResumeDoesNotRepeatArmsAndKeepsPending(t *testing.T) {
	cfg := pairTestConfig()
	p := filepath.Join(t.TempDir(), "pair.jsonl")
	calls := 0
	factory := func(_ context.Context, _ string, _ ConversationCase) (ConversationDriver, error) {
		calls++
		return rechargeDriver(), nil
	}
	if err := RunPreparedPair(context.Background(), p, cfg, nil, factory); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("arms=%d", calls)
	}
	if err := RunPreparedPair(context.Background(), p, cfg, nil, factory); err != nil || calls != 2 {
		t.Fatalf("resume resent arms: %d %v", calls, err)
	}
	r, err := ReportPreparedPair(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Rows[0].Status != "needs_review" || r.CompletionRate != nil || r.EffectClaimsAllowed {
		t.Fatalf("pending turned into success: %+v", r)
	}
}
func TestPairRunUnauthorizedOrChangedContractHasNoSideEffects(t *testing.T) {
	cfg := pairTestConfig()
	cfg.Model.EvidenceKind = "recorded_run"
	cfg.Selective.EvidenceKind = "recorded_run"
	p := filepath.Join(t.TempDir(), "pair")
	calls := 0
	f := func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		calls++
		return rechargeDriver(), nil
	}
	if err := RunPreparedPair(context.Background(), p, cfg, nil, f); err == nil || calls != 0 {
		t.Fatal("paid run not guarded")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("unauthorized run created journal")
	}
	cfg = pairTestConfig()
	if err := RunPreparedPair(context.Background(), p, cfg, nil, f); err != nil {
		t.Fatal(err)
	}
	cfg.Model.Model = "changed"
	cfg.Selective.Model = "changed"
	if err := RunPreparedPair(context.Background(), p, cfg, nil, f); err == nil || calls != 2 {
		t.Fatal("changed config resumed")
	}
}
func TestPairRunFailedFactoryRetainedAndInterruptedArmNotRepeated(t *testing.T) {
	cfg := pairTestConfig()
	p := filepath.Join(t.TempDir(), "pair")
	calls := 0
	f := func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		calls++
		return nil, fmt.Errorf("fixture failed")
	}
	if err := RunPreparedPair(context.Background(), p, cfg, nil, f); err != nil {
		t.Fatal(err)
	}
	r, err := ReportPreparedPair(p)
	if err != nil || len(r.Rows) != 2 || r.Rows[0].Status != "setup_failed" {
		t.Fatalf("failure lost: %+v %v", r, err)
	}
	// Simulate crash after the durable start, with no matching finish.
	file, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	e := pairRunEvent{Kind: "start", Key: "unexpected", Arm: "model"}
	raw, _ := json.Marshal(e)
	file.Write(append(raw, '\n'))
	file.Close()
	if err := RunPreparedPair(context.Background(), p, cfg, nil, f); err == nil || calls != 2 {
		t.Fatal("ambiguous arm automatically repeated")
	}
}

type pairFaultDriver struct {
	ConversationDriver
	stage string
	next  int
}

func (d *pairFaultDriver) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	f, s, e := d.ConversationDriver.Setup(ctx, c)
	if d.stage == "setup" {
		e = fmt.Errorf("valid initial but failed setup")
	}
	return f, s, e
}
func (d *pairFaultDriver) Step(ctx context.Context, f Fixture, s, m string) (ConversationObservation, error) {
	o, e := d.ConversationDriver.Step(ctx, f, s, m)
	d.next++
	if d.stage == "step" && d.next == 1 {
		e = fmt.Errorf("oracle failed")
	}
	return o, e
}
func (d *pairFaultDriver) Final(ctx context.Context, f Fixture, s string) (ConversationState, error) {
	v, e := d.ConversationDriver.Final(ctx, f, s)
	if d.stage == "final" {
		e = fmt.Errorf("final oracle failed")
	}
	return v, e
}
func (d *pairFaultDriver) Close() error {
	if d.stage == "cleanup" {
		return fmt.Errorf("cleanup failed")
	}
	return nil
}
func TestPairReportPreservesExecutionFailures(t *testing.T) {
	for _, stage := range []string{"setup", "step", "final", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "pair")
			cfg := pairTestConfig()
			if e := RunPreparedPair(context.Background(), p, cfg, nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
				return &pairFaultDriver{ConversationDriver: rechargeDriver(), stage: stage}, nil
			}); e != nil {
				t.Fatal(e)
			}
			r, e := ReportPreparedPair(p)
			if e != nil {
				t.Fatal(e)
			}
			if r.FinishedArms != 2 || len(r.Rows) != 2 || r.Rows[0].StructuralPass || (r.Rows[0].Status != "failed" && r.Rows[0].Status != "setup_failed") {
				t.Fatalf("%s execution failure lost: %+v", stage, r)
			}
		})
	}
}
func TestPairRunRejectsFreshBudgetOnResume(t *testing.T) {
	cfg := pairTestConfig()
	dir := t.TempDir()
	p := filepath.Join(dir, "pair")
	limits := pairedLimits()
	b, e := OpenPairBudget(filepath.Join(dir, "original"), PairRunContract(cfg), limits, http.DefaultTransport)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	f := func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		calls++
		cancel()
		return rechargeDriver(), nil
	}
	_ = RunPreparedPair(ctx, p, cfg, b, f)
	b.Close()
	if calls != 1 {
		t.Fatalf("expected partial run: %d", calls)
	}
	b, e = OpenPairBudget(filepath.Join(dir, "fresh"), PairRunContract(cfg), limits, http.DefaultTransport)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if e = RunPreparedPair(context.Background(), p, cfg, b, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		calls++
		return rechargeDriver(), nil
	}); e == nil || calls != 1 {
		t.Fatal("fresh ledger reset run budget")
	}
}

func TestPairReportRejectsSyntheticReviewInPartialRecordedRun(t *testing.T) {
	cfg := pairTestConfig()
	cfg.Model.EvidenceKind = "recorded_run"
	cfg.Selective.EvidenceKind = "recorded_run"
	r := RunConversation(context.Background(), cfg.Suite.Cases[0], rechargeDriver())
	r.Observations[0].Reviews = []ConversationReview{{Method: "local_stub"}}
	p := filepath.Join(t.TempDir(), "pair")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range []pairRunEvent{{Kind: "config", Config: &cfg}, {Kind: "start", Key: cfg.Suite.Cases[0].Key(), Arm: "model"}, {Kind: "finish", Key: cfg.Suite.Cases[0].Key(), Arm: "model", Result: &r}} {
		if e = writePairEvent(f, event); e != nil {
			t.Fatal(e)
		}
	}
	f.Close()
	if _, e = ReportPreparedPair(p); e == nil {
		t.Fatal("partial recorded report accepted synthetic review")
	}
}

type pairSecretErrorDriver struct{ ConversationDriver }

func (d *pairSecretErrorDriver) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	f, s, _ := d.ConversationDriver.Setup(ctx, c)
	return f, s, fmt.Errorf("secret-marker-from-DSN")
}
func TestPairJournalDoesNotPersistOracleErrorSecrets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pair")
	if e := RunPreparedPair(context.Background(), p, pairTestConfig(), nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		return &pairSecretErrorDriver{rechargeDriver()}, nil
	}); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "secret-marker-from-DSN") {
		t.Fatal("dependency error payload leaked into journal")
	}
}

func TestPairRunNilFactoryDriverIsARecordedSetupFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pair")
	defer func() {
		if value := recover(); value != nil {
			t.Errorf("nil driver panicked instead of being recorded: %v", value)
		}
	}()
	if e := RunPreparedPair(context.Background(), p, pairTestConfig(), nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) { return nil, nil }); e != nil {
		t.Fatal(e)
	}
	r, e := ReportPreparedPair(p)
	if e != nil || r.Rows[0].Status != "setup_failed" {
		t.Fatal("nil driver failure lost")
	}
}
