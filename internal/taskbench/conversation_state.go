package taskbench

import (
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"math"
	"reflect"
	"time"
)

func conversationInteger(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && math.Trunc(n) == n && math.Abs(n) < 9007199254740992 {
			return int64(n)
		}
	}
	return math.MinInt64
}
func validateConversationState(s ConversationState, f Fixture) error {
	if f.TenantID == 0 || !s.Own.Consistent || s.Own.BalanceCent != s.Own.LedgerBalance {
		return fmt.Errorf("own wallet/ledger identity invalid")
	}
	if len(s.Controls) == 0 {
		return fmt.Errorf("foreign control evidence missing")
	}
	check := func(snap Snapshot, tid uint64) error {
		if !snap.Consistent || snap.BalanceCent != snap.LedgerBalance {
			return fmt.Errorf("wallet/ledger mismatch")
		}
		for _, o := range snap.Orders {
			if o.TenantID != tid {
				return fmt.Errorf("cross-tenant order in snapshot")
			}
		}
		for _, o := range snap.Subscriptions {
			if o.TenantID != tid {
				return fmt.Errorf("cross-tenant subscription in snapshot")
			}
		}
		for _, o := range snap.Topups {
			if o.TenantID != tid {
				return fmt.Errorf("cross-tenant ledger row in snapshot")
			}
		}
		for _, o := range snap.Audits {
			if o.TenantID != tid {
				return fmt.Errorf("cross-tenant audit in snapshot")
			}
		}
		return nil
	}
	if err := check(s.Own, f.TenantID); err != nil {
		return err
	}
	for tid, control := range s.Controls {
		if tid == 0 || tid == f.TenantID {
			return fmt.Errorf("invalid foreign control tenant")
		}
		if err := check(control, tid); err != nil {
			return err
		}
	}
	return nil
}

func validateConversationInitial(c ConversationCase, f Fixture, s ConversationState) error {
	wantBalance := int64(500000)
	switch c.Scenario.Profile {
	case "low_balance":
		wantBalance = 2500
	case "empty_balance":
		wantBalance = 0
	}
	if s.Own.BalanceCent != wantBalance || f.Standard.ID == 0 || f.Flagship.ID == 0 || f.Standard.ID == f.Flagship.ID {
		return fmt.Errorf("initial account profile balance/catalogue mismatch")
	}
	wantPlan := uint64(0)
	wantStatus := store.SubActive
	switch c.Scenario.Profile {
	case "active_standard":
		wantPlan = f.Standard.ID
	case "active_flagship":
		wantPlan = f.Flagship.ID
	case "cancelled_standard":
		wantPlan = f.Standard.ID
		wantStatus = store.SubCancelled
	}
	if wantPlan == 0 {
		if len(s.Own.Subscriptions) != 0 || len(s.Allowance) != 0 {
			return fmt.Errorf("initial no-subscription profile mismatch")
		}
		return nil
	}
	if len(s.Own.Subscriptions) != 1 {
		return fmt.Errorf("initial subscription count mismatch")
	}
	sub := s.Own.Subscriptions[0]
	if sub.PlanID != wantPlan || sub.Status != wantStatus || sub.PlanType != store.PlanTypeRecurring || sub.AutoRenew != (wantStatus == store.SubActive) || sub.PendingPlanID != nil || sub.CycleStart == nil || sub.CycleEnd == nil || s.CapturedAt.IsZero() || !s.CapturedAt.Before(*sub.CycleEnd) {
		return fmt.Errorf("initial subscription profile state mismatch")
	}
	plan := f.Standard
	if wantPlan == f.Flagship.ID {
		plan = f.Flagship
	}
	quotas := plan.TierQuotaMap()
	if len(quotas) == 0 || len(s.Allowance) != len(quotas) {
		return fmt.Errorf("initial allowance profile missing")
	}
	seen := map[string]bool{}
	for _, a := range s.Allowance {
		used := int64(0)
		if a.Tier == "文本" {
			used = 3000
		}
		if seen[a.Tier] || a.Quota != quotas[a.Tier] || a.Consumed != used || a.Remaining != a.Quota-used {
			return fmt.Errorf("initial window allowance profile mismatch")
		}
		seen[a.Tier] = true
	}
	return nil
}

