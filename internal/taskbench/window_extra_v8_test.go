package taskbench

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

// A synthetic owned in-memory fixture; never actual readiness or real claims.
func fixtureWindowV8Extra(t *testing.T, provider string) (*windowCampaign, windowOwnedRetryContext) {
	return fixtureWindowV8Campaign(t, provider, true)
}
func fixtureWindowV8Campaign(t *testing.T, provider string, unknown bool, policy ...string) (*windowCampaign, windowOwnedRetryContext) {
	t.Helper()
	parent := stoppedEvidenceRoot(t)
	root := filepath.Join(parent, "synthetic-local-stub-campaign")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	info, _ := os.Lstat(root)
	s, p, b := windowScopeInputs(t)
	if len(policy) > 0 {
		b.PolicySHA256 = policy[0]
	}
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	sha, _, err := validateFullWindowScope(scope, s, p, b)
	if err != nil {
		t.Fatal(err)
	}
	limits := windowV4ExpectedLimits()
	exec, err := executionSHAWindowCampaign(sha, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	contract := contractWindowCampaign(scope, sha, scope.Windows[0], limits)
	c := &windowCampaign{root: root, rootInfo: info, lock: lock, prepared: windowCampaignPrepared{root: root, scopeSHA: sha, executionSHA: exec, scope: scope, limits: limits, contracts: []windowAccountingContract{contract}}, baseline: windowV8CandidateBaseline(expectedStoppedV7WindowBaseline()), index: 1, claimSHA: sha, model: 51200203 + scope.Windows[0].ModelCap, jev: 399529 + scope.Windows[0].JEVCap, files: map[string]string{}, ledgerAudits: map[string]string{}}
	c.ledgerDir = filepath.Join(root, "ledger")
	c.ledger, err = newWindowAccounting(c.ledgerDir, contract)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.ledger.Close() })
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	reply := filepath.Join(parent, "synthetic-reply.txt")
	replyRaw := []byte("synthetic fixture, no real authorization")
	os.WriteFile(reply, replyRaw, 0600)
	m := windowActualManifest{Schema: "full-window-actual-ready.v8", Scope: scope, ScopeSHA256: sha, ExecutionSHA256: exec, BuildAttestationPublicKey: hex.EncodeToString(pub), Sources: map[string]string{"synthetic.go": sha}}
	sources, _ := json.Marshal(m.Sources)
	binding := windowV8ExecutionBinding{Schema: "full-window-execution-authorization.v8", HumanAuthorized: true, RetryPolicyApproved: true, HumanReply: windowActualFile{reply, conversationHash(replyRaw)}, ManifestSHA256: sha, SourceSHA256: conversationHash(sources), BinarySHA256: sha, ScopeSHA256: sha, ExecutionSHA256: exec, BudgetDeclarationSHA256: scope.BudgetDeclarationSHA256, RetryPolicySHA256: scope.RetryPolicySHA256, Parent: windowV8Amounts{140000000, 1100000}, OwnerID: "synthetic-test-owner", SingleOwner: true, NoRelaunch: true}
	canonical, _ := json.Marshal(binding)
	raw, _ := json.Marshal(windowV8ExecutionAuthorization{binding, hex.EncodeToString(ed25519.Sign(key, canonical))})
	authPath := filepath.Join(parent, "synthetic-auth.json")
	os.WriteFile(authPath, raw, 0600)
	c.prepared.v8Authority = &windowV8Authority{manifest: m, manifestSHA: sha, binarySHA: sha, authorizationPath: authPath, authorizationSHA: conversationHash(raw), raw: raw, binding: binding}
	env := &PairedFixtureEnvironment{}
	permit := &windowAcquisitionPermit{campaign: c, ledger: c.ledger, executionSHA: exec, windowIndex: 1, environment: env}
	slot := scope.Slots[0]
	d := &windowOwnedDriver{permit: permit, ledger: c.ledger, environment: env, slot: slot, arm: slot.ArmOrder[0], setupDone: true}
	phase := &windowOwnedPhase{driver: d, nonce: "synthetic-phase", parent: context.Background()}
	d.phase = phase
	permit.active = d
	r := windowRequestIdentity{Pass: slot.Pass, CaseKey: slot.CaseKey, Arm: d.arm, Turn: 1, Provider: provider, Attempt: 1, NativeAttempt: 1, BodySHA256: conversationHash([]byte("{}")), Bytes: 2}
	if provider == "model" {
		r.Model = "deepseek-flash"
	} else {
		r.Model = "jev-1.13.0"
		r.Arm = "jev_selective"
		d.arm = r.Arm
		permit.nextArm = 1
		permit.proofs = make([]*windowOwnedArmProof, 1)
		permit.proofSHA = []string{"synthetic-prior-arm-placeholder"}
	}
	if !unknown {
		payload, e := newWindowPayloadStore(filepath.Join(parent, "synthetic-payload"), root, contract, "local_stub")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { payload.Close() })
		permit.payload = payload
		permit.evidenceKind = "local_stub"
		return c, windowOwnedRetryContext{driver: d, phase: phase, request: r, requestContext: context.WithValue(phase.parent, windowOwnedPhaseKey{}, phase)}
	}
	if provider == "model" {
		phase.model = 1
	} else {
		phase.jev = 1
	}
	ticket, err := c.ledger.Begin(r)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := newWindowPayloadStore(filepath.Join(parent, "synthetic-payload"), root, contract, "local_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { payload.Close() })
	permit.payload = payload
	permit.evidenceKind = "local_stub"
	if err = payload.writeWindowRequest(ticket.sequence, r, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err = payload.writeWindowResponseWithFailure(ticket.sequence, r, 0, nil, false, "round_trip", &windowTransportFailure{Cause: "eof", Phase: "tls_handshake", TraceObserved: true}); err != nil {
		t.Fatal(err)
	}
	descriptor, err := windowV8FailureDescriptor(payload, ticket.sequence, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ledger.EndUnknown(ticket, descriptor); err != nil {
		t.Fatal(err)
	}
	return c, windowOwnedRetryContext{driver: d, phase: phase, sequence: ticket.sequence, request: r, descriptorSHA: descriptor, requestContext: context.WithValue(phase.parent, windowOwnedPhaseKey{}, phase)}
}
func TestWindowV8ExclusiveExtra(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, r := fixtureWindowV8Extra(t, provider)
			receipt, err := c.claimWindowExtra(r)
			if err != nil {
				t.Fatalf("exclusive extra rejected: %v", err)
			}
			if receipt == nil || receipt.owner != c || receipt.ledger != c.ledger || receipt.claim.UnknownSequence != r.sequence || receipt.claim.Request != r.request {
				t.Fatal("receipt association")
			}
			if _, err = c.claimWindowExtra(r); err == nil {
				t.Fatal("duplicate extra accepted")
			}
		})
	}
}

