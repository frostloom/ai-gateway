package taskbench

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// Dedicated V9 build anchors. Authority records prior autonomous delegation and the latest budget reply; it is not a new human review of these exact hashes.
var fullWindowV9ManifestSHA, fullWindowV9ArchiveCommit, fullWindowV9ArchiveTree string

// The authenticated signer reviews the retained autonomous instruction and latest
// budget reply. Go verifies their bytes and exact technical binding; it does not
// infer human intent or label the new scope as human reviewed.
type windowV9ExecutionBinding struct {
	windowV8ExecutionBinding
	AutonomousAuthorization windowActualFile `json:"autonomous_authorization"`
	GlobalBudget            windowV8Amounts  `json:"global_budget"`
	HumanReviewedExactScope bool             `json:"human_reviewed_exact_scope"`
}

type windowV9ExecutionAuthorization struct {
	windowV9ExecutionBinding
	Signature string `json:"signature"`
}
type windowV9Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV9ExecutionBinding
}

func verifyWindowV9ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV9ExecutionBinding, error) {
	reject := func() (windowV9ExecutionBinding, error) {
		return windowV9ExecutionBinding{}, fmt.Errorf("v9 signed delegated execution authorization rejected")
	}
	var a windowV9ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v9" || m.Scope.Schema != "full-window-scope.v9" || validateWindowV9Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV9ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v9" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV9BudgetDeclaration().Parent {
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
	if b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
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

func (a *windowV9Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v9 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v9 authorization changed")
	}
	b, err := verifyWindowV9ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || b != a.binding {
		return fmt.Errorf("v9 authorization binding changed")
	}
	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV9Preparation struct{ ready *FullWindowActualReady }

func CheckFullWindowV9Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV9Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 9)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV9Preparation{r}, nil
}
func AuthorizeFullWindowV9Preparation(p *FullWindowV9Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v9 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 9)
	if err != nil || !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v9 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v9 attestation absent")
	}
	binding, err := verifyWindowV9ExecutionAuthorization(raw, fresh.manifest, fullWindowV9ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV9Authority{manifest: fresh.manifest, manifestSHA: fullWindowV9ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV9() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v9 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v9 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV9Preparation(&FullWindowV9Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV8, fresh.stoppedV8) {
		return fmt.Errorf("v9 ready changed")
	}
	return nil
}
func loadWindowActualStoppedBaselineV9(m windowActualManifest) (windowStoppedV8Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV8Baseline{}, fmt.Errorf("v9 typed baseline absent")
	}
	p, err := loadWindowStoppedV8Baseline(m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV9(m, p) != nil {
		return windowStoppedV8Baseline{}, fmt.Errorf("v9 typed baseline rejected")
	}
	return p, nil
}
func validateWindowActualStoppedBindingV9(m windowActualManifest, p windowStoppedV8Baseline) error {
	if m.Schema != "full-window-actual-ready.v9" || m.Scope.Schema != "full-window-scope.v9" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV9BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV8WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV9Declarations(m.Scope) != nil {
		return fmt.Errorf("v9 baseline binding rejected")
	}
	for _, root := range p.windowStoppedPredecessorRootsV9() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("v9 roots overlap")
		}
	}
	return nil
}
func (r *FullWindowActualReady) revalidateStoppedV9() error {
	if r == nil || validateWindowActualStoppedBindingV9(r.manifest, r.stoppedV8) != nil {
		return fmt.Errorf("v9 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV9(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV8) || !reflect.DeepEqual(r.baseline, windowV9CandidateBaseline(p.base)) {
		return fmt.Errorf("v9 stopped evidence changed")
	}
	return nil
}

const windowV9AutonomousAuthorizationSHA = "8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95"

// This internal view lets the unchanged native accounting and retry algorithm
// consume the same scalar fields while revalidation checks the V9 signature.
func (a *windowV9Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v9: a}
}
func windowScopeUsesNativeV8(schema string) bool {
	return schema == "full-window-scope.v8" || schema == "full-window-scope.v9"
}
func validateWindowNativeDeclarations(scope fullWindowScope) error {
	if scope.Schema == "full-window-scope.v9" {
		return validateWindowV9Declarations(scope)
	}
	return validateWindowV8Declarations(scope)
}
func windowNativeParent(scope fullWindowScope) windowV8Amounts {
	if scope.Schema == "full-window-scope.v9" {
		return expectedWindowV9BudgetDeclaration().Parent
	}
	return expectedWindowV8BudgetDeclaration().Parent
}

func validateWindowActualHelperCatalog(m windowActualManifest, manifestPath string) error {
	if m.Schema != "full-window-actual-ready.v9" {
		if m.HelperCatalog != nil {
			return fmt.Errorf("historical epoch cannot carry a new helper catalog")
		}
		return nil
	}
	if m.HelperCatalog == nil || !comparisonDigest(m.HelperCatalog.SHA256) || !filepath.IsAbs(manifestPath) || m.HelperCatalog.Path != filepath.Join(filepath.Dir(manifestPath), "helper-build/catalog.json") {
		return fmt.Errorf("v9 exact helper catalog reference required")
	}
	if _, err := readWindowStoppedV7File(m.HelperCatalog.Path, m.HelperCatalog.SHA256, 1<<20); err != nil {
		return fmt.Errorf("v9 helper catalog changed: %w", err)
	}
	return nil
}
