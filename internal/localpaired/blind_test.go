package localpaired

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBlindPacketOmitsArm(t *testing.T) {
	const toolResult = "88.00 元，来自 model 工具。"
	packet, err := Blind(Record{
		ID:          "local-read-history-001",
		Arm:         "jev_selective",
		RoutingMode: "jev_selective",
		JEVCalls:    1,
		Rubric: []RubricItem{{
			ID:   "local-read-history-001:balance",
			Text: "回复当前余额，数值来自工具结果。",
		}},
		Turns: []GradedTurn{{
			User:  "查一下余额",
			Reply: "当前余额是 88.00 元，model 账户。",
			Tools: []ToolRecord{{
				Name:   "get_balance",
				Result: toolResult,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.ID != "local-read-history-001" {
		t.Fatalf("ID = %q", packet.ID)
	}
	if packet.Turns[0].Tools[0].Result != toolResult {
		t.Fatalf("tool result = %q", packet.Turns[0].Tools[0].Result)
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"jev_selective", "routing_mode", "jev_calls", "model_calls"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("packet JSON contains %q: %s", forbidden, text)
		}
	}
}

func TestBlindRejectsHiddenArmText(t *testing.T) {
	packet, err := Blind(Record{
		ID:          "local-read-history-001",
		Arm:         "model",
		RoutingMode: "model",
		JEVCalls:    0,
		Turns: []GradedTurn{{
			User:  "查一下余额",
			Reply: "jev_selective",
		}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !reflect.DeepEqual(packet, Packet{}) {
		t.Fatalf("packet = %+v, want zero", packet)
	}
}
