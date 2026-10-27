package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixtureWindowCampaign(t *testing.T) (windowCampaignPrepared, fullWindowBaseline) {
	t.Helper()
	source, profiles, bindings := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{ModelCap: 10, JEVCap: 1}
	}
	scope, e := prepareFullWindowScope(source, profiles, 100, caps, bindings)
	if e != nil {
		t.Fatal(e)
	}
	sha, _, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		t.Fatal(e)
	}
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "new")
	limits := windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxRequestBytes: 1024, MaxInput: 1, MaxOutput: 128, InputRate: 1, OutputRate: 1}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0", MaxRequestBytes: 1024, MaxInput: 1, InputRate: 1}}
	execution, e := executionSHAWindowCampaign(sha, root, limits)
	if e != nil {
		t.Fatal(e)
	}
	p, e := prepareWindowCampaign(root, root, scope, source, profiles, bindings, sha, execution, limits)
	if e != nil {
		t.Fatal(e)
	}
	return p, fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29061120, PermanentJEV: 223104, RemainingModel: 70938880, RemainingJEV: 776896, ActualModelCalls: 143, ActualJEVCalls: 37, ClaimSHA256: fullWindowCanonicalClaims()}
}
func TestWindowCampaignPreparesRealFullTypedSource(t *testing.T) {
	p, _ := fixtureWindowCampaign(t)
	if len(p.scope.Slots) != 3660 || len(p.contracts) != 37 {
		t.Fatal("missing full scope")
	}
	for i, c := range p.contracts {
		w := p.scope.Windows[i]
		if c.Model.MaxAttempts != w.MaxModelCalls || c.JEV.MaxAttempts != w.MaxJEVCalls || c.Model.Model != "deepseek-flash" || c.JEV.Model != "jev-1.13.0" {
			t.Fatal("wrong frozen window bounds")
		}
	}
}

// Only this test fixture mints synthetic completion tokens. No production issuer exists.
func liveFixtureWindowCampaign(t *testing.T, c *windowCampaign) *windowCampaignOwnedCheckpoint {
	t.Helper()
	l, e := c.openWindowCampaignLedger()
	if e != nil {
		t.Fatal(e)
	}
	p := &windowCampaignOwnedCheckpoint{owner: c, ledger: l, index: c.index}
	w := c.prepared.scope.Windows[c.index-1]
	for _, s := range c.prepared.scope.Slots[w.FirstOrdinal-1 : w.LastOrdinal] {
		for _, arm := range s.ArmOrder {
			p.evidence = append(p.evidence, windowCampaignArmEvidence{Ordinal: s.Ordinal, Pass: s.Pass, ObservedTurns: s.UserTurns, Key: s.CaseKey, Arm: arm, Cleanup: true, Observer: true, RecordingSHA256: strings.Repeat("a", 64)})
		}
	}
	return p
}

type faultWindowCampaignJournal struct {
	*os.File
	short, syncFailure bool
}

func (f *faultWindowCampaignJournal) Write(b []byte) (int, error) {
	if f.short && len(b) > 1 {
		return f.File.Write(b[:len(b)/2])
	}
	return f.File.Write(b)
}
func (f *faultWindowCampaignJournal) Sync() error {
	if f.syncFailure {
		return fmt.Errorf("injected sync failure")
	}
	return f.File.Sync()
}
func TestWindowCampaignLiveCloseFailureStaysFailed(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, e := startWindowCampaign(p, b)
	if e != nil {
		t.Fatal(e)
	}
	token := liveFixtureWindowCampaign(t, c)
	c.ledger.file = &faultWindowCampaignJournal{File: c.ledger.file.(*os.File), syncFailure: true}
	if _, e := c.advanceWindowCampaign(token); e == nil {
		t.Fatal("live Close failure advanced")
	}
	if e := c.closeWindowCampaign(); e == nil {
		t.Fatal("owner Close failure missing")
	}
	if e := c.closeWindowCampaign(); e == nil {
		t.Fatal("repeated Close lost failure")
	}
}

