package taskbench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// All keys/replies in this fixture are synthetic and cannot authorize an actual
// manifest or owner. The verifier returns caller DATA, not an execution role.
func fixtureWindowV14OperationApproval(t *testing.T) (windowActualManifest, windowV14OperationPolicyApprovalBinding, ed25519.PrivateKey) {
	t.Helper()
	s, p, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	scope, err := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], bindings, windowV14BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := stoppedEvidenceRoot(t)
	reply := []byte("SYNTHETIC TEST FIXTURE: no real human policy approval")
	path := root + "/operation-policy-human-reply.txt"
	if err := os.WriteFile(path, reply, 0600); err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v14", Scope: scope, ScopeSHA256: windowV8DeclarationSHA(scope), ExecutionSHA256: strings.Repeat("2", 64), Sources: map[string]string{"synthetic.go": strings.Repeat("3", 64)}, CampaignRoot: root + "/campaign", BuildAttestationPublicKey: hex.EncodeToString(pub)}
	b := windowV14OperationPolicyApprovalBinding{Schema: "full-window-operation-policy-human-approval.v14", HumanPolicyApproved: true, HumanReply: windowActualFile{path, conversationHash(reply)}, ManifestSHA256: strings.Repeat("4", 64), SourceSHA256: windowV8DeclarationSHA(m.Sources), BinarySHA256: strings.Repeat("5", 64), ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, OperationPolicySHA256: windowV8DeclarationSHA(expectedWindowV14OperationPolicy())}
	return m, b, key
}

func signWindowV14PolicyFixture(t *testing.T, b windowV14OperationPolicyApprovalBinding, key ed25519.PrivateKey) []byte {
	t.Helper()
	message, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	a := windowV14OperationPolicyApproval{b, hex.EncodeToString(ed25519.Sign(key, message))}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWindowV14OperationApprovalRequiresDistinctGrant(t *testing.T) {
	m, b, key := fixtureWindowV14OperationApproval(t)
	raw := signWindowV14PolicyFixture(t, b, key)
	got, err := verifyWindowV14OperationPolicyApproval(raw, m, b.ManifestSHA256, b.BinarySHA256)
	if err != nil || got != b {
		t.Fatal("synthetic DATA signature rejected", err)
	}
	for name, mutate := range map[string]func(*windowV14OperationPolicyApprovalBinding){
		"old-schema": func(x *windowV14OperationPolicyApprovalBinding) {
			x.Schema = "full-window-operation-policy-human-approval.v13"
		},
		"not-approved":     func(x *windowV14OperationPolicyApprovalBinding) { x.HumanPolicyApproved = false },
		"fake-human-exact": func(x *windowV14OperationPolicyApprovalBinding) { x.HumanReviewedExactScope = true },
		"old-budget-reply": func(x *windowV14OperationPolicyApprovalBinding) { x.HumanReply.SHA256 = windowStoppedV8ApprovalSHA },
		"old-autonomy-reply": func(x *windowV14OperationPolicyApprovalBinding) {
			x.HumanReply.SHA256 = windowV9AutonomousAuthorizationSHA
		},
		"wrong-reply-path": func(x *windowV14OperationPolicyApprovalBinding) {
			x.HumanReply.Path = m.CampaignRoot + "/human-reply.txt"
		},
		"manifest":  func(x *windowV14OperationPolicyApprovalBinding) { x.ManifestSHA256 = strings.Repeat("6", 64) },
		"source":    func(x *windowV14OperationPolicyApprovalBinding) { x.SourceSHA256 = strings.Repeat("6", 64) },
		"binary":    func(x *windowV14OperationPolicyApprovalBinding) { x.BinarySHA256 = strings.Repeat("6", 64) },
		"scope":     func(x *windowV14OperationPolicyApprovalBinding) { x.ScopeSHA256 = strings.Repeat("6", 64) },
		"execution": func(x *windowV14OperationPolicyApprovalBinding) { x.ExecutionSHA256 = strings.Repeat("6", 64) },
		"policy":    func(x *windowV14OperationPolicyApprovalBinding) { x.OperationPolicySHA256 = strings.Repeat("6", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := b
			mutate(&bad)
			if _, err := verifyWindowV14OperationPolicyApproval(signWindowV14PolicyFixture(t, bad, key), m, b.ManifestSHA256, b.BinarySHA256); err == nil {
				t.Fatal("rebound or old approval accepted")
			}
		})
	}
	_, other, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWindowV14OperationPolicyApproval(signWindowV14PolicyFixture(t, b, other), m, b.ManifestSHA256, b.BinarySHA256); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := os.WriteFile(b.HumanReply.Path, []byte("changed synthetic reply"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWindowV14OperationPolicyApproval(raw, m, b.ManifestSHA256, b.BinarySHA256); err == nil {
		t.Fatal("changed reply accepted")
	}
}

func TestWindowV14OperationApprovalStrictJSON(t *testing.T) {
	m, b, key := fixtureWindowV14OperationApproval(t)
	raw := signWindowV14PolicyFixture(t, b, key)
	for _, bad := range [][]byte{nil, append(append([]byte{}, raw...), []byte(` {}`)...), append([]byte(`{"unknown":true,`), raw[1:]...), append([]byte(`{"human_policy_approved":true,`), raw[1:]...)} {
		if _, err := verifyWindowV14OperationPolicyApproval(bad, m, b.ManifestSHA256, b.BinarySHA256); err == nil {
			t.Fatal("ambiguous/unknown JSON accepted")
		}
	}
	m.Schema = "full-window-actual-ready.v13"
	if _, err := verifyWindowV14OperationPolicyApproval(raw, m, b.ManifestSHA256, b.BinarySHA256); err == nil {
		t.Fatal("legacy manifest accepted new grant")
	}
}
