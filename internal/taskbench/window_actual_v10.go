package taskbench

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Dedicated V10 build anchors. Authority records prior autonomous delegation and the latest budget reply; it is not a new human review of these exact hashes.
var fullWindowV10ManifestSHA, fullWindowV10ArchiveCommit, fullWindowV10ArchiveTree string

// The authenticated signer reviews the retained autonomous instruction and latest
// budget reply. Go verifies their bytes and exact technical binding; it does not
// infer human intent or label the new scope as human reviewed.
type windowV10ExecutionBinding struct {
	windowV8ExecutionBinding
	AutonomousAuthorization   windowActualFile `json:"autonomous_authorization"`
	GlobalBudget              windowV8Amounts  `json:"global_budget"`
	HumanReviewedExactScope   bool             `json:"human_reviewed_exact_scope"`
	CompletionSHA256          string           `json:"completion_sha256"`
	ExecutorEquivalenceSHA256 string           `json:"executor_equivalence_sha256"`
}

type windowV10ExecutionAuthorization struct {
	windowV10ExecutionBinding
	Signature string `json:"signature"`
}
type windowV10Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV10ExecutionBinding
}

func verifyWindowV10ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV10ExecutionBinding, error) {
	reject := func() (windowV10ExecutionBinding, error) {
		return windowV10ExecutionBinding{}, fmt.Errorf("v10 signed delegated execution authorization rejected")
	}
	var a windowV10ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v10" || m.Scope.Schema != "full-window-scope.v10" || validateWindowV10Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV10ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v10" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV10BudgetDeclaration().Parent {
		return reject()
	}
	if !comparisonDigest(manifestSHA) || !comparisonDigest(binarySHA) || b.ManifestSHA256 != manifestSHA || b.BinarySHA256 != binarySHA || b.SourceSHA256 != conversationHash(sources) || !comparisonDigest(m.ScopeSHA256) || !comparisonDigest(m.ExecutionSHA256) || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.BudgetDeclarationSHA256 != m.Scope.BudgetDeclarationSHA256 || b.RetryPolicySHA256 != m.Scope.RetryPolicySHA256 {
		return reject()
	}
	pub, err := hex.DecodeString(m.BuildAttestationPublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return reject()
	}
	signature, err := hex.DecodeString(a.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return reject()
	}
	canonical, err := json.Marshal(b)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), canonical, signature) {
		return reject()
	}
	if b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.Completion) || m.ExecutorEquivalence == nil || !comparisonDigest(m.ExecutorEquivalence.SHA256) || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
		return reject()
	}
	if _, err = readWindowStoppedV7File(b.AutonomousAuthorization.Path, b.AutonomousAuthorization.SHA256, 65536); err != nil {
		return reject()
	}
	if !comparisonDigest(b.HumanReply.SHA256) {
		return reject()
	}
	if _, err = readWindowStoppedV7File(b.HumanReply.Path, b.HumanReply.SHA256, 1<<20); err != nil {
		return reject()
	}
	return b, nil
}

func (a *windowV10Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v10 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v10 authorization changed")
	}
	b, err := verifyWindowV10ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || !reflect.DeepEqual(b, a.binding) {
		return fmt.Errorf("v10 authorization binding changed")
	}
	if a.manifest.ExecutorEquivalence == nil {
		return fmt.Errorf("v10 equivalence absent")
	}
	f := *a.manifest.ExecutorEquivalence
	if _, err := readWindowStoppedV7File(f.Path, f.SHA256, 8<<20); err != nil {
		return fmt.Errorf("v10 equivalence changed")
	}
	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV10Preparation struct{ ready *FullWindowActualReady }

func CheckFullWindowV10Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV10Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 10)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV10Preparation{r}, nil
}
func AuthorizeFullWindowV10Preparation(p *FullWindowV10Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v10 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 10)
	if err != nil || !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v10 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v10 attestation absent")
	}
	binding, err := verifyWindowV10ExecutionAuthorization(raw, fresh.manifest, fullWindowV10ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV10Authority{manifest: fresh.manifest, manifestSHA: fullWindowV10ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV10() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v10 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v10 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV10Preparation(&FullWindowV10Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV9, fresh.stoppedV9) {
		return fmt.Errorf("v10 ready changed")
	}
	return nil
}
func loadWindowActualStoppedBaselineV10(m windowActualManifest) (windowStoppedV9Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV9Baseline{}, fmt.Errorf("v10 typed baseline absent")
	}
	p, err := loadWindowStoppedV9BaselineContext(context.Background(), m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV10(m, p) != nil {
		return windowStoppedV9Baseline{}, fmt.Errorf("v10 typed baseline rejected")
	}
	return p, nil
}
func validateWindowActualStoppedBindingV10(m windowActualManifest, p windowStoppedV9Baseline) error {
	if m.Schema != "full-window-actual-ready.v10" || m.Scope.Schema != "full-window-scope.v10" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV10BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV9WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV10Declarations(m.Scope) != nil {
		return fmt.Errorf("v10 baseline binding rejected")
	}
	for _, root := range p.windowStoppedPredecessorRootsV10() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("v10 roots overlap")
		}
	}
	return nil
}
func (r *FullWindowActualReady) revalidateStoppedV10() error {
	if r == nil || validateWindowActualStoppedBindingV10(r.manifest, r.stoppedV9) != nil {
		return fmt.Errorf("v10 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV10(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV9) || !reflect.DeepEqual(r.baseline, windowV10CandidateBaseline(p.base)) {
		return fmt.Errorf("v10 stopped evidence changed")
	}
	return nil
}

// Native accounting stays unchanged; the opaque view checks V10 authorization.
func (a *windowV10Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v10: a}
}

// Public summaries must not expose mutable private completion snapshots.
func detachedWindowActualStatusV10(in FullWindowActualStatus) FullWindowActualStatus {
	if in.Completion != nil {
		value := *in.Completion
		value.PrefixWindowIDs = append([]string(nil), value.PrefixWindowIDs...)
		in.Completion = &value
	}
	return in
}
