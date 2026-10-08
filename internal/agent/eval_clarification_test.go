package agent

import (
	"encoding/json"
	"testing"
)

func TestEvalChecksClarificationAtItsOwnTurn(t *testing.T) {
	var c EvalCase
	if err := json.Unmarshal([]byte(`{"expect":{"turn_reply_contains":[["多少元"],["请回复"],["充值成功"]]}}`), &c); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name        string
		replies     []turnResp
		wantFailure bool
	}{
		{"proper-flow", []turnResp{{Code: 200, Reply: "请问要充值多少元？"}, {Code: 200, Reply: "请回复确认。"}, {Code: 200, Reply: "充值成功"}}, false},
		{"skipped-clarification", []turnResp{{Code: 200, Reply: "我直接充值了"}, {Code: 200, Reply: "请回复确认。"}, {Code: 200, Reply: "充值成功"}}, true},
		{"missing-turn", []turnResp{{Code: 200, Reply: "请问要充值多少元？"}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			failures := evaluate(c, tt.replies, nil, true)
			if (len(failures) > 0) != tt.wantFailure {
				t.Fatalf("wrong per-turn verification: %v", failures)
			}
		})
	}
}
