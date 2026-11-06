package taskbench

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// Dedicated V14 build anchors. New timing-policy approval is mandatory in
// addition to retained budget/autonomy evidence; exact-scope review stays false.
var fullWindowV14ManifestSHA, fullWindowV14ArchiveCommit, fullWindowV14ArchiveTree string

type windowV14Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV14ExecutionBinding
}

func verifyWindowV14ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV14ExecutionBinding, error) {
	reject := func() (windowV14ExecutionBinding, error) {
		return windowV14ExecutionBinding{}, fmt.Errorf("v14 signed delegated execution authorization rejected")
	}
	var a windowV14ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v14" || m.Scope.Schema != "full-window-scope.v14" || validateWindowV14Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV14ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v14" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV14BudgetDeclaration().Parent {
		return reject()
	}
	if !comparisonDigest(manifestSHA) || !comparisonDigest(binarySHA) || b.ManifestSHA256 != manifestSHA || b.BinarySHA256 != binarySHA || b.SourceSHA256 != conversationHash(sources) || !comparisonDigest(m.ScopeSHA256) || !comparisonDigest(m.ExecutionSHA256) || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.BudgetDeclarationSHA256 != m.Scope.BudgetDeclarationSHA256 || b.RetryPolicySHA256 != m.Scope.RetryPolicySHA256 {
		return reject()
	}
	if validateWindowV14OperationPolicyBinding(b, m) != nil {
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
	approvalRaw, err := readWindowStoppedV7File(b.OperationPolicyApproval.Path, b.OperationPolicyApproval.SHA256, 65536)
	if err != nil {
		return reject()
	}
	approval, err := verifyWindowV14OperationPolicyApproval(approvalRaw, m, manifestSHA, binarySHA)
	if err != nil || approval.OperationPolicySHA256 != b.OperationPolicySHA256 {
		return reject()
	}
	if b.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || b.CollectionPolicySHA256 != m.Scope.CollectionPolicySHA256 || b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.CompletionV14) || m.ExecutorEquivalence == nil || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || validateWindowV14ExecutorEquivalence(m) != nil || b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || b.Observer != m.Observer || validateWindowV11SpoolBinding(m) != nil || b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
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

func (a *windowV14Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v14 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v14 authorization changed")
	}
	b, err := verifyWindowV14ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || !reflect.DeepEqual(b, a.binding) {
		return fmt.Errorf("v14 authorization binding changed")
	}

	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV14Preparation struct{ ready *FullWindowActualReady }

// Retain the readiness cause without changing the established public message.
type windowV14PreparationChanged struct{ cause error }

func (e *windowV14PreparationChanged) Error() string { return "v14 preparation changed" }
func (e *windowV14PreparationChanged) Unwrap() error { return e.cause }

func CheckFullWindowV14Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV14Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 14)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV14Preparation{r}, nil
}
func AuthorizeFullWindowV14Preparation(p *FullWindowV14Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v14 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 14)
	if err != nil {
		return nil, &windowV14PreparationChanged{cause: err}
	}
	if !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v14 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v14 attestation absent")
	}
	binding, err := verifyWindowV14ExecutionAuthorization(raw, fresh.manifest, fullWindowV14ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV14Authority{manifest: fresh.manifest, manifestSHA: fullWindowV14ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV14() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v14 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v14 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV14Preparation(&FullWindowV14Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV13, fresh.stoppedV13) {
		return fmt.Errorf("v14 ready changed")
	}
	return nil
}

// The Background wrapper is limited to read-only technical preparation.
func loadWindowActualStoppedBaselineV14(m windowActualManifest) (windowStoppedV13Baseline, error) {
	return loadWindowActualStoppedBaselineV14Context(context.Background(), m)
}
func loadWindowActualStoppedBaselineV14Context(ctx context.Context, m windowActualManifest) (windowStoppedV13Baseline, error) {
	if ctx == nil {
		return windowStoppedV13Baseline{}, fmt.Errorf("v14 stopped context absent")
	}
	if err := ctx.Err(); err != nil {
		return windowStoppedV13Baseline{}, err
	}
	if m.BaselineEvidence == nil {
		return windowStoppedV13Baseline{}, fmt.Errorf("v14 typed baseline absent")
	}
	p, err := loadWindowStoppedV13BaselineContext(ctx, m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if ctx.Err() != nil {
		return windowStoppedV13Baseline{}, ctx.Err()
	}
	if err != nil || validateWindowActualStoppedBindingV14(m, p) != nil {
		return windowStoppedV13Baseline{}, fmt.Errorf("v14 typed baseline rejected")
	}
	if ctx.Err() != nil {
		return windowStoppedV13Baseline{}, ctx.Err()
	}
	return p, nil
}

func validateWindowActualStoppedBindingV14(m windowActualManifest, p windowStoppedV13Baseline) error {
	if m.Schema != "full-window-actual-ready.v14" || m.Scope.Schema != "full-window-scope.v14" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV14BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV13WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV14Declarations(m.Scope) != nil {
		return fmt.Errorf("v14 baseline binding rejected")
	}
	for _, root := range p.predecessorRoots() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) || !windowPayloadDisjoint(m.SpoolRoot, root) || !windowPayloadDisjoint(m.SourceRoot, root) {
			return fmt.Errorf("v14 roots overlap")
		}
	}
	return nil
}

