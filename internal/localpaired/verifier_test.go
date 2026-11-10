package localpaired

import (
	"reflect"
	"testing"
)

func TestVerifierPinsRoutingOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		in      Observation
		pass    bool
		reasons []string
	}{
		{
			name: "JEV 直读",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "read_balance",
				ModelCalls:      0,
				Tools:           []string{"get_balance"},
				DatabaseChanged: false,
				HighConfidence:  true,
			},
			pass: true,
		},
		{
			name: "JEV 直读却调了模型",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "read_balance",
				ModelCalls:      1,
				Tools:           []string{"get_balance"},
				DatabaseChanged: false,
				HighConfidence:  true,
			},
			pass:    false,
			reasons: []string{"model_called"},
		},
		{
			name: "JEV 闲聊",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "off_topic",
				ModelCalls:      0,
				Tools:           nil,
				DatabaseChanged: false,
				HighConfidence:  true,
			},
			pass: true,
		},
		{
			name: "JEV 闲聊却调了模型",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "off_topic",
				ModelCalls:      1,
				Tools:           nil,
				DatabaseChanged: false,
				HighConfidence:  true,
			},
			pass:    false,
			reasons: []string{"model_called"},
		},
		{
			name: "低置信度闲聊进模型",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "off_topic",
				ModelCalls:      1,
				Tools:           nil,
				DatabaseChanged: false,
				HighConfidence:  false,
			},
			pass: true,
		},
		{
			name: "直读清掉挂起",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "pending_read_balance",
				ModelCalls:      0,
				Tools:           []string{"get_balance"},
				PendingBefore:   true,
				PendingAfter:    false,
				DatabaseChanged: false,
				HighConfidence:  true,
			},
			pass:    false,
			reasons: []string{"pending_lost"},
		},
		{
			name: "闲聊改了库",
			in: Observation{
				Arm:             "jev_selective",
				Kind:            "pending_off_topic",
				ModelCalls:      0,
				Tools:           nil,
				PendingBefore:   true,
				PendingAfter:    true,
				DatabaseChanged: true,
				HighConfidence:  true,
			},
			pass:    false,
			reasons: []string{"database_changed"},
		},
		{
			name: "模型臂闲聊",
			in: Observation{
				Arm:             "model",
				Kind:            "off_topic",
				ModelCalls:      1,
				Tools:           nil,
				DatabaseChanged: false,
				HighConfidence:  true,
			},
			pass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Verify(tt.in)
			if got.Pass != tt.pass {
				t.Fatalf("Pass = %v, want %v, reasons = %#v", got.Pass, tt.pass, got.Reasons)
			}
			if !reflect.DeepEqual(got.Reasons, tt.reasons) {
				t.Fatalf("Reasons = %#v, want %#v", got.Reasons, tt.reasons)
			}
		})
	}
}

func TestVerifyRejectsUncheckedKind(t *testing.T) {
	got := Verify(Observation{
		Arm:        "jev_selective",
		Kind:       "confirm",
		ModelCalls: 1,
		Tools:      []string{"get_balance"},
	})
	if got.Pass {
		t.Fatal("Pass = true, want false")
	}
	if !reflect.DeepEqual(got.Reasons, []string{"unchecked_kind"}) {
		t.Fatalf("Reasons = %#v, want %#v", got.Reasons, []string{"unchecked_kind"})
	}
}
