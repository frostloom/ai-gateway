package taskbench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
)

type fullWindowBindings struct {
	PolicySHA256          string `json:"policy_sha256"`
	ExecutorRevision      string `json:"executor_revision"`
	CustomerRevision      string `json:"customer_revision"`
	PriceEvidenceSHA256   string `json:"price_evidence_sha256"`
	RequestEnvelopeSHA256 string `json:"request_envelope_sha256"`
}
type fullWindowCaps struct {
	ModelCap int64 `json:"model_cap_micro_cny"`
	JEVCap   int64 `json:"jev_cap_micro_usd"`
}
type fullWindowSlot struct {
	Ordinal   int       `json:"ordinal"`
	Pass      int       `json:"pass"`
	CaseKey   string    `json:"case_key"`
	ArmOrder  [2]string `json:"arm_order"`
	WindowID  string    `json:"window_id"`
	UserTurns int       `json:"user_turns"`
}
type fullWindowDeclaration struct {
	ID            string `json:"id"`
	FirstOrdinal  int    `json:"first_ordinal"`
	LastOrdinal   int    `json:"last_ordinal"`
	MaxModelCalls int    `json:"max_model_calls"`
	MaxJEVCalls   int    `json:"max_jev_calls"`
	fullWindowCaps
}
type fullWindowScope struct {
	BudgetDeclaration        *windowV8BudgetDeclaration `json:"budget_declaration,omitempty"`
	RetryDeclaration         *windowV8RetryDeclaration  `json:"retry_declaration,omitempty"`
	BudgetDeclarationSHA256  string                     `json:"budget_declaration_sha256,omitempty"`
	RetryPolicySHA256        string                     `json:"retry_policy_sha256,omitempty"`
	Schema                   string                     `json:"schema"`
	BaselineEvidenceSHA256   string                     `json:"baseline_evidence_sha256,omitempty"`
	BudgetApprovalSHA256     string                     `json:"budget_approval_sha256,omitempty"`
	Bindings                 fullWindowBindings         `json:"bindings"`
	DefinitionSHA256         string                     `json:"definition_sha256"`
	ManifestSHA256           string                     `json:"manifest_sha256"`
	SelectionSHA256          string                     `json:"selection_sha256"`
	ProfileSHA256            string                     `json:"profile_sha256"`
	Width                    int                        `json:"width"`
	Slots                    []fullWindowSlot           `json:"slots"`
	Windows                  []fullWindowDeclaration    `json:"windows"`
	ScheduleSHA256           string                     `json:"schedule_sha256"`
	OddPassFirstArmImbalance bool                       `json:"odd_pass_first_arm_imbalance"`
	AllowPaid                bool                       `json:"allow_paid"`
	ReadyForPaidRun          bool                       `json:"ready_for_paid_run"`
}
type fullWindowTotals struct {
	Pairs, Arms, DefinedUserTurns, Boundaries int
	ModelCap, JEVCap                          int64
}

// Preparation is an offline declaration, never execution or claim permission.
func prepareFullWindowScope(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings) (fullWindowScope, error) {
	return prepareFullWindowScopeWithBounds(s, p, width, caps, b, 70938880, 776896)
}

