package taskbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
)

// The collector issues owned checkpoints from live execution proofs. There is
// no production acquisition-authorization issuer or paid entry. The standalone
// first-claim primitive cannot be run first and resumed here: the campaign owner
// must acquire the first claim and all later claims under one reviewed contract.
type windowCampaignProviderBounds struct {
	Model             string `json:"model"`
	MaxRequestBytes   int    `json:"max_request_bytes"`
	MaxInput          int64  `json:"max_input_tokens"`
	MaxOutput         int64  `json:"max_output_tokens"`
	InputRate         int64  `json:"input_micro_per_million"`
	OutputRate        int64  `json:"output_micro_per_million"`
	CacheHitInputRate int64  `json:"cache_hit_input_micro_per_million,omitempty"`
}
type windowCampaignLimits struct {
	Model windowCampaignProviderBounds `json:"model"`
	JEV   windowCampaignProviderBounds `json:"jev"`
}
type windowCampaignExecutionBinding struct {
	Schema       string               `json:"schema"`
	ScopeSHA256  string               `json:"scope_sha256"`
	AbsoluteRoot string               `json:"absolute_root"`
	Limits       windowCampaignLimits `json:"provider_limits"`
}

func executionSHAWindowCampaign(scopeSHA, absoluteRoot string, limits windowCampaignLimits) (string, error) {
	if e := canonicalRootWindowCampaign(absoluteRoot); e != nil {
		return "", e
	}
	if !comparisonDigest(scopeSHA) {
		return "", fmt.Errorf("validated scope digest required")
	}
	if limits.Model.Model != "deepseek-flash" || limits.JEV.Model != "jev-1.13.0" || limits.Model.MaxOutput != 128 {
		return "", fmt.Errorf("fixed campaign models/output bound required")
	}
	for i, p := range []windowCampaignProviderBounds{limits.Model, limits.JEV} {
		provider := windowProviderLimits{Model: p.Model, MaxAttempts: 1, MaxRequestBytes: p.MaxRequestBytes, MaxInput: p.MaxInput, MaxOutput: p.MaxOutput, InputRate: p.InputRate, OutputRate: p.OutputRate, CacheHitInputRate: p.CacheHitInputRate}
		if p.CacheHitInputRate < 0 || p.CacheHitInputRate > p.InputRate || (i == 1 && p.CacheHitInputRate != 0) {
			return "", fmt.Errorf("invalid execution cache rate")
		}
		if e := provider.validate(i == 0); e != nil {
			return "", e
		}
	}
	raw, e := json.Marshal(windowCampaignExecutionBinding{Schema: "full-window-campaign-execution.v1", ScopeSHA256: scopeSHA, AbsoluteRoot: absoluteRoot, Limits: limits})
	if e != nil {
		return "", e
	}
	return conversationHash(raw), nil
}

type windowCampaignPrepared struct {
	root           string
	scopeSHA       string
	executionSHA   string
	limits         windowCampaignLimits
	scope          fullWindowScope
	contracts      []windowAccountingContract
	stoppedHistory windowStoppedHistory
	v8Authority    *windowV8Authority
}
type windowCampaignArmEvidence struct {
	QualityEnd                   *windowQualityEndEvidence `json:",omitempty"`
	Ordinal, Pass, ObservedTurns int
	Key, Arm                     string
	RecordingSHA256              string
	Cleanup, Observer            bool
}

