package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func previewFailureCompoundExample(t *testing.T) qualityPreviewInput {
	t.Helper()
	c, d := collectionFailedPreviewExample(t)
	// First failure is admitted, while turn2 previews the wrong (standard) plan.
	o := &d.steps[1]
	o.State = cloneConversationState(d.steps[0].State)
	args := map[string]any{"plan_id": float64(47), "plan_name": "月度·标准"}
	o.Pending = &Pending{Tool: "change_plan", TenantID: 19, Args: args}
	o.Turn.Response.Preview = "切换到月度·标准"
	o.Turn.Response.Telemetry = &Telemetry{TurnTelemetry: agent.TurnTelemetry{LangfuseTraceID: "trace", Runtime: &agent.RuntimeMetadata{}, LLMCalls: 1, LLMUsageResponses: 1}}
	o.Turn.Response.Trace = []Trace{{Tool: "change_plan", Args: `{"plan_id":47}`, ResolvedArgs: `{"plan_id":47,"plan_name":"月度·标准"}`, Preview: o.Turn.Response.Preview, Result: "等待确认", Phase: "preview", Outcome: "pending"}}
	o.State.Own.Audits = append(o.State.Own.Audits, store.AgentAuditLog{ID: 6, SessionID: "session-19", TenantID: 19, Tool: "change_plan", Args: o.Turn.Response.Trace[0].ResolvedArgs, Preview: o.Turn.Response.Preview, Result: "pending_confirmation"})
	o.Turn.RawBody = string(mustCollectionJSON(t, o.Turn.Response))
	o.Turn.TraceRaw = mustCollectionJSON(t, o.Turn.Response.Trace)
	// Final has no execution: the original confirmation remains defined/unrun.
	d.steps = d.steps[:2]
	record := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	if len(record.Result.Observations) != 2 || record.Disposition != "halt" || len(record.PreviewFailures) != 1 {
		t.Fatalf("compound path: %+v", record.Issues)
	}
	if err := verifyCollectionPreviewFailureRecording(record); err != nil {
		t.Fatal(err)
	}
	before := d.evidence.After
	after, _ := cloneWindowOwned(before)
	after.State = record.Result.Observations[1].State
	after.Audits = after.State.Own.Audits
	pending := &qualityPendingCapture{Pending: *o.Pending, Preview: o.Turn.Response.Preview, IdemKey: "change-plan-id", SessionID: "session-19"}
	pending.Raw = mustCollectionJSON(t, map[string]any{"history": []agent.Message{}, "pending_action": map[string]any{"tool": pending.Pending.Tool, "args": args, "preview": pending.Preview, "idem_key": pending.IdemKey, "tenant_id": 19}})
	return qualityPreviewInput{Record: record.collectionRecording, PreviewFailures: record.PreviewFailures, Turn: 2, BeforeSession: d.evidence.AfterSession, Pending: pending, Before: before, After: after, Plans: []store.Plan{d.f.Standard, d.f.Flagship}}
}
func TestWindowV13CompoundFailureThenUnsafePreview(t *testing.T) {
	in := previewFailureCompoundExample(t)
	got, err := qualityPreviewCandidateV13(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyID != "full-customer-owned-preview-candidate.v13" || len(got.Unrun) != 1 || got.Unrun[0].Turn != 3 || got.Evidence.Record.Result.Success || !got.AuditOnly {
		t.Fatal("compound negative/suffix promoted")
	}
	if err = verifyWindowQualityCandidate(got); err != nil {
		t.Fatal(err)
	}
	if _, err = qualityPreviewCandidate(in); err == nil {
		t.Fatal("old v1 accepted new recording")
	}
	raw, _ := json.Marshal(got)
	var restored qualityPreviewSidecar
	if json.Unmarshal(raw, &restored) != nil || verifyWindowQualityCandidate(restored) != nil {
		t.Fatal("compound sidecar roundtrip rejected")
	}
	for _, fault := range []string{"admission", "pending", "sql", "unrun", "old-policy"} {
		t.Run(fault, func(t *testing.T) {
			var bad qualityPreviewSidecar
			_ = json.Unmarshal(raw, &bad)
			switch fault {
			case "admission":
				bad.Evidence.PreviewFailures = nil
			case "pending":
				bad.Evidence.Pending.IdemKey = ""
			case "sql":
				table := bad.Evidence.After.Tables["bills"]
				table.Rows = nil
				bad.Evidence.After.Tables["bills"] = table
			case "unrun":
				bad.Unrun = nil
			case "old-policy":
				bad.PolicyID = qualityPreviewPolicyID
				bad.PolicySHA256 = qualityPreviewPolicySHA()
			}
			if verifyWindowQualityCandidate(bad) == nil {
				t.Fatal("unproven compound accepted")
			}
		})
	}
}
func TestWindowV13NormalSidecarReplayAndNoDataAuthority(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	record := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	sidecar := windowPreviewFailureSidecar{Schema: "full-window-preview-failure-sidecar.v13", Record: record}
	raw := mustCollectionJSON(t, sidecar)
	ref := &windowPreviewFailureEvidence{PolicySHA256: windowOwnedPreviewFailurePolicySHA(), RecordingSHA256: windowV8DeclarationSHA(record.collectionRecording), SidecarFile: "preview-failure-000393-jev_selective.json", SidecarSHA256: conversationHash(raw)}
	if err := verifyWindowPreviewFailureSidecar(raw, record.collectionRecording, ref); err != nil {
		t.Fatal(err)
	}
	forged := &windowOwnedArmProof{recording: record.collectionRecording, recordingSHA: ref.RecordingSHA256, previewFailure: ref}
	if _, err := windowPreviewFailureProofEvidence(forged); err == nil {
		t.Fatal("data-only proof gained authority")
	}
	ref.RecordingSHA256 = ref.SidecarSHA256
	if verifyWindowPreviewFailureSidecar(raw, record.collectionRecording, ref) == nil {
		t.Fatal("sidecar not bound to base record")
	}
}

func TestWindowV13ArchiveRetainsExactFailureSidecar(t *testing.T) {
	s := actualTestSession(t)
	defer s.Close()
	s.ready.manifest.Schema = "full-window-actual-ready.v13"
	name := "window-000001"
	dir := filepath.Join(s.ready.manifest.SessionRoot, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	rawRecord, rawSidecar := []byte(`{"synthetic":"record"}`), []byte(`{"synthetic":"sidecar"}`)
	ref := &windowPreviewFailureEvidence{PolicySHA256: windowOwnedPreviewFailurePolicySHA(), RecordingSHA256: conversationHash(rawRecord), SidecarFile: "preview-failure-000386-model.json", SidecarSHA256: conversationHash(rawSidecar)}
	report := windowAcquisitionReceipt{Schema: "full-window-acquisition-receipt.v13", Rows: []windowAcquisitionRow{{RecordingFile: "arm-000386-model.json", RecordingSHA256: conversationHash(rawRecord), PreviewFailure: ref}}}
	raw := mustCollectionJSON(t, report)
	for n, data := range map[string][]byte{"receipt.json": raw, "arm-000386-model.json": rawRecord, ref.SidecarFile: rawSidecar} {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.rememberReport(name, report); err != nil {
		t.Fatalf("bound sidecar rejected: %v", err)
	}
	s.reportRoots[name] = true
	if err := s.verifyReports(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref.SidecarFile), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if s.verifyReports() == nil {
		t.Fatal("sidecar mutation accepted")
	}
}
