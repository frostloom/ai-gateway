package taskbench

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"time"
)

// The original owned Step failed while its context reached its deadline, and
// Final was refused by that consumed owner. The ordinary V13 replay driver
// returns nil Step/Final errors under Background and cannot reproduce this.
// This fixed DATA-only driver supplies exactly those recorded failures to the
// unchanged collection policy; it cannot invoke a tool or restore an owner.
type windowStoppedV14InterruptedContext struct{ context.Context }
type windowStoppedV14InterruptedReplay struct {
	*replayConversationDriver
	interrupted *windowStoppedV14InterruptedContext
}

func (d *windowStoppedV14InterruptedReplay) Step(ctx context.Context, f Fixture, session, message string) (ConversationObservation, error) {
	obs, e := d.replayConversationDriver.Step(ctx, f, session, message)
	if e != nil {
		return obs, e
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Time{})
	cancel()
	d.interrupted.Context = expired
	return obs, fmt.Errorf("owned Handler arm stopped")
}
func (d *windowStoppedV14InterruptedReplay) Final(context.Context, Fixture, string) (ConversationState, error) {
	return ConversationState{}, fmt.Errorf("owned Handler arm stopped")
}
func validateWindowStoppedV14InterruptedRecording(r collectionRecording) error {
	fail := func() error { return fmt.Errorf("stopped14 interrupted recording DATA differs") }
	// Anchor the entire captured partial state/tool-result data. Replay below is
	// additional semantic validation, never a completion or oracle truth claim.
	if r.PolicyID != collectionPreviewFailurePolicyID || r.PolicySHA256 != collectionPreviewFailurePolicySHA() || r.ResultSHA256 != "26c1bd66840ca7d3f8ac65a2d6074669c804633fdaf1ed3d25caabbbb3705aea" || windowV8DeclarationSHA(r.Result) != r.ResultSHA256 || r.Disposition != "halt" || r.Result.Case.Key() != "change_plan-067/answer_complete" || len(r.Result.Observations) != 1 || r.Result.Success || r.Result.StructuralPass || r.Result.SetupError || r.Result.Status != "failed" {
		return fail()
	}
	original := r.Result
	ctx := &windowStoppedV14InterruptedContext{context.Background()}
	d := &windowStoppedV14InterruptedReplay{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: original.Case.Key(), Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}, interrupted: ctx}
	replay := recordCollectionPreviewFailureConversation(ctx, original.Case, d)
	if d.next != 1 || ctx.Err() != context.DeadlineExceeded || replay.Disposition != r.Disposition || !reflect.DeepEqual(replay.Issues, r.Issues) || len(replay.PreviewFailures) != 0 {
		return fail()
	}
	want, e := collectionNormalizedResult(original)
	if e != nil {
		return e
	}
	got, e := collectionNormalizedResult(replay.Result)
	if e != nil {
		return e
	}
	if !bytes.Equal(want, got) {
		return fail()
	}
	return nil
}
