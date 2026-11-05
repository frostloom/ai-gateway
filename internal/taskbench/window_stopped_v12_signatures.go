package taskbench

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// This authenticates stopped caller DATA. It deliberately does not create an
// authority, validate current settings, inspect keys, or open the old executable.
func readWindowStoppedV12Signatures(m windowActualManifest, files map[string][]byte) error {
	if conversationHash(files["execution-authorization.json"]) != windowStoppedV12AuthorizationSHA {
		return fmt.Errorf("original authorization anchor")
	}
	var a windowV12ExecutionAuthorization
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(files["execution-authorization.json"], &a) != nil || decodeWindowActualJSON(files["actual-manifest.json.binary-attestation.json"], &att) != nil {
		return fmt.Errorf("original signature JSON")
	}
	if e := validateWindowStoppedV12SignedData(m, a, att); e != nil {
		return e
	}
	var operator struct {
		Schema     string           `json:"schema"`
		Commit     string           `json:"answering_archive_commit"`
		Sources    string           `json:"answering_source_sha256"`
		FullGo     string           `json:"full_go_applies_only_to_archive_commit"`
		Mutated    bool             `json:"archive_mutated"`
		Authorized bool             `json:"actual_execution_authorized"`
		Paths      []string         `json:"operator_changed_paths"`
		Owner      string           `json:"rendered_owner_sha256"`
		Verifier   string           `json:"rendered_verifier_sha256"`
		Review     windowActualFile `json:"review"`
		Template   string           `json:"selected_operator_template_sha256"`
	}
	if conversationHash(files["operator-selection.json"]) != "e2fcebf8b94835f38dd4e977c74ac6b8e338870d46d94bd436d2401af206376f" || decodeWindowActualJSON(files["operator-selection.json"], &operator) != nil || operator.Schema != "full1220-v12-independent-operator-selection.v1" || operator.Commit != m.ArchiveCommit || operator.FullGo != m.ArchiveCommit || operator.Sources != windowStoppedV12SourceSHA || operator.Mutated || operator.Authorized || len(operator.Paths) != 0 || !comparisonDigest(operator.Owner) || !comparisonDigest(operator.Verifier) || !comparisonDigest(operator.Template) || !comparisonDigest(operator.Review.SHA256) {
		return fmt.Errorf("original independent operator selection")
	}
	return nil
}
func validateWindowStoppedV12SignedData(m windowActualManifest, a windowV12ExecutionAuthorization, att windowActualBinaryAttestation) error {
	fail := func() error { return fmt.Errorf("stopped Root4 signed DATA rejected") }
	pub, e := hex.DecodeString(m.BuildAttestationPublicKey)
	if e != nil || len(pub) != ed25519.PublicKeySize {
		return fail()
	}
	verify := func(v any, s string) bool {
		signature, e := hex.DecodeString(s)
		raw, me := json.Marshal(v)
		return e == nil && me == nil && len(signature) == ed25519.SignatureSize && ed25519.Verify(ed25519.PublicKey(pub), raw, signature)
	}
	b := a.windowV12ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if !verify(b, a.Signature) || !verify(att.windowActualBinaryBinding, att.Signature) || att.Schema != "full-window-actual-binary-attestation.v1" || att.ArchiveCommit != m.ArchiveCommit || att.ArchiveTree != m.ArchiveTree || att.ReadyManifestSHA256 != windowStoppedV12ManifestSHA || !comparisonDigest(att.BinarySHA256) || b.Schema != "full-window-delegated-execution-authorization.v12" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || b.OwnerID == "" || b.HumanReviewedExactScope || b.Parent != (windowV8Amounts{195000000, 3000000}) || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.ManifestSHA256 != windowStoppedV12ManifestSHA || b.BinarySHA256 != att.BinarySHA256 || b.SourceSHA256 != conversationHash(sources) || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.BudgetDeclarationSHA256 != m.Scope.BudgetDeclarationSHA256 || b.RetryPolicySHA256 != m.Scope.RetryPolicySHA256 || b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.CompletionV12) || m.ExecutorEquivalence == nil || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || !reflect.DeepEqual(b.Observer, m.Observer) || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA {
		return fail()
	}
	return nil
}

func readWindowStoppedV12CompanionRefs(p *historyReadPass, m windowActualManifest, files map[string][]byte) error {
	var a windowV12ExecutionAuthorization
	if decodeWindowActualJSON(files["execution-authorization.json"], &a) != nil {
		return fmt.Errorf("Root4 companion authorization")
	}
	if a.HumanReply.Path != windowStoppedV12Root+"/human-reply.txt" || a.AutonomousAuthorization.Path != windowStoppedV12Root+"/autonomous-authorization.private.json" {
		return fmt.Errorf("Root4 caller evidence path")
	}
	for _, ref := range []windowActualFile{a.HumanReply, a.AutonomousAuthorization} {
		if _, e := historyStoppedV7File(p, ref.Path, ref.SHA256, 1<<20); e != nil {
			return e
		}
	}
	mapRaw, e := historyStoppedV7File(p, windowStoppedV12Root+"/regression/source-sha256.json", "cfbd09a7bd06efdee766f98bf3be5bbf0fb5d0a00fdb49c9d0daf160ee5ed9f5", 4<<20)
	var sources map[string]string
	if e != nil || decodeWindowActualJSON(mapRaw, &sources) != nil || !reflect.DeepEqual(sources, m.Sources) {
		return fmt.Errorf("Root4 source companion map")
	}
	if m.ExecutorEquivalence == nil || *m.ExecutorEquivalence != (windowActualFile{windowStoppedV12Root + "/executor-equivalence-v12.json", "0e15b1da1c44cdc854ca00263395807ed50b6ead6cd91bc1a6d3e023e3fa9f8a"}) || m.ExecutorEquivalencePins == nil || *m.ExecutorEquivalencePins != (windowActualFile{windowStoppedV12Root + "/executor-equivalence-pins-v12.json", "f796209366dcfa436b6a08518c410086ad3323263bd0bd78ea3b3840335532c9"}) {
		return fmt.Errorf("Root4 fixed equivalence refs")
	}
	raw, e := historyStoppedV7File(p, m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var eq windowExecutorEquivalenceV12
	if e != nil || decodeWindowActualJSON(raw, &eq) != nil {
		return fmt.Errorf("Root4 equivalence DATA")
	}
	raw, e = historyStoppedV7File(p, m.ExecutorEquivalencePins.Path, m.ExecutorEquivalencePins.SHA256, 65536)
	var pins windowV12ReviewedPins
	if e != nil || decodeWindowActualJSON(raw, &pins) != nil || pins.Schema != "full-window-executor-equivalence-pins.v12" || validateWindowV12ExecutorEquivalencePinnedData(m.Sources, eq, windowExecutorEquivalencePinsV12{pins.OriginalSourcesSHA256, pins.SourcesSHA256, pins.ChangesSHA256}) != nil {
		return fmt.Errorf("Root4 equivalence pins DATA")
	}
	return historyStoppedBinaryAttestation(p, files["actual-manifest.json.binary-attestation.json"], m, windowStoppedV12ManifestSHA, windowStoppedV12Root+"/full-window-actual-v12")
}