// Unexported ownership fields cannot be populated by JSON or journal replay.
// The trusted collector mints checkpoints only after validating live driver
// proofs. Read-only data and JSON cannot populate the owner fields.
type windowCampaignOwnedCheckpoint struct {
	owner    *windowCampaign
	ledger   *windowAccounting
	index    int
	evidence []windowCampaignArmEvidence
}
type windowCampaignClaim struct {
	Schema                   string                `json:"schema"`
	BaselineEvidenceSHA256   string                `json:"baseline_evidence_sha256,omitempty"`
	BudgetApprovalSHA256     string                `json:"budget_approval_sha256,omitempty"`
	ParentModel              int64                 `json:"parent_model_micro_cny,omitempty"`
	ParentJEV                int64                 `json:"parent_jev_micro_usd,omitempty"`
	Root                     string                `json:"root"`
	Index                    int                   `json:"index"`
	ScopeSHA256              string                `json:"scope_sha256"`
	ExecutionSHA256          string                `json:"execution_sha256"`
	DefinitionSHA256         string                `json:"definition_sha256"`
	ManifestSHA256           string                `json:"manifest_sha256"`
	SelectionSHA256          string                `json:"selection_sha256"`
	ProfileSHA256            string                `json:"profile_sha256"`
	ScheduleSHA256           string                `json:"schedule_sha256"`
	PolicySHA256             string                `json:"policy_sha256"`
	PreviousClaimSHA256      string                `json:"previous_claim_sha256,omitempty"`
	PreviousCheckpointSHA256 string                `json:"previous_checkpoint_sha256,omitempty"`
	Window                   fullWindowDeclaration `json:"window"`
	HistoricalClaims         map[string]string     `json:"historical_claims"`
	ModelPermanent           int64                 `json:"permanent_model_micro_cny"`
	JEVPermanent             int64                 `json:"permanent_jev_micro_usd"`
}
type windowCampaignCheckpoint struct {
	V8               *windowV8RuntimeSummary     `json:"v8,omitempty"`
	Schema           string                      `json:"schema"`
	Index            int                         `json:"index"`
	ScopeSHA256      string                      `json:"scope_sha256"`
	ExecutionSHA256  string                      `json:"execution_sha256"`
	DefinitionSHA256 string                      `json:"definition_sha256"`
	ScheduleSHA256   string                      `json:"schedule_sha256"`
	PolicySHA256     string                      `json:"policy_sha256"`
	ClaimSHA256      string                      `json:"claim_sha256"`
	LedgerSHA256     string                      `json:"ledger_sha256"`
	Evidence         []windowCampaignArmEvidence `json:"evidence"`
}
type windowCampaign struct {
	acquisitionContext      context.Context
	acquisitionContextIndex int
	mu                      sync.Mutex
	root                    string
	rootInfo                os.FileInfo
	lock                    *os.File
	openFile                func(string) (windowJournalFile, error)
	prepared                windowCampaignPrepared
	baseline                fullWindowBaseline
	historicalDir           string
	index                   int
	model, jev              int64
	claimSHA                string
	files                   map[string]string
	ledgerAudits            map[string]string
	ledger                  *windowAccounting
	ledgerDir               string
	poisoned, closed        bool
	closeErr                error
	extras                  []windowV8ExtraClaim
	extraSHA                string
	modelExtras, jevExtras  int
}

