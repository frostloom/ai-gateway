package taskbench

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/jev"
	"os"
	"path/filepath"
	"testing"
)

func TestPairedRuntimeDisabledBeforeSideEffects(t *testing.T) {
	_, e := OpenPairedFixtureEnvironment(context.Background(), PairedFixtureOptions{})
	if e == nil {
		t.Fatal("default environment created database")
	}
	p := filepath.Join(t.TempDir(), "run")
	if e = RunAgentPair(context.Background(), p, AgentPairOptions{}); e == nil {
		t.Fatal("default adapter ran")
	}
	if _, e = LoadPairedProfiles("../../eval/runtime-profiles/2026-10-13-six-profiles-v1"); e != nil {
		t.Fatal(e)
	}
}

func TestPairJEVGuardPersistsAttemptsOnResume(t *testing.T) {
	p := filepath.Join(t.TempDir(), "jev")
	calls := 0
	f := func() jev.Evaluator { calls++; return jev.NewFake(localJevScript("get_balance", .99)) }
	b, e := openPairJEVGuard(p, "contract", 1, "", f)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.Evaluate(context.Background(), "balance", nil); e != nil {
		t.Fatal(e)
	}
	b.Close()
	b, e = openPairJEVGuard(p, "contract", 1, "", f)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if _, e = b.Evaluate(context.Background(), "balance", nil); e == nil || calls != 1 {
		t.Fatal("resume reset JEV limit")
	}
	if _, e = os.Stat(p); e != nil {
		t.Fatal(e)
	}
}
