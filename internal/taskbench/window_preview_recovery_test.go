package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowPreviewRecoverySidecarBindingAndIsolation(t *testing.T) {
	c, d := collectionPreviewRecoveryConversationExample(t)
	record := recordCollectionPreviewRecoveryConversation(context.Background(), c, d)
	if record.Disposition != "continue_recorded" {
		t.Fatal(record.Issues)
	}
	sidecar := windowPreviewRecoverySidecar{Schema: "full-window-preview-recovery-sidecar.v16", Record: record}
	raw := mustCollectionJSON(t, sidecar)
	ref := &windowPreviewRecoveryEvidence{PolicySHA256: windowOwnedPreviewRecoveryPolicySHA(), RecordingSHA256: windowV8DeclarationSHA(record.collectionRecording), SidecarFile: "preview-recovery-000953-model.json", SidecarSHA256: conversationHash(raw)}
	if err := verifyWindowPreviewRecoverySidecar(raw, record.collectionRecording, ref); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"raw", "policy", "record", "path", "schema", "capture", "duplicate"} {
		t.Run(fault, func(t *testing.T) {
			bad := *ref
			body := append([]byte(nil), raw...)
			switch fault {
			case "raw":
				body = append(body, ' ')
			case "policy":
				bad.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
			case "record":
				bad.RecordingSHA256 = strings.Repeat("a", 64)
			case "path":
				bad.SidecarFile = "../" + bad.SidecarFile
			default:
				var changed windowPreviewRecoverySidecar
				if json.Unmarshal(body, &changed) != nil {
					t.Fatal("decode")
				}
				switch fault {
				case "schema":
					changed.Schema = "full-window-preview-failure-sidecar.v13"
				case "capture":
					changed.Record.PreviewRecoveries[0].Pending.IdemKey = ""
				case "duplicate":
					changed.Record.PreviewRecoveries = append(changed.Record.PreviewRecoveries, changed.Record.PreviewRecoveries[0])
				}
				body = mustCollectionJSON(t, changed)
				bad.SidecarSHA256 = conversationHash(body)
			}
			if verifyWindowPreviewRecoverySidecar(body, record.collectionRecording, &bad) == nil {
				t.Fatal("unproven sidecar admitted")
			}
		})
	}
	forged := &windowOwnedArmProof{recording: record.collectionRecording, recordingSHA: ref.RecordingSHA256, previewRecovery: ref}
	if _, err := windowPreviewRecoveryProofEvidence(forged); err == nil {
		t.Fatal("data acquired live proof authority")
	}
}

func TestWindowPreviewRecoveryOnlyExactNewMode(t *testing.T) {
	for _, schema := range []string{"full-window-scope.v1", "full-window-scope.v12", "full-window-scope.v13", "full-window-scope.v14", "full-window-scope.v15", "full-window-scope.v16"} {
		d := previewFailureObservationDriver(schema, windowOwnedPreviewRecoveryPolicySHA(), collectionPreviewRecoveryPolicySHA())
		want := schema == "full-window-scope.v16"
		if d.windowPreviewRecoveryMode() != want || windowOwnedRecordingPolicyAllowed(d.permit.campaign.prepared.scope) != want {
			t.Fatal("cross-epoch policy selection", schema)
		}
		if d.windowPreviewFailureMode() {
			t.Fatal("new recovery selected old mode", schema)
		}
	}
	d := previewFailureObservationDriver("full-window-scope.v16", windowOwnedPreviewFailurePolicySHA(), collectionPreviewFailurePolicySHA())
	if d.windowPreviewRecoveryMode() || windowOwnedRecordingPolicyAllowed(d.permit.campaign.prepared.scope) {
		t.Fatal("new scope accepted old policy")
	}
}

