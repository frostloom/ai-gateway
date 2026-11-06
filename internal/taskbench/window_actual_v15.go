package taskbench

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// Dedicated V15 build anchors. A fresh signature of the identical approved timing policy is mandatory in
// addition to retained budget/autonomy evidence; exact-scope review stays false.
var fullWindowV15ManifestSHA, fullWindowV15ArchiveCommit, fullWindowV15ArchiveTree string

type windowV15Authority struct {
	manifest                                                    windowActualManifest
	manifestSHA, binarySHA, authorizationPath, authorizationSHA string
	raw                                                         []byte
	binding                                                     windowV15ExecutionBinding
}

func verifyWindowV15ExecutionAuthorization(raw []byte, m windowActualManifest, manifestSHA, binarySHA string) (windowV15ExecutionBinding, error) {
	reject := func() (windowV15ExecutionBinding, error) {
		return windowV15ExecutionBinding{}, fmt.Errorf("v15 signed delegated execution authorization rejected")
	}
	var a windowV15ExecutionAuthorization
	if decodeWindowActualJSON(raw, &a) != nil || m.Schema != "full-window-actual-ready.v15" || m.Scope.Schema != "full-window-scope.v15" || validateWindowV15Declarations(m.Scope) != nil {
		return reject()
	}
	b := a.windowV15ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if b.Schema != "full-window-delegated-execution-authorization.v15" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || strings.TrimSpace(b.OwnerID) != b.OwnerID || b.OwnerID == "" || len(b.OwnerID) > 128 || strings.ContainsAny(b.OwnerID, "\r\n") || b.Parent != expectedWindowV15BudgetDeclaration().Parent {
		return reject()
	}
	if !comparisonDigest(manifestSHA) || !comparisonDigest(binarySHA) || b.ManifestSHA256 != manifestSHA || b.BinarySHA256 != binarySHA || b.SourceSHA256 != conversationHash(sources) || !comparisonDigest(m.ScopeSHA256) || !comparisonDigest(m.ExecutionSHA256) || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.BudgetDeclarationSHA256 != m.Scope.BudgetDeclarationSHA256 || b.RetryPolicySHA256 != m.Scope.RetryPolicySHA256 {
		return reject()
	}
	if validateWindowV15OperationPolicyBinding(b, m) != nil {
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
	approval, err := verifyWindowV15OperationPolicyApproval(approvalRaw, m, manifestSHA, binarySHA)
	if err != nil || approval.OperationPolicySHA256 != b.OperationPolicySHA256 {
		return reject()
	}
	if b.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || b.CollectionPolicySHA256 != m.Scope.CollectionPolicySHA256 || b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.CompletionV15) || m.ExecutorEquivalence == nil || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || validateWindowV15ExecutorEquivalence(m) != nil || b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || b.Observer != m.Observer || validateWindowV11SpoolBinding(m) != nil || b.HumanReviewedExactScope || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA {
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

func (a *windowV15Authority) revalidate() error {
	if a == nil || a.authorizationPath == "" || !comparisonDigest(a.authorizationSHA) {
		return fmt.Errorf("v15 authority absent")
	}
	raw, err := readWindowStoppedV7File(a.authorizationPath, a.authorizationSHA, 65536)
	if err != nil || !bytes.Equal(raw, a.raw) {
		return fmt.Errorf("v15 authorization changed")
	}
	b, err := verifyWindowV15ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA)
	if err != nil || !reflect.DeepEqual(b, a.binding) {
		return fmt.Errorf("v15 authorization binding changed")
	}

	return nil
}

// Preparation is opaque and cannot be passed to OpenFullWindowActual.
type FullWindowV15Preparation struct{ ready *FullWindowActualReady }

// Retain the readiness cause without changing the established public message.
type windowV15PreparationChanged struct{ cause error }

func (e *windowV15PreparationChanged) Error() string { return "v15 preparation changed" }
func (e *windowV15PreparationChanged) Unwrap() error { return e.cause }

func CheckFullWindowV15Preparation(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowV15Preparation, error) {
	summary, r, err := checkFullWindowActualReadyEpoch(o, 15)
	if err != nil {
		return summary, nil, err
	}
	return summary, &FullWindowV15Preparation{r}, nil
}
func AuthorizeFullWindowV15Preparation(p *FullWindowV15Preparation, authorizationPath string) (*FullWindowActualReady, error) {
	if p == nil || p.ready == nil {
		return nil, fmt.Errorf("v15 preparation absent")
	}
	_, fresh, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{p.ready.manifestPath}, 15)
	if err != nil {
		return nil, &windowV15PreparationChanged{cause: err}
	}
	if !bytes.Equal(p.ready.manifestRaw, fresh.manifestRaw) {
		return nil, fmt.Errorf("v15 preparation changed")
	}
	raw, err := readWindowV8SignedBytes(authorizationPath, 65536)
	if err != nil {
		return nil, err
	}
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(fresh.attestationRaw, &att) != nil {
		return nil, fmt.Errorf("v15 attestation absent")
	}
	binding, err := verifyWindowV15ExecutionAuthorization(raw, fresh.manifest, fullWindowV15ManifestSHA, att.BinarySHA256)
	if err != nil {
		return nil, err
	}
	authority := &windowV15Authority{manifest: fresh.manifest, manifestSHA: fullWindowV15ManifestSHA, binarySHA: att.BinarySHA256, authorizationPath: authorizationPath, authorizationSHA: conversationHash(raw), raw: append([]byte(nil), raw...), binding: binding}
	view := authority.nativeView()
	fresh.v8Authority = view
	fresh.prepared.v8Authority = view
	return fresh, nil
}
func (r *FullWindowActualReady) revalidateV15() error {
	if r == nil || r.v8Authority == nil || r.v8Authority.v15 == nil || r.v8Authority.revalidate() != nil {
		return fmt.Errorf("v15 authority absent or changed")
	}
	fresh, err := AuthorizeFullWindowV15Preparation(&FullWindowV15Preparation{r}, r.v8Authority.authorizationPath)
	if err != nil || !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.stoppedV14, fresh.stoppedV14) {
		return fmt.Errorf("v15 ready changed")
	}
	return nil
}

