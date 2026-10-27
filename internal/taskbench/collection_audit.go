package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"reflect"
	"regexp"
)

type CollectionJournalReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type CollectionPolicyAuditRow struct {
	RuntimeComplete           bool              `json:"runtime_complete"`
	LegacyAssessmentUnchanged bool              `json:"legacy_assessment_unchanged"`
	Key                       string            `json:"key"`
	Arm                       string            `json:"arm"`
	TraceID                   string            `json:"local_trace_id,omitempty"`
	JournalSHA256             string            `json:"journal_sha256"`
	OriginalResultSHA256      string            `json:"original_result_sha256"`
	SemanticResultSHA256      string            `json:"semantic_result_sha256"`
	OriginalStatus            string            `json:"original_status"`
	TaskStatus                string            `json:"task_status"`
	TaskSuccess               bool              `json:"task_success"`
	StructuralPass            bool              `json:"structural_pass"`
	ObservedTurns             int               `json:"observed_turns"`
	Disposition               string            `json:"disposition"`
	Issues                    []collectionIssue `json:"issues"`
}
type CollectionPolicyAudit struct {
	RuntimeComplete        bool                       `json:"runtime_complete"`
	MissingRuntimeTurns    int                        `json:"missing_runtime_turns"`
	Schema                 string                     `json:"schema"`
	EvidenceKind           string                     `json:"evidence_kind"`
	PolicyID               string                     `json:"policy_id"`
	PolicySHA256           string                     `json:"policy_sha256"`
	SelectionSHA256        string                     `json:"selection_sha256"`
	DefinitionSHA256       string                     `json:"definition_sha256"`
	ManifestSHA256         string                     `json:"manifest_sha256"`
	ProfileSHA256          string                     `json:"profile_sha256"`
	PlannedPrimaryCases    int                        `json:"planned_primary_cases"`
	SuppliedPairs          int                        `json:"supplied_pairs"`
	UnrunSourceCases       int                        `json:"unrun_source_cases"`
	RecordedArms           int                        `json:"recorded_arms"`
	ObservedTurns          int                        `json:"observed_turns"`
	ContinueArms           int                        `json:"continue_recorded_arms"`
	HaltArms               int                        `json:"halt_arms"`
	Rows                   []CollectionPolicyAuditRow `json:"rows"`
	AllowPaid              bool                       `json:"allow_paid"`
	ActualFull1220Complete bool                       `json:"actual_full1220_complete"`
	NewModelRequests       int                        `json:"new_model_requests"`
	NewJEVRequests         int                        `json:"new_jev_requests"`
	Limitations            []string                   `json:"limitations"`
}