func TestWindowPreviewRecoveryOwnedRecordingDispatch(t *testing.T) {
	c, driver := collectionPreviewRecoveryConversationExample(t)
	d := previewFailureObservationDriver("full-window-scope.v16", windowOwnedPreviewRecoveryPolicySHA(), collectionPreviewRecoveryPolicySHA())
	got := d.recordWindowOwnedConversation(context.Background(), c, driver)
	if got.PolicyID != collectionPreviewRecoveryPolicyID || got.Disposition != "continue_recorded" || len(driver.sent) != 2 {
		t.Fatalf("new owned dispatch did not retain confirmation: %+v", got.Issues)
	}
	d.previewRecoveries = []collectionPreviewRecoveryEvidence{driver.evidence}
	if err := d.verifyOwnedCollectionRecord(got); err != nil {
		t.Fatal(err)
	}
	if !d.windowQualityMode() || d.windowQualityOwnedPolicySHA() != windowOwnedPreviewRecoveryPolicySHA() {
		t.Fatal("new mode omitted owned SQL/session/quality guards")
	}
	for _, schema := range []string{"full-window-scope.v13", "full-window-scope.v14", "full-window-scope.v15"} {
		c, driver := collectionPreviewRecoveryConversationExample(t)
		old := previewFailureObservationDriver(schema, windowOwnedPreviewFailurePolicySHA(), collectionPreviewFailurePolicySHA())
		record := old.recordWindowOwnedConversation(context.Background(), c, driver)
		if record.PolicyID != collectionPreviewFailurePolicyID || record.Disposition != "halt" || len(driver.sent) != 1 {
			t.Fatal("old scope admitted same-turn pending recovery", schema)
		}
	}
}

func TestWindowPreviewRecoveryProofRejectsMissingPermit(t *testing.T) {
	d := previewFailureObservationDriver("full-window-scope.v16", windowOwnedPreviewRecoveryPolicySHA(), collectionPreviewRecoveryPolicySHA())
	if _, err := windowPreviewRecoveryProofEvidence(&windowOwnedArmProof{driver: d}); err == nil {
		t.Fatal("unowned proof admitted")
	}
}

func TestWindowPreviewRecoveryArchiveRejectsRecordSidecarCollision(t *testing.T) {
	s := actualTestSession(t)
	defer s.Close()
	s.ready.manifest.Schema = "full-window-actual-ready.v16"
	name := "window-000001"
	dir := filepath.Join(s.ready.manifest.SessionRoot, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"synthetic":"same-file"}`)
	ref := &windowPreviewRecoveryEvidence{PolicySHA256: windowOwnedPreviewRecoveryPolicySHA(), RecordingSHA256: conversationHash(raw), SidecarFile: "preview-recovery-000953-model.json", SidecarSHA256: conversationHash(raw)}
	report := windowAcquisitionReceipt{Schema: "full-window-acquisition-receipt.v16", Rows: []windowAcquisitionRow{{RecordingFile: ref.SidecarFile, RecordingSHA256: conversationHash(raw), PreviewRecovery: ref}}}
	for n, b := range map[string][]byte{"receipt.json": mustCollectionJSON(t, report), ref.SidecarFile: raw} {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if s.rememberReport(name, report) == nil {
		t.Fatal("record and recovery sidecar shared a file")
	}
}

func TestWindowPreviewRecoveryObservationUsesDetachedWireCapture(t *testing.T) {
	_, _, live, stored, _ := previewFailureObservationFixture(t)
	d := previewFailureObservationDriver("full-window-scope.v16", windowOwnedPreviewRecoveryPolicySHA(), collectionPreviewRecoveryPolicySHA())
	got, err := d.observationForWindowRecording(live, stored)
	if err != nil || !reflect.DeepEqual(got, stored) {
		t.Fatal("new mode returned non-wire live values", err)
	}
	got.State.Own.BalanceCent++
	got.Turn.Response.Trace[0].Result = "mutated"
	if reflect.DeepEqual(got, stored) || stored.Turn.Response.Trace[0].Result == "mutated" {
		t.Fatal("capture was aliased")
	}
}

func TestWindowPreviewRecoveryArchiveRejectsOldEpochAndRetainsNewSidecar(t *testing.T) {
	for _, epoch := range []string{"v13", "v14", "v15", "v16"} {
		t.Run(epoch, func(t *testing.T) {
			s := actualTestSession(t)
			defer s.Close()
			s.ready.manifest.Schema = "full-window-actual-ready." + epoch
			name := "window-000001"
			dir := filepath.Join(s.ready.manifest.SessionRoot, name)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			record, sidecar := []byte(`{"synthetic":"record"}`), []byte(`{"synthetic":"sidecar"}`)
			ref := &windowPreviewRecoveryEvidence{PolicySHA256: windowOwnedPreviewRecoveryPolicySHA(), RecordingSHA256: conversationHash(record), SidecarFile: "preview-recovery-000953-model.json", SidecarSHA256: conversationHash(sidecar)}
			report := windowAcquisitionReceipt{Schema: "full-window-acquisition-receipt." + epoch, Rows: []windowAcquisitionRow{{RecordingFile: "arm-000953-model.json", RecordingSHA256: conversationHash(record), PreviewRecovery: ref}}}
			for n, raw := range map[string][]byte{"receipt.json": mustCollectionJSON(t, report), "arm-000953-model.json": record, ref.SidecarFile: sidecar} {
				if epoch != "v16" && n == ref.SidecarFile {
					continue
				} // reject the typed field itself, not an orphan file
				if err := os.WriteFile(filepath.Join(dir, n), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := s.rememberReport(name, report)
			if epoch != "v16" {
				if err == nil {
					t.Fatal("old epoch accepted recovery evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			s.reportRoots[name] = true
			if err = s.verifyReports(); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, ref.SidecarFile), []byte(`{"changed":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			if s.verifyReports() == nil {
				t.Fatal("changed recovery sidecar admitted")
			}
		})
	}
}

