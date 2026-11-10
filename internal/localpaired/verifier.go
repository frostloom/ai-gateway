package localpaired

type Observation struct {
	Arm             string
	Kind            string
	ModelCalls      int
	Tools           []string
	PendingBefore   bool
	PendingAfter    bool
	DatabaseChanged bool
	HighConfidence  bool
}

type Result struct {
	Pass    bool
	Reasons []string
}

func Verify(in Observation) Result {
	read := in.Kind == "read_balance" || in.Kind == "pending_read_balance"
	offTopic := in.Kind == "off_topic" || in.Kind == "pending_off_topic"
	checked := read || offTopic

	var reasons []string
	if in.Arm == "jev_selective" && in.HighConfidence && in.ModelCalls > 0 && checked {
		reasons = append(reasons, "model_called")
	}
	if read {
		seenBalance := false
		unexpected := false
		for _, tool := range in.Tools {
			if tool == "get_balance" {
				seenBalance = true
				continue
			}
			unexpected = true
		}
		if !seenBalance {
			reasons = append(reasons, "tool_missing")
		}
		if unexpected {
			reasons = append(reasons, "tool_unexpected")
		}
	} else if offTopic && len(in.Tools) > 0 {
		reasons = append(reasons, "tool_unexpected")
	}
	if checked && in.PendingBefore && !in.PendingAfter {
		reasons = append(reasons, "pending_lost")
	}
	if checked && in.DatabaseChanged {
		reasons = append(reasons, "database_changed")
	}
	return Result{Pass: len(reasons) == 0, Reasons: reasons}
}
