package taskbench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// RunCampaignBatch is deliberately not exposed by a paid CLI. The caller must
// supply authorized configuration, owned fixtures and guarded provider clients.
// A lease binds one exact batch; any failure stops expansion of the campaign.
func RunCampaignBatch(ctx context.Context, lease *CampaignBatchLease, o AgentPairOptions) (runErr error) {
	return runCampaignBatch(ctx, lease, o, RunAgentPair)
}

func runCampaignBatch(ctx context.Context, lease *CampaignBatchLease, o AgentPairOptions, run func(context.Context, string, AgentPairOptions) error) (runErr error) {
	if !o.AllowExecution || lease == nil {
		return fmt.Errorf("campaign execution disabled")
	}
	c := lease.campaign
	c.mu.Lock()
	e, reserved := c.reserved[lease.ID]
	active := reserved && e.Binding == lease.Binding && filepath.Clean(lease.Dir) == filepath.Join(c.dir, lease.ID) && !c.closed && !c.stopped && !lease.finished && !lease.running && c.pending == lease.ID
	if active {
		lease.running = true
	}
	c.mu.Unlock()
	if !active {
		return fmt.Errorf("campaign lease inactive")
	}
	defer func() { runErr = errors.Join(runErr, lease.finish(runErr != nil, true)) }()
	if o.Budget == nil || lease.Binding != PairRunContract(o.Config) || !reflect.DeepEqual(lease.limits, o.Budget.limits) || lease.jevCalls != o.MaxJEVCalls {
		return fmt.Errorf("campaign batch budget/configuration differs")
	}
	expected, err := filepath.Abs(filepath.Join(lease.Dir, "budget.jsonl"))
	if err != nil {
		return err
	}
	actual, err := filepath.Abs(o.Budget.path)
	if err != nil {
		return err
	}
	if filepath.Clean(expected) != filepath.Clean(actual) {
		return fmt.Errorf("campaign child budget path differs")
	}
	path := filepath.Join(lease.Dir, "run.jsonl")
	if err = run(ctx, path, o); err != nil {
		return err
	}
	report, err := ReportPreparedPair(path)
	if err != nil {
		return err
	}
	if report.FinishedArms != report.PlannedArms {
		return fmt.Errorf("campaign batch incomplete")
	}
	for _, row := range report.Rows {
		if !row.StructuralPass || row.HTTPFailures > 0 {
			return fmt.Errorf("campaign batch structural failure")
		}
	}
	// RunAgentPair closes its JEV guard; inspect the durable outcome rather than
	// transient companion state, which is detached during close.
	f, err := os.Open(path + ".jev.jsonl")
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, f.Close()) }()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1<<20)
	for s.Scan() {
		var e pairJEVEvent
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return fmt.Errorf("invalid campaign JEV evidence")
		}
		if e.Kind == "settle" && e.Failed {
			return fmt.Errorf("campaign JEV failure; further batches stopped")
		}
	}
	if err = s.Err(); err != nil {
		return err
	}
	if o.Config.Model.EvidenceKind == "recorded_run" {
		for _, row := range report.Rows {
			if row.Telemetry.Tokens == nil {
				return fmt.Errorf("campaign usage unknown; further batches stopped")
			}
		}
	}
	return nil
}
