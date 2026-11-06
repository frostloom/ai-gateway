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

// Keys and manifest bindings are synthetic and cannot authorize an actual
// owner. The public approval text is the exact already approved policy reply. The verifier returns caller DATA, not an execution role.
func fixtureWindowV15OperationApproval(t *testing.T) (windowActualManifest, windowV15OperationPolicyApprovalBinding, ed25519.PrivateKey) {
	t.Helper()
	s, p, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	scope, err := prepareFullWindowScopeV15(s, p, windowV9TestCaps(t)[55:], bindings, windowV15BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := stoppedEvidenceRoot(t)
	reply := []byte("批准，继续吧")
	path := root + "/operation-policy-human-reply.txt"
	if err := os.WriteFile(path, reply, 0600); err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v15", Scope: scope, ScopeSHA256: windowV8DeclarationSHA(scope), ExecutionSHA256: strings.Repeat("2", 64), Sources: map[string]string{"synthetic.go": strings.Repeat("3", 64)}, CampaignRoot: root + "/campaign", BuildAttestationPublicKey: hex.EncodeToString(pub)}
	b := windowV15OperationPolicyApprovalBinding{Schema: "full-window-operation-policy-human-approval.v15", HumanPolicyApproved: true, HumanReply: windowActualFile{path, conversationHash(reply)}, ManifestSHA256: strings.Repeat("4", 64), SourceSHA256: windowV8DeclarationSHA(m.Sources), BinarySHA256: strings.Repeat("5", 64), ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, OperationPolicySHA256: windowV8DeclarationSHA(expectedWindowV14OperationPolicy())}
	return m, b, key
}

func signWindowV15PolicyFixture(t *testing.T, b windowV15OperationPolicyApprovalBinding, key ed25519.PrivateKey) []byte {
	t.Helper()
	message, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	a := windowV15OperationPolicyApproval{b, hex.EncodeToString(ed25519.Sign(key, message))}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWindowV15OperationApprovalRequiresDistinctGrant(t *testing.T) {
	m, b, key := fixtureWindowV15OperationApproval(t)
	raw := signWindowV15PolicyFixture(t, b, key)
	got, err := verifyWindowV15OperationPolicyApproval(raw, m, b.ManifestSHA256, b.BinarySHA256)
	if err != nil || got != b {
		t.Fatal("synthetic DATA signature rejected", err)
	}
	for name, mutate := range map[string]func(*windowV15OperationPolicyApprovalBinding){
		"old-schema": func(x *windowV15OperationPolicyApprovalBinding) {
			x.Schema = "full-window-operation-policy-human-approval.v14"
		},
		"not-approved":     func(x *windowV15OperationPolicyApprovalBinding) { x.HumanPolicyApproved = false },
		"fake-human-exact": func(x *windowV15OperationPolicyApprovalBinding) { x.HumanReviewedExactScope = true },
		"unapproved-reply": func(x *windowV15OperationPolicyApprovalBinding) { x.HumanReply.SHA256 = strings.Repeat("7", 64) },
		"old-budget-reply": func(x *windowV15OperationPolicyApprovalBinding) { x.HumanReply.SHA256 = windowStoppedV8ApprovalSHA },
		"old-autonomy-reply": func(x *windowV15OperationPolicyApprovalBinding) {
			x.HumanReply.SHA256 = windowV9AutonomousAuthorizationSHA
		},
		"wrong-reply-path": func(x *windowV15OperationPolicyApprovalBinding) {
			x.HumanReply.Path = m.CampaignRoot + "/human-reply.txt"
		},
		"manifest":  func(x *windowV15OperationPolicyApprovalBinding) { x.ManifestSHA256 = strings.Repeat("6", 64) },
		"source":    func(x *windowV15OperationPolicyApprovalBinding) { x.SourceSHA256 = strings.Repeat("6", 64) },
		"binary":    func(x *windowV15OperationPolicyApprovalBinding) { x.BinarySHA256 = strings.Repeat("6", 64) },
		"scope":     func(x *windowV15OperationPolicyApprovalBinding) { x.ScopeSHA256 = strings.Repeat("6", 64) },
		"execution": func(x *windowV15OperationPolicyApprovalBinding) { x.ExecutionSHA256 = strings.Repeat("6", 64) },
		"policy":    func(x *windowV15OperationPolicyApprovalBinding) { x.OperationPolicySHA256 = strings.Repeat("6", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := b
			mutate(&bad)
			if _, err := verifyWindowV15OperationPolicyApproval(signWindowV15PolicyFixture(t, bad, key), m, b.ManifestSHA256, b.BinarySHA256); err == nil {
				t.Fatal("rebound or old approval accepted")
			}
		})
	}
	_, other, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWindowV15OperationPolicyApproval(signWindowV15PolicyFixture(t, b, other), m, b.ManifestSHA256, b.BinarySHA256); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := os.WriteFile(b.HumanReply.Path, []byte("changed synthetic reply"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWindowV15OperationPolicyApproval(raw, m, b.ManifestSHA256, b.BinarySHA256); err == nil {
		t.Fatal("changed reply accepted")
	}
}

func TestWindowV15OperationApprovalStrictJSON(t *testing.T) {
	m, b, key := fixtureWindowV15OperationApproval(t)
	raw := signWindowV15PolicyFixture(t, b, key)
	for _, bad := range [][]byte{nil, append(append([]byte{}, raw...), []byte(` {}`)...), append([]byte(`{"unknown":true,`), raw[1:]...), append([]byte(`{"human_policy_approved":true,`), raw[1:]...)} {
		if _, err := verifyWindowV15OperationPolicyApproval(bad, m, b.ManifestSHA256, b.BinarySHA256); err == nil {
			t.Fatal("ambiguous/unknown JSON accepted")
		}
	}
	m.Schema = "full-window-actual-ready.v13"
	if _, err := verifyWindowV15OperationPolicyApproval(raw, m, b.ManifestSHA256, b.BinarySHA256); err == nil {
		t.Fatal("legacy manifest accepted new grant")
	}
}