func canonicalRootWindowCampaign(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return fmt.Errorf("canonical absolute campaign root required")
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(root))
	if e != nil || filepath.Join(parent, filepath.Base(root)) != root {
		return fmt.Errorf("campaign root has unresolved or aliased parent")
	}
	return nil
}
func contractWindowCampaign(scope fullWindowScope, sha string, w fullWindowDeclaration, limits windowCampaignLimits) windowAccountingContract {
	convert := func(p windowCampaignProviderBounds, attempts int) windowProviderLimits {
		return windowProviderLimits{Model: p.Model, MaxAttempts: attempts, MaxRequestBytes: p.MaxRequestBytes, MaxInput: p.MaxInput, MaxOutput: p.MaxOutput, InputRate: p.InputRate, OutputRate: p.OutputRate, CacheHitInputRate: p.CacheHitInputRate}
	}
	schema := "full-window-accounting.v1"
	if scope.Schema == "full-window-scope.v4" || scope.Schema == "full-window-scope.v5" || scope.Schema == "full-window-scope.v6" || scope.Schema == "full-window-scope.v7" {
		schema = "full-window-accounting.v2"
	}
	if scope.Schema == "full-window-scope.v8" {
		schema = windowAccountingV3
		w.MaxModelCalls += 20
		w.MaxJEVCalls += 20
	}
	return windowAccountingContract{Schema: schema, ScopeSHA256: sha, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: scope.Bindings.PolicySHA256, ClaimSHA256: sha, WindowID: w.ID, ModelCap: w.ModelCap, JEVCap: w.JEVCap, Model: convert(limits.Model, w.MaxModelCalls), JEV: convert(limits.JEV, w.MaxJEVCalls)}
}
func prepareWindowCampaign(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	var out windowCampaignPrepared
	if scope.Schema == "full-window-scope.v8" {
		return out, fmt.Errorf("v8 is a data-only declaration; campaign integration unavailable")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func prepareWindowCampaignData(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	var out windowCampaignPrepared
	if e := canonicalRootWindowCampaign(root); e != nil {
		return out, e
	}
	if root != frozenRoot {
		return out, fmt.Errorf("campaign root is not independently frozen root")
	}
	if e := validateWindowCampaignV7OrHistoricalLimits(scope, limits); e != nil {
		return out, e
	}
	sha, _, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		return out, e
	}
	if !comparisonDigest(frozenSHA) || sha != frozenSHA {
		return out, fmt.Errorf("campaign scope is not independently frozen scope")
	}
	if limits.Model.Model != "deepseek-flash" || limits.JEV.Model != "jev-1.13.0" || limits.Model.MaxOutput != 128 {
		return out, fmt.Errorf("fixed campaign models/output bound required")
	}
	raw, e := json.Marshal(scope)
	if e != nil {
		return out, e
	}
	var detached fullWindowScope
	if e = json.Unmarshal(raw, &detached); e != nil {
		return out, e
	}
	executionSHA, e := executionSHAWindowCampaign(sha, root, limits)
	if e != nil {
		return out, e
	}
	if !comparisonDigest(frozenExecutionSHA) || executionSHA != frozenExecutionSHA {
		return out, fmt.Errorf("campaign execution limits/root are not independently frozen")
	}
	out = windowCampaignPrepared{root: root, scopeSHA: sha, executionSHA: executionSHA, limits: limits, scope: detached}
	for _, w := range scope.Windows {
		c := contractWindowCampaign(scope, sha, w, limits)
		if e := c.validate(); e != nil {
			return windowCampaignPrepared{}, e
		}
		out.contracts = append(out.contracts, c)
	}
	return out, nil
}

// Production-validation wrapper reloads the exact raw nine predecessor claims
// and rederives the actual full source before any new registry write. The caller
// must obtain frozenRoot/frozenSHA/frozenExecutionSHA from independently frozen
// authorization. The execution digest binds actual rates/envelopes, but does not
// certify their evidence-file contents or official tariff interpretation. This
// function does not select a real root, cap, paid entry or certify price content.
func newWindowCampaign(root, frozenRoot, historicalDir string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v1" || scope.BaselineEvidenceSHA256 != "" {
		return nil, fmt.Errorf("legacy campaign scope version rejected")
	}
	p, e := prepareWindowCampaign(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
	if e != nil {
		return nil, e
	}
	baseline, e := loadFullWindowBaseline(historicalDir)
	if e != nil {
		return nil, e
	}
	c, e := startWindowCampaign(p, baseline)
	if e != nil {
		return nil, e
	}
	c.historicalDir = historicalDir
	return c, nil
}

// Internal primitive accepts already prepared private data. Only the validating
// wrapper calls it in production. It creates no provider capability or token.
func startWindowCampaign(p windowCampaignPrepared, baseline fullWindowBaseline) (*windowCampaign, error) {
	if p.scope.Schema == "full-window-scope.v8" && (p.v8Authority == nil || p.v8Authority.revalidate() != nil) {
		return nil, fmt.Errorf("v8 signed authority required")
	}
	if e := validatePreparedWindowCampaign(p, baseline); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(p.scope)
	if e != nil {
		return nil, e
	}
	var detached fullWindowScope
	if e = json.Unmarshal(raw, &detached); e != nil {
		return nil, e
	}
	p.scope = detached
	p.contracts = append([]windowAccountingContract(nil), p.contracts...)
	if e = os.Mkdir(p.root, 0700); e != nil {
		return nil, fmt.Errorf("existing or unavailable registry; audit only: %w", e)
	}
	lock, e := os.OpenFile(filepath.Join(p.root, ".lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		lock.Close()
		return nil, e
	}
	info, e := os.Lstat(p.root)
	if e != nil {
		lock.Close()
		return nil, e
	}
	c := &windowCampaign{root: p.root, rootInfo: info, lock: lock, prepared: p, baseline: baseline, model: baseline.PermanentModel, jev: baseline.PermanentJEV, files: map[string]string{}, ledgerAudits: map[string]string{}}
	c.baseline.ClaimSHA256 = cloneWindowHistoricalClaims(baseline.ClaimSHA256)
	if e = c.persistClaimWindowCampaign(0, "", ""); e != nil {
		c.closeWindowCampaign()
		return nil, e
	}
	return c, nil
}
func validatePreparedWindowCampaign(p windowCampaignPrepared, b fullWindowBaseline) error {
	if e := canonicalRootWindowCampaign(p.root); e != nil {
		return e
	}
	switch p.scope.Schema {
	case "full-window-scope.v8":
		if err := validateStoppedV8CampaignHistory(p, b); err != nil {
			return err
		}
	case "full-window-scope.v1":
		expected := fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29061120, PermanentJEV: 223104, RemainingModel: 70938880, RemainingJEV: 776896, ActualModelCalls: 143, ActualJEVCalls: 37, ClaimSHA256: fullWindowCanonicalClaims()}
		if p.stoppedHistory != nil || !reflect.DeepEqual(expected, b) {
			return fmt.Errorf("historical baseline differs from fixed permanent chain")
		}
	case "full-window-scope.v2":
		if err := validateStoppedCampaignHistory(p, b); err != nil {
			return err
		}
	case "full-window-scope.v3":
		if err := validateStoppedV3CampaignHistory(p, b); err != nil {
			return err
		}
	case "full-window-scope.v4":
		if err := validateStoppedV4CampaignHistory(p, b); err != nil {
			return err
		}
	case "full-window-scope.v5":
		if err := validateStoppedV5CampaignHistory(p, b); err != nil {
			return err
		}
	case "full-window-scope.v6":
		if err := validateStoppedV6CampaignHistory(p, b); err != nil {
			return err
		}
	case "full-window-scope.v7":
		if err := validateStoppedV7CampaignHistory(p, b); err != nil {
			return err
		}
	default:
		return fmt.Errorf("prepared campaign scope version rejected")
	}
	if err := validateWindowCampaignV7OrHistoricalLimits(p.scope, p.limits); err != nil {
		return err
	}
	raw, e := json.Marshal(p.scope)
	if e != nil || conversationHash(raw) != p.scopeSHA {
		return fmt.Errorf("prepared campaign scope changed")
	}
	executionSHA, e := executionSHAWindowCampaign(p.scopeSHA, p.root, p.limits)
	if e != nil || !comparisonDigest(p.executionSHA) || executionSHA != p.executionSHA {
		return fmt.Errorf("prepared campaign execution binding changed")
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: p.scopeSHA, ScheduleSHA256: p.scope.ScheduleSHA256, PolicySHA256: p.scope.Bindings.PolicySHA256, Windows: p.scope.Windows}
	for _, w := range d.Windows {
		if w.ModelCap > b.RemainingModel-d.ModelTotal || w.JEVCap > b.RemainingJEV-d.JEVTotal || w.ModelCap < 1 || w.JEVCap < 1 {
			return fmt.Errorf("campaign caps exceed baseline")
		}
		d.ModelTotal += w.ModelCap
		d.JEVTotal += w.JEVCap
	}
	claimModelBound, claimJEVBound := int64(70938880), int64(776896)
	if p.scope.Schema == "full-window-scope.v6" {
		claimModelBound, claimJEVBound = 85015146, 655442
	} else if p.scope.Schema == "full-window-scope.v7" {
		claimModelBound, claimJEVBound = 84510611, 650855
	} else if p.scope.Schema == "full-window-scope.v8" {
		claimModelBound, claimJEVBound = 84500000, 600000
	}
	if e := d.validateWithBounds(claimModelBound, claimJEVBound); e != nil {
		return e
	}
	if len(p.contracts) != len(d.Windows) {
		return fmt.Errorf("missing campaign contract")
	}
	for i, c := range p.contracts {
		w := d.Windows[i]
		if e := c.validate(); e != nil {
			return e
		}
		if c != contractWindowCampaign(p.scope, p.scopeSHA, w, p.limits) {
			return fmt.Errorf("prepared campaign contract changed")
		}
	}
	return nil
}
func (c *windowCampaign) persistWindowCampaign(name string, v any) error {
	return c.persistWindowCampaignContext(context.Background(), name, v)
}
func (c *windowCampaign) persistWindowCampaignContext(ctx context.Context, name string, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	raw = append(raw, '\n')
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return err
	}
	var f windowJournalFile
	if c.openFile != nil {
		f, e = c.openFile(filepath.Join(c.root, name))
	} else {
		f, e = os.OpenFile(filepath.Join(c.root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	}
	if e != nil {
		return e
	}
	n, w := f.Write(raw)
	if w == nil && n != len(raw) {
		w = fmt.Errorf("short write")
	}
	e = errors.Join(w, f.Sync(), f.Close(), windowSyncDirectory(c.root), windowSyncDirectory(filepath.Dir(c.root)))
	if e != nil {
		return errors.Join(e, c.checkWindowCampaignContextLocked(ctx))
	}
	c.files[name] = conversationHash(raw)
	return c.checkWindowCampaignContextLocked(ctx)
}
func (c *windowCampaign) persistClaimWindowCampaign(i int, previous, checkpoint string) error {
	return c.persistClaimWindowCampaignContext(context.Background(), i, previous, checkpoint)
}
func (c *windowCampaign) persistClaimWindowCampaignContext(ctx context.Context, i int, previous, checkpoint string) (result error) {
	defer func() {
		if result != nil {
			c.poisoned = true
		}
	}()
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return err
	}
	if i < 0 || i >= len(c.prepared.scope.Windows) {
		return fmt.Errorf("no next window")
	}
	if err := c.revalidateWindowV8AuthorityLocked(); err != nil {
		return err
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return err
	}
	if c.prepared.scope.Schema == "full-window-scope.v2" {
		if err := validateStoppedCampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	} else if c.prepared.scope.Schema == "full-window-scope.v3" {
		if err := validateStoppedV3CampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	} else if c.prepared.scope.Schema == "full-window-scope.v4" {
		if err := validateStoppedV4CampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	} else if c.prepared.scope.Schema == "full-window-scope.v5" {
		if err := validateStoppedV5CampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	} else if c.prepared.scope.Schema == "full-window-scope.v6" {
		if err := validateStoppedV6CampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	} else if c.prepared.scope.Schema == "full-window-scope.v7" {
		if err := validateStoppedV7CampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	} else if c.historicalDir != "" {
		baseline, e := loadFullWindowBaseline(c.historicalDir)
		if e != nil || !reflect.DeepEqual(baseline, c.baseline) {
			return fmt.Errorf("historical claim chain changed")
		}
	}
	if c.prepared.scope.Schema == "full-window-scope.v8" {
		if err := validateStoppedV8CampaignHistory(c.prepared, c.baseline); err != nil {
			return err
		}
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return err
	}
	w := c.prepared.scope.Windows[i]
	parentModel, parentJEV := int64(100000000), int64(1000000)
	if c.prepared.scope.Schema == "full-window-scope.v3" || c.prepared.scope.Schema == "full-window-scope.v4" || c.prepared.scope.Schema == "full-window-scope.v5" || c.prepared.scope.Schema == "full-window-scope.v6" || c.prepared.scope.Schema == "full-window-scope.v7" {
		parentModel, parentJEV = c.baseline.TotalModel, c.baseline.TotalJEV
	}
	if c.prepared.scope.Schema == "full-window-scope.v8" {
		parentModel, parentJEV = 140000000, 1100000
	}
	if w.ModelCap > parentModel-c.model || w.JEVCap > parentJEV-c.jev {
		return fmt.Errorf("permanent currency cap exhausted")
	}
	d := c.prepared.scope
	r := windowCampaignClaim{Schema: "full-window-campaign-claim.v1", Root: c.root, Index: i + 1, ScopeSHA256: c.prepared.scopeSHA, ExecutionSHA256: c.prepared.executionSHA, DefinitionSHA256: d.DefinitionSHA256, ManifestSHA256: d.ManifestSHA256, SelectionSHA256: d.SelectionSHA256, ProfileSHA256: d.ProfileSHA256, ScheduleSHA256: d.ScheduleSHA256, PolicySHA256: d.Bindings.PolicySHA256, PreviousClaimSHA256: previous, PreviousCheckpointSHA256: checkpoint, Window: w, HistoricalClaims: cloneWindowHistoricalClaims(c.baseline.ClaimSHA256), ModelPermanent: c.model + w.ModelCap, JEVPermanent: c.jev + w.JEVCap}
	if d.Schema == "full-window-scope.v2" {
		r.Schema, r.BaselineEvidenceSHA256 = "full-window-campaign-claim.v2", d.BaselineEvidenceSHA256
	} else if d.Schema == "full-window-scope.v3" {
		r.Schema, r.BaselineEvidenceSHA256 = "full-window-campaign-claim.v3", d.BaselineEvidenceSHA256
	}
	if d.Schema == "full-window-scope.v4" {
		r.Schema = "full-window-campaign-claim.v4"
		r.BaselineEvidenceSHA256 = d.BaselineEvidenceSHA256
		r.BudgetApprovalSHA256 = d.BudgetApprovalSHA256
		r.ParentModel = parentModel
		r.ParentJEV = parentJEV
	}
	if d.Schema == "full-window-scope.v5" {
		r.Schema = "full-window-campaign-claim.v5"
		r.BaselineEvidenceSHA256 = d.BaselineEvidenceSHA256
		r.BudgetApprovalSHA256 = d.BudgetApprovalSHA256
		r.ParentModel = parentModel
		r.ParentJEV = parentJEV
	}
	if d.Schema == "full-window-scope.v6" {
		r.Schema = "full-window-campaign-claim.v6"
		r.BaselineEvidenceSHA256 = d.BaselineEvidenceSHA256
		r.BudgetApprovalSHA256 = d.BudgetApprovalSHA256
		r.ParentModel = parentModel
		r.ParentJEV = parentJEV
	}
	if d.Schema == "full-window-scope.v7" {
		r.Schema = "full-window-campaign-claim.v7"
		r.BaselineEvidenceSHA256 = d.BaselineEvidenceSHA256
		r.BudgetApprovalSHA256 = d.BudgetApprovalSHA256
		r.ParentModel = parentModel
		r.ParentJEV = parentJEV
	}
	if d.Schema == "full-window-scope.v8" {
		r.Schema = "full-window-campaign-claim.v8"
		r.BaselineEvidenceSHA256 = d.BaselineEvidenceSHA256
		r.BudgetApprovalSHA256 = c.prepared.v8Authority.authorizationSHA
		r.ParentModel = parentModel
		r.ParentJEV = parentJEV
	}
	// A claim attempt consumes its whole cap conservatively even if the new
	// claim file is partial or its directory sync fails. This owner then halts;
	// reopening is prohibited and cannot interpret a partial claim as free.
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return err
	}
	c.model = r.ModelPermanent
	c.jev = r.JEVPermanent
	name := fmt.Sprintf("claim-%06d.json", i+1)
	if e := c.persistWindowCampaignContext(ctx, name, r); e != nil {
		return e
	}
	c.index = i + 1
	c.claimSHA = c.files[name]
	return nil
}
func (c *windowCampaign) inventoryWindowCampaign() error {
	info, e := os.Lstat(c.root)
	if e != nil || !os.SameFile(c.rootInfo, info) {
		return fmt.Errorf("root identity changed")
	}
	lockInfo, e := c.lock.Stat()
	if e != nil || lockInfo.Size() != 0 || c.prepared.scope.Schema == "full-window-scope.v8" && !windowStoppedV7SingleLink(lockInfo) {
		return fmt.Errorf("owned lock content changed")
	}
	pathInfo, e := os.Lstat(filepath.Join(c.root, ".lock"))
	if e != nil || !os.SameFile(lockInfo, pathInfo) || !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("owned lock changed")
	}
	entries, e := os.ReadDir(c.root)
	if e != nil {
		return e
	}
	want := 1 + len(c.files) + len(c.ledgerAudits)
	if c.ledgerDir != "" {
		want++
	}
	if len(entries) != want {
		return fmt.Errorf("unknown/partial registry files")
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".lock" {
			continue
		}
		if c.ledgerDir != "" && name == filepath.Base(c.ledgerDir) && entry.IsDir() {
			continue
		}
		if sha, ok := c.ledgerAudits[name]; ok && entry.IsDir() {
			if c.prepared.scope.Schema == "full-window-scope.v8" {
				if _, err := readWindowStoppedV7File(filepath.Join(c.root, name, "accounting.jsonl"), sha, 64<<20); err != nil {
					return fmt.Errorf("v8 sealed ledger changed")
				}
			}
			if e := verifySealedWindowAccounting(filepath.Join(c.root, name), sha); e != nil {
				return fmt.Errorf("previous ledger changed")
			}
			continue
		}
		h, ok := c.files[name]
		if !ok || !entry.Type().IsRegular() {
			return fmt.Errorf("unknown registry entry")
		}
		var raw []byte
		if c.prepared.scope.Schema == "full-window-scope.v8" {
			raw, e = readWindowStoppedV7File(filepath.Join(c.root, name), h, 8<<20)
		} else {
			raw, e = os.ReadFile(filepath.Join(c.root, name))
		}
		if e != nil || conversationHash(raw) != h {
			return fmt.Errorf("claim/checkpoint content changed")
		}
	}
	return nil
}