func prepareFullWindowScopeWithBounds(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, modelBound, jevBound int64) (fullWindowScope, error) {
	var out fullWindowScope
	if width < 1 || width > 100 {
		return out, fmt.Errorf("window width must be 1..100")
	}
	for _, h := range []string{b.PolicySHA256, b.PriceEvidenceSHA256, b.RequestEnvelopeSHA256} {
		if !comparisonDigest(h) {
			return out, fmt.Errorf("missing frozen evidence digest")
		}
	}
	for _, r := range []string{b.ExecutorRevision, b.CustomerRevision} {
		if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(r) {
			return out, fmt.Errorf("invalid frozen revision")
		}
	}
	primary, digest, e := fullCustomerSelection(s)
	if e != nil {
		return out, e
	}
	profileRaw, e := json.Marshal(p.archive)
	if e != nil || p.SHA256 != "83a78d7492c75e0d153cbb9ca4a1a87aa2f15714ad1e855534a084f4848e257d" || conversationHash(profileRaw) != "9f7c0714b32f27cb717dbf751b292667b6583096a4b8bf4318ef5fba4b32a4a0" {
		return out, fmt.Errorf("frozen profile content changed")
	}
	n := (3660 + width - 1) / width
	if len(caps) != n {
		return out, fmt.Errorf("every predetermined window needs one cap")
	}
	out = fullWindowScope{Schema: "full-window-scope.v1", Bindings: b, DefinitionSHA256: s.DefinitionSHA256, ManifestSHA256: s.ManifestSHA256, SelectionSHA256: digest, ProfileSHA256: p.SHA256, Width: width, OddPassFirstArmImbalance: true}
	var model, jev int64
	for i, c := range caps {
		if c.ModelCap < 1 || c.ModelCap > modelBound-model || c.JEVCap < 1 || c.JEVCap > jevBound-jev {
			return fullWindowScope{}, fmt.Errorf("window permanent caps exceed remaining separate currencies")
		}
		model += c.ModelCap
		jev += c.JEVCap
		out.Windows = append(out.Windows, fullWindowDeclaration{ID: fmt.Sprintf("window-%03d", i+1), FirstOrdinal: i*width + 1, fullWindowCaps: c})
	}
	for pass := 0; pass < 3; pass++ {
		for index := 0; index < 1220; index++ {
			original := (index + 406*pass) % 1220
			c := primary.Cases[original]
			ordinal := len(out.Slots) + 1
			wi := (ordinal - 1) / width
			w := &out.Windows[wi]
			turns := len(c.Scenario.Turns)
			if _, ok := p.archive.Accounts[c.Scenario.Profile]; !ok {
				return fullWindowScope{}, fmt.Errorf("case profile missing")
			}
			order := [2]string{"model", "jev_selective"}
			if (original+pass)%2 == 1 {
				order = [2]string{"jev_selective", "model"}
			}
			out.Slots = append(out.Slots, fullWindowSlot{Ordinal: ordinal, Pass: pass + 1, CaseKey: c.Key(), ArmOrder: order, WindowID: w.ID, UserTurns: turns})
			w.LastOrdinal = ordinal
			w.MaxModelCalls += 2 * 5 * turns
			w.MaxJEVCalls += turns
		}
	}
	raw, e := json.Marshal(out.Slots)
	if e != nil {
		return fullWindowScope{}, e
	}
	out.ScheduleSHA256 = conversationHash(raw)
	return out, nil
}

// Validation rederives all fixed schedule fields and checks externally frozen bindings.
// A coherently rewritten schedule plus a newly computed hash is still rejected.
func validateFullWindowScope(scope fullWindowScope, s ConversationSuite, p PairedProfiles, b fullWindowBindings) (string, fullWindowTotals, error) {
	var totals fullWindowTotals
	caps := make([]fullWindowCaps, len(scope.Windows))
	for i, w := range scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	var expected fullWindowScope
	var e error
	switch scope.Schema {
	case "full-window-scope.v1":
		expected, e = prepareFullWindowScope(s, p, scope.Width, caps, b)
	case "full-window-scope.v2":
		expected, e = prepareFullWindowScopeV2(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256)
	case "full-window-scope.v3":
		expected, e = prepareFullWindowScopeV3(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256, scope.BudgetApprovalSHA256)
	case "full-window-scope.v4":
		expected, e = prepareFullWindowScopeV4(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256, scope.BudgetApprovalSHA256)
	case "full-window-scope.v5":
		expected, e = prepareFullWindowScopeV5(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256, scope.BudgetApprovalSHA256)
	case "full-window-scope.v6":
		expected, e = prepareFullWindowScopeV6(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256, scope.BudgetApprovalSHA256)
	case "full-window-scope.v7":
		expected, e = prepareFullWindowScopeV7(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256, scope.BudgetApprovalSHA256)
	case "full-window-scope.v8":
		if err := validateWindowV8Declarations(scope); err != nil {
			return "", totals, err
		}
		expected, e = prepareFullWindowScopeV8(s, p, scope.Width, caps, b, scope.BaselineEvidenceSHA256)
	default:
		return "", totals, fmt.Errorf("unsupported complete-window scope version")
	}
	if e != nil {
		return "", totals, e
	}
	if !reflect.DeepEqual(expected, scope) {
		return "", totals, fmt.Errorf("scope differs from rederived frozen schedule")
	}
	totals = fullWindowTotals{Pairs: 3660, Arms: 7320, Boundaries: 1851}
	for _, slot := range scope.Slots {
		totals.DefinedUserTurns += 2 * slot.UserTurns
	}
	for _, w := range scope.Windows {
		totals.ModelCap += w.ModelCap
		totals.JEVCap += w.JEVCap
	}
	raw, e := json.Marshal(scope)
	if e != nil {
		return "", fullWindowTotals{}, e
	}
	return conversationHash(raw), totals, nil
}

