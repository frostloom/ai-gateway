package taskbench

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// Dedicated technical build anchors. Final human reply bytes are never build inputs.
var fullWindowV8ManifestSHA, fullWindowV8ArchiveCommit, fullWindowV8ArchiveTree string

// The authenticated signing boundary must review the exact human reply before
// signing. Go checks its pinned bytes and signature; it cannot infer human intent.
type windowV8ExecutionBinding struct {
	Schema                  string           `json:"schema"`
	HumanAuthorized         bool             `json:"human_authorized"`
	RetryPolicyApproved     bool             `json:"retry_policy_approved"`
	HumanReply              windowActualFile `json:"human_reply"`
	ManifestSHA256          string           `json:"manifest_sha256"`
	SourceSHA256            string           `json:"source_sha256"`
	BinarySHA256            string           `json:"binary_sha256"`
	ScopeSHA256             string           `json:"scope_sha256"`
	ExecutionSHA256         string           `json:"execution_sha256"`
	BudgetDeclarationSHA256 string           `json:"budget_declaration_sha256"`
	RetryPolicySHA256       string           `json:"retry_policy_sha256"`
	Parent                  windowV8Amounts  `json:"parent"`
	OwnerID                 string           `json:"owner_id"`
	SingleOwner             bool             `json:"single_owner"`
	NoRelaunch              bool             `json:"no_relaunch"`
}
type windowV8ExecutionAuthorization struct {
	windowV8ExecutionBinding
	Signature string `json:"signature"`
}
type windowV8Authority struct {
	v9                                                          *windowV9Authority
	v10                                                         *windowV10Authority
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV8ExecutionBinding
}

func verifyWindowV8ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV8ExecutionBinding, error) {
	reject := func() (windowV8ExecutionBinding, error) {
		return windowV8ExecutionBinding{}, fmt.Errorf("v8 exact signed human execution authorization rejected")
	}
	var a windowV8ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v8" || m.Scope.Schema != "full-window-scope.v8" || validateWindowV8Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV8ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-execution-authorization.v8" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV8BudgetDeclaration().Parent {
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
	if !comparisonDigest(b.HumanReply.SHA256) {
		return reject()
	}
	if _, err = readWindowStoppedV7File(b.HumanReply.Path, b.HumanReply.SHA256, 1<<20); err != nil {
		return reject()
	}
	return b, nil
}

func (a *windowV8Authority) revalidate() error {
	if a != nil && a.v10 != nil {
		v := a.v10
		if a.v9 != nil || a.binding != v.binding.windowV8ExecutionBinding || a.authorizationPath != v.authorizationPath || a.authorizationSHA != v.authorizationSHA || a.manifestSHA != v.manifestSHA || a.binarySHA != v.binarySHA || !bytes.Equal(a.raw, v.raw) || !reflect.DeepEqual(a.manifest, v.manifest) {
			return fmt.Errorf("v10 native authority view changed")
		}
		return v.revalidate()
	}

	if a != nil && a.v9 != nil {
		v := a.v9
		if a.binding != v.binding.windowV8ExecutionBinding || a.authorizationPath != v.authorizationPath || a.authorizationSHA != v.authorizationSHA || a.manifestSHA != v.manifestSHA || a.binarySHA != v.binarySHA || !bytes.Equal(a.raw, v.raw) || !reflect.DeepEqual(a.manifest, v.manifest) {
			return fmt.Errorf("v9 native authority view changed")
		}
		return v.revalidate()
	}
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v8 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v8 authorization changed")
	}
	b, err := verifyWindowV8ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || b != a.binding {
		return fmt.Errorf("v8 authorization binding changed")
	}
	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV8Preparation struct{ ready *FullWindowActualReady }

func CheckFullWindowV8Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV8Preparation, error) {
	summary, r, err := checkFullWindowActualReady(o, true)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV8Preparation{r}, nil
}
func AuthorizeFullWindowV8Preparation(p *FullWindowV8Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v8 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReady(FullWindowActualReadyOptions{p.ready.manifestPath}, true)
	if err != nil || !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v8 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v8 attestation absent")
	}
	binding, err := verifyWindowV8ExecutionAuthorization(raw, fresh.manifest, fullWindowV8ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV8Authority{manifest: fresh.manifest, manifestSHA: fullWindowV8ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	fresh.v8Authority = authority
	fresh.prepared.v8Authority = authority
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV8() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v8 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV8Preparation(&FullWindowV8Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV7, fresh.stoppedV7) {
		return fmt.Errorf("v8 ready changed")
	}
	return nil
}
func windowActualBuildAnchors(m windowActualManifest) (string, string, string) {
	if m.Schema == "full-window-actual-ready.v10" {
		return fullWindowV10ManifestSHA, fullWindowV10ArchiveCommit, fullWindowV10ArchiveTree
	}
	if m.Schema == "full-window-actual-ready.v9" {
		return fullWindowV9ManifestSHA, fullWindowV9ArchiveCommit, fullWindowV9ArchiveTree
	}
	if m.Schema == "full-window-actual-ready.v8" {
		return fullWindowV8ManifestSHA, fullWindowV8ArchiveCommit, fullWindowV8ArchiveTree
	}
	return fullWindowActualManifestSHA, fullWindowActualArchiveCommit, fullWindowActualArchiveTree
}
func windowActualManifestAnchor(m windowActualManifest) string {
	sha, _, _ := windowActualBuildAnchors(m)
	return sha
}
func loadWindowActualStoppedBaselineV8(m windowActualManifest) (windowStoppedV7Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV7Baseline{}, fmt.Errorf("v8 typed baseline absent")
	}
	p, err := loadWindowStoppedV7Baseline(m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV8(m, p) != nil {
		return windowStoppedV7Baseline{}, fmt.Errorf("v8 typed baseline rejected")
	}
	return p, nil
}
func validateWindowActualStoppedBindingV8(m windowActualManifest, p windowStoppedV7Baseline) error {
	if m.Schema != "full-window-actual-ready.v8" || m.Scope.Schema != "full-window-scope.v8" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV8BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV7WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV8Declarations(m.Scope) != nil {
		return fmt.Errorf("v8 baseline binding rejected")
	}
	for _, root := range p.windowStoppedPredecessorRootsV8() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("v8 roots overlap")
		}
	}
	return nil
}
func (r *FullWindowActualReady) revalidateStoppedV8() error {
	if r == nil || validateWindowActualStoppedBindingV8(r.manifest, r.stoppedV7) != nil {
		return fmt.Errorf("v8 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV8(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV7) || !reflect.DeepEqual(r.baseline, windowV8CandidateBaseline(p.base)) {
		return fmt.Errorf("v8 stopped evidence changed")
	}
	return nil
}

func readWindowV8SignedBytes(path string, max int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !windowStoppedV7SingleLink(before) {
		return nil, fmt.Errorf("v8 signed file alias")
	}
	raw, err := readWindowActualBytes(path, "", max)
	after, e := os.Lstat(path)
	if err != nil || e != nil || !windowStoppedV7SingleLink(after) || !os.SameFile(before, after) {
		return nil, fmt.Errorf("v8 signed file changed")
	}
	return raw, nil
}

func requireWindowV8ExecutionAuthority(a *windowV8Authority) error {
	if a == nil || a.revalidate() != nil {
		return fmt.Errorf("v8 execution authority missing or changed before window outputs")
	}
	return nil
}
