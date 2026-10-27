package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
)

const FullCustomerPrimaryScope = "full-customer-primary.v1"

func fullCustomerSelection(source ConversationSuite) (ConversationSuite, string, error) {
	plan, err := PrepareFullCustomerPair(source)
	if err != nil {
		return ConversationSuite{}, "", err
	}
	raw, err := json.Marshal(plan.Suite)
	if err != nil {
		return ConversationSuite{}, "", err
	}
	return plan.Suite, conversationHash(raw), nil
}

// PrepareFullCustomerAgentPairConfig declares an explicit full-primary child
// batch. It neither enables paid execution nor creates a lease or dependency.
func PrepareFullCustomerAgentPairConfig(source ConversationSuite, keys []string, profiles PairedProfiles, model PairModelSettings, evidence, revision, jevModel string) (PairRunConfig, error) {
	if len(keys) < 1 || len(keys) > 12 {
		return PairRunConfig{}, fmt.Errorf("full selection requires 1..12 primary tasks")
	}
	suite, digest, err := fullCustomerSelection(source)
	if err != nil {
		return PairRunConfig{}, err
	}
	byKey := map[string]ConversationCase{}
	for _, c := range suite.Cases {
		byKey[c.Key()] = c
	}
	selected := []ConversationCase{}
	seen := map[string]bool{}
	for _, key := range keys {
		c, ok := byKey[key]
		if !ok || seen[key] {
			return PairRunConfig{}, fmt.Errorf("missing, non-primary or duplicate full case: %s", key)
		}
		seen[key] = true
		selected = append(selected, c)
	}
	suite.Cases = selected
	cfg, err := PrepareAgentPairConfig(suite, profiles, model, evidence, revision, jevModel)
	if err != nil {
		return PairRunConfig{}, err
	}
	cfg.SelectionScope = FullCustomerPrimaryScope
	cfg.SelectionSHA256 = digest
	return cfg, validatePairRun(cfg)
}

// RunFullCustomerAgentPair is a separate guarded adapter, not a new paid CLI.
// It requires the same durable observer, budget and stop policies as pilots.
func RunFullCustomerAgentPair(ctx context.Context, path string, o AgentPairOptions) error {
	return runAgentPair(ctx, path, o, true)
}

func RunFullCustomerCampaignBatch(ctx context.Context, lease *CampaignBatchLease, o AgentPairOptions) error {
	return runCampaignBatch(ctx, lease, o, RunFullCustomerAgentPair)
}