func TestWindowPreviewRecoveryCompoundUnsafePreviewPreservesNegative(t *testing.T) {
	in := previewFailureCompoundExample(t)
	original := in.Record.Result
	captures := []collectionPreviewRecoveryEvidence{}
	for _, e := range in.PreviewFailures {
		captures = append(captures, collectionPreviewRecoveryEvidence{Orders: e.Orders, Turn: e.Turn, BeforeSession: e.BeforeSession, AfterSession: e.AfterSession, Before: e.Before, After: e.After})
	}
	d := &collectionPreviewRecoveryReplay{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: original.Case.Key(), Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}, evidence: captures}
	record := recordCollectionPreviewRecoveryConversation(context.Background(), original.Case, d)
	if record.Disposition != "halt" || len(record.Result.Observations) != 2 || len(record.PreviewRecoveries) != 1 || verifyCollectionPreviewRecoveryRecording(record) != nil {
		t.Fatalf("compound fixture differs: %+v", record.Issues)
	}
	in.Record, in.PreviewFailures, in.PreviewRecoveries = record.collectionRecording, nil, record.PreviewRecoveries
	got, err := qualityPreviewCandidateV16(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyID != "full-customer-owned-preview-candidate.v16" || !got.AuditOnly || len(got.Unrun) != 1 || got.Unrun[0].Turn != 3 || got.Evidence.Record.Result.Success {
		t.Fatal("negative or original unrun confirmation promoted")
	}
	if err = verifyWindowQualityCandidate(got); err != nil {
		t.Fatal(err)
	}
	if _, err = qualityPreviewCandidateV13(in); err == nil {
		t.Fatal("old candidate accepted new recovery evidence")
	}
	if _, err = qualityPreviewCandidate(in); err == nil {
		t.Fatal("legacy candidate accepted new recovery evidence")
	}
	for _, fault := range []string{"capture", "pending", "unrun", "policy"} {
		t.Run(fault, func(t *testing.T) {
			var bad qualityPreviewSidecar
			if json.Unmarshal(mustCollectionJSON(t, got), &bad) != nil {
				t.Fatal("decode")
			}
			switch fault {
			case "capture":
				bad.Evidence.PreviewRecoveries = nil
			case "pending":
				bad.Evidence.Pending.IdemKey = ""
			case "unrun":
				bad.Unrun = nil
			case "policy":
				bad.PolicyID = "full-customer-owned-preview-candidate.v13"
			}
			if verifyWindowQualityCandidate(bad) == nil {
				t.Fatal("compound tamper admitted")
			}
		})
	}
}
