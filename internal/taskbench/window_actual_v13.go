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

// Dedicated V13 build anchors. Authority records prior autonomous delegation and the latest budget reply; it is not a new human review of these exact hashes.
var fullWindowV13ManifestSHA, fullWindowV13ArchiveCommit, fullWindowV13ArchiveTree string

// The authenticated signer reviews the retained autonomous instruction and latest
// budget reply. Go verifies their bytes and exact technical binding; it does not
// infer human intent or label the new scope as human reviewed.
type windowV13ExecutionBinding struct {
	windowV11ExecutionBinding
	CollectionPolicySHA256    string `json:"collection_policy_sha256"`
	CompletionSHA256          string `json:"completion_sha256"`
	ExecutorEquivalenceSHA256 string `json:"executor_equivalence_sha256"`
}

type windowV13ExecutionAuthorization struct {
	windowV13ExecutionBinding
	Signature string `json:"signature"`
}
type windowV13Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV13ExecutionBinding
}

func verifyWindowV13ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV13ExecutionBinding, error) {
	reject := func() (windowV13ExecutionBinding, error) {
		return windowV13ExecutionBinding{}, fmt.Errorf("v13 signed delegated execution authorization rejected")
	}
	var a windowV13ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v13" || m.Scope.Schema != "full-window-scope.v13" || validateWindowV13Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV13ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v13" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV13BudgetDeclaration().Parent {
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
	if b.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || b.CollectionPolicySHA256 != m.Scope.CollectionPolicySHA256 || b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.CompletionV13) || m.ExecutorEquivalence == nil || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || validateWindowV13ExecutorEquivalence(m) != nil || b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || b.Observer != m.Observer || validateWindowV11SpoolBinding(m) != nil || b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
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

func (a *windowV13Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v13 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v13 authorization changed")
	}
	b, err := verifyWindowV13ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || !reflect.DeepEqual(b, a.binding) {
		return fmt.Errorf("v13 authorization binding changed")
	}

	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV13Preparation struct{ ready *FullWindowActualReady }

// Retain the readiness cause without changing the established public message.
type windowV13PreparationChanged struct{ cause error }

func (e *windowV13PreparationChanged) Error() string { return "v13 preparation changed" }
func (e *windowV13PreparationChanged) Unwrap() error { return e.cause }

func CheckFullWindowV13Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV13Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 13)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV13Preparation{r}, nil
}
func AuthorizeFullWindowV13Preparation(p *FullWindowV13Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v13 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 13)
	if err != nil {
		return nil, &windowV13PreparationChanged{cause: err}
	}
	if !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v13 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v13 attestation absent")
	}
	binding, err := verifyWindowV13ExecutionAuthorization(raw, fresh.manifest, fullWindowV13ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV13Authority{manifest: fresh.manifest, manifestSHA: fullWindowV13ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV13() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v13 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v13 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV13Preparation(&FullWindowV13Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV12, fresh.stoppedV12) {
		return fmt.Errorf("v13 ready changed")
	}
	return nil
}
func loadWindowActualStoppedBaselineV13(m windowActualManifest) (windowStoppedV12Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV12Baseline{}, fmt.Errorf("v13 typed baseline absent")
	}
	p, err := loadWindowStoppedV12BaselineContext(context.Background(), m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV13(m, p) != nil {
		return windowStoppedV12Baseline{}, fmt.Errorf("v13 typed baseline rejected")
	}
	return p, nil
}
func validateWindowActualStoppedBindingV13(m windowActualManifest, p windowStoppedV12Baseline) error {
	if m.Schema != "full-window-actual-ready.v13" || m.Scope.Schema != "full-window-scope.v13" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV13BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV12WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV13Declarations(m.Scope) != nil {
		return fmt.Errorf("v13 baseline binding rejected")
	}
	for _, root := range p.predecessorRoots() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) || !windowPayloadDisjoint(m.SpoolRoot, root) || !windowPayloadDisjoint(m.SourceRoot, root) {
			return fmt.Errorf("v13 roots overlap")
		}
	}
	return nil
}
func (r *FullWindowActualReady) revalidateStoppedV13() error {
	if r == nil || validateWindowActualStoppedBindingV13(r.manifest, r.stoppedV12) != nil {
		return fmt.Errorf("v13 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV13(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV12) || !reflect.DeepEqual(r.baseline, windowV13CandidateBaseline(p.base)) {
		return fmt.Errorf("v13 stopped evidence changed")
	}
	return nil
}

// Native accounting stays unchanged; the opaque view checks V13 authorization.
func (a *windowV13Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v13: a}
}

// The independently reviewed pins artifact is outside the source map, avoiding
// self-hashing. Only the dedicated compiled anchor can select it.
var fullWindowV13EquivalencePinsSHA string

type windowV13ReviewedPins struct {
	Schema                string `json:"schema"`
	OriginalSourcesSHA256 string `json:"original_sources_sha256"`
	SourcesSHA256         string `json:"sources_sha256"`
	ChangesSHA256         string `json:"changes_sha256"`
}

func validateWindowV13ExecutorEquivalence(m windowActualManifest) error {
	if m.Schema != "full-window-actual-ready.v13" {
		if m.ExecutorEquivalencePins != nil {
			return fmt.Errorf("old epoch carries v13 review pins")
		}
		return nil
	}
	if !comparisonDigest(fullWindowV13EquivalencePinsSHA) || m.ExecutorEquivalencePins == nil || m.ExecutorEquivalencePins.SHA256 != fullWindowV13EquivalencePinsSHA || m.ExecutorEquivalence == nil {
		return fmt.Errorf("v13 trusted review anchor absent")
	}
	raw, e := readWindowStoppedV7File(m.ExecutorEquivalencePins.Path, fullWindowV13EquivalencePinsSHA, 65536)
	var p windowV13ReviewedPins
	if e != nil || decodeWindowActualJSON(raw, &p) != nil || p.Schema != "full-window-executor-equivalence-pins.v13" || p.OriginalSourcesSHA256 != expectedWindowV13ExecutorEquivalencePins().OriginalSourcesSHA256 {
		return fmt.Errorf("v13 review pins rejected")
	}
	raw, e = readWindowStoppedV7File(m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var eq windowExecutorEquivalenceV13
	if e != nil || decodeWindowActualJSON(raw, &eq) != nil {
		return fmt.Errorf("v13 equivalence unreadable")
	}
	return validateWindowV13ExecutorEquivalencePinnedData(m.Sources, eq, windowExecutorEquivalencePinsV13{p.OriginalSourcesSHA256, p.SourcesSHA256, p.ChangesSHA256})
}
