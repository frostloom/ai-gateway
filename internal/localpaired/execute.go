package localpaired

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Outcome struct {
	Reply           string
	Tools           []ToolRecord
	ModelCalls      int
	JEVCalls        int
	HighConfidence  bool
	PendingBefore   bool
	PendingAfter    bool
	DatabaseChanged bool
	Trace           []byte
}

type Dialog func(context.Context, string, Turn) (Outcome, error)

type storedArm struct {
	Arm        string   `json:"arm"`
	Pass       bool     `json:"pass"`
	Reasons    []string `json:"reasons,omitempty"`
	ModelCalls int      `json:"model_calls"`
	JEVCalls   int      `json:"jev_calls"`
	Packet     string   `json:"packet"`
	Traces     []string `json:"traces"`
}

type storedRecord struct {
	ID   string      `json:"id"`
	Arms []storedArm `json:"arms"`
}

func (b Bundle) ExecuteCase(ctx context.Context, item storedCase, arms []string, dialog Dialog, post func(context.Context, string, []byte) error) error {
	if b.Root == "" || pathRefused(b.Root) {
		return fmt.Errorf("refusing protected path")
	}
	if !singleSegment(item.ID) {
		return fmt.Errorf("invalid record id")
	}
	if len(item.Turns) == 0 {
		return fmt.Errorf("case has no turns")
	}
	if dialog == nil || post == nil {
		return fmt.Errorf("dialog and trace export are required")
	}
	if len(arms) == 0 {
		arms = []string{"model", "jev_selective"}
	}
	record := storedRecord{ID: item.ID}
	for _, arm := range arms {
		if !singleSegment(arm) {
			return fmt.Errorf("invalid arm")
		}
		stored, err := b.collectArm(ctx, item, arm, dialog)
		if err != nil {
			return err
		}
		record.Arms = append(record.Arms, stored)
	}
	for i := range record.Arms {
		traces := make([]string, 0, len(record.Arms[i].Traces))
		for _, body := range traceBodies(record.Arms[i].Traces) {
			path, err := b.SaveTrace(ctx, []byte(body), post)
			if err != nil {
				return err
			}
			rel, err := relativeInside(b.Root, path)
			if err != nil {
				return err
			}
			traces = append(traces, rel)
		}
		record.Arms[i].Traces = traces
		packetRel, err := b.saveHashedPacket(record.Arms[i].Packet)
		if err != nil {
			return err
		}
		record.Arms[i].Packet = packetRel
	}
	return writeJSON(b.Root, filepath.Join("records", item.ID+".json"), record)
}

func (b Bundle) collectArm(ctx context.Context, item storedCase, arm string, dialog Dialog) (storedArm, error) {
	rubric := make([]RubricItem, 0, len(item.Rubric))
	for _, piece := range item.Rubric {
		rubric = append(rubric, RubricItem{ID: piece.ID, Text: piece.Text})
	}
	graded := make([]GradedTurn, 0, len(item.Turns))
	var reasons []string
	var modelCalls, jevCalls int
	bodies := make([]string, 0, len(item.Turns))
	pass := true
	for _, turn := range item.Turns {
		outcome, err := dialog(ctx, arm, Turn{User: turn.User, Kind: turn.Kind})
		if err != nil {
			return storedArm{}, err
		}
		graded = append(graded, GradedTurn{User: turn.User, Reply: outcome.Reply, Tools: outcome.Tools})
		modelCalls += outcome.ModelCalls
		jevCalls += outcome.JEVCalls
		names := make([]string, 0, len(outcome.Tools))
		for _, tool := range outcome.Tools {
			names = append(names, tool.Name)
		}
		result := Verify(Observation{
			Arm:             arm,
			Kind:            turn.Kind,
			ModelCalls:      outcome.ModelCalls,
			Tools:           names,
			PendingBefore:   outcome.PendingBefore,
			PendingAfter:    outcome.PendingAfter,
			DatabaseChanged: outcome.DatabaseChanged,
			HighConfidence:  outcome.HighConfidence,
		})
		if !result.Pass {
			pass = false
		}
		reasons = append(reasons, result.Reasons...)
		body := outcome.Trace
		if len(body) == 0 {
			raw, err := json.Marshal(struct {
				User  string       `json:"user"`
				Reply string       `json:"reply"`
				Tools []ToolRecord `json:"tools,omitempty"`
			}{User: turn.User, Reply: outcome.Reply, Tools: outcome.Tools})
			if err != nil {
				return storedArm{}, err
			}
			body = raw
		}
		bodies = append(bodies, string(body))
	}
	packet, err := Blind(Record{ID: item.ID, Arm: arm, Rubric: rubric, Turns: graded, JEVCalls: jevCalls})
	if err != nil {
		return storedArm{}, err
	}
	raw, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return storedArm{}, err
	}
	return storedArm{
		Arm:        arm,
		Pass:       pass,
		Reasons:    reasons,
		ModelCalls: modelCalls,
		JEVCalls:   jevCalls,
		Packet:     string(append(raw, '\n')),
		Traces:     bodies,
	}, nil
}

func (b Bundle) saveHashedPacket(raw string) (string, error) {
	sum := sha256.Sum256([]byte(raw))
	path := filepath.Join(b.Root, "packets", hex.EncodeToString(sum[:])+".json")
	if !within(b.Root, path) {
		return "", fmt.Errorf("refusing write outside %s", b.Root)
	}
	if err := os.WriteFile(path, []byte(raw), bundleFileMode); err != nil {
		return "", err
	}
	return relativeInside(b.Root, path)
}

func traceBodies(bodies []string) []string {
	out := make([]string, len(bodies))
	copy(out, bodies)
	return out
}

func relativeInside(root, path string) (string, error) {
	if !within(root, path) {
		return "", fmt.Errorf("refusing write outside %s", root)
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
