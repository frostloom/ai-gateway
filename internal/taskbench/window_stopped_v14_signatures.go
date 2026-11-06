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
func readWindowStoppedV14Signatures(m windowActualManifest, files map[string][]byte) error {
	if conversationHash(files["execution-authorization.json"]) != windowStoppedV14AuthorizationSHA {
		return fmt.Errorf("original authorization anchor")
	}
	var a windowV14ExecutionAuthorization
	var att windowActualBinaryAttestation
	if decodeWindowActualJSON(files["execution-authorization.json"], &a) != nil || decodeWindowActualJSON(files["actual-manifest.json.binary-attestation.json"], &att) != nil {
		return fmt.Errorf("original signature JSON")
	}
	if e := validateWindowStoppedV14SignedData(m, a, att); e != nil {
		return e
	}
	if conversationHash(files["operation-policy-approval.json"]) != "53b64a1d36c32a3ee9201ae624ba9a358607a3ad245e2d5c7bd3456ce6505935" {
		return fmt.Errorf("stopped14 separate policy anchor")
	}
	var approval windowV14OperationPolicyApproval
	if decodeWindowActualJSON(files["operation-policy-approval.json"], &approval) != nil {
		return fmt.Errorf("stopped14 policy JSON")
	}
	if e := validateWindowStoppedV14PolicyData(m, a, att, approval, files); e != nil {
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
	if conversationHash(files["operator-selection.json"]) != "4fada35c19b84fc0cf929838291dd47b81de78f90469d93729a302ab81e41288" || decodeWindowActualJSON(files["operator-selection.json"], &operator) != nil || operator.Schema != "full1220-v14-independent-operator-selection.v1" || operator.Commit != m.ArchiveCommit || operator.FullGo != m.ArchiveCommit || operator.Sources != windowStoppedV14SourceSHA || operator.Mutated || operator.Authorized || len(operator.Paths) != 0 || !comparisonDigest(operator.Owner) || !comparisonDigest(operator.Verifier) || !comparisonDigest(operator.Template) || !comparisonDigest(operator.Review.SHA256) {
		return fmt.Errorf("original independent operator selection")
	}
	return nil
}
func validateWindowStoppedV14SignedData(m windowActualManifest, a windowV14ExecutionAuthorization, att windowActualBinaryAttestation) error {
	fail := func() error { return fmt.Errorf("stopped Root14 signed DATA rejected") }
	if m.BuildAttestationPublicKey != windowStoppedV14PublicKey {
		return fail()
	}
	pub, e := hex.DecodeString(m.BuildAttestationPublicKey)
	if e != nil || len(pub) != ed25519.PublicKeySize {
		return fail()
	}
	verify := func(v any, s string) bool {
		signature, e := hex.DecodeString(s)
		raw, me := json.Marshal(v)
		return e == nil && me == nil && len(signature) == ed25519.SignatureSize && ed25519.Verify(ed25519.PublicKey(pub), raw, signature)
	}
	b := a.windowV14ExecutionBinding
	sources, _ := json.Marshal(m.Sources)
	if validateWindowV14OperationPolicyBinding(b, m) != nil || b.OperationPolicyApproval.SHA256 != "53b64a1d36c32a3ee9201ae624ba9a358607a3ad245e2d5c7bd3456ce6505935" || !verify(b, a.Signature) || !verify(att.windowActualBinaryBinding, att.Signature) || att.Schema != "full-window-actual-binary-attestation.v1" || att.ArchiveCommit != m.ArchiveCommit || att.ArchiveTree != m.ArchiveTree || att.ReadyManifestSHA256 != windowStoppedV14ManifestSHA || !comparisonDigest(att.BinarySHA256) || b.Schema != "full-window-delegated-execution-authorization.v14" || !b.HumanAuthorized || !b.RetryPolicyApproved || !b.SingleOwner || !b.NoRelaunch || b.OwnerID == "" || b.HumanReviewedExactScope || b.Parent != (windowV8Amounts{200000000, 3000000}) || b.GlobalBudget != (windowV8Amounts{200000000, 4000000}) || b.ManifestSHA256 != windowStoppedV14ManifestSHA || b.BinarySHA256 != att.BinarySHA256 || b.SourceSHA256 != conversationHash(sources) || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.BudgetDeclarationSHA256 != m.Scope.BudgetDeclarationSHA256 || b.RetryPolicySHA256 != m.Scope.RetryPolicySHA256 || b.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || b.CompletionSHA256 != windowV8DeclarationSHA(m.Scope.CompletionV14) || m.ExecutorEquivalence == nil || b.ExecutorEquivalenceSHA256 != m.ExecutorEquivalence.SHA256 || b.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || !comparisonDigest(b.PrivateSettingsSHA256) || b.SpoolRoot != m.SpoolRoot || !reflect.DeepEqual(b.Observer, m.Observer) || b.HumanReply.SHA256 != windowStoppedV8ApprovalSHA || b.AutonomousAuthorization.SHA256 != windowV9AutonomousAuthorizationSHA {
		return fail()
	}
	return nil
}

func readWindowStoppedV14CompanionRefs(p *historyReadPass, m windowActualManifest, files map[string][]byte) error {
	var a windowV14ExecutionAuthorization
	if decodeWindowActualJSON(files["execution-authorization.json"], &a) != nil {
		return fmt.Errorf("Root14 companion authorization")
	}
	if a.HumanReply.Path != windowStoppedV14Root+"/human-reply.txt" || a.AutonomousAuthorization.Path != windowStoppedV14Root+"/autonomous-authorization.private.json" {
		return fmt.Errorf("Root14 caller evidence path")
	}
	for _, ref := range []windowActualFile{a.HumanReply, a.AutonomousAuthorization} {
		if _, e := historyStoppedV7File(p, ref.Path, ref.SHA256, 1<<20); e != nil {
			return e
		}
	}
	mapRaw, e := historyStoppedV7File(p, windowStoppedV14Root+"/regression/source-sha256.json", "272f88edded55dbc35964a1284e73310f31187d486c079fb398015703dfb423f", 4<<20)
	var sources map[string]string
	if e != nil || decodeWindowActualJSON(mapRaw, &sources) != nil || !reflect.DeepEqual(sources, m.Sources) {
		return fmt.Errorf("Root14 source companion map")
	}
	if m.ExecutorEquivalence == nil || *m.ExecutorEquivalence != (windowActualFile{windowStoppedV14Root + "/executor-equivalence-v14-go-ordered.json", "bcb80e2061fbd8c2b044495a5e6c172b2089014e192d0066adec736d3d30619e"}) || m.ExecutorEquivalencePins == nil || *m.ExecutorEquivalencePins != (windowActualFile{windowStoppedV14Root + "/executor-equivalence-pins-v14.json", "0c49ef5a86eb813b14d55c7e044e27ad723acae9b0ea413cf66d7a8d2b6f2b8a"}) {
		return fmt.Errorf("Root14 fixed equivalence refs")
	}
	raw, e := historyStoppedV7File(p, m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var eq windowExecutorEquivalenceV14
	if e != nil || decodeWindowActualJSON(raw, &eq) != nil {
		return fmt.Errorf("Root14 equivalence DATA")
	}
	raw, e = historyStoppedV7File(p, m.ExecutorEquivalencePins.Path, m.ExecutorEquivalencePins.SHA256, 65536)
	var pins windowV12ReviewedPins
	if e != nil || decodeWindowActualJSON(raw, &pins) != nil || pins.Schema != "full-window-executor-equivalence-pins.v14" || validateWindowV14ExecutorEquivalencePinnedData(m.Sources, eq, windowExecutorEquivalencePinsV14{pins.OriginalSourcesSHA256, pins.SourcesSHA256, pins.ChangesSHA256}) != nil {
		return fmt.Errorf("Root14 equivalence pins DATA")
	}
	return historyStoppedBinaryAttestation(p, files["actual-manifest.json.binary-attestation.json"], m, windowStoppedV14ManifestSHA, windowStoppedV14Root+"/full-window-actual-v14")
}

// All input bytes here were obtained by the same stopped history pass. This
// deliberately does not call the actual verifier's ordinary filesystem reader.
func validateWindowStoppedV14PolicyData(m windowActualManifest, a windowV14ExecutionAuthorization, att windowActualBinaryAttestation, approval windowV14OperationPolicyApproval, files map[string][]byte) error {
	b := approval.windowV14OperationPolicyApprovalBinding
	raw, e := windowV14OperationPolicyApprovalMessage(b)
	pub, pe := hex.DecodeString(m.BuildAttestationPublicKey)
	sig, se := hex.DecodeString(approval.Signature)
	if e != nil || pe != nil || se != nil || m.BuildAttestationPublicKey != windowStoppedV14PublicKey || len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(pub), raw, sig) || b.ManifestSHA256 != windowStoppedV14ManifestSHA || b.SourceSHA256 != windowStoppedV14SourceSHA || b.BinarySHA256 != windowStoppedV14BinarySHA || att.BinarySHA256 != b.BinarySHA256 || b.ScopeSHA256 != m.ScopeSHA256 || b.ExecutionSHA256 != m.ExecutionSHA256 || b.OperationPolicySHA256 != windowV8DeclarationSHA(m.Scope.OperationPolicyV14) || b.OperationPolicySHA256 != a.OperationPolicySHA256 || b.HumanReply.Path != windowStoppedV14Root+"/operation-policy-human-reply.txt" || b.HumanReply.SHA256 != "2560180d500f5740d102c5e55822b25dc322a9493506c3685cee473c5b6ec4af" || conversationHash(files["operation-policy-human-reply.txt"]) != b.HumanReply.SHA256 {
		return fmt.Errorf("stopped14 distinct signed operation-policy DATA")
	}
	return nil
}