// Baseline is historical data only. It is not a request capability or permission.
type fullWindowBaseline struct {
	TotalModel       int64             `json:"total_model_micro_cny"`
	TotalJEV         int64             `json:"total_jev_micro_usd"`
	PermanentModel   int64             `json:"permanent_model_micro_cny"`
	PermanentJEV     int64             `json:"permanent_jev_micro_usd"`
	RemainingModel   int64             `json:"remaining_model_micro_cny"`
	RemainingJEV     int64             `json:"remaining_jev_micro_usd"`
	ActualModelCalls int               `json:"historical_actual_model_calls"`
	ActualJEVCalls   int               `json:"historical_actual_jev_calls"`
	ClaimSHA256      map[string]string `json:"claim_sha256"`
}

func fullWindowCanonicalClaims() map[string]string {
	return map[string]string{
		RechargeConfirmID:          RechargePriorClaimSHA,
		RechargeDiagnosticRetestID: RechargeDiagnosticClaimSHA,
		RechargeCancelID:           RechargeCancelClaimSHA,
		RechargeSupplementID:       RechargeSupplementClaimSHA,
		RefundCapID:                RefundCapClaimSHA,
		V5SixCohortID:              V5SixCohortClaimSHA,
		V5RepeatedID:               V5RepeatedClaimSHA,
		V5RepeatedDiagnosticID:     V5ExportDiagnosticClaimSHA,
		V5ReceiverRepeatedID:       "f09de5bb9ab4147f140f54db7a25c5f419b7a4e48137d63c1782ea7c38d768b3",
	}
}

// loadFullWindowBaseline reads only the nine exact original raw claims from an
// explicit directory. Its fixed expectations cannot be replaced by caller data.
func loadFullWindowBaseline(claimDir string) (fullWindowBaseline, error) {
	claims := map[string][]byte{}
	expected := fullWindowCanonicalClaims()
	for id := range expected {
		path := filepath.Join(claimDir, id+".json")
		f, e := os.Open(path)
		if e != nil {
			return fullWindowBaseline{}, fmt.Errorf("missing canonical historical claim: %s", id)
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
			f.Close()
			return fullWindowBaseline{}, fmt.Errorf("invalid historical claim file")
		}
		raw, e := io.ReadAll(io.LimitReader(f, 65537))
		closeErr := f.Close()
		if e != nil || closeErr != nil || len(raw) > 65536 {
			return fullWindowBaseline{}, fmt.Errorf("cannot read historical claim")
		}
		claims[id] = raw
	}
	return verifyFullWindowBaselineBytes(claims, expected, V5ReceiverRepeatedID)
}

// Pure content verifier; the production loader always supplies hardbound hashes.
func verifyFullWindowBaselineBytes(claims map[string][]byte, expected map[string]string, last string) (fullWindowBaseline, error) {
	var out fullWindowBaseline
	if len(claims) != len(expected) || len(expected) == 0 {
		return out, fmt.Errorf("incomplete predecessor chain")
	}
	for id, h := range expected {
		raw, ok := claims[id]
		if !ok || !comparisonDigest(h) || conversationHash(raw) != h {
			return out, fmt.Errorf("historical claim changed: %s", id)
		}
	}
	var c struct {
		Schema     string `json:"schema"`
		Campaign   string `json:"campaign_id"`
		Status     string `json:"status"`
		CNY        int64  `json:"cumulative_reserved_model_micro_cny"`
		USD        int64  `json:"cumulative_reserved_jev_micro_usd"`
		Model      int64  `json:"reserved_model_micro_cny"`
		JEV        int64  `json:"reserved_jev_micro_usd"`
		ModelCalls int    `json:"reserved_model_calls"`
		JEVCalls   int    `json:"reserved_jev_calls"`
	}
	if e := json.Unmarshal(claims[last], &c); e != nil || c.Schema != "paired-live-receiver-repeated-claim.v1" || c.Campaign != V5ReceiverRepeatedID || c.Status != "consumed_before_execution" || c.CNY != 29061120 || c.USD != 223104 || c.Model != 8380416 || c.JEV != 64512 || c.ModelCalls != 248 || c.JEVCalls != 24 {
		return out, fmt.Errorf("invalid final historical claim schema or cumulative reservation")
	}
	out = fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29061120, PermanentJEV: 223104, RemainingModel: 70938880, RemainingJEV: 776896, ActualModelCalls: 143, ActualJEVCalls: 37, ClaimSHA256: map[string]string{}}
	for id, h := range expected {
		out.ClaimSHA256[id] = h
	}
	return out, nil
}
