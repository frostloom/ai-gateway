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

// Dedicated V12 build anchors. Authority records prior autonomous delegation and the latest budget reply; it is not a new human review of these exact hashes.
var fullWindowV12ManifestSHA, fullWindowV12ArchiveCommit, fullWindowV12ArchiveTree string

// The authenticated signer reviews the retained autonomous instruction and latest
// budget reply. Go verifies their bytes and exact technical binding; it does not
// infer human intent or label the new scope as human reviewed.
type windowV12ExecutionBinding struct {
	windowV11ExecutionBinding
	CompletionSHA256          string `json:"completion_sha256"`
	ExecutorEquivalenceSHA256 string `json:"executor_equivalence_sha256"`
}

type windowV12ExecutionAuthorization struct {
	windowV12ExecutionBinding
	Signature string `json:"signature"`
}
type windowV12Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV12ExecutionBinding
}

func verifyWindowV12ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV12ExecutionBinding, error) {
	reject := func() (windowV12ExecutionBinding, error) {
		return windowV12ExecutionBinding{}, fmt.Errorf("v12 signed delegated execution authorization rejected")
	}
	var a windowV12ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v12" || m.Scope.Schema != "full-window-scope.v12" || validateWindowV12Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV12ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v12" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV12BudgetDeclaration().Parent {
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
	if b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.CompletionV12) || m.ExecutorEquivalence == nil || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || validateWindowV12ExecutorEquivalence(m) != nil || b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || b.Observer != m.Observer || validateWindowV11SpoolBinding(m) != nil || b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
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

func (a *windowV12Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v12 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v12 authorization changed")
	}
	b, err := verifyWindowV12ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || !reflect.DeepEqual(b, a.binding) {
		return fmt.Errorf("v12 authorization binding changed")
	}

	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV12Preparation struct{ ready *FullWindowActualReady }

// Retain the readiness cause without changing the established public message.
type windowV12PreparationChanged struct{ cause error }

func (e *windowV12PreparationChanged) Error() string { return "v12 preparation changed" }
func (e *windowV12PreparationChanged) Unwrap() error { return e.cause }

func CheckFullWindowV12Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV12Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 12)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV12Preparation{r}, nil
}
func AuthorizeFullWindowV12Preparation(p *FullWindowV12Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v12 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 12)
	if err != nil {
		return nil, &windowV12PreparationChanged{cause: err}
	}
	if !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v12 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v12 attestation absent")
	}
	binding, err := verifyWindowV12ExecutionAuthorization(raw, fresh.manifest, fullWindowV12ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV12Authority{manifest: fresh.manifest, manifestSHA: fullWindowV12ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV12() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v12 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v12 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV12Preparation(&FullWindowV12Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV11, fresh.stoppedV11) {
		return fmt.Errorf("v12 ready changed")
	}
	return nil
}
func loadWindowActualStoppedBaselineV12(m windowActualManifest) (windowStoppedV11Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV11Baseline{}, fmt.Errorf("v12 typed baseline absent")
	}
	p, err := loadWindowStoppedV11BaselineContext(context.Background(), m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV12(m, p) != nil {
		return windowStoppedV11Baseline{}, fmt.Errorf("v12 typed baseline rejected")
	}
	return p, nil
}
func validateWindowActualStoppedBindingV12(m windowActualManifest, p windowStoppedV11Baseline) error {
	if m.Schema != "full-window-actual-ready.v12" || m.Scope.Schema != "full-window-scope.v12" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV12BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV11WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV12Declarations(m.Scope) != nil {
		return fmt.Errorf("v12 baseline binding rejected")
	}
	for _, root := range p.predecessorRoots() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) || !windowPayloadDisjoint(m.SpoolRoot, root) || !windowPayloadDisjoint(m.SourceRoot, root) {
			return fmt.Errorf("v12 roots overlap")
		}
	}
	return nil
}
func (r *FullWindowActualReady) revalidateStoppedV12() error {
	if r == nil || validateWindowActualStoppedBindingV12(r.manifest, r.stoppedV11) != nil {
		return fmt.Errorf("v12 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV12(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV11) || !reflect.DeepEqual(r.baseline, windowV12CandidateBaseline(p.base)) {
		return fmt.Errorf("v12 stopped evidence changed")
	}
	return nil
}

// Native accounting stays unchanged; the opaque view checks V12 authorization.
func (a *windowV12Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v12: a}
}

// The independently reviewed pins artifact is outside the source map, avoiding
// self-hashing. Only the dedicated compiled anchor can select it.
var fullWindowV12EquivalencePinsSHA string

type windowV12ReviewedPins struct {
	Schema                string `json:"schema"`
	OriginalSourcesSHA256 string `json:"original_sources_sha256"`
	SourcesSHA256         string `json:"sources_sha256"`
	ChangesSHA256         string `json:"changes_sha256"`
}

func validateWindowV12ExecutorEquivalence(m windowActualManifest) error {
	if m.Schema != "full-window-actual-ready.v12" {
		if m.ExecutorEquivalencePins != nil {
			return fmt.Errorf("old epoch carries v12 review pins")
		}
		return nil
	}
	if !comparisonDigest(fullWindowV12EquivalencePinsSHA) || m.ExecutorEquivalencePins == nil || m.ExecutorEquivalencePins.SHA256 != fullWindowV12EquivalencePinsSHA || m.ExecutorEquivalence == nil {
		return fmt.Errorf("v12 trusted review anchor absent")
	}
	raw, e := readWindowStoppedV7File(m.ExecutorEquivalencePins.Path, fullWindowV12EquivalencePinsSHA, 65536)
	var p windowV12ReviewedPins
	if e != nil || decodeWindowActualJSON(raw, &p) != nil || p.Schema != "full-window-executor-equivalence-pins.v12" || p.OriginalSourcesSHA256 != expectedWindowV12ExecutorEquivalencePins().OriginalSourcesSHA256 {
		return fmt.Errorf("v12 review pins rejected")
	}
	raw, e = readWindowStoppedV7File(m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var eq windowExecutorEquivalenceV12
	if e != nil || decodeWindowActualJSON(raw, &eq) != nil {
		return fmt.Errorf("v12 equivalence unreadable")
	}
	return validateWindowV12ExecutorEquivalencePinnedData(m.Sources, eq, windowExecutorEquivalencePinsV12{p.OriginalSourcesSHA256, p.SourcesSHA256, p.ChangesSHA256})
}
func (r *FullWindowActualReady) revalidateStoppedActualEpoch() error {
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v15" {
		return r.revalidateStoppedV15()
	}
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v14" {
		return r.revalidateStoppedV14()
	}
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v13" {
		return r.revalidateStoppedV13()
	}
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v12" {
		return r.revalidateStoppedV12()
	}
	return r.revalidateStopped()
}
