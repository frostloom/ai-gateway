package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWindowV15PhaseBoundsCloneAndNoAuthority(t *testing.T) {
	s := &FullWindowActualSession{ready: &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v15"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	sentinel := errors.New("must-not-appear-secret/path")
	for i := 0; i < 20; i++ {
		s.recordPhaseV15(ctx, "history", time.Now(), sentinel)
	}
	got := s.Status()
	if len(got.PhaseTimingsV15) != 16 {
		t.Fatalf("unbounded diagnostics: %d", len(got.PhaseTimingsV15))
	}
	for _, p := range got.PhaseTimingsV15 {
		if p.Stage != "history" || p.Outcome != "error" || p.DurationMS < 0 || p.DeadlineRemainingMS < 0 || p.DeadlineRemainingMS > 600000 {
			t.Fatalf("invalid timing: %+v", p)
		}
	}
	got.PhaseTimingsV15[0].Stage = "changed"
	if s.Status().PhaseTimingsV15[0].Stage != "history" {
		t.Fatal("status aliases private timing")
	}
	raw, _ := json.Marshal(s.Status())
	if strings.Contains(string(raw), "must-not-appear") {
		t.Fatal("error detail leaked")
	}
	if session, err := OpenFullWindowActual(ctx, s.ready, "/must-not-open-credentials"); err == nil || session != nil {
		t.Fatal("timing fabricated execution authority")
	}
	s.resetPhaseTimingsV15()
	if len(s.Status().PhaseTimingsV15) != 0 {
		t.Fatal("previous window diagnostics retained")
	}
}

func TestWindowV15PhaseCallThroughCancellationAndLegacyOmission(t *testing.T) {
	for _, epoch := range []string{"v1", "v8", "v13", "v14"} {
		s := &FullWindowActualSession{ready: &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready." + epoch}}}
		s.recordPhaseV15(context.Background(), "history", time.Now(), nil)
		raw, _ := json.Marshal(s.Status())
		if strings.Contains(string(raw), "phase_timings_v15") {
			t.Fatal("legacy JSON changed", epoch)
		}
	}
	s := &FullWindowActualSession{ready: &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v15"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := s.timedPhaseV15(ctx, "history", func() error { calls++; return ctx.Err() })
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("timing changed call-through", calls, err)
	}
	p := s.Status().PhaseTimingsV15
	if len(p) != 1 || p[0].Outcome != "canceled" {
		t.Fatal("cancellation lost", p)
	}
	s.recordPhaseV15(ctx, "secret/path", time.Now(), nil)
	if len(s.Status().PhaseTimingsV15) != 1 {
		t.Fatal("arbitrary phase leaked")
	}
}