// The Background wrapper is limited to read-only technical preparation.
func loadWindowActualStoppedBaselineV15(m windowActualManifest) (windowStoppedV14Baseline, error) {
	return loadWindowActualStoppedBaselineV15Context(context.Background(), m)
}
func loadWindowActualStoppedBaselineV15Context(ctx context.Context, m windowActualManifest) (windowStoppedV14Baseline, error) {
	if ctx == nil {
		return windowStoppedV14Baseline{}, fmt.Errorf("v15 stopped context absent")
	}
	if err := ctx.Err(); err != nil {
		return windowStoppedV14Baseline{}, err
	}
	if m.BaselineEvidence == nil {
		return windowStoppedV14Baseline{}, fmt.Errorf("v15 typed baseline absent")
	}
	p, err := loadWindowStoppedV14BaselineContext(ctx, m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if ctx.Err() != nil {
		return windowStoppedV14Baseline{}, ctx.Err()
	}
	if err != nil || validateWindowActualStoppedBindingV15(m, p) != nil {
		return windowStoppedV14Baseline{}, fmt.Errorf("v15 typed baseline rejected")
	}
	if ctx.Err() != nil {
		return windowStoppedV14Baseline{}, ctx.Err()
	}
	return p, nil
}

func validateWindowActualStoppedBindingV15(m windowActualManifest, p windowStoppedV14Baseline) error {
	if m.Schema != "full-window-actual-ready.v15" || m.Scope.Schema != "full-window-scope.v15" || m.BaselineEvidence == nil || m.BaselineEvidence.SHA256 != windowV15BaselineSHA || m.BaselineEvidence.SHA256 != p.evidenceSHA256 || m.BaselineEvidence.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV14WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV15Declarations(m.Scope) != nil {
		return fmt.Errorf("v15 baseline binding rejected")
	}
	for _, root := range p.predecessorRoots() {
		if !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) || !windowPayloadDisjoint(m.SpoolRoot, root) || !windowPayloadDisjoint(m.SourceRoot, root) {
			return fmt.Errorf("v15 roots overlap")
		}
	}
	return nil
}

// Operations must call the context variant with the whole-window context.
func (r *FullWindowActualReady) revalidateStoppedV15() error {
	return r.revalidateStoppedV15Context(context.Background())
}
func (r *FullWindowActualReady) revalidateStoppedV15Context(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("v15 stopped context absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || validateWindowActualStoppedBindingV15(r.manifest, r.stoppedV14) != nil {
		return fmt.Errorf("v15 stopped snapshot rejected")
	}
	p, err := loadWindowActualStoppedBaselineV15Context(ctx, r.manifest)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !reflect.DeepEqual(p, r.stoppedV14) || !reflect.DeepEqual(r.baseline, windowV15CandidateBaseline(p.base)) {
		return fmt.Errorf("v15 stopped evidence changed")
	}
	return nil
}

