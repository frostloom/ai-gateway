package taskbench

import (
	"encoding/json"
	"fmt"
)

// FullCustomerPairPlan is offline selection, not an execution authorization.
// It preserves the October 14 primary priority and keeps boundary scenarios
// outside the 1220-source denominator. Callers load the pinned archive first.
type FullCustomerPairPlan struct {
	Schema          string            `json:"schema"`
	Suite           ConversationSuite `json:"suite"`
	BoundaryKeys    []string          `json:"boundary_keys"`
	AllowPaid       bool              `json:"allow_paid"`
	ReadyForPaidRun bool              `json:"ready_for_paid_run"`
}

func PrepareFullCustomerPair(source ConversationSuite) (FullCustomerPairPlan, error) {
	p := FullCustomerPairPlan{Schema: "full-customer-pair-preparation.v1", Suite: ConversationSuite{DefinitionSHA256: source.DefinitionSHA256, ManifestSHA256: source.ManifestSHA256}}
	if source.ManifestSHA256 != conversationManifestSHA || source.DefinitionSHA256 != "50848e347b985669e3424688f5f1e57b369d6c5577f3aa7e5de25e03144b7ebd" || len(source.Cases) != 4291 {
		return p, fmt.Errorf("complete pinned conversation archive required")
	}
	// Bind the typed content too: callers may mutate a loaded suite while retaining
	// its raw-file digests. JSON encoding is deterministic for these frozen types.
	canonical, err := json.Marshal(source.Cases)
	if err != nil || conversationHash(canonical) != "fb97ee5db92382f33751883589aa65f177f6cd42556b3dbb2f12e9cfd1585352" {
		return p, fmt.Errorf("conversation content differs from frozen archive")
	}
	bySource := map[string]map[string]ConversationCase{}
	order := []string{}
	for _, c := range source.Cases {
		if err := validateConversation(c); err != nil {
			return p, err
		}
		if bySource[c.SourceID] == nil {
			bySource[c.SourceID] = map[string]ConversationCase{}
			order = append(order, c.SourceID)
		}
		if _, exists := bySource[c.SourceID][c.Scenario.ID]; exists {
			return p, fmt.Errorf("duplicate conversation key %s", c.Key())
		}
		bySource[c.SourceID][c.Scenario.ID] = c
	}
	if len(order) != 1220 {
		return p, fmt.Errorf("exactly 1220 source cases required")
	}
	selected := map[string]bool{}
	for _, id := range order {
		found := false
		for _, kind := range []string{"answer_complete", "explicit_scope_followup", "supplement_then_confirm", "confirm", "no_subscription"} {
			if c, ok := bySource[id][kind]; ok && c.Scenario.FullTaskMetricEligible && c.Scenario.MeasurementScope == "complete_task" {
				p.Suite.Cases = append(p.Suite.Cases, c)
				selected[c.Key()] = true
				found = true
				break
			}
		}
		if !found {
			return p, fmt.Errorf("no primary complete task for %s", id)
		}
	}
	for _, c := range source.Cases {
		if c.Scenario.FullTaskMetricEligible && !selected[c.Key()] {
			p.BoundaryKeys = append(p.BoundaryKeys, c.Key())
		}
	}
	if len(p.BoundaryKeys) != 1851 {
		return p, fmt.Errorf("complete boundary scenario set required")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	var detached FullCustomerPairPlan
	err = json.Unmarshal(raw, &detached)
	return detached, err
}
