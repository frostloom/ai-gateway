package taskbench

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowOwnedQualityPolicyAndEmptyEvidence(t *testing.T) {
	if windowOwnedQualityPolicySHA() == collectionPolicySHA() || windowOwnedQualityPolicySHA() == qualityPreviewPolicySHA() {
		t.Fatal("owned policy must bind its own cleanup/registry contract")
	}
	if _, err := windowQualityProofEvidence(nil); err == nil {
		t.Fatal("nil proof accepted")
	}
	p := &windowOwnedArmProof{}
	if evidence, err := windowQualityProofEvidence(p); err != nil || evidence != nil {
		t.Fatal("normal proof must retain old branch")
	}
	p.quality = &windowQualityOwnedEnd{}
	if _, err := windowQualityProofEvidence(p); err == nil {
		t.Fatal("unsealed quality evidence accepted")
	}
}

func fixtureWindowQualityOwnedCandidate(t *testing.T, endRoot ...bool) (*windowOwnedDriver, collectionRecording) {
	t.Helper()
	p, source, _, _ := fixtureWindowOwnedPermit(t, windowOwnedQualityPolicySHA())
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	r := &windowAcquisitionRun{auth: windowAcquisitionAuthorization{campaign: p.campaign, privateReportRoot: root}, scope: p.campaign.prepared.scope, window: p.campaign.prepared.scope.Windows[0], ledger: p.ledger, permit: p, environment: p.environment, rootInfo: info, recordFiles: map[string]string{}}
	p.acquisition = r
	slot := r.scope.Slots[0]
	suite, _, err := fullCustomerSelection(source)
	if err != nil {
		t.Fatal(err)
	}
	c := suite.Cases[0]
	calls := 0
	upstream := closedWindowOwnedRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		body := `{"model":"deepseek-flash","content":[{"type":"tool_use","id":"wrong-preview","name":"recharge","input":{"amount_money":100}}],"stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":8}}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	d, err := newWindowOwnedDriver(p, source, slot, "model", p.environment, p.observer, "synthetic", "synthetic", upstream, closedJEVWindowOwned(new(int)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, run := p.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "closed-quality-preview", CaseKey: c.Key(), Arm: "model", EvidenceKind: "local_stub"})
	record := recordCollectionConversation(ctx, c, d)
	if len(endRoot) == 0 || endRoot[0] {
		run.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass})
	} else {
		t.Cleanup(func() { run.End(observability.EvaluationResult{Status: record.Result.Status}) })
	}
	if p.stopped || calls != 1 || record.Disposition != "halt" {
		t.Fatalf("safe wrong preview globally halted: stopped=%v calls=%d record=%+v", p.stopped, calls, record.Issues)
	}
	return d, record
}

func TestWindowQualityOwnedWrongPreviewEndsOnlyArm(t *testing.T) {
	d, record := fixtureWindowQualityOwnedCandidate(t)
	p := d.permit
	c := d.caseDefinition
	proof, err := d.finishWindowOwnedArm(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateWindowOwnedArmProof(p, proof); err != nil {
		t.Fatal(err)
	}
	ev, err := windowQualityProofEvidence(proof)
	if err != nil || ev == nil || ev.ObservedTurns != 1 || ev.UnrunTurns != len(c.Scenario.Turns)-1 {
		t.Fatalf("missing certified suffix: %+v %v", ev, err)
	}
	if proof.recording.Disposition != "halt" || proof.recording.Result.Success || p.nextArm != 1 || p.active != nil {
		t.Fatal("original negative or cursor lost")
	}

	originalQ, originalSHA := proof.quality, proof.qualitySHA
	proof.quality = nil
	proof.qualitySHA = ""
	if _, err = windowQualityProofEvidence(proof); err == nil {
		t.Fatal("stripped quality branch accepted")
	}
	proof.quality = originalQ
	proof.qualitySHA = originalSHA
	copyProof := *proof
	if _, err = windowQualityProofEvidence(&copyProof); err == nil {
		t.Fatal("copied registry proof accepted")
	}
	proof.quality.Deleted = 0
	if _, err = windowQualityProofEvidence(proof); err == nil {
		t.Fatal("mutated quality cleanup seal accepted")
	}
}

func TestWindowQualityOwnedCertificationFailuresGloballyStop(t *testing.T) {
	for _, name := range []string{"record", "candidate", "durability", "session_missing", "actual_effect", "final", "observer_identity"} {
		t.Run(name, func(t *testing.T) {
			d, record := fixtureWindowQualityOwnedCandidate(t)
			p := d.permit
			ctx := context.Background()
			switch name {
			case "record":
				record.Result.Observations[0].Turn.RawBody = "{}"
			case "candidate":
				d.qualityCandidate.Evidence.Pending.IdemKey = "forged"
			case "durability":
				if err := os.WriteFile(filepath.Join(p.acquisition.auth.privateReportRoot, "unexpected.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "session_missing":
				if err := p.environment.rdb.Del(ctx, "agent:session:"+d.session).Err(); err != nil {
					t.Fatal(err)
				}
			case "actual_effect":
				if err := p.environment.db.Exec("UPDATE tenants SET initial_quota=initial_quota+1 WHERE id=?", d.fixture.TenantID).Error; err != nil {
					t.Fatal(err)
				}
			case "final":
				d.final.CapturedAt = d.final.CapturedAt.AddDate(0, 0, 1)
			case "observer_identity":
				p.observerIdentity.Release = "changed"
			}
			proof, err := d.finishWindowOwnedArm(ctx, record)
			if err == nil || proof != nil || !p.stopped || p.nextArm != 0 {
				t.Fatalf("certification failure minted continuation: proof=%v err=%v stopped=%v", proof, err, p.stopped)
			}
		})
	}
}

func TestWindowQualityOwnedRequiresEndedTaskRoot(t *testing.T) {
	d, record := fixtureWindowQualityOwnedCandidate(t, false)
	if proof, err := d.finishWindowOwnedArm(context.Background(), record); err == nil || proof != nil || !d.permit.stopped {
		t.Fatal("unended task root minted proof")
	}
}