func conversationTerminalErrors(c ConversationCase, f Fixture, initial, final ConversationState, observations []ConversationObservation) []error {
	var errors []error
	bad := func(key string) { errors = append(errors, conversationTerminalError{key: key}) }
	if err := validateConversationState(final, f); err != nil {
		errors = append(errors, err)
	}
	if !sameConversationControls(initial.Controls, final.Controls) {
		bad("foreign controls")
	}
	executed := 0
	for _, obs := range observations {
		for _, tr := range obs.Turn.Response.Trace {
			if isConversationWrite(tr.Tool) && tr.Phase == "execute" {
				executed++
			}
		}
	}
	op := c.Scenario.Operation
	tool := ""
	if op != nil {
		tool = op.Tool
	}
	terminal := c.Scenario.Terminal
	delta := final.Own.BalanceCent - initial.Own.BalanceCent
	addedRefund := 0
	var addedRefundCent int64
	oldTopups := map[uint64]store.Topup{}
	for _, row := range initial.Own.Topups {
		oldTopups[row.ID] = row
	}
	for _, row := range final.Own.Topups {
		if _, ok := oldTopups[row.ID]; !ok && row.Source == "refund" {
			addedRefund++
			addedRefundCent -= row.Amount
		}
	}
	newPaid := 0
	oldOrders := map[uint64]store.RechargeOrder{}
	for _, row := range initial.Own.Orders {
		oldOrders[row.ID] = row
	}
	for _, row := range final.Own.Orders {
		if _, ok := oldOrders[row.ID]; !ok && row.Status == store.RechargePaid {
			newPaid++
		}
	}
	var oldSub, newSub *store.Subscription
	if len(initial.Own.Subscriptions) > 0 {
		oldSub = &initial.Own.Subscriptions[0]
	}
	if oldSub != nil {
		for i := range final.Own.Subscriptions {
			if final.Own.Subscriptions[i].ID == oldSub.ID {
				newSub = &final.Own.Subscriptions[i]
				break
			}
		}
	} else if len(final.Own.Subscriptions) == 1 {
		newSub = &final.Own.Subscriptions[0]
	}
	planID := func(v any) uint64 {
		switch v {
		case "standard":
			return f.Standard.ID
		case "flagship":
			return f.Flagship.ID
		}
		return 0
	}
	for key, value := range terminal {
		ok := false
		switch key {
		case "business_state_unchanged":
			want, isBool := value.(bool)
			ok = isBool && sameConversationOwn(initial, final) == want
		case "authorized_write_count":
			ok = int64(executed) == conversationInteger(value)
		case "balance_delta_cent":
			ok = delta == conversationInteger(value) && final.Own.LedgerBalance-initial.Own.LedgerBalance == delta
		case "cash_ledger_consistent":
			ok = value == true && final.Own.Consistent && final.Own.BalanceCent == final.Own.LedgerBalance
		case "refund_cent":
			if tool == "cancel_subscription" {
				ok = conversationInteger(value) == 0 && delta == 0
			} else {
				ok = addedRefundCent == conversationInteger(value)
			}
		case "refund_ledger_rows_added":
			ok = int64(addedRefund) == conversationInteger(value)
		case "requested_cent":
			ok = op != nil && conversationInteger(op.Arguments["amount_money"])*100 == conversationInteger(value)
		case "owned_seed_order_status":
			for _, row := range final.Own.Orders {
				if row.OrderNo == f.OrderNo && row.TenantID == f.TenantID && row.Status == value {
					ok = true
				}
			}
		case "new_paid_order_count":
			ok = int64(newPaid) == conversationInteger(value)
		case "subscriptions_added":
			ok = int64(len(final.Own.Subscriptions)-len(initial.Own.Subscriptions)) == conversationInteger(value)
		case "subscription_status":
			ok = newSub != nil && newSub.Status == value
		case "current_plan":
			id := planID(value)
			ok = id != 0 && newSub != nil && newSub.PlanID == id
		case "auto_renew":
			want, isBool := value.(bool)
			ok = isBool && newSub != nil && newSub.AutoRenew == want
		case "pending_plan":
			id := planID(value)
			ok = id != 0 && newSub != nil && newSub.PendingPlanID != nil && *newSub.PendingPlanID == id
		case "cycle_end_unchanged":
			ok = value == true && oldSub != nil && newSub != nil && oldSub.CycleEnd != nil && newSub.CycleEnd != nil && oldSub.CycleEnd.Equal(*newSub.CycleEnd)
		case "current_allowance_unchanged":
			ok = value == true && len(initial.Allowance) > 0 && reflect.DeepEqual(initial.Allowance, final.Allowance)
		case "effective":
			ok = value == "next_renewal" && oldSub != nil && newSub != nil && newSub.PlanID == oldSub.PlanID && newSub.AutoRenew && newSub.PendingPlanID != nil
		case "cycle_days":
			ok = newSub != nil && newSub.CycleStart != nil && newSub.CycleEnd != nil && newSub.CycleEnd.Sub(*newSub.CycleStart) == time.Duration(conversationInteger(value))*24*time.Hour
		case "simulated_month_fee_cash_debit":
			ok = value == false && delta == 0
		case "current_cycle_access_retained":
			ok = value == true && len(initial.Allowance) > 0 && reflect.DeepEqual(initial.Allowance, final.Allowance) && newSub != nil && newSub.CycleEnd != nil && !final.CapturedAt.IsZero() && final.CapturedAt.Before(*newSub.CycleEnd)
		case "revalidate_order_ownership_and_paid_status":
			if value == true {
				for _, row := range initial.Own.Orders {
					if row.OrderNo == f.OrderNo && row.TenantID == f.TenantID && row.Status == store.RechargePaid {
						ok = true
					}
				}
			}
		case "unrelated_orders_and_subscriptions_unchanged":
			ok = value == true && conversationAllowedChanges(tool, f, initial.Own, final.Own, delta)
		default:
			errors = append(errors, fmt.Errorf("unsupported terminal assertion: %s", key))
			continue
		}
		if !ok {
			bad(key)
		}
	}
	// Preserve unrelated records even when a branch omits the explicit terminal
	// key (e.g. order refund), and check the real cash entry rather than a claim.
	if executed > 0 && !conversationAllowedChanges(tool, f, initial.Own, final.Own, delta) {
		bad("unrelated business records or ledger entries")
	}
	if executed > 0 && tool != "subscribe_plan" && !reflect.DeepEqual(initial.Allowance, final.Allowance) {
		bad("unrelated allowance changed")
	}
	if executed > 0 && tool == "subscribe_plan" && newSub != nil {
		plan := f.Standard
		if newSub.PlanID == f.Flagship.ID {
			plan = f.Flagship
		}
		quotas := plan.TierQuotaMap()
		if len(quotas) == 0 || len(final.Allowance) != len(quotas) {
			bad("new subscription allowance missing")
		}
		seen := map[string]bool{}
		for _, a := range final.Allowance {
			if seen[a.Tier] || a.Quota != quotas[a.Tier] || a.Consumed != 0 || a.Remaining != a.Quota {
				bad("new subscription allowance mismatch")
			}
			seen[a.Tier] = true
		}
	}
	return errors
}