func TestWindowCampaignAllWindowsPermanentAndOwned(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, e := startWindowCampaign(p, b)
	if e != nil {
		t.Fatal(e)
	}
	defer c.closeWindowCampaign()
	previous := ""
	for index := 1; index <= 37; index++ {
		token := liveFixtureWindowCampaign(t, c)
		current := c.claimSHA
		if _, e := c.advanceWindowCampaign(token); e != nil {
			t.Fatalf("window%d: %v", index, e)
		}
		raw, e := os.ReadFile(filepath.Join(c.root, fmt.Sprintf("claim-%06d.json", index)))
		if e != nil {
			t.Fatal(e)
		}
		var r windowCampaignClaim
		if e = json.Unmarshal(raw, &r); e != nil {
			t.Fatal(e)
		}
		if r.Index != index || r.PreviousClaimSHA256 != previous || r.ModelPermanent != 29061120+int64(index)*10 || r.JEVPermanent != 223104+int64(index) || r.Root != p.root || r.ScopeSHA256 != p.scopeSHA || r.DefinitionSHA256 != p.scope.DefinitionSHA256 || r.ManifestSHA256 != p.scope.ManifestSHA256 || r.SelectionSHA256 != p.scope.SelectionSHA256 || r.ProfileSHA256 != p.scope.ProfileSHA256 || r.ScheduleSHA256 != p.scope.ScheduleSHA256 || r.PolicySHA256 != p.scope.Bindings.PolicySHA256 || !reflect.DeepEqual(r.HistoricalClaims, fullWindowCanonicalClaims()) {
			t.Fatalf("bad permanent claim %+v", r)
		}
		if index > 1 && !comparisonDigest(r.PreviousCheckpointSHA256) {
			t.Fatal("unbound previous checkpoint")
		}
		previous = current
	}
	if _, e := c.advanceWindowCampaign(nil); e == nil {
		t.Fatal("sealed owner resumed")
	}
	c.closeWindowCampaign()
	if _, e := startWindowCampaign(p, b); e == nil {
		t.Fatal("closed owner reopened")
	}
}
func TestWindowCampaignRejectsNonOwnedPartialAndReplayEvidence(t *testing.T) {
	for _, name := range []string{"nil", "owner", "ledger", "index", "missing", "duplicate", "turn", "case", "pass", "cleanup", "observer", "missing_recording_sha", "malformed_recording_sha", "JSON", "replay", "pending"} {
		t.Run(name, func(t *testing.T) {
			p, b := fixtureWindowCampaign(t)
			c, e := startWindowCampaign(p, b)
			if e != nil {
				t.Fatal(e)
			}
			defer c.closeWindowCampaign()
			token := liveFixtureWindowCampaign(t, c)
			switch name {
			case "nil":
				token = nil
			case "owner":
				token.owner = &windowCampaign{}
			case "ledger":
				token.ledger = &windowAccounting{}
			case "index":
				token.index++
			case "missing":
				token.evidence = token.evidence[1:]
			case "duplicate":
				token.evidence[1] = token.evidence[0]
			case "turn":
				token.evidence[0].ObservedTurns++
			case "case":
				token.evidence[0].Key = "other"
			case "pass":
				token.evidence[0].Pass++
			case "cleanup":
				token.evidence[0].Cleanup = false
			case "observer":
				token.evidence[0].Observer = false
			case "missing_recording_sha":
				token.evidence[0].RecordingSHA256 = ""
			case "malformed_recording_sha":
				token.evidence[0].RecordingSHA256 = "not-a-digest"
			case "JSON":
				raw, _ := json.Marshal(token)
				var replay windowCampaignOwnedCheckpoint
				json.Unmarshal(raw, &replay)
				token = &replay
			case "replay":
				if e := c.ledger.Close(); e != nil {
					t.Fatal(e)
				}
				audit, e := readWindowAccounting(c.ledgerDir)
				if e != nil || audit.CompletionProof != "unavailable_from_journal" {
					t.Fatal(audit, e)
				}
				token.ledger = &windowAccounting{contract: audit.Contract, state: audit.State}
			case "pending":
				slot := p.scope.Slots[0]
				_, e := c.ledger.Begin(windowRequestIdentity{Provider: "model", Model: "deepseek-flash", CaseKey: slot.CaseKey, Pass: slot.Pass, Arm: slot.ArmOrder[0], Turn: 1, Attempt: 1, BodySHA256: p.scopeSHA, Bytes: 1})
				if e != nil {
					t.Fatal(e)
				}
			}
			if _, e := c.advanceWindowCampaign(token); e == nil {
				t.Fatal("bad proof advanced")
			}
			if _, e := os.Stat(filepath.Join(c.root, "claim-000002.json")); !os.IsNotExist(e) {
				t.Fatal("next claim exists")
			}
			if _, e := c.advanceWindowCampaign(token); e == nil {
				t.Fatal("halt retried")
			}
		})
	}
}
func TestWindowCampaignPartialWritesHoldWholeCapAndCannotResume(t *testing.T) {
	for _, target := range []string{"checkpoint", "claim"} {
		for _, fault := range []string{"short", "sync"} {
			t.Run(target+"/"+fault, func(t *testing.T) {
				p, b := fixtureWindowCampaign(t)
				c, e := startWindowCampaign(p, b)
				if e != nil {
					t.Fatal(e)
				}
				token := liveFixtureWindowCampaign(t, c)
				c.openFile = func(path string) (windowJournalFile, error) {
					f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if e != nil {
						return nil, e
					}
					if strings.HasPrefix(filepath.Base(path), target+"-") {
						return &faultWindowCampaignJournal{File: f, short: fault == "short", syncFailure: fault == "sync"}, nil
					}
					return f, nil
				}
				if _, e := c.advanceWindowCampaign(token); e == nil {
					t.Fatal("durability failure advanced")
				}
				name := "checkpoint-000001.json"
				if target == "claim" {
					name = "claim-000002.json"
					if c.model != 29061140 || c.jev != 223106 {
						t.Fatal("attempted permanent cap released")
					}
				}
				raw, e := os.ReadFile(filepath.Join(c.root, name))
				if e != nil || len(raw) == 0 {
					t.Fatal("failure evidence removed")
				}
				if _, e := c.advanceWindowCampaign(token); e == nil {
					t.Fatal("failure retried")
				}
				c.closeWindowCampaign()
				if _, e := startWindowCampaign(p, b); e == nil {
					t.Fatal("partial root reopened")
				}
			})
		}
	}
}
func TestWindowCampaignInventoryTamperAndScopeRenameRefused(t *testing.T) {
	for _, name := range []string{"unknown", "claim", "root", "lock", "prior ledger", "during checkpoint"} {
		t.Run(name, func(t *testing.T) {
			p, b := fixtureWindowCampaign(t)
			c, e := startWindowCampaign(p, b)
			if e != nil {
				t.Fatal(e)
			}
			defer c.closeWindowCampaign()
			token := liveFixtureWindowCampaign(t, c)
			switch name {
			case "unknown":
				os.WriteFile(filepath.Join(c.root, "orphan"), []byte("x"), 0600)
			case "claim":
				os.WriteFile(filepath.Join(c.root, "claim-000001.json"), []byte("{}"), 0600)
			case "root":
				os.Rename(c.root, c.root+"-moved")
				os.Mkdir(c.root, 0700)
			case "lock":
				os.Rename(filepath.Join(c.root, ".lock"), filepath.Join(c.root, "old-lock"))
				os.WriteFile(filepath.Join(c.root, ".lock"), nil, 0600)
			case "prior ledger":
				if _, e := c.advanceWindowCampaign(token); e != nil {
					t.Fatal(e)
				}
				token = liveFixtureWindowCampaign(t, c)
				os.WriteFile(filepath.Join(c.root, "ledger-000001", "orphan"), []byte("x"), 0600)
			case "during checkpoint":
				c.openFile = func(path string) (windowJournalFile, error) {
					f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if strings.HasPrefix(filepath.Base(path), "checkpoint-") {
						os.WriteFile(filepath.Join(c.root, "orphan"), []byte("x"), 0600)
					}
					return f, e
				}
			}
			if _, e := c.advanceWindowCampaign(token); e == nil {
				t.Fatal("tamper advanced")
			}
			c.closeWindowCampaign()
			p.scope.Bindings.PolicySHA256 = strings.Repeat("f", 64)
			raw, _ := json.Marshal(p.scope)
			p.scopeSHA = conversationHash(raw)
			if _, e := startWindowCampaign(p, b); e == nil {
				t.Fatal("renamed scope reclaimed root")
			}
		})
	}
}

