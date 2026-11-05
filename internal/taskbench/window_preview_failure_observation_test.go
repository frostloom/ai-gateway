package taskbench

import (
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"
)

// This is only the pure recording-boundary selector, never a valid permit.
func previewFailureObservationDriver(schema, owned, collection string) *windowOwnedDriver {
	return &windowOwnedDriver{permit: &windowAcquisitionPermit{campaign: &windowCampaign{prepared: windowCampaignPrepared{scope: fullWindowScope{Schema: schema, Bindings: fullWindowBindings{PolicySHA256: owned}, CollectionPolicySHA256: collection}}}}}
}

func previewFailureObservationFixture(t *testing.T) (ConversationCase, Fixture, ConversationObservation, ConversationObservation, collectionPreviewFailureEvidence) {
	t.Helper()
	c, fixture := collectionFailedPreviewExample(t)
	live, err := cloneWindowOwned(fixture.steps[0])
	if err != nil {
		t.Fatal(err)
	}
	live.Turn.Message = c.Scenario.Turns[0].User
	in, err := cloneWindowOwned(fixture.evidence)
	if err != nil {
		t.Fatal(err)
	}
	// time.Now carries a real monotonic reading. Shift only the wall instant to
	// the existing synthetic fixture day, retaining that reading and location.
	at := time.Now()
	at = at.Add(in.Before.State.CapturedAt.Sub(at))
	if reflect.DeepEqual(at, at.Round(0)) {
		t.Fatal("fixture needs a real monotonic clock reading")
	}
	in.Before.State.CapturedAt = at.Round(0)
	in.Before.State.AnswerAuthority.CapturedAt = at.Round(0)
	live.State.CapturedAt = at
	live.State.AnswerAuthority.CapturedAt = at
	stored, err := cloneWindowOwned(live)
	if err != nil {
		t.Fatal(err)
	}
	in.After.State = stored.State
	if reflect.DeepEqual(live.State, stored.State) || !qualityJSONEqual(live, stored) {
		t.Fatal("fixture did not reproduce same-wire live/capture representation mismatch")
	}
	if err := verifyCollectionPreviewFailureEvidence(c, fixture.f, in.Before.State, stored, in); err != nil {
		t.Fatal("detached capture fixture must be valid", err)
	}
	if verifyCollectionPreviewFailureEvidence(c, fixture.f, in.Before.State, live, in) == nil {
		t.Fatal("raw monotonic observation unexpectedly passed strict capture linkage")
	}
	return c, fixture.f, live, stored, in
}

