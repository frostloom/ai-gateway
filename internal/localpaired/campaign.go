package localpaired

import (
	"sort"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

type Campaign struct {
	Passes   int
	Arms     []string
	Source   taskbench.ConversationSuite
	Selected []string
	Added    []Case
}

type Case struct {
	ID     string
	Turns  []Turn
	Rubric []RubricItem
}

type Turn struct {
	User string
	Kind string
}

type RubricItem struct{}

func LoadCampaign(definitionsDir string) (Campaign, error) {
	source, err := taskbench.LoadConversations(definitionsDir)
	if err != nil {
		return Campaign{}, err
	}

	sourceIDs := make(map[string]struct{}, len(source.Cases))
	for _, conversation := range source.Cases {
		sourceIDs[conversation.SourceID] = struct{}{}
	}
	selected := make([]string, 0, len(sourceIDs))
	for sourceID := range sourceIDs {
		selected = append(selected, sourceID)
	}
	sort.Strings(selected)

	return Campaign{
		Passes:   1,
		Arms:     []string{"model", "jev_selective"},
		Source:   source,
		Selected: selected,
		Added:    []Case{},
	}, nil
}