func inputFixtureWindowCampaign(t *testing.T) (ConversationSuite, PairedProfiles, fullWindowBindings, fullWindowScope, string, windowCampaignLimits, string) {
	t.Helper()
	source, profiles, bindings := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{10, 1}
	}
	scope, e := prepareFullWindowScope(source, profiles, 100, caps, bindings)
	if e != nil {
		t.Fatal(e)
	}
	sha, _, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		t.Fatal(e)
	}
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "registry")
	limits := windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxRequestBytes: 1024, MaxInput: 1, MaxOutput: 128, InputRate: 1, OutputRate: 1}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0", MaxRequestBytes: 1024, MaxInput: 1, InputRate: 1}}
	return source, profiles, bindings, scope, sha, limits, root
}
func TestWindowCampaignPreflightRejectsBeforeRootWrite(t *testing.T) {
	for _, name := range []string{"root mismatch", "relative root", "scope hash", "source mutation", "profile mutation", "schedule rewrite", "wrong model", "wrong jev", "output bound", "zero bytes", "invalid rate", "later invalid hold", "overflow cap", "missing history", "changed history"} {
		t.Run(name, func(t *testing.T) {
			s, p, b, scope, sha, limits, root := inputFixtureWindowCampaign(t)
			frozenRoot := root
			execution, e := executionSHAWindowCampaign(sha, root, limits)
			if e != nil {
				t.Fatal(e)
			}
			history := t.TempDir()
			switch name {
			case "root mismatch":
				frozenRoot = root + "-other"
			case "relative root":
				root = "relative-campaign-root"
				frozenRoot = root
			case "scope hash":
				sha = strings.Repeat("f", 64)
			case "source mutation":
				s.Cases[0].Scenario.Turns[0].User += " mutation"
			case "profile mutation":
				p.archive.Accounts["active_flagship"] = runtimeAccount{}
			case "schedule rewrite":
				scope.Slots[0].Pass = 2
				raw, _ := json.Marshal(scope.Slots)
				scope.ScheduleSHA256 = conversationHash(raw)
				raw, _ = json.Marshal(scope)
				sha = conversationHash(raw)
			case "wrong model":
				limits.Model.Model = "other"
			case "wrong jev":
				limits.JEV.Model = "other"
			case "output bound":
				limits.Model.MaxOutput = 512
			case "zero bytes":
				limits.JEV.MaxRequestBytes = 0
			case "invalid rate":
				limits.Model.InputRate = 1 << 62
			case "later invalid hold":
				scope.Windows[len(scope.Windows)-1].ModelCap = 1
				sha, _, _ = validateFullWindowScope(scope, s, p, b)
			case "overflow cap":
				scope.Windows[0].JEVCap = 1 << 62
				raw, _ := json.Marshal(scope)
				sha = conversationHash(raw)
			case "changed history":
				for id := range fullWindowCanonicalClaims() {
					os.WriteFile(filepath.Join(history, id+".json"), []byte("{}"), 0600)
				}
			}
			if _, e := newWindowCampaign(root, frozenRoot, history, scope, s, p, b, sha, execution, limits); e == nil {
				t.Fatal("invalid readiness accepted")
			}
			if _, e := os.Lstat(root); !os.IsNotExist(e) {
				t.Fatal("preflight failure wrote registry", e)
			}
		})
	}
}
func TestWindowCampaignPreparedAllContractsCheckedBeforeFirstClaim(t *testing.T) {
	for _, name := range []string{"last cap", "last model", "last bytes", "last attempts", "baseline", "duplicate slot"} {
		t.Run(name, func(t *testing.T) {
			p, b := fixtureWindowCampaign(t)
			switch name {
			case "last cap":
				p.contracts[len(p.contracts)-1].ModelCap = 1
			case "last model":
				p.contracts[len(p.contracts)-1].JEV.Model = "other"
			case "last bytes":
				p.contracts[len(p.contracts)-1].JEV.MaxRequestBytes = 0
			case "last attempts":
				p.contracts[len(p.contracts)-1].Model.MaxAttempts++
			case "baseline":
				b.PermanentModel--
			case "duplicate slot":
				p.scope.Slots[1] = p.scope.Slots[0]
			}
			if _, e := startWindowCampaign(p, b); e == nil {
				t.Fatal("invalid prepared data accepted")
			}
			if _, e := os.Lstat(p.root); !os.IsNotExist(e) {
				t.Fatal("first claim written before all window checks")
			}
		})
	}
}
func TestWindowCampaignOwnerDetachesDataAndRefusesReopen(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, e := startWindowCampaign(p, b)
	if e != nil {
		t.Fatal(e)
	}
	originalKey := c.prepared.scope.Slots[0].CaseKey
	p.scope.Slots[0].CaseKey = "mutation"
	p.scope.Windows[0].ModelCap++
	p.contracts[0].Model.Model = "other"
	for key := range b.ClaimSHA256 {
		b.ClaimSHA256[key] = "other"
	}
	if c.prepared.scope.Slots[0].CaseKey != originalKey || c.prepared.scope.Windows[0].ModelCap != 10 || c.prepared.contracts[0].Model.Model != "deepseek-flash" || !reflect.DeepEqual(c.baseline.ClaimSHA256, fullWindowCanonicalClaims()) {
		t.Fatal("caller mutation reached owner")
	}
	c.closeWindowCampaign()
	fresh, base := fixtureWindowCampaign(t)
	fresh.root = c.root
	if _, e := startWindowCampaign(fresh, base); e == nil {
		t.Fatal("existing registry resumed")
	}
}
func TestWindowCampaignConcurrentFirstOwnerAndHistoricalChangeStop(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	type result struct {
		c *windowCampaign
		e error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { c, e := startWindowCampaign(p, b); results <- result{c, e} }()
	}
	var owner *windowCampaign
	wins := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.e == nil {
			wins++
			owner = r.c
		}
	}
	if wins != 1 {
		t.Fatalf("owners %d", wins)
	}
	defer owner.closeWindowCampaign()
	token := liveFixtureWindowCampaign(t, owner)
	owner.historicalDir = t.TempDir()
	if _, e := owner.advanceWindowCampaign(token); e == nil {
		t.Fatal("changed raw nine chain admitted next claim")
	}
	if _, e := os.Stat(filepath.Join(owner.root, "claim-000002.json")); !os.IsNotExist(e) {
		t.Fatal("new claim before historical recheck")
	}
}

