package taskbench

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// The new header retains the complete V14 canonical field layout in Go's canonical struct order.
// A parsed binding is DATA; only a separately validated actual preparation may
// ever turn a complete authorization into an execution role.
const windowV15OperationPolicyReplySHA = "2560180d500f5740d102c5e55822b25dc322a9493506c3685cee473c5b6ec4af"

type windowV15ExecutionBinding struct {
	windowV13ExecutionBinding
	OperationPolicySHA256   string           `json:"operation_policy_sha256"`
	OperationPolicyApproval windowActualFile `json:"operation_policy_approval"`
}

type windowV15ExecutionAuthorization struct {
	windowV15ExecutionBinding
	Signature string `json:"signature"`
}

type windowV15OperationPolicyApprovalBinding struct {
	Schema                  string           `json:"schema"`
	HumanPolicyApproved     bool             `json:"human_policy_approved"`
	HumanReply              windowActualFile `json:"human_reply"`
	ManifestSHA256          string           `json:"manifest_sha256"`
	SourceSHA256            string           `json:"source_sha256"`
	BinarySHA256            string           `json:"binary_sha256"`
	ScopeSHA256             string           `json:"scope_sha256"`
	ExecutionSHA256         string           `json:"execution_sha256"`
	OperationPolicySHA256   string           `json:"operation_policy_sha256"`
	HumanReviewedExactScope bool             `json:"human_reviewed_exact_scope"`
}

type windowV15OperationPolicyApproval struct {
	windowV15OperationPolicyApprovalBinding
	Signature string `json:"signature"`
}

func windowV15OperationPolicyApprovalMessage(b windowV15OperationPolicyApprovalBinding) ([]byte, error) {
	if b.Schema != "full-window-operation-policy-human-approval.v15" || !b.HumanPolicyApproved || b.HumanReviewedExactScope || !filepath.IsAbs(b.HumanReply.Path) || filepath.Clean(b.HumanReply.Path) != b.HumanReply.Path || b.HumanReply.SHA256 != windowV15OperationPolicyReplySHA || b.HumanReply.SHA256 == windowStoppedV8ApprovalSHA || b.HumanReply.SHA256 == windowV9AutonomousAuthorizationSHA {
		return nil, fmt.Errorf("v15 distinct human timing-policy DATA required")
	}
	for _, digest := range []string{b.ManifestSHA256, b.SourceSHA256, b.BinarySHA256, b.ScopeSHA256, b.ExecutionSHA256, b.OperationPolicySHA256} {
		if !comparisonDigest(digest) {
			return nil, fmt.Errorf("v15 policy approval digest required")
		}
	}
	if b.OperationPolicySHA256 != windowV8DeclarationSHA(expectedWindowV14OperationPolicy()) {
		return nil, fmt.Errorf("v15 exact operation policy required")
	}
	return json.Marshal(b)
}

func windowV15ExecutionAuthorizationMessage(b windowV15ExecutionBinding) ([]byte, error) {
	if b.Schema != "full-window-delegated-execution-authorization.v15" {
		return nil, fmt.Errorf("v15 binding message required")
	}
	return json.Marshal(b)
}

// Verify only separately signed caller DATA and the already approved reply bytes. This does
// not read credentials, create an actual preparation/owner, or issue a claim.
// The trusted manifest key must come from the actual preparation's freeze.
func verifyWindowV15OperationPolicyApproval(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV15OperationPolicyApprovalBinding, error) {
	reject := func() (windowV15OperationPolicyApprovalBinding, error) {
		return windowV15OperationPolicyApprovalBinding{}, fmt.Errorf("v15 separate human operation-policy approval rejected")
	}
	var a windowV15OperationPolicyApproval
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v15" || validateWindowV15Declarations(m.Scope) != nil || !filepath.IsAbs(m.CampaignRoot) || filepath.Clean(m.CampaignRoot) != m.CampaignRoot || filepath.Base(m.CampaignRoot) != "campaign" {
		return reject()
	}
	b := a.windowV15OperationPolicyApprovalBinding
	message, err := windowV15OperationPolicyApprovalMessage(b)
	if err != nil || !comparisonDigest(manifestSHA) || !comparisonDigest(binarySHA) || b.ManifestSHA256 != manifestSHA || b.BinarySHA256 != binarySHA || b.SourceSHA256 != windowV8DeclarationSHA(m.Sources) || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.OperationPolicySHA256 != windowV8DeclarationSHA(m.Scope.OperationPolicyV14) || b.HumanReply.Path != filepath.Join(filepath.Dir(m.CampaignRoot), "operation-policy-human-reply.txt") {
		return reject()
	}
	pub, err := hex.DecodeString(m.BuildAttestationPublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return reject()
	}
	signature, err := hex.DecodeString(a.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(pub), message, signature) {
		return reject()
	}
	if _, err := readWindowStoppedV7File(b.HumanReply.Path, b.HumanReply.SHA256, 1<<20); err != nil {
		return reject()
	}
	return b, nil
}