func TestWindowV8ExtraFundingAndDurability(t *testing.T) {
	for _, mode := range []string{"fund", "short", "write", "sync", "funding_sync", "hardlink", "tamper", "quota", "foreign_body", "foreign_phase", "forged_descriptor"} {
		t.Run(mode, func(t *testing.T) {
			c, r := fixtureWindowV8Extra(t, "model")
			if mode == "quota" {
				c.modelExtras = 20
			}
			if mode == "foreign_body" {
				r.request.BodySHA256 = windowV8BaselineSHA
			}
			if mode == "foreign_phase" {
				r.phase = &windowOwnedPhase{driver: r.driver, nonce: "foreign"}
			}
			if mode == "forged_descriptor" {
				r.descriptorSHA = windowV8BaselineSHA
			}
			if mode == "short" || mode == "write" || mode == "sync" {
				c.openFile = func(path string) (windowJournalFile, error) {
					f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
					if err != nil {
						return nil, err
					}
					return &failingWindowFile{windowJournalFile: f, shortWrite: mode == "short", writeFailure: mode == "write", syncFailure: mode == "sync"}, nil
				}
			}
			receipt, err := c.claimWindowExtra(r)
			if mode == "quota" || mode == "foreign_body" || mode == "foreign_phase" || mode == "forged_descriptor" {
				if err == nil {
					t.Fatal("foreign/quota accepted")
				}
				if len(c.extras) != 0 {
					t.Fatal("new liability for rejected identity")
				}
				return
			}
			if mode == "short" || mode == "write" || mode == "sync" {
				if err == nil || !c.poisoned || c.modelExtras != 1 || len(c.extras) != 1 {
					t.Fatal("uncertain extra not retained", err, c.modelExtras)
				}
				if _, err = os.Lstat(filepath.Join(c.root, "extra-000001.json")); err != nil {
					t.Fatal("partial claim removed", err)
				}
				state := c.ledger.Snapshot()
				if state.ModelUnknownHeld != 74752 || state.ModelExtraFunded != 0 {
					t.Fatal("unknown responsibility lost", state)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "hardlink" {
				if err = os.Link(filepath.Join(c.root, "extra-000001.json"), filepath.Join(filepath.Dir(c.root), "alias-extra.json")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "tamper" {
				if err = os.WriteFile(filepath.Join(c.root, "extra-000001.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "funding_sync" {
				c.ledger.file = &failingWindowFile{windowJournalFile: c.ledger.file, syncFailure: true}
			}
			err = receipt.recordFunding()
			if mode != "fund" {
				if err == nil || !c.poisoned || c.modelExtras != 1 {
					t.Fatal("bad durable funding accepted", err)
				}
				if c.ledger.Snapshot().ModelUnknownHeld != 74752 {
					t.Fatal("unknown lost")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state := c.ledger.Snapshot()
			if state.ModelUnknownHeld != 74752 || state.ModelExtraFunded != 74752 || state.UnknownIntents[0].ExtraClaimSHA256 != receipt.sha {
				t.Fatal("funding erased unknown", state)
			}
			if receipt.recordFunding() == nil {
				t.Fatal("funding receipt reused")
			}
		})
	}
}

func TestWindowV8ConcurrentExtraIsExclusive(t *testing.T) {
	c, r := fixtureWindowV8Extra(t, "model")
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.claimWindowExtra(r); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || c.modelExtras != 1 || len(c.extras) != 1 {
		t.Fatal("duplicate exclusive liability", successes.Load(), c.modelExtras)
	}
}

func TestWindowV8ActiveLedgerHardlinkRejects(t *testing.T) {
	c, _ := fixtureWindowV8Campaign(t, "model", false)
	if err := os.Link(filepath.Join(c.ledgerDir, "accounting.jsonl"), filepath.Join(filepath.Dir(c.root), "synthetic-ledger-alias")); err != nil {
		t.Fatal(err)
	}
	if c.ledger.validateOwnedPath() == nil {
		t.Fatal("V8 active ledger accepted a hardlink alias")
	}
}

func TestWindowV8ExtraCanceledParentCreatesNoClaim(t *testing.T) {
	c, r := fixtureWindowV8Extra(t, "model")
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	r.phase.parent = parent
	if _, err := c.claimWindowExtra(r); err == nil || len(c.extras) != 0 {
		t.Fatal("canceled parent created permanent extra")
	}
}

// Exercise the actual durable adapter across two synthetic windows. No
// concrete44/actual proof, real claim, provider request or restart is involved.
func TestWindowV8GlobalTwentyExtrasAcrossWindowsThenRejectsTwentyFirst(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c, initial := fixtureWindowV8Campaign(t, provider, false)
			p := initial.driver.permit
			d := initial.driver
			c.prepared.contracts = nil
			for _, w := range c.prepared.scope.Windows {
				c.prepared.contracts = append(c.prepared.contracts, contractWindowCampaign(c.prepared.scope, c.prepared.scopeSHA, w, c.prepared.limits))
			}
			for i := 0; i <= 20; i++ {
				if i == 10 {
					oldDir := c.ledgerDir
					if err := c.ledger.Close(); err != nil {
						t.Fatal(err)
					}
					audit, err := readWindowAccounting(oldDir)
					if err != nil {
						t.Fatal(err)
					}
					c.ledgerAudits[filepath.Base(oldDir)] = audit.JournalSHA256
					c.index = 2
					c.model += c.prepared.scope.Windows[1].ModelCap
					c.jev += c.prepared.scope.Windows[1].JEVCap
					if err = c.persistWindowCampaign("claim-000002.json", map[string]any{"schema": "synthetic-base-window-only", "index": 2}); err != nil {
						t.Fatal(err)
					}
					c.claimSHA = c.files["claim-000002.json"]
					contract := c.prepared.contracts[1]
					contract.ClaimSHA256 = c.claimSHA
					c.ledgerDir = filepath.Join(c.root, "ledger-000002")
					c.ledger, err = newWindowAccounting(c.ledgerDir, contract)
					if err != nil {
						t.Fatal(err)
					}
					p.ledger = c.ledger
					d.ledger = c.ledger
					p.windowIndex = 2
					payload, err := newWindowPayloadStore(filepath.Join(filepath.Dir(c.root), "synthetic-payload-window2"), c.root, contract, "local_stub")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { payload.Close() })
					p.payload = payload
				}
				local := i
				if i >= 10 {
					local = i - 10
				}
				w := c.prepared.scope.Windows[c.index-1]
				slot := c.prepared.scope.Slots[w.FirstOrdinal-1+local]
				arm := "model"
				if provider == "jev" {
					arm = "jev_selective"
				}
				armOffset := 0
				if slot.ArmOrder[0] != arm {
					armOffset = 1
				}
				p.nextArm = 2*local + armOffset
				p.proofs = make([]*windowOwnedArmProof, p.nextArm)
				p.proofSHA = make([]string, p.nextArm)
				d.slot = slot
				d.arm = arm
				d.nextTurn = 0
				phase := &windowOwnedPhase{driver: d, nonce: fmt.Sprintf("synthetic-window-%d-logical-%d", c.index, i), parent: context.Background()}
				if provider == "model" {
					phase.model = 1
				} else {
					phase.jev = 1
				}
				d.phase = phase
				request := windowRequestIdentity{NativeAttempt: 1, Provider: provider, Model: "deepseek-flash", CaseKey: slot.CaseKey, Pass: slot.Pass, Arm: arm, Turn: 1, Attempt: 1, Bytes: 2, BodySHA256: conversationHash([]byte("{}"))}
				if provider == "jev" {
					request.Model = "jev-1.13.0"
				}
				ticket, err := c.ledger.Begin(request)
				if err != nil {
					t.Fatal(err)
				}
				if err = p.payload.writeWindowRequest(ticket.sequence, request, []byte("{}")); err != nil {
					t.Fatal(err)
				}
				if err = p.payload.writeWindowResponseWithFailure(ticket.sequence, request, 0, nil, false, "round_trip", &windowTransportFailure{Cause: "eof", Phase: "tls_handshake", TraceObserved: true}); err != nil {
					t.Fatal(err)
				}
				descriptor, err := windowV8FailureDescriptor(p.payload, ticket.sequence, request)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.ledger.EndUnknown(ticket, descriptor); err != nil {
					t.Fatal(err)
				}
				receipt, err := c.claimWindowExtra(windowOwnedRetryContext{driver: d, phase: phase, sequence: ticket.sequence, request: request, descriptorSHA: descriptor, requestContext: context.WithValue(phase.parent, windowOwnedPhaseKey{}, phase)})
				if i == 20 {
					if err == nil || len(c.extras) != 20 || c.modelExtras+c.jevExtras != 20 || !c.poisoned {
						t.Fatal("21st global extra accepted or quota reclaimed", err, len(c.extras))
					}
					break
				}
				if err != nil {
					t.Fatalf("extra %d rejected: %v", i+1, err)
				}
				if receipt.claim.Index != i+1 || receipt.claim.ProviderIndex != i+1 || receipt.claim.PreviousExtraSHA256 == "" && i > 0 {
					t.Fatal("global chain/index reset")
				}
				if err = receipt.recordFunding(); err != nil {
					t.Fatal(err)
				}
				request.NativeAttempt = 2
				retry, err := c.ledger.Begin(request)
				if err != nil {
					t.Fatal(err)
				}
				if err = p.payload.writeWindowRequest(retry.sequence, request, []byte("{}")); err != nil {
					t.Fatal(err)
				}
				response := windowTransportResponse(provider)
				raw, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if err = p.payload.writeWindowResponse(retry.sequence, request, 200, raw, true, "complete"); err != nil {
					t.Fatal(err)
				}
				limits, _ := c.ledger.contract.provider(provider)
				usage, err := parseWindowResponseUsage(provider, raw, limits)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.ledger.Settle(retry, conversationHash(raw), usage); err != nil {
					t.Fatal(err)
				}
				if i == 9 {
					state := c.ledger.Snapshot()
					c.mu.Lock()
					err = c.validateWindowV8ExtrasLocked(state)
					c.mu.Unlock()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