func TestWindowCampaignExecutionBindingCanonical(t *testing.T) {
	_, _, _, _, sha, limits, root := inputFixtureWindowCampaign(t)
	got, e := executionSHAWindowCampaign(sha, root, limits)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(windowCampaignExecutionBinding{Schema: "full-window-campaign-execution.v1", ScopeSHA256: sha, AbsoluteRoot: root, Limits: limits})
	if e != nil {
		t.Fatal(e)
	}
	if got != conversationHash(raw) {
		t.Fatal("execution binding canonical identity")
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil || obj["schema"] != "full-window-campaign-execution.v1" || obj["scope_sha256"] != sha || obj["absolute_root"] != root {
		t.Fatal("execution domain identity")
	}
	providers := obj["provider_limits"].(map[string]any)
	for _, name := range []string{"model", "jev"} {
		p := providers[name].(map[string]any)
		for _, key := range []string{"model", "max_request_bytes", "max_input_tokens", "max_output_tokens", "input_micro_per_million", "output_micro_per_million"} {
			if _, ok := p[key]; !ok {
				t.Fatal("unbound field", name, key)
			}
		}
	}
}

func TestWindowCampaignExecutionBindingRejectsStaleDigest(t *testing.T) {
	source, profiles, bindings, scope, scopeSHA, limits, root := inputFixtureWindowCampaign(t)
	frozenExecution, e := executionSHAWindowCampaign(scopeSHA, root, limits)
	if e != nil {
		t.Fatal(e)
	}
	mutations := map[string]func(*windowCampaignLimits, *string, *fullWindowScope){
		"model name":        func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.Model.Model = "other" },
		"model bytes":       func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.Model.MaxRequestBytes++ },
		"model input":       func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.Model.MaxInput++ },
		"model output":      func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.Model.MaxOutput-- },
		"model input rate":  func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.Model.InputRate++ },
		"model output rate": func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.Model.OutputRate++ },
		"jev name":          func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.JEV.Model = "other" },
		"jev bytes":         func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.JEV.MaxRequestBytes++ },
		"jev input":         func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.JEV.MaxInput++ },
		"jev output":        func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.JEV.MaxOutput++ },
		"jev input rate":    func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.JEV.InputRate++ },
		"jev output rate":   func(l *windowCampaignLimits, _ *string, _ *fullWindowScope) { l.JEV.OutputRate++ },
		"root":              func(_ *windowCampaignLimits, r *string, _ *fullWindowScope) { *r += "-other" },
		"scope":             func(_ *windowCampaignLimits, _ *string, s *fullWindowScope) { s.Windows[0].ModelCap++ },
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			changedLimits := limits
			changedRoot := root
			raw, _ := json.Marshal(scope)
			var changedScope fullWindowScope
			json.Unmarshal(raw, &changedScope)
			mutation(&changedLimits, &changedRoot, &changedScope)
			changedSHA, _, e := validateFullWindowScope(changedScope, source, profiles, bindings)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := prepareWindowCampaign(changedRoot, changedRoot, changedScope, source, profiles, bindings, changedSHA, frozenExecution, changedLimits); e == nil {
				t.Fatal("stale execution digest accepted changed limits/root/scope")
			}
			if _, e := os.Lstat(changedRoot); !os.IsNotExist(e) {
				t.Fatal("stale execution digest wrote registry")
			}
		})
	}
}

