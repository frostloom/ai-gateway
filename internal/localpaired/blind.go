package localpaired

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Record struct {
	ID          string
	Arm         string
	RoutingMode string
	JEVCalls    int
	Rubric      []RubricItem
	Turns       []GradedTurn
}

type GradedTurn struct {
	User  string
	Reply string
	Tools []ToolRecord
}

type ToolRecord struct {
	Name   string
	Result string
}

type Packet struct {
	ID     string
	Rubric []RubricItem
	Turns  []GradedTurn
}

// "model" alone may appear in a reply or a tool result. model_calls is a field name.
var blindForbidden = []string{
	"jev_selective",
	"routing_mode",
	"jev_calls",
	"model_calls",
}

func Blind(in Record) (Packet, error) {
	packet := Packet{
		ID:     in.ID,
		Rubric: in.Rubric,
		Turns:  in.Turns,
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		return Packet{}, err
	}
	text := string(raw)
	for _, token := range blindForbidden {
		if strings.Contains(text, token) {
			return Packet{}, fmt.Errorf("blind packet contains %s", token)
		}
	}
	return packet, nil
}
