package taskbench

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRepeatedFirstFactMismatchStops(t *testing.T) {
	cfg := pairTestConfig()
	path := filepath.Join(t.TempDir(), "run")
	calls := 0
	err := runPreparedPair(context.Background(), path, cfg, nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
		calls++
		d := rechargeDriver()
		d.steps[2].Turn.Response.Reply = "充值成功：到账 ¥999。"
		return d, nil
	}, nil, true, true, true)
	if err == nil || calls != 1 {
		t.Fatalf("fact mismatch continued: %v arms=%d", err, calls)
	}
	r, e := ReportPreparedPair(path)
	if e != nil || r.FinishedArms != 1 || r.UnrunArms != 1 || r.Rows[0].FactErrors == 0 {
		t.Fatalf("fact failure lost: %+v %v", r, e)
	}
}