// Native accounting stays unchanged; the opaque view checks V15 authorization.
func (a *windowV15Authority) nativeView() *windowV8Authority {
	return &windowV8Authority{manifest: a.manifest, manifestSHA: a.manifestSHA, binarySHA: a.binarySHA, authorizationPath: a.authorizationPath, authorizationSHA: a.authorizationSHA, raw: append([]byte(nil), a.raw...), binding: a.binding.windowV8ExecutionBinding, v15: a}
}

// The independently reviewed pins artifact is outside the source map, avoiding
// self-hashing. Only the dedicated compiled anchor can select it.
var fullWindowV15EquivalencePinsSHA string

type windowV15ReviewedPins struct {
	Schema                string `json:"schema"`
	OriginalSourcesSHA256 string `json:"original_sources_sha256"`
	SourcesSHA256         string `json:"sources_sha256"`
	ChangesSHA256         string `json:"changes_sha256"`
}

func validateWindowV15ExecutorEquivalence(m windowActualManifest) error {
	if m.Schema != "full-window-actual-ready.v15" {
		if m.ExecutorEquivalencePins != nil {
			return fmt.Errorf("old epoch carries v15 review pins")
		}
		return nil
	}
	if !comparisonDigest(fullWindowV15EquivalencePinsSHA) || m.ExecutorEquivalencePins == nil || m.ExecutorEquivalencePins.SHA256 != fullWindowV15EquivalencePinsSHA || m.ExecutorEquivalence == nil {
		return fmt.Errorf("v15 trusted review anchor absent")
	}
	raw, e := readWindowStoppedV7File(m.ExecutorEquivalencePins.Path, fullWindowV15EquivalencePinsSHA, 65536)
	var p windowV15ReviewedPins
	if e != nil || decodeWindowActualJSON(raw, &p) != nil || p.Schema != "full-window-executor-equivalence-pins.v15" || p.OriginalSourcesSHA256 != expectedWindowV15ExecutorEquivalencePins().OriginalSourcesSHA256 {
		return fmt.Errorf("v15 review pins rejected")
	}
	raw, e = readWindowStoppedV7File(m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var eq windowExecutorEquivalenceV15
	if e != nil || decodeWindowActualJSON(raw, &eq) != nil {
		return fmt.Errorf("v15 equivalence unreadable")
	}
	return validateWindowV15ExecutorEquivalencePinnedData(m.Sources, eq, windowExecutorEquivalencePinsV15{p.OriginalSourcesSHA256, p.SourcesSHA256, p.ChangesSHA256})
}

// The policy declaration alone is not permission. The execution signer must bind
// a different approval object at the one fixed companion path, and the verifier
// below must authenticate that object's same approved human reply bytes.
func validateWindowV15OperationPolicyBinding(b windowV15ExecutionBinding, m windowActualManifest) error {
	want := windowV8DeclarationSHA(expectedWindowV14OperationPolicy())
	ref := b.OperationPolicyApproval
	if m.Schema != "full-window-actual-ready.v15" || !filepath.IsAbs(m.CampaignRoot) || filepath.Clean(m.CampaignRoot) != m.CampaignRoot || filepath.Base(m.CampaignRoot) != "campaign" || b.OperationPolicySHA256 != want || windowV8DeclarationSHA(m.Scope.OperationPolicyV14) != want || ref.Path != filepath.Join(filepath.Dir(m.CampaignRoot), "operation-policy-approval.json") || !comparisonDigest(ref.SHA256) || ref.SHA256 == windowStoppedV8ApprovalSHA || ref.SHA256 == windowV9AutonomousAuthorizationSHA {
		return fmt.Errorf("v15 distinct fixed operation-policy approval required")
	}
	return nil
}

// FullWindowPhaseTimingV15 is bounded diagnostic DATA, never execution or
// completion evidence. Values carry neither input content nor error details.
type FullWindowPhaseTimingV15 struct {
	Stage               string `json:"stage"`
	DurationMS          int64  `json:"duration_ms"`
	DeadlineRemainingMS int64  `json:"deadline_remaining_ms"`
	Outcome             string `json:"outcome"`
}

func (s *FullWindowActualSession) resetPhaseTimingsV15() {
	if s == nil || s.ready == nil || s.ready.manifest.Schema != "full-window-actual-ready.v15" {
		return
	}
	s.mu.Lock()
	s.status.PhaseTimingsV15 = nil
	s.mu.Unlock()
}
func (s *FullWindowActualSession) recordPhaseV15(ctx context.Context, stage string, started time.Time, err error) {
	if s == nil || s.ready == nil || s.ready.manifest.Schema != "full-window-actual-ready.v15" {
		return
	}
	switch stage {
	case "authority", "history", "current_source", "other_inputs", "observer", "reports", "registry", "acquisition":
	default:
		return
	}
	duration := time.Since(started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	var remaining int64
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			remaining = time.Until(deadline).Milliseconds()
		}
	}
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 600000 {
		remaining = 600000
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
		if errors.Is(err, context.DeadlineExceeded) {
			outcome = "deadline_exceeded"
		} else if errors.Is(err, context.Canceled) {
			outcome = "canceled"
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.status.PhaseTimingsV15) < 16 {
		s.status.PhaseTimingsV15 = append(s.status.PhaseTimingsV15, FullWindowPhaseTimingV15{stage, duration, remaining, outcome})
	}
}
func (s *FullWindowActualSession) timedPhaseV15(ctx context.Context, stage string, run func() error) error {
	started := time.Now()
	err := run()
	s.recordPhaseV15(ctx, stage, started, err)
	return err
}

// The same checks and read order as the V14 input path; timers only observe
// their return values. Every history reader receives the whole-window context.
func (s *FullWindowActualSession) verifyInputsV15Context(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("actual verification operation context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r := s.ready
	if r == nil || len(r.manifestRaw) == 0 {
		return fmt.Errorf("actual frozen inputs absent")
	}
	if err := s.timedPhaseV15(ctx, "authority", func() error { return requireWindowV8ExecutionAuthority(r.v8Authority) }); err != nil {
		return err
	}
	if err := s.timedPhaseV15(ctx, "history", func() error { return r.revalidateStoppedV15Context(ctx) }); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m := r.manifest
	if err := s.timedPhaseV15(ctx, "other_inputs", func() error {
		if err := validateWindowActualHelperCatalog(m, r.manifestPath); err != nil {
			return err
		}
		raw, e := readWindowActualFile(r.manifestPath, windowActualManifestAnchor(m), 8<<20)
		if e != nil || !reflect.DeepEqual(raw, r.manifestRaw) {
			return fmt.Errorf("actual manifest changed")
		}
		return r.verifyBinaryAttestation()
	}); err != nil {
		return err
	}
	if err := s.timedPhaseV15(ctx, "current_source", func() error {
		if _, e := validateWindowActualSource(m); e != nil {
			return e
		}
		source, e := LoadConversations(m.DefinitionsDir)
		if e != nil || !reflect.DeepEqual(source, r.source) {
			return fmt.Errorf("actual source changed")
		}
		profiles, e := LoadPairedProfiles(m.ProfilesDir)
		if e != nil || !reflect.DeepEqual(profiles, r.profiles) {
			return fmt.Errorf("actual profiles changed")
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.timedPhaseV15(ctx, "other_inputs", func() error {
		for _, f := range []windowActualFile{m.FullGo.Result, m.Prices, m.Envelope} {
			raw, e := readWindowActualFile(f.Path, f.SHA256, 64<<20)
			if e != nil {
				return e
			}
			if f.Path != m.FullGo.Result.Path {
				var evidence windowActualProviderEvidence
				if decodeWindowActualJSON(raw, &evidence) != nil {
					return fmt.Errorf("actual evidence invalid")
				}
				for _, basis := range evidence.Basis {
					if _, e := readWindowActualFile(basis.Path, basis.SHA256, 64<<20); e != nil {
						return e
					}
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return s.timedPhaseV15(ctx, "observer", func() error {
		if s.auth.observer == nil || s.auth.observer.EvaluationIdentity() != m.Observer || s.auth.observerIdentity != m.Observer {
			return fmt.Errorf("actual observer changed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return checkWindowObservationReceipt(ctx, m.Scope.Schema, s.auth.observer, m.Observer)
	})
}
func (s *FullWindowActualSession) verifyV15Context(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("actual verification operation context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.verifyInputsV15Context(ctx); err != nil {
		return err
	}
	if err := s.timedPhaseV15(ctx, "reports", s.verifyReports); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.timedPhaseV15(ctx, "registry", func() error {
		if s.campaign == nil {
			return fmt.Errorf("actual campaign absent")
		}
		s.campaign.mu.Lock()
		defer s.campaign.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.campaign.closed || s.campaign.poisoned || s.campaign.inventoryWindowCampaign() != nil {
			return fmt.Errorf("actual live owner changed")
		}
		return nil
	})
}
