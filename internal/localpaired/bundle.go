package localpaired

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	CampaignDir    = "eval/campaigns/2026-11-10-local-paired"
	CustomerModel  = "gpt-6-luna"
	bundleFileMode = 0o644
	bundleDirMode  = 0o755
)

var bundleDirs = []string{"records", "traces", "packets", "scores"}

type Bundle struct {
	Root string
}

type storedCase struct {
	ID     string         `json:"id"`
	Turns  []storedTurn   `json:"turns"`
	Rubric []storedRubric `json:"rubric"`
}

type storedTurn struct {
	User string `json:"user"`
	Kind string `json:"kind,omitempty"`
}

type storedRubric struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type storedManifest struct {
	Root          string   `json:"root"`
	Passes        int      `json:"passes"`
	Arms          []string `json:"arms"`
	GraderModel   string   `json:"grader_model"`
	CustomerModel string   `json:"customer_model"`
	CaseCount     int      `json:"case_count"`
	Dirs          []string `json:"dirs"`
}

func Prepare(root string, campaign Campaign) (Bundle, error) {
	root = filepath.Clean(root)
	if pathRefused(root) {
		return Bundle{}, fmt.Errorf("refusing protected path")
	}
	if err := os.MkdirAll(root, bundleDirMode); err != nil {
		return Bundle{}, err
	}
	for _, dir := range bundleDirs {
		if err := os.MkdirAll(filepath.Join(root, dir), bundleDirMode); err != nil {
			return Bundle{}, err
		}
	}
	cases := storedCases(campaign)
	if err := writeJSON(root, "cases.json", cases); err != nil {
		return Bundle{}, err
	}
	manifest := storedManifest{
		Root:          CampaignDir,
		Passes:        campaign.Passes,
		Arms:          campaign.Arms,
		GraderModel:   campaign.GraderModel,
		CustomerModel: CustomerModel,
		CaseCount:     len(cases),
		Dirs:          append([]string(nil), bundleDirs...),
	}
	if err := writeJSON(root, "manifest.json", manifest); err != nil {
		return Bundle{}, err
	}
	return Bundle{Root: root}, nil
}

func storedCases(campaign Campaign) []storedCase {
	users := map[string][]Turn{}
	for _, item := range campaign.Added {
		users[item.ID] = item.Turns
	}
	seen := map[string]struct{}{}
	for _, conversation := range campaign.Source.Cases {
		if _, ok := seen[conversation.SourceID]; ok {
			continue
		}
		seen[conversation.SourceID] = struct{}{}
		turns := make([]Turn, 0, len(conversation.Scenario.Turns))
		for _, turn := range conversation.Scenario.Turns {
			turns = append(turns, Turn{User: turn.User})
		}
		if _, exists := users[conversation.SourceID]; !exists {
			users[conversation.SourceID] = turns
		}
	}
	out := make([]storedCase, 0, len(campaign.Selected))
	for _, id := range campaign.Selected {
		item := storedCase{ID: id, Rubric: storedRubrics(campaign.Rubrics[id])}
		for _, turn := range users[id] {
			item.Turns = append(item.Turns, storedTurn{User: turn.User, Kind: turn.Kind})
		}
		out = append(out, item)
	}
	return out
}

func storedRubrics(items []RubricItem) []storedRubric {
	out := make([]storedRubric, 0, len(items))
	for _, item := range items {
		out = append(out, storedRubric{ID: item.ID, Text: item.Text})
	}
	return out
}

func writeJSON(root, name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := filepath.Join(root, name)
	if !within(root, path) {
		return fmt.Errorf("refusing write outside %s", root)
	}
	return os.WriteFile(path, raw, bundleFileMode)
}

func (b Bundle) SaveBlind(in Record) (string, error) {
	if !singleSegment(in.ID) {
		return "", fmt.Errorf("invalid record id")
	}
	packet, err := Blind(in)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	path := filepath.Join(b.Root, "packets", in.ID+".json")
	if !within(b.Root, path) {
		return "", fmt.Errorf("refusing write outside %s", b.Root)
	}
	if err := os.WriteFile(path, raw, bundleFileMode); err != nil {
		return "", err
	}
	return path, nil
}

func (b Bundle) SaveTrace(ctx context.Context, body []byte, post func(context.Context, string, []byte) error) (string, error) {
	return SaveAndExport(ctx, filepath.Join(b.Root, "traces"), "", body, post)
}

func singleSegment(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return false
	}
	return true
}

func within(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