// Operations must call the context variant with the whole-window context.
func (r *FullWindowActualReady) revalidateStoppedV14() error {
	return r.revalidateStoppedV14Context(context.Background())
}
func (r *FullWindowActualReady) revalidateStoppedV14Context(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("v14 stopped context absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || validateWindowActualStoppedBindingV14(r.manifest, r.stoppedV13) != nil {
		return fmt.Errorf("v14 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV14Context(ctx, r.manifest)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !reflect.DeepEqual(p, r.stoppedV13) || !reflect.DeepEqual(r.baseline, windowV14CandidateBaseline(p.base)) {
		return fmt.Errorf("v14 stopped evidence changed")
	}
	return nil
}

// Native accounting stays unchanged; the opaque view checks V14 authorization.
func (a *windowV14Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v14: a}
}

// The independently reviewed pins artifact is outside the source map, avoiding
// self-hashing. Only the dedicated compiled anchor can select it.
var fullWindowV14EquivalencePinsSHA string

type windowV14ReviewedPins struct {
	Schema                string `json:"schema"`
	OriginalSourcesSHA256 string `json:"original_sources_sha256"`
	SourcesSHA256         string `json:"sources_sha256"`
	ChangesSHA256         string `json:"changes_sha256"`
}

func validateWindowV14ExecutorEquivalence(m windowActualManifest) error {
	if m.Schema != "full-window-actual-ready.v14" {
		if m.ExecutorEquivalencePins != nil {
			return fmt.Errorf("old epoch carries v14 review pins")
		}
		return nil
	}
	if !comparisonDigest(fullWindowV14EquivalencePinsSHA) || m.ExecutorEquivalencePins == nil || m.ExecutorEquivalencePins.SHA256 != fullWindowV14EquivalencePinsSHA || m.ExecutorEquivalence == nil {
		return fmt.Errorf("v14 trusted review anchor absent")
	}
	raw, e := readWindowStoppedV7File(m.ExecutorEquivalencePins.Path, fullWindowV14EquivalencePinsSHA, 65536)
	var p windowV14ReviewedPins
	if e != nil || decodeWindowActualJSON(raw, &p) != nil || p.Schema != "full-window-executor-equivalence-pins.v14" || p.OriginalSourcesSHA256 != expectedWindowV14ExecutorEquivalencePins().OriginalSourcesSHA256 {
		return fmt.Errorf("v14 review pins rejected")
	}
	raw, e = readWindowStoppedV7File(m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var eq windowExecutorEquivalenceV14
	if e != nil || decodeWindowActualJSON(raw, &eq) != nil {
		return fmt.Errorf("v14 equivalence unreadable")
	}
	return validateWindowV14ExecutorEquivalencePinnedData(m.Sources, eq, windowExecutorEquivalencePinsV14{p.OriginalSourcesSHA256, p.SourcesSHA256, p.ChangesSHA256})
}

// The policy declaration alone is not permission. The execution signer must bind
// a different approval object at the one fixed companion path, and the verifier
// below must authenticate that object's separate new human reply bytes.
func validateWindowV14OperationPolicyBinding(b windowV14ExecutionBinding, m windowActualManifest) error {
	want := windowV8DeclarationSHA(expectedWindowV14OperationPolicy())
	ref := b.OperationPolicyApproval
	if m.Schema != "full-window-actual-ready.v14" || !filepath.IsAbs(m.CampaignRoot) || filepath.Clean(m.CampaignRoot) != m.CampaignRoot || filepath.Base(m.CampaignRoot) != "campaign" || b.OperationPolicySHA256 != want || windowV8DeclarationSHA(m.Scope.OperationPolicyV14) != want || ref.Path != filepath.Join(filepath.Dir(m.CampaignRoot), "operation-policy-approval.json") || !comparisonDigest(ref.SHA256) || ref.SHA256 == windowStoppedV8ApprovalSHA || ref.SHA256 == windowV9AutonomousAuthorizationSHA {
		return fmt.Errorf("v14 distinct fixed operation-policy approval required")
	}
	return nil
}
