package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// Data is auditable but cannot be decoded into a funding receipt.
type windowV8ExtraClaim struct {
	Schema              string                `json:"schema"`
	Index               int                   `json:"index"`
	ProviderIndex       int                   `json:"provider_index"`
	Root                string                `json:"root"`
	ScopeSHA256         string                `json:"scope_sha256"`
	ExecutionSHA256     string                `json:"execution_sha256"`
	SourceSHA256        string                `json:"source_sha256"`
	PolicySHA256        string                `json:"policy_sha256"`
	AuthorizationSHA256 string                `json:"authorization_sha256"`
	OwnerID             string                `json:"owner_id"`
	BaseClaimSHA256     string                `json:"base_claim_sha256"`
	WindowID            string                `json:"window_id"`
	PreviousExtraSHA256 string                `json:"previous_extra_sha256,omitempty"`
	UnknownSequence     int                   `json:"unknown_sequence"`
	Request             windowRequestIdentity `json:"request"`
	DescriptorSHA256    string                `json:"descriptor_sha256"`
	Amount              windowV8Amounts       `json:"amount"`
	ModelExtras         int                   `json:"model_extras"`
	JEVExtras           int                   `json:"jev_extras"`
	Permanent           windowV8Amounts       `json:"permanent"`
}
type windowOwnedRetryContext struct {
	driver         *windowOwnedDriver
	phase          *windowOwnedPhase
	sequence       int
	request        windowRequestIdentity
	descriptorSHA  string
	requestContext context.Context
}
type windowExtraFundingReceipt struct {
	owner    *windowCampaign
	ledger   *windowAccounting
	claim    windowV8ExtraClaim
	sha      string
	consumed bool
}

