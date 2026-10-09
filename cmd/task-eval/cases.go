package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func loadCases(path string) ([]taskbench.Case, error) {
	cases := taskbench.DefaultCases()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		cases = nil
		if err := decoder.Decode(&cases); err != nil {
			return nil, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("trailing JSON after task cases")
		}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("task dataset is empty")
	}
	seen := map[string]bool{}
	for i, c := range cases {
		if strings.TrimSpace(c.ID) == "" {
			return nil, fmt.Errorf("case %d: id is empty", i+1)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		if err := validateCase(c); err != nil {
			return nil, fmt.Errorf("case %q: %w", c.ID, err)
		}
	}
	return cases, nil
}

// Only admit expectations supported by the task runner and state scorer.
func validateCase(c taskbench.Case) error {
	if strings.TrimSpace(c.Message) == "" {
		return fmt.Errorf("message is empty")
	}
	readTool := c.Tool == "get_balance" || c.Tool == "get_my_subscription"
	amountTool := c.Tool == "recharge" || c.Tool == "refund_recharge"
	planTool := c.Tool == "subscribe_plan" || c.Tool == "change_plan"
	if !readTool && !amountTool && !planTool && c.Tool != "cancel_subscription" {
		return fmt.Errorf("unsupported tool %q", c.Tool)
	}
	if c.Read != readTool {
		return fmt.Errorf("read flag must match tool")
	}
	if c.Read && (c.Cancel || c.DuplicateConfirm || c.OrderRefund || c.MissingSlot != "" || c.Followup != "" || c.RequireBalanceAnswer) {
		return fmt.Errorf("read conflicts with write workflow flags")
	}
	if c.Cancel && c.DuplicateConfirm {
		return fmt.Errorf("cancel conflicts with duplicate_confirm")
	}
	if c.InitiallyCancelled && !c.InitialSubscription {
		return fmt.Errorf("initially_cancelled requires initial_subscription")
	}
	if c.Tool == "change_plan" || c.Tool == "cancel_subscription" {
		if !c.InitialSubscription {
			return fmt.Errorf("tool requires initial_subscription")
		}
		if c.InitiallyCancelled {
			return fmt.Errorf("tool conflicts with initially_cancelled")
		}
	}
	if c.Tool == "subscribe_plan" && c.InitialSubscription && !c.InitiallyCancelled {
		return fmt.Errorf("subscribe_plan conflicts with active initial_subscription")
	}
	if c.OrderRefund && c.Tool != "refund_recharge" {
		return fmt.Errorf("order_refund requires refund_recharge")
	}
	if c.RequireBalanceAnswer && c.Tool != "refund_recharge" {
		return fmt.Errorf("require_balance_answer only supported for refund_recharge")
	}
	if c.MissingSlot != "" {
		if strings.TrimSpace(c.Followup) == "" {
			return fmt.Errorf("missing slot requires followup")
		}
		if (c.MissingSlot != "amount" || !amountTool || c.OrderRefund) && (c.MissingSlot != "plan" || !planTool) {
			return fmt.Errorf("unsupported missing_slot for tool")
		}
	} else if c.Followup != "" {
		return fmt.Errorf("followup requires missing_slot")
	}
	if amountTool && !c.OrderRefund {
		if c.AmountMoney <= 0 || c.AmountMoney > math.MaxInt64/100 {
			return fmt.Errorf("amount_money must be positive and fit cent conversion")
		}
	} else if c.AmountMoney != 0 {
		return fmt.Errorf("amount_money conflicts with tool or order_refund")
	}
	if c.OrderRefund && !strings.Contains(c.Message, "{order_no}") {
		return fmt.Errorf("order_refund message requires {order_no}")
	}
	if planTool {
		if c.Plan != "standard" && c.Plan != "flagship" {
			return fmt.Errorf("plan must be standard or flagship")
		}
		if c.Tool == "change_plan" && c.Plan == "standard" {
			return fmt.Errorf("plan already matches initial standard subscription")
		}
	} else if c.Plan != "" {
		return fmt.Errorf("plan conflicts with tool")
	}
	return nil
}
