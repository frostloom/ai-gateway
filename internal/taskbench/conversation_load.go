package taskbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const conversationManifestSHA = "afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181"

func conversationHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// LoadConversations accepts only the frozen v1 archive. A future definition
// version needs an explicit adapter rather than silently relaxing this binding.
func LoadConversations(dir string) (ConversationSuite, error) {
	out := ConversationSuite{}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return out, err
	}
	if conversationHash(raw) != conversationManifestSHA {
		return out, fmt.Errorf("frozen conversation manifest changed")
	}
	var manifest struct {
		Schema string            `json:"schema"`
		Files  map[string]string `json:"files"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return out, err
	}
	if manifest.Schema != "customer-task-definitions-manifest.v1" {
		return out, fmt.Errorf("unsupported definition schema")
	}
	out.ManifestSHA256 = conversationHash(raw)
	var definitions []byte
	for name, digest := range manifest.Files {
		if filepath.Base(name) != name {
			return out, fmt.Errorf("invalid artifact name")
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return out, err
		}
		if conversationHash(b) != digest {
			return out, fmt.Errorf("definition artifact changed: %s", name)
		}
		if name == "definitions.json" {
			definitions = b
		}
	}
	var rows []struct {
		SourceID   string                  `json:"source_case_id"`
		Assertions []ConversationAssertion `json:"answer_assertions"`
		Scenarios  []ConversationScenario  `json:"scenarios"`
	}
	if err = json.Unmarshal(definitions, &rows); err != nil {
		return out, err
	}
	out.DefinitionSHA256 = conversationHash(definitions)
	seen := map[string]bool{}
	for _, row := range rows {
		for _, s := range row.Scenarios {
			c := ConversationCase{SourceID: row.SourceID, Assertions: row.Assertions, Scenario: s}
			if err = validateConversation(c); err != nil {
				return out, err
			}
			if seen[c.Key()] {
				return out, fmt.Errorf("duplicate scenario %s", c.Key())
			}
			seen[c.Key()] = true
			out.Cases = append(out.Cases, c)
		}
	}
	return out, nil
}
func validateConversation(c ConversationCase) error {
	s := c.Scenario
	if c.SourceID == "" || s.ID == "" || len(s.Turns) == 0 || len(s.Terminal) == 0 {
		return fmt.Errorf("incomplete scenario %s", c.Key())
	}
	if (s.MeasurementScope == "complete_task") != s.FullTaskMetricEligible || (!s.FullTaskMetricEligible && s.MeasurementScope != "first_turn_diagnostic") {
		return fmt.Errorf("invalid measurement scope")
	}
	switch s.Profile {
	case "no_subscription", "active_standard", "active_flagship", "cancelled_standard", "low_balance", "empty_balance":
	default:
		return fmt.Errorf("unknown profile %s", s.Profile)
	}
	for _, t := range s.Turns {
		if strings.TrimSpace(t.User) == "" || len(t.ExpectedActions) == 0 {
			return fmt.Errorf("empty conversation turn")
		}
	}
	return nil
}

func resolveConversationMessage(text string, f Fixture) (string, error) {
	const order = "${account.paid_order.order_no}"
	if strings.Contains(text, order) && f.OrderNo == "" {
		return "", fmt.Errorf("owned paid order is unavailable")
	}
	text = strings.ReplaceAll(text, order, f.OrderNo)
	if strings.Contains(text, "${") {
		return "", fmt.Errorf("unresolved conversation placeholder")
	}
	return text, nil
}
