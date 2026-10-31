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

	"github.com/frostloom/ai-gateway/internal/observability"
)

// Dedicated V11 build anchors. Authority records prior autonomous delegation and the latest budget reply; it is not a new human review of these exact hashes.
var fullWindowV11ManifestSHA, fullWindowV11ArchiveCommit, fullWindowV11ArchiveTree string

// The authenticated signer reviews the retained autonomous instruction and latest
// budget reply. Go verifies their bytes and exact technical binding; it does not
// infer human intent or label the new scope as human reviewed.
type windowV11ExecutionBinding struct {
	windowV8ExecutionBinding
	AutonomousAuthorization windowActualFile                 `json:"autonomous_authorization"`
	GlobalBudget            windowV8Amounts                  `json:"global_budget"`
	HumanReviewedExactScope bool                             `json:"human_reviewed_exact_scope"`
	PrivateSettingsSHA256   string                           `json:"private_settings_sha256"`
	SpoolRoot               string                           `json:"spool_root"`
	Observer                observability.EvaluationIdentity `json:"observer"`
}

type windowV11ExecutionAuthorization struct {
	windowV11ExecutionBinding
	Signature string `json:"signature"`
}
type windowV11Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV11ExecutionBinding
}

func verifyWindowV11ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV11ExecutionBinding, error) {
	reject := func() (windowV11ExecutionBinding, error) {
		return windowV11ExecutionBinding{}, fmt.Errorf("v11 signed delegated execution authorization rejected")
	}
	var a windowV11ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v11" || m.Scope.Schema != "full-window-scope.v11" || validateWindowV11Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV11ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v11" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV11BudgetDeclaration().Parent {
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
	if b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || b.Observer != m.Observer || validateWindowV11SpoolBinding(m) != nil || b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
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

func (a *windowV11Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v11 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v11 authorization changed")
	}
	b, err := verifyWindowV11ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || !reflect.DeepEqual(b, a.binding) {
		return fmt.Errorf("v11 authorization binding changed")
	}

	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV11Preparation struct{ ready *FullWindowActualReady }

func CheckFullWindowV11Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV11Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 11)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV11Preparation{r}, nil
}
func AuthorizeFullWindowV11Preparation(p *FullWindowV11Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v11 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 11)
	if err != nil || !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v11 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v11 attestation absent")
	}
	binding, err := verifyWindowV11ExecutionAuthorization(raw, fresh.manifest, fullWindowV11ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV11Authority{manifest: fresh.manifest, manifestSHA: fullWindowV11ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV11() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v11 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v11 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV11Preparation(&FullWindowV11Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV10, fresh.stoppedV10) {
		return fmt.Errorf("v11 ready changed")
	}
	return nil
}
func loadWindowActualStoppedBaselineV11(m windowActualManifest) (windowStoppedV10Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV10Baseline{}, fmt.Errorf("v11 typed baseline absent")
	}
	p, err := loadWindowStoppedV10BaselineContext(context.Background(), m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV11(m, p) != nil {
		return windowStoppedV10Baseline{}, fmt.Errorf("v11 typed baseline rejected")
	}
	return p, nil
}
func validateWindowActualStoppedBindingV11(m windowActualManifest, p windowStoppedV10Baseline) error {
	if m.Schema != "full-window-actual-ready.v11" || m.Scope.Schema != "full-window-scope.v11" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV11BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV10WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV11Declarations(m.Scope) != nil {
		return fmt.Errorf("v11 baseline binding rejected")
	}
	for _, root := range p.predecessorRoots() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) || !windowPayloadDisjoint(m.SpoolRoot, root) || !windowPayloadDisjoint(m.SourceRoot, root) {
			return fmt.Errorf("v11 roots overlap")
		}
	}
	return nil
}
func (r *FullWindowActualReady) revalidateStoppedV11() error {
	if r == nil || validateWindowActualStoppedBindingV11(r.manifest, r.stoppedV10) != nil {
		return fmt.Errorf("v11 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV11(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV10) || !reflect.DeepEqual(r.baseline, windowV11CandidateBaseline(p.base)) {
		return fmt.Errorf("v11 stopped evidence changed")
	}
	return nil
}

// Native accounting stays unchanged; the opaque view checks V11 authorization.
func (a *windowV11Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v11: a}
}
