package taskbench

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// New fields follow the complete V13 binding in Go's canonical struct order.
// A parsed binding is DATA; only a separately validated actual preparation may
// ever turn a complete authorization into an execution role.
type windowV14ExecutionBinding struct {
	windowV13ExecutionBinding
	OperationPolicySHA256   string           `json:"operation_policy_sha256"`
	OperationPolicyApproval windowActualFile `json:"operation_policy_approval"`
}

type windowV14ExecutionAuthorization struct {
	windowV14ExecutionBinding
	Signature string `json:"signature"`
}

type windowV14OperationPolicyApprovalBinding struct {
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

type windowV14OperationPolicyApproval struct {
	windowV14OperationPolicyApprovalBinding
	Signature string `json:"signature"`
}

func windowV14OperationPolicyApprovalMessage(b windowV14OperationPolicyApprovalBinding) ([]byte, error) {
	if b.Schema != "full-window-operation-policy-human-approval.v14" || !b.HumanPolicyApproved || b.HumanReviewedExactScope || !filepath.IsAbs(b.HumanReply.Path) || filepath.Clean(b.HumanReply.Path) != b.HumanReply.Path || !comparisonDigest(b.HumanReply.SHA256) || b.HumanReply.SHA256 == windowStoppedV8ApprovalSHA || b.HumanReply.SHA256 == windowV9AutonomousAuthorizationSHA {
		return nil, fmt.Errorf("v14 distinct human timing-policy DATA required")
	}
	for _, digest := range []string{b.ManifestSHA256, b.SourceSHA256, b.BinarySHA256, b.ScopeSHA256, b.ExecutionSHA256, b.OperationPolicySHA256} {
		if !comparisonDigest(digest) {
			return nil, fmt.Errorf("v14 policy approval digest required")
		}
	}
	if b.OperationPolicySHA256 != windowV8DeclarationSHA(expectedWindowV14OperationPolicy()) {
		return nil, fmt.Errorf("v14 exact operation policy required")
	}
	return json.Marshal(b)
}

func windowV14ExecutionAuthorizationMessage(b windowV14ExecutionBinding) ([]byte, error) {
	if b.Schema != "full-window-delegated-execution-authorization.v14" {
		return nil, fmt.Errorf("v14 binding message required")
	}
	return json.Marshal(b)
}

// Verify only separately signed caller DATA and the new reply bytes. This does
// not read credentials, create an actual preparation/owner, or issue a claim.
// The trusted manifest key must come from the actual preparation's freeze.
func verifyWindowV14OperationPolicyApproval(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV14OperationPolicyApprovalBinding, error) {
	reject := func() (windowV14OperationPolicyApprovalBinding, error) {
		return windowV14OperationPolicyApprovalBinding{}, fmt.Errorf("v14 separate human operation-policy approval rejected")
	}
	var a windowV14OperationPolicyApproval
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v14" || validateWindowV14Declarations(m.Scope) != nil || !filepath.IsAbs(m.CampaignRoot) || filepath.Clean(m.CampaignRoot) != m.CampaignRoot || filepath.Base(m.CampaignRoot) != "campaign" {
		return reject()
	}
	b := a.windowV14OperationPolicyApprovalBinding
	message, err := windowV14OperationPolicyApprovalMessage(b)
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