func TestWindowCampaignExecutionBindingIndependentRefreezeIsDataOnly(t *testing.T) {
	source, profiles, bindings, scope, scopeSHA, limits, root := inputFixtureWindowCampaign(t)
	old, e := executionSHAWindowCampaign(scopeSHA, root, limits)
	if e != nil {
		t.Fatal(e)
	}
	changed := limits
	changed.Model.InputRate++
	changed.Model.MaxInput++
	changed.JEV.MaxRequestBytes++
	// This is synthetic independently frozen test data, not new tariff evidence.
	refrozen, e := executionSHAWindowCampaign(scopeSHA, root, changed)
	if e != nil || refrozen == old {
		t.Fatal("changed config did not change binding", e)
	}
	p, e := prepareWindowCampaign(root, root, scope, source, profiles, bindings, scopeSHA, refrozen, changed)
	if e != nil {
		t.Fatal(e)
	}
	if p.executionSHA != refrozen || p.limits != changed || p.scope.ReadyForPaidRun || p.scope.AllowPaid {
		t.Fatal("refreeze invented authority")
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		t.Fatal("data-only preparation wrote root")
	}
	for _, bad := range []string{"", strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		if _, e := prepareWindowCampaign(root, root, scope, source, profiles, bindings, scopeSHA, bad, changed); e == nil {
			t.Fatal("missing/invalid execution digest accepted")
		}
	}
}
func TestWindowCampaignExecutionBindingPersistsAndRejectsPreparedDrift(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	for _, mutation := range []string{"limits", "contract", "root", "execution"} {
		t.Run(mutation, func(t *testing.T) {
			copy := p
			copy.contracts = append([]windowAccountingContract(nil), p.contracts...)
			switch mutation {
			case "limits":
				copy.limits.JEV.InputRate++
			case "contract":
				copy.contracts[len(copy.contracts)-1].Model.InputRate++
			case "root":
				copy.root += "-other"
			case "execution":
				copy.executionSHA = strings.Repeat("f", 64)
			}
			if _, e := startWindowCampaign(copy, b); e == nil {
				t.Fatal("prepared execution drift accepted")
			}
			if _, e := os.Lstat(copy.root); !os.IsNotExist(e) {
				t.Fatal("prepared drift wrote root")
			}
		})
	}
	c, e := startWindowCampaign(p, b)
	if e != nil {
		t.Fatal(e)
	}
	defer c.closeWindowCampaign()
	token := liveFixtureWindowCampaign(t, c)
	ledgerClaimSHA := c.ledger.contract.ClaimSHA256
	if _, e := c.advanceWindowCampaign(token); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"claim-000001.json", "checkpoint-000001.json", "claim-000002.json"} {
		raw, e := os.ReadFile(filepath.Join(c.root, name))
		if e != nil {
			t.Fatal(e)
		}
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil || obj["execution_sha256"] != p.executionSHA || obj["scope_sha256"] != p.scopeSHA {
			t.Fatal("missing persisted execution/source identities", name)
		}
		if name == "claim-000001.json" && conversationHash(raw) != ledgerClaimSHA {
			t.Fatal("ledger lost transitive execution binding")
		}
	}
}