func conversationAllowedChanges(tool string, f Fixture, a, b Snapshot, delta int64) bool {
	topups := map[uint64]store.Topup{}
	for _, row := range b.Topups {
		if _, ok := topups[row.ID]; ok {
			return false
		}
		topups[row.ID] = row
	}
	for _, row := range a.Topups {
		got, ok := topups[row.ID]
		if !ok || !reflect.DeepEqual(row, got) {
			return false
		}
		delete(topups, row.ID)
	}
	expectedEntries := 0
	source := ""
	if tool == "recharge" {
		expectedEntries = 1
		source = "recharge"
	} else if tool == "refund_recharge" {
		expectedEntries = 1
		source = "refund"
	}
	if len(topups) != expectedEntries {
		return false
	}
	for _, row := range topups {
		if row.Source != source || row.Amount != delta || row.TenantID != f.TenantID {
			return false
		}
	}
	orders := map[uint64]store.RechargeOrder{}
	for _, row := range b.Orders {
		if _, ok := orders[row.ID]; ok {
			return false
		}
		orders[row.ID] = row
	}
	for _, row := range a.Orders {
		got, ok := orders[row.ID]
		if !ok {
			return false
		}
		if tool == "refund_recharge" && row.OrderNo == f.OrderNo && got.Status == store.RechargeRefunded {
			got.Status = row.Status
			got.UpdatedAt = row.UpdatedAt
		}
		if !reflect.DeepEqual(row, got) {
			return false
		}
		delete(orders, row.ID)
	}
	if tool == "recharge" {
		if len(orders) != 1 {
			return false
		}
		for _, row := range orders {
			if row.Status != store.RechargePaid || row.AmountCent != delta || row.AmountMoney*100 != row.AmountCent {
				return false
			}
		}
	} else if len(orders) != 0 {
		return false
	}
	subs := map[uint64]store.Subscription{}
	for _, row := range b.Subscriptions {
		if _, ok := subs[row.ID]; ok {
			return false
		}
		subs[row.ID] = row
	}
	for _, row := range a.Subscriptions {
		got, ok := subs[row.ID]
		if !ok {
			return false
		}
		if tool == "change_plan" {
			got.PendingPlanID = row.PendingPlanID
			got.UpdatedAt = row.UpdatedAt
		}
		if tool == "cancel_subscription" {
			got.Status = row.Status
			got.AutoRenew = row.AutoRenew
			got.CancelledAt = row.CancelledAt
			got.UpdatedAt = row.UpdatedAt
		}
		if !reflect.DeepEqual(row, got) {
			return false
		}
		delete(subs, row.ID)
	}
	if tool == "subscribe_plan" {
		return len(subs) == 1 && delta == 0
	}
	return len(subs) == 0
}