func TestWindowV13ObservationBoundaryRetainsStrictFailedPreviewLink(t *testing.T) {
	c, f, live, stored, in := previewFailureObservationFixture(t)
	d := previewFailureObservationDriver("full-window-scope.v13", windowOwnedPreviewFailurePolicySHA(), collectionPreviewFailurePolicySHA())
	got, err := d.observationForWindowRecording(live, stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCollectionPreviewFailureEvidence(c, f, in.Before.State, got, in); err != nil {
		t.Fatal("returned V13 observation differs from its retained capture", err)
	}
	if !reflect.DeepEqual(got, stored) || !bytes.Equal(mustCollectionJSON(t, got), mustCollectionJSON(t, live)) {
		t.Fatal("V13 boundary changed wire evidence or retained values")
	}
	if collectionPreviewFailurePolicySHA() != "3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213" || windowOwnedPreviewFailurePolicySHA() != "eed1d1deb720a48b1ebd2725dfa3b0586f35e28b21e03f9635fe8e015cc618a0" {
		t.Fatal("recording policy identity changed")
	}
}

func TestWindowV13ObservationBoundaryKeepsLegacyLiveValue(t *testing.T) {
	_, _, live, stored, _ := previewFailureObservationFixture(t)
	for _, schema := range []string{"full-window-scope.v1", "full-window-scope.v12", "full-window-scope.v13"} {
		t.Run(schema, func(t *testing.T) {
			d := previewFailureObservationDriver(schema, windowOwnedQualityPolicySHA(), collectionPolicySHA())
			got, err := d.observationForWindowRecording(live, stored)
			if err != nil || !reflect.DeepEqual(got, live) {
				t.Fatal("legacy/nonmatching policy changed the live return", err)
			}
		})
	}
	for _, d := range []*windowOwnedDriver{nil, {}, {permit: &windowAcquisitionPermit{}}} {
		got, err := d.observationForWindowRecording(live, stored)
		if err != nil || !reflect.DeepEqual(got, live) {
			t.Fatal("absent mode changed the original return")
		}
	}
}

func TestWindowV13ObservationBoundaryDetachedFromRetainedEvidence(t *testing.T) {
	_, _, live, stored, _ := previewFailureObservationFixture(t)
	d := previewFailureObservationDriver("full-window-scope.v13", windowOwnedPreviewFailurePolicySHA(), collectionPreviewFailurePolicySHA())
	d.observations = []ConversationObservation{stored}
	before := mustCollectionJSON(t, stored)
	got, err := d.observationForWindowRecording(live, stored)
	if err != nil {
		t.Fatal(err)
	}
	got.State.Controls[999] = Snapshot{BalanceCent: 1}
	got.State.Own.Subscriptions[0].Status = "changed"
	got.State.AnswerAuthority.Plans[0].Name = "changed"
	got.Turn.Response.Trace[0].Result = "changed"
	got.Turn.TraceRaw[0] = ' '
	if !bytes.Equal(before, mustCollectionJSON(t, stored)) || !bytes.Equal(before, mustCollectionJSON(t, d.observations[0])) {
		t.Fatal("caller mutated retained owner evidence through returned aliases")
	}
}

func TestWindowV13ObservationBoundaryRejectsEvidenceMutation(t *testing.T) {
	c, f, live, stored, in := previewFailureObservationFixture(t)
	d := previewFailureObservationDriver("full-window-scope.v13", windowOwnedPreviewFailurePolicySHA(), collectionPreviewFailurePolicySHA())
	got, err := d.observationForWindowRecording(live, stored)
	if err != nil || verifyCollectionPreviewFailureEvidence(c, f, in.Before.State, got, in) != nil {
		t.Fatal("valid recording boundary rejected", err)
	}
	for _, change := range []string{"nanosecond", "offset", "business", "control"} {
		t.Run(change, func(t *testing.T) {
			bad, err := cloneWindowOwned(got)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "nanosecond":
				bad.State.CapturedAt = bad.State.CapturedAt.Add(time.Nanosecond)
				bad.State.AnswerAuthority.CapturedAt = bad.State.CapturedAt
			case "offset":
				_, offset := bad.State.CapturedAt.Zone()
				bad.State.CapturedAt = bad.State.CapturedAt.In(time.FixedZone("changed", offset+3600))
				bad.State.AnswerAuthority.CapturedAt = bad.State.CapturedAt
			case "business":
				bad.State.Own.BalanceCent++
			case "control":
				bad.State.Controls[999] = Snapshot{BalanceCent: 1}
			}
			if verifyCollectionPreviewFailureEvidence(c, f, in.Before.State, bad, in) == nil {
				t.Fatal("strict evidence mutation admitted")
			}
		})
	}
}

func TestWindowV13ObservationBoundaryCloneErrorDoesNotFallBack(t *testing.T) {
	_, _, live, stored, _ := previewFailureObservationFixture(t)
	stored.Pending = &Pending{Args: map[string]any{"invalid": math.NaN()}}
	d := previewFailureObservationDriver("full-window-scope.v13", windowOwnedPreviewFailurePolicySHA(), collectionPreviewFailurePolicySHA())
	if _, err := d.observationForWindowRecording(live, stored); err == nil {
		t.Fatal("invalid retained evidence fell back to live observation")
	}
	d = previewFailureObservationDriver("full-window-scope.v12", windowOwnedQualityPolicySHA(), collectionPolicySHA())
	got, err := d.observationForWindowRecording(live, stored)
	if err != nil || !reflect.DeepEqual(got, live) {
		t.Fatal("legacy return unexpectedly cloned retained evidence", err)
	}
}