// Only a ledger fully replayed and closed by advanceWindowCampaign enters
// ledgerAudits. Rechecking its complete byte digest proves it is still exactly
// that validated journal; repeating semantic replay cannot add evidence. This
// check grants no execution or completion authority and never covers an active
// ledger, whose live descriptor and bytes are checked separately.
func verifySealedWindowAccounting(dir, expectedSHA string) error {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "accounting.jsonl" || !entries[0].Type().IsRegular() {
		return fmt.Errorf("sealed ledger inventory changed")
	}
	path := filepath.Join(dir, "accounting.jsonl")
	named, err := os.Lstat(path)
	if err != nil || !named.Mode().IsRegular() || named.Size() < 1 || named.Size() > 64<<20 {
		return fmt.Errorf("sealed ledger file changed")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(named, opened) {
		_ = file.Close()
		return fmt.Errorf("sealed ledger identity changed while opening")
	}
	digest := sha256.New()
	n, readErr := io.Copy(digest, io.LimitReader(file, (64<<20)+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return err
	}
	if n != named.Size() || n > 64<<20 || hex.EncodeToString(digest.Sum(nil)) != expectedSHA {
		return fmt.Errorf("sealed ledger bytes changed")
	}
	return nil
}

// openLedger binds a fresh live ledger to this owner's current durable claim.
// This creates no provider capability and cannot mint completion permission.
func (c *windowCampaign) openWindowCampaignLedger() (*windowAccounting, error) {
	return c.openWindowCampaignLedgerContext(context.Background())
}
func (c *windowCampaign) openWindowCampaignLedgerContext(ctx context.Context) (*windowAccounting, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := windowCampaignSuppliedContext(ctx); err != nil {
		c.poisoned = true
		return nil, err
	}
	if c.acquisitionContextIndex == c.index {
		if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
			c.poisoned = true
			return nil, err
		}
	}
	if err := c.revalidateWindowV8AuthorityLocked(); err != nil {
		return nil, err
	}
	if c.closed || c.poisoned || c.ledger != nil {
		return nil, fmt.Errorf("window campaign owner unavailable")
	}
	if e := c.inventoryWindowCampaign(); e != nil {
		c.poisoned = true
		return nil, e
	}
	if err := windowCampaignSuppliedContext(ctx); err != nil {
		c.poisoned = true
		return nil, err
	}
	if c.acquisitionContextIndex != c.index {
		c.acquisitionContext = ctx
		c.acquisitionContextIndex = c.index
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		c.poisoned = true
		return nil, err
	}
	contract := c.prepared.contracts[c.index-1]
	contract.ClaimSHA256 = c.claimSHA
	dir := filepath.Join(c.root, fmt.Sprintf("ledger-%06d", c.index))
	l, e := newWindowAccounting(dir, contract)
	if e != nil {
		c.poisoned = true
		return nil, e
	}
	c.ledger = l
	c.ledgerDir = dir
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		c.poisoned = true
		return nil, err
	}
	return l, nil
}
func (c *windowCampaign) validateEvidenceWindowCampaign(p *windowCampaignOwnedCheckpoint) error {
	if p == nil || p.owner != c || p.index != c.index || p.ledger == nil || p.ledger != c.ledger {
		return fmt.Errorf("missing live owned driver checkpoint")
	}
	w := c.prepared.scope.Windows[c.index-1]
	if len(p.evidence) != 2*(w.LastOrdinal-w.FirstOrdinal+1) {
		return fmt.Errorf("incomplete arm evidence")
	}
	j := 0
	for _, slot := range c.prepared.scope.Slots[w.FirstOrdinal-1 : w.LastOrdinal] {
		for _, arm := range slot.ArmOrder {
			e := p.evidence[j]
			j++
			if e.Ordinal != slot.Ordinal || e.Pass != slot.Pass || e.Key != slot.CaseKey || e.Arm != arm || (e.QualityEnd == nil && e.ObservedTurns != slot.UserTurns) || !e.Cleanup || !e.Observer || !comparisonDigest(e.RecordingSHA256) {
				return fmt.Errorf("wrong/missing owned execution evidence")
			}
			if e.QualityEnd != nil && validateWindowQualityEndEvidence(c.prepared.scope.Bindings.PolicySHA256, slot, arm, e.ObservedTurns, e.QualityEnd) != nil {
				return fmt.Errorf("invalid quality-end evidence")
			}

		}
	}
	return nil
}
func (c *windowCampaign) advanceWindowCampaign(p *windowCampaignOwnedCheckpoint) (string, error) {
	return c.advanceWindowCampaignContext(context.Background(), p)
}
func (c *windowCampaign) advanceWindowCampaignContext(ctx context.Context, p *windowCampaignOwnedCheckpoint) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.poisoned {
		return "", fmt.Errorf("owner halted")
	}
	fail := func(e error) (string, error) { c.poisoned = true; return "", e }
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	if e := c.validateEvidenceWindowCampaign(p); e != nil {
		return fail(e)
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	if e := c.inventoryWindowCampaign(); e != nil {
		return fail(e)
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	if e := c.ledger.Close(); e != nil {
		return fail(e)
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	audit, e := readWindowAccounting(c.ledgerDir)
	if e != nil || audit.CompletionProof != "unavailable_from_journal" || !audit.State.Closed || audit.State.Halt != "" || audit.State.Pending != nil {
		return fail(fmt.Errorf("live ledger closure not verified"))
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	d := c.prepared.scope
	checkpoint := windowCampaignCheckpoint{Schema: "full-window-campaign-owned-checkpoint.v1", Index: c.index, ScopeSHA256: c.prepared.scopeSHA, ExecutionSHA256: c.prepared.executionSHA, DefinitionSHA256: d.DefinitionSHA256, ScheduleSHA256: d.ScheduleSHA256, PolicySHA256: d.Bindings.PolicySHA256, ClaimSHA256: c.claimSHA, LedgerSHA256: audit.JournalSHA256, Evidence: append([]windowCampaignArmEvidence(nil), p.evidence...)}
	if d.Schema == "full-window-scope.v8" {
		if c.validateWindowV8ExtrasLocked(audit.State) != nil {
			return fail(fmt.Errorf("v8 extras unresolved"))
		}
		checkpoint.Schema = "full-window-campaign-owned-checkpoint.v8"
		checkpoint.V8 = c.snapshotWindowV8RuntimeLocked(audit.State, c.ledger.contract.WindowID)
	}
	name := fmt.Sprintf("checkpoint-%06d.json", c.index)
	if e := c.persistWindowCampaignContext(ctx, name, checkpoint); e != nil {
		return fail(e)
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	if e := c.inventoryWindowCampaign(); e != nil {
		return fail(e)
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	previous := c.claimSHA
	checkpointSHA := c.files[name]
	if c.index == len(d.Windows) {
		c.poisoned = true
		return checkpointSHA, nil
	}
	// Keep all previous ledger dirs in inventory as immutable audit evidence.
	c.ledgerAudits[filepath.Base(c.ledgerDir)] = audit.JournalSHA256
	c.ledger = nil
	c.ledgerDir = ""
	if e := c.persistClaimWindowCampaignContext(ctx, c.index, previous, checkpointSHA); e != nil {
		return fail(e)
	}
	if err := c.checkWindowCampaignContextLocked(ctx); err != nil {
		return fail(err)
	}
	return c.claimSHA, nil
}
func (c *windowCampaign) closeWindowCampaign() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return c.closeErr
	}
	c.closed = true
	var e error
	if c.ledger != nil {
		e = c.ledger.Close()
	}
	c.closeErr = errors.Join(e, syscall.Flock(int(c.lock.Fd()), syscall.LOCK_UN), c.lock.Close())
	return c.closeErr
}

// This validates checkpoint data after the collector checked its live proof.
// It cannot mint a checkpoint or replace genuine driver/cleanup authority.
func validateWindowQualityEndEvidence(policy string, slot fullWindowSlot, arm string, observed int, q *windowQualityEndEvidence) error {
	if q == nil || policy != windowOwnedQualityPolicySHA() || q.PolicySHA256 != policy || observed < 1 || observed > slot.UserTurns || q.ObservedTurns != observed || q.UnrunTurns != slot.UserTurns-observed || !comparisonDigest(q.SourceSHA256) || !comparisonDigest(q.RuleSHA256) || !comparisonDigest(q.CandidateSHA256) || !comparisonDigest(q.CleanupSHA256) || !comparisonDigest(q.EnvelopeSHA256) || q.CandidateFile == q.CleanupFile {
		return fmt.Errorf("invalid quality-end evidence")
	}
	for _, name := range []string{q.CandidateFile, q.CleanupFile} {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".json") || name == ".json" || strings.ContainsAny(name, "/\\") {
			return fmt.Errorf("invalid quality-end file")
		}
	}
	if arm != "model" && arm != "jev_selective" {
		return fmt.Errorf("invalid quality-end arm")
	}
	return nil
}
