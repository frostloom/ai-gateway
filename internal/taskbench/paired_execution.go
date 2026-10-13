package taskbench

import (
	"context"
	"fmt"
)

// Stage evidence retains failures that cannot be reconstructed from snapshots,
// without persisting dependency error payloads or credentials.
type PairExecutionFailure struct {
	Stage string `json:"stage"`
	Turn  int    `json:"turn,omitempty"`
}
type pairTrackingDriver struct {
	ConversationDriver
	turn     int
	failures []PairExecutionFailure
}

func (d *pairTrackingDriver) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	f, s, e := d.ConversationDriver.Setup(ctx, c)
	if e != nil {
		d.failures = append(d.failures, PairExecutionFailure{Stage: "setup"})
		e = fmt.Errorf("driver setup failed")
	}
	return f, s, e
}
func (d *pairTrackingDriver) Step(ctx context.Context, f Fixture, s, m string) (ConversationObservation, error) {
	d.turn++
	o, e := d.ConversationDriver.Step(ctx, f, s, m)
	if e != nil {
		d.failures = append(d.failures, PairExecutionFailure{Stage: "step", Turn: d.turn})
		e = fmt.Errorf("driver step failed")
	}
	return o, e
}
func (d *pairTrackingDriver) Final(ctx context.Context, f Fixture, s string) (ConversationState, error) {
	v, e := d.ConversationDriver.Final(ctx, f, s)
	if e != nil {
		d.failures = append(d.failures, PairExecutionFailure{Stage: "final"})
		e = fmt.Errorf("driver final failed")
	}
	return v, e
}

type pairReplayDriver struct {
	*replayConversationDriver
	failures []PairExecutionFailure
}

func (d *pairReplayDriver) failure(stage string, turn int) error {
	for _, f := range d.failures {
		if f.Stage == stage && f.Turn == turn {
			return fmt.Errorf("recorded %s execution failure", stage)
		}
	}
	return nil
}
func (d *pairReplayDriver) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	f, s, e := d.replayConversationDriver.Setup(ctx, c)
	if fail := d.failure("factory", 0); fail != nil {
		return f, s, fail
	}
	if fail := d.failure("setup", 0); fail != nil {
		return f, s, fail
	}
	return f, s, e
}
func (d *pairReplayDriver) Step(ctx context.Context, f Fixture, s, m string) (ConversationObservation, error) {
	o, e := d.replayConversationDriver.Step(ctx, f, s, m)
	if fail := d.failure("step", d.next); fail != nil {
		return o, fail
	}
	return o, e
}
func (d *pairReplayDriver) Final(ctx context.Context, f Fixture, s string) (ConversationState, error) {
	v, e := d.replayConversationDriver.Final(ctx, f, s)
	if fail := d.failure("final", 0); fail != nil {
		return v, fail
	}
	return v, e
}
