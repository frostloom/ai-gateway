package taskbench

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/observability"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowV8RuntimeContractKeepsBaseAndSeparatesNative(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	w := scope.Windows[0]
	c := contractWindowCampaign(scope, windowV8BaselineSHA, w, windowV4ExpectedLimits())
	if c.Schema != windowAccountingV3 || c.ModelCap != w.ModelCap || c.JEVCap != w.JEVCap || c.Model.MaxAttempts != w.MaxModelCalls+20 || c.JEV.MaxAttempts != w.MaxJEVCalls+20 {
		t.Fatalf("V8 immutable base/native contract not integrated: %+v", c)
	}
	if err = c.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowV8RuntimeSignedAuthority(t *testing.T) { testWindowV8Authority(t) }

func TestWindowV8ReportRetainsCompletedWindowIdentity(t *testing.T) {
	c, _ := fixtureWindowV8Extra(t, "model")
	ledger := c.ledger
	run := &windowAcquisitionRun{auth: windowAcquisitionAuthorization{campaign: c}, ledger: ledger}
	c.ledger = nil
	defer func() { c.ledger = ledger }()
	run.captureWindowAcquisitionAccounting()
	if run.receipt.V8 == nil || len(run.receipt.V8.UnknownIntents) != 1 || run.receipt.V8.UnknownIntents[0].WindowID != ledger.contract.WindowID {
		t.Fatal("completed window identity lost")
	}
}

func TestWindowV8AuthorityRecheckedBeforeWindowOutputs(t *testing.T) {
	for _, boundary := range []string{"session", "acquisition", "ledger", "claim"} {
		for _, mode := range []string{"missing", "changed_authorization", "changed_reply"} {
			t.Run(boundary+"/"+mode, func(t *testing.T) {
				c, _ := fixtureWindowV8Campaign(t, "model", false)
				authority := c.prepared.v8Authority
				manifest := authority.manifest
				switch mode {
				case "missing":
					c.prepared.v8Authority = nil
				case "changed_authorization":
					if err := os.WriteFile(authority.authorizationPath, []byte("{}"), 0600); err != nil {
						t.Fatal(err)
					}
				case "changed_reply":
					if err := os.WriteFile(authority.binding.HumanReply.Path, []byte("synthetic changed reply"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				reportRoot := filepath.Join(filepath.Dir(c.root), "must-not-create-report")
				var err error
				switch boundary {
				case "session":
					r := &FullWindowActualReady{manifest: manifest, manifestRaw: []byte("{}"), v8Authority: c.prepared.v8Authority}
					s := &FullWindowActualSession{ready: r}
					err = s.verifyInputs()
				case "acquisition":
					observer, e := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: "http://127.0.0.1:1/api/public/otel/v1/traces", PublicKey: "synthetic-pk", SecretKey: "synthetic-sk", HashKey: strings.Repeat("h", 32), Release: c.prepared.scope.Bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 16, ExportTimeout: 10 * time.Second})
					if e != nil {
						t.Fatal(e)
					}
					defer observer.Shutdown(context.Background())
					up := &windowActualNoRedirectTransport{&http.Transport{}}
					auth := &windowAcquisitionAuthorization{campaign: c, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "synthetic-model", jevKey: "synthetic-jev", modelUpstream: up, jevUpstream: up, evidenceKind: "local_stub", privateReportRoot: reportRoot, fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true}}
					_, err = openWindowAcquisition(context.Background(), auth)
				case "ledger":
					old := c.ledger
					if e := old.Close(); e != nil {
						t.Fatal(e)
					}
					if e := os.RemoveAll(c.ledgerDir); e != nil {
						t.Fatal(e)
					}
					c.ledger = nil
					c.ledgerDir = ""
					defer func() {
						if c.ledger != nil {
							c.ledger.Close()
						}
						c.ledger = old
					}()
					_, err = c.openWindowCampaignLedger()
				case "claim":
					err = c.persistClaimWindowCampaign(1, c.claimSHA, "")
				}
				expectedReason := "v8 execution authority"
				if mode == "missing" && boundary != "session" {
					expectedReason = "native exact epoch authority absent"
				}
				if err == nil || !strings.Contains(err.Error(), expectedReason) {
					t.Fatalf("final auth was not the first V8 window input gate: %v", err)
				}
				if boundary == "ledger" {
					if _, e := os.Lstat(filepath.Join(c.root, "ledger-000001")); !os.IsNotExist(e) {
						t.Fatal("ledger created after auth changed", e)
					}
				}
				if _, e := os.Lstat(reportRoot); !os.IsNotExist(e) {
					t.Fatal("report root created after auth changed", e)
				}
				if _, e := os.Lstat(filepath.Join(c.root, "claim-000002.json")); !os.IsNotExist(e) {
					t.Fatal("base claim created after auth changed", e)
				}
			})
		}
	}
}

func TestWindowV8ValidWindowAuthorityAllowsOnlyTheLedgerGate(t *testing.T) {
	c, _ := fixtureWindowV8Campaign(t, "model", false)
	old := c.ledger
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(c.ledgerDir); err != nil {
		t.Fatal(err)
	}
	c.ledger = nil
	c.ledgerDir = ""
	defer func() {
		if c.ledger != nil {
			c.ledger.Close()
		}
		c.ledger = old
	}()
	ledger, err := c.openWindowCampaignLedger()
	if err != nil || ledger == nil || ledger.contract.Schema != windowAccountingV3 {
		t.Fatal("valid synthetic signed authority rejected at ledger gate", err)
	}
	if state := ledger.Snapshot(); state.ModelAttempts != 0 || state.JEVAttempts != 0 || state.Pending != nil {
		t.Fatal("ledger gate performed provider work", state)
	}
}
