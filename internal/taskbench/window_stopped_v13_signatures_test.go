package taskbench

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestWindowStoppedV13SignedDataBoundWithoutAuthority(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	sha := strings.Repeat("a", 64)
	m := windowActualManifest{ArchiveCommit: "synthetic-archive", ArchiveTree: "synthetic-tree", ScopeSHA256: sha, ExecutionSHA256: sha, PrivateSettingsSHA256: sha, SpoolRoot: "/synthetic/spool", Sources: map[string]string{"source.go": sha}, BuildAttestationPublicKey: public, ExecutorEquivalence: &windowActualFile{Path: "/synthetic/eq.json", SHA256: sha}}
	m.Scope.BudgetDeclarationSHA256 = sha
	m.Scope.RetryPolicySHA256 = sha
	att := windowActualBinaryAttestation{windowActualBinaryBinding: windowActualBinaryBinding{Schema: "full-window-actual-binary-attestation.v1", ArchiveCommit: m.ArchiveCommit, ArchiveTree: m.ArchiveTree, ReadyManifestSHA256: windowStoppedV13ManifestSHA, BinarySHA256: sha}}
	a := windowV13ExecutionAuthorization{}
	a.Schema = "full-window-delegated-execution-authorization.v13"
	a.HumanAuthorized = true
	a.RetryPolicyApproved = true
	a.SingleOwner = true
	a.NoRelaunch = true
	a.OwnerID = "synthetic-owned-caller"
	a.Parent = windowV8Amounts{195000000, 3000000}
	a.GlobalBudget = windowV8Amounts{200000000, 4000000}
	a.ManifestSHA256 = windowStoppedV13ManifestSHA
	a.BinarySHA256 = sha
	a.SourceSHA256 = windowV8DeclarationSHA(m.Sources)
	a.ScopeSHA256 = m.ScopeSHA256
	a.ExecutionSHA256 = m.ExecutionSHA256
	a.BudgetDeclarationSHA256 = sha
	a.RetryPolicySHA256 = sha
	a.CollectionPolicySHA256 = collectionPreviewFailurePolicySHA()
	a.CompletionSHA256 = windowV8DeclarationSHA(m.Scope.CompletionV13)
	a.ExecutorEquivalenceSHA256 = sha
	a.PrivateSettingsSHA256 = sha
	a.SpoolRoot = m.SpoolRoot
	a.Observer = m.Observer
	a.HumanReply.SHA256 = windowStoppedV8ApprovalSHA
	a.AutonomousAuthorization.SHA256 = windowV9AutonomousAuthorizationSHA
	sign := func(v any) string { raw, _ := json.Marshal(v); return hex.EncodeToString(ed25519.Sign(key, raw)) }
	a.Signature = sign(a.windowV13ExecutionBinding)
	att.Signature = sign(att.windowActualBinaryBinding)
	if e := validateWindowStoppedV13SignedData(m, a, att); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"signature", "att-signature", "pubkey", "epoch", "human-exact", "global", "parent", "owner", "manifest", "binary", "sources", "scope", "execution", "budget", "retry", "completion", "equivalence", "settings", "spool", "observer", "reply", "autonomy", "att-commit", "att-manifest"} {
		t.Run(name, func(t *testing.T) {
			x, y, z := m, a, att
			switch name {
			case "signature":
				y.Signature = strings.Repeat("0", 128)
			case "att-signature":
				z.Signature = strings.Repeat("0", 128)
			case "pubkey":
				x.BuildAttestationPublicKey = "bad"
			case "epoch":
				y.Schema = "full-window-delegated-execution-authorization.v12"
			case "human-exact":
				y.HumanReviewedExactScope = true
			case "global":
				y.GlobalBudget.Model++
			case "parent":
				y.Parent.JEV++
			case "owner":
				y.OwnerID = ""
			case "manifest":
				y.ManifestSHA256 = sha
			case "binary":
				y.BinarySHA256 = strings.Repeat("b", 64)
			case "sources":
				y.SourceSHA256 = sha
			case "scope":
				y.ScopeSHA256 = "bad"
			case "execution":
				y.ExecutionSHA256 = "bad"
			case "budget":
				y.BudgetDeclarationSHA256 = "bad"
			case "retry":
				y.RetryPolicySHA256 = "bad"
			case "completion":
				y.CompletionSHA256 = "bad"
			case "equivalence":
				y.ExecutorEquivalenceSHA256 = "bad"
			case "settings":
				y.PrivateSettingsSHA256 = "bad"
			case "spool":
				y.SpoolRoot = "/other"
			case "observer":
				y.Observer.SpoolRunID = "other"
			case "reply":
				y.HumanReply.SHA256 = sha
			case "autonomy":
				y.AutonomousAuthorization.SHA256 = sha
			case "att-commit":
				z.ArchiveCommit = "other"
			case "att-manifest":
				z.ReadyManifestSHA256 = sha
			}
			if name != "signature" {
				y.Signature = sign(y.windowV13ExecutionBinding)
			}
			if name != "att-signature" {
				z.Signature = sign(z.windowActualBinaryBinding)
			}
			if validateWindowStoppedV13SignedData(x, y, z) == nil {
				t.Fatal("coherently signed mutation admitted")
			}
		})
	}
}