// AuditFullCollectionPolicy is read-only acceptance of the policy against
// explicitly SHA-bound, complete full-primary LOCAL journals. It cannot accept
// recorded provider runs, create a fixture, authorize a request, or validate
// window accounting/observation delivery. Those have distinct future gates.
func AuditFullCollectionPolicy(definitionsDir, profilesDir string, refs []CollectionJournalReference) (CollectionPolicyAudit, error) {
	var report CollectionPolicyAudit
	if len(refs) < 1 || len(refs) > 1220 {
		return report, fmt.Errorf("explicit 1..1220 local journal references required")
	}
	paths := map[string]bool{}
	for _, ref := range refs {
		if ref.Path == "" || !comparisonDigest(ref.SHA256) || paths[ref.Path] {
			return report, fmt.Errorf("invalid or duplicate journal reference")
		}
		paths[ref.Path] = true
	}
	source, err := LoadConversations(definitionsDir)
	if err != nil {
		return report, err
	}
	suite, digest, err := fullCustomerSelection(source)
	if err != nil {
		return report, err
	}
	profiles, err := LoadPairedProfiles(profilesDir)
	if err != nil {
		return report, err
	}
	catalogue := map[string]ConversationCase{}
	for _, c := range suite.Cases {
		catalogue[c.Key()] = c
	}
	report = CollectionPolicyAudit{Schema: "full-local-collection-policy-audit.v1", EvidenceKind: "local_stub", PolicyID: collectionPolicyID, PolicySHA256: collectionPolicySHA(), SelectionSHA256: digest, DefinitionSHA256: source.DefinitionSHA256, ManifestSHA256: source.ManifestSHA256, ProfileSHA256: profiles.SHA256, PlannedPrimaryCases: 1220, UnrunSourceCases: 1220, Rows: []CollectionPolicyAuditRow{}, Limitations: []string{
		"Read-only counterfactual policy replay of supplied complete local journals; not new Handler/provider execution or full actual evaluation.",
		"Source/body provenance and oracle snapshots are trusted inputs, not cryptographic execution attestations.",
		"Legacy assessment comparison ignores wall durations, error-list order and journal-redacted derived review requirements only; original result/journal hashes retain exact original evidence.",
		"Task results stay failed/needs_review where recorded; collection continuation is not semantic certification or permission.",
		"The semantic result digest excludes only replay duration and measured turn latency; original journal/result digests retain the original evidence.",
		"Missing synthetic runtime identity is explicitly counted as incomplete; contradictory available runtime metadata is rejected.",
		"No claim, accounting/usage/observer readiness, provider charge, quality benefit or model/JEV request is established by this audit."}}
	var sharedRuntime *agent.RuntimeMetadata
	seen := map[string]bool{}
	for _, ref := range refs {
		journal, err := loadPairJournal(ref.Path)
		if err != nil {
			return report, err
		}
		cfg := journal.cfg
		if journal.sha != ref.SHA256 || cfg.SelectionScope != FullCustomerPrimaryScope || cfg.SelectionSHA256 != digest || len(cfg.Suite.Cases) != 1 || journal.pending != nil || len(journal.finished) != 2 || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(cfg.Model.ExecutionRevision) {
			return report, fmt.Errorf("complete local full-source journal binding required")
		}
		key := cfg.Suite.Cases[0].Key()
		c, ok := catalogue[key]
		if !ok || seen[key] {
			return report, fmt.Errorf("non-primary or duplicate source journal")
		}
		seen[key] = true
		one := suite
		one.Cases = []ConversationCase{c}
		want, err := PrepareAgentPairConfig(one, profiles, PairModelSettings{BaseURL: "http://127.0.0.1", Model: "local-full-customer", Protocol: "anthropic"}, "local_stub", cfg.Model.ExecutionRevision, "")
		if err != nil {
			return report, err
		}
		want.SelectionScope = FullCustomerPrimaryScope
		want.SelectionSHA256 = digest
		want.ArmOrder = cfg.ArmOrder
		if !reflect.DeepEqual(want, cfg) {
			return report, fmt.Errorf("frozen local source/runtime configuration differs")
		}
		for _, arm := range []string{"model", "jev_selective"} {
			event := journal.finished[key+":"+arm]
			original := *event.Result
			armcfg := cfg.Model
			if arm == "jev_selective" {
				armcfg = cfg.Selective
			}
			runtimeComplete := true
			for _, obs := range original.Observations {
				complete, err := comparisonRuntime(armcfg, obs.Turn.TelemetryRaw, &sharedRuntime)
				if err != nil {
					return report, fmt.Errorf("runtime evidence for %s/%s: %w", key, arm, err)
				}
				if !complete {
					report.MissingRuntimeTurns++
					runtimeComplete = false
				}
			}

			legacyDriver := &replayConversationDriver{episode: ConversationEpisode{Key: key, Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}
			legacy, _ := RunFactCheckedConversation(context.Background(), c, legacyDriver)
			before, err := collectionLegacyAssessment(original)
			if err != nil {
				return report, err
			}
			after, err := collectionLegacyAssessment(legacy)
			if err != nil {
				return report, err
			}
			if legacyDriver.next != len(original.Observations) || string(before) != string(after) {
				return report, fmt.Errorf("legacy assessment changed for %s/%s", key, arm)
			}
			driver := &replayConversationDriver{episode: ConversationEpisode{Key: key, Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}
			recording := recordCollectionConversation(context.Background(), c, driver)
			if driver.next != len(original.Observations) {
				return report, fmt.Errorf("unused local observation evidence")
			}
			if err := verifyCollectionRecording(recording); err != nil {
				return report, err
			}
			oldRaw, err := json.Marshal(original)
			if err != nil {
				return report, err
			}
			semantic, err := collectionNormalizedResult(recording.Result)
			if err != nil {
				return report, err
			}
			report.Rows = append(report.Rows, CollectionPolicyAuditRow{RuntimeComplete: runtimeComplete, LegacyAssessmentUnchanged: true, Key: key, Arm: arm, TraceID: event.LangfuseTraceID, JournalSHA256: journal.sha, OriginalResultSHA256: conversationHash(oldRaw), SemanticResultSHA256: conversationHash(semantic), OriginalStatus: original.Status, TaskStatus: recording.Result.Status, TaskSuccess: recording.Result.Success, StructuralPass: recording.Result.StructuralPass, ObservedTurns: len(recording.Result.Observations), Disposition: recording.Disposition, Issues: recording.Issues})
			report.RecordedArms++
			report.ObservedTurns += len(recording.Result.Observations)
			if recording.Disposition == "continue_recorded" {
				report.ContinueArms++
			} else {
				report.HaltArms++
			}
		}
		report.SuppliedPairs++
		report.UnrunSourceCases--
	}
	report.RuntimeComplete = report.MissingRuntimeTurns == 0 && report.RecordedArms > 0
	return report, nil
}