func (c *windowCampaign) claimWindowExtra(r windowOwnedRetryContext) (*windowExtraFundingReceipt, error) {
	if c == nil || r.driver == nil || r.driver.permit == nil {
		return nil, fmt.Errorf("v8 extra owner absent")
	}
	p := r.driver.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.campaign != c || p.guardWindowOwnedLocked(r.driver) != nil || r.phase == nil || r.driver.phase != r.phase || r.phase.driver != r.driver || r.phase.nonce == "" || r.phase.parent == nil || r.phase.parent.Err() != nil || r.requestContext == nil || r.requestContext.Err() != nil || r.requestContext.Value(windowOwnedPhaseKey{}) != r.phase {
		return nil, fmt.Errorf("v8 extra live phase rejected")
	}
	if !r.driver.setupDone || r.request.CaseKey != r.driver.slot.CaseKey || r.request.Pass != r.driver.slot.Pass || r.request.Arm != r.driver.arm || r.request.Turn != r.driver.nextTurn+1 || r.request.Provider == "model" && r.request.Attempt != r.phase.model || r.request.Provider == "jev" && r.request.Attempt != r.phase.jev {
		return nil, fmt.Errorf("v8 extra logical phase identity changed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fail := func() (*windowExtraFundingReceipt, error) {
		c.poisoned = true
		return nil, fmt.Errorf("v8 exclusive extra rejected")
	}
	if c.prepared.scope.Schema != "full-window-scope.v8" || c.closed || c.poisoned || c.ledger == nil || c.ledger != r.driver.ledger || c.prepared.v8Authority == nil || c.prepared.v8Authority.revalidate() != nil || c.inventoryWindowCampaign() != nil {
		return fail()
	}
	scopeRaw, _ := json.Marshal(c.prepared.scope)
	if conversationHash(scopeRaw) != c.prepared.scopeSHA || c.prepared.v8Authority.binding.ScopeSHA256 != c.prepared.scopeSHA || c.prepared.v8Authority.binding.ExecutionSHA256 != c.prepared.executionSHA {
		return fail()
	}
	state := c.ledger.Snapshot()
	if state.Halt != "" || state.Closed || state.Pending != nil || state.RetryRequired == nil || state.RetryRequired.Sequence != r.sequence || state.RetryRequired.Request != r.request || r.request.NativeAttempt != 1 {
		return fail()
	}
	var unknown *windowUnknownIntent
	for i := range state.UnknownIntents {
		u := &state.UnknownIntents[i]
		if u.Sequence == r.sequence {
			unknown = u
		}
	}
	if unknown == nil || unknown.Request != r.request || unknown.DescriptorSHA256 != r.descriptorSHA || unknown.ExtraClaimSHA256 != "" {
		return fail()
	}
	descriptor, err := windowV8FailureDescriptor(p.payload, r.sequence, r.request)
	if err != nil || descriptor != r.descriptorSHA {
		return fail()
	}
	for _, extra := range c.extras {
		if extra.WindowID == c.ledger.contract.WindowID && extra.UnknownSequence == r.sequence {
			return fail()
		}
	}
	amount := windowV8Amounts{}
	providerIndex := 0
	switch r.request.Provider {
	case "model":
		if c.modelExtras >= 20 || unknown.MaxHold != 74752 {
			return fail()
		}
		amount.Model = 74752
		providerIndex = c.modelExtras + 1
	case "jev":
		if c.jevExtras >= 20 || unknown.MaxHold != 2688 {
			return fail()
		}
		amount.JEV = 2688
		providerIndex = c.jevExtras + 1
	default:
		return fail()
	}
	model, err := windowCheckedAdd(c.model, amount.Model)
	if err != nil {
		return fail()
	}
	jev, err := windowCheckedAdd(c.jev, amount.JEV)
	if err != nil {
		return fail()
	}
	if model > 140000000-2023424 || jev > 1100000-65 {
		return fail()
	}
	a := c.prepared.v8Authority
	claim := windowV8ExtraClaim{Schema: "full-window-extra-claim.v8", Index: len(c.extras) + 1, ProviderIndex: providerIndex, Root: c.root, ScopeSHA256: c.prepared.scopeSHA, ExecutionSHA256: c.prepared.executionSHA, SourceSHA256: a.binding.SourceSHA256, PolicySHA256: c.prepared.scope.Bindings.PolicySHA256, AuthorizationSHA256: a.authorizationSHA, OwnerID: a.binding.OwnerID, BaseClaimSHA256: c.claimSHA, WindowID: c.ledger.contract.WindowID, PreviousExtraSHA256: c.extraSHA, UnknownSequence: r.sequence, Request: r.request, DescriptorSHA256: r.descriptorSHA, Amount: amount, ModelExtras: c.modelExtras, JEVExtras: c.jevExtras, Permanent: windowV8Amounts{model, jev}}
	if r.request.Provider == "model" {
		claim.ModelExtras++
	} else {
		claim.JEVExtras++
	}
	// Creation attempt consumes quota and full liability before any IO. Failure
	// leaves that reservation live, poisons this owner, and never removes a file.
	c.model, c.jev = model, jev
	c.modelExtras, c.jevExtras = claim.ModelExtras, claim.JEVExtras
	c.extras = append(c.extras, claim)
	name := fmt.Sprintf("extra-%06d.json", claim.Index)
	if c.persistWindowCampaign(name, claim) != nil {
		return fail()
	}
	sha := c.files[name]
	if _, err = readWindowStoppedV7File(filepath.Join(c.root, name), sha, 65536); err != nil {
		return fail()
	}
	c.extraSHA = sha
	return &windowExtraFundingReceipt{owner: c, ledger: c.ledger, claim: claim, sha: sha}, nil
}

func (r *windowExtraFundingReceipt) recordFunding() error {
	if r == nil || r.owner == nil {
		return fmt.Errorf("v8 owned funding receipt absent")
	}
	c := r.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	fail := func() error { c.poisoned = true; return fmt.Errorf("v8 owned funding receipt rejected") }
	if r.consumed || c.closed || c.poisoned || c.ledger != r.ledger || c.prepared.v8Authority == nil || c.prepared.v8Authority.revalidate() != nil || c.inventoryWindowCampaign() != nil || r.claim.Index < 1 || r.claim.Index > len(c.extras) || c.extras[r.claim.Index-1] != r.claim || c.files[fmt.Sprintf("extra-%06d.json", r.claim.Index)] != r.sha {
		return fail()
	}
	r.consumed = true
	amount := r.claim.Amount.Model
	if r.claim.Request.Provider == "jev" {
		amount = r.claim.Amount.JEV
	}
	if r.ledger.RecordExtraFunding(r.claim.UnknownSequence, r.claim.Request.Provider, amount, r.sha) != nil {
		return fail()
	}
	return nil
}
func windowV8FailureDescriptor(s *windowPayloadStore, sequence int, r windowRequestIdentity) (string, error) {
	if s == nil {
		return "", fmt.Errorf("v8 failure packet absent")
	}
	manifest, _, err := verifyWindowPayloadStore(s)
	if err != nil || sequence < 1 || sequence > len(manifest.Packets) {
		return "", fmt.Errorf("v8 failure packet invalid")
	}
	packet := manifest.Packets[sequence-1]
	response := packet.Response
	if packet.Sequence != sequence || packet.Identity != r || response == nil || response.Status != 0 || response.Code != "round_trip" || response.Complete || response.Available || response.Body != nil || response.TransportFailure == nil || response.TransportFailure.Cause != "eof" || response.TransportFailure.Phase != "tls_handshake" || !response.TransportFailure.TraceObserved || response.TransportFailure.ConnectionReused || response.TransportFailure.RequestWritten || response.TransportFailure.FirstByte {
		return "", fmt.Errorf("v8 first failure descriptor rejected")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[response.DescriptorFile]
	if !ok {
		return "", fmt.Errorf("v8 descriptor missing")
	}
	return f.sha, nil
}

func windowV8UnknownResponse(response *windowPayloadResponse) bool {
	return response != nil && response.Status == 0 && response.Code == "round_trip" && !response.Complete && !response.Available && response.Body == nil && response.TransportFailure != nil && response.TransportFailure.Cause == "eof" && response.TransportFailure.Phase == "tls_handshake" && response.TransportFailure.TraceObserved && !response.TransportFailure.ConnectionReused && !response.TransportFailure.RequestWritten && !response.TransportFailure.FirstByte
}

// Completion reconciliation is separate from claiming; stopped owners retain
// orphaned liabilities but can never pass this join or reconstruct a proof.
func (c *windowCampaign) validateWindowV8ExtrasLocked(state windowAccountingState) error {
	if state.Halt != "" || state.Pending != nil || state.RetryRequired != nil {
		return fmt.Errorf("v8 unresolved native state")
	}
	linked := map[int]bool{}
	shaSeen := map[string]bool{}
	for _, extra := range c.extras {
		if extra.WindowID != c.ledger.contract.WindowID {
			continue
		}
		sha := c.files[fmt.Sprintf("extra-%06d.json", extra.Index)]
		if !comparisonDigest(sha) || shaSeen[sha] || extra.BaseClaimSHA256 != c.claimSHA || extra.ScopeSHA256 != c.prepared.scopeSHA || extra.ExecutionSHA256 != c.prepared.executionSHA {
			return fmt.Errorf("v8 extra campaign mismatch")
		}
		shaSeen[sha] = true
		found := false
		for _, u := range state.UnknownIntents {
			if u.Sequence == extra.UnknownSequence && u.Request == extra.Request && u.DescriptorSHA256 == extra.DescriptorSHA256 && u.ExtraClaimSHA256 == sha && !linked[u.Sequence] {
				found = true
				linked[u.Sequence] = true
			}
		}
		if !found {
			return fmt.Errorf("v8 orphaned or duplicate extra")
		}
	}
	if len(linked) != len(state.UnknownIntents) {
		return fmt.Errorf("v8 unfunded unknown")
	}
	return nil
}
