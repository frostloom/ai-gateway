package taskbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// Derive a business rejection from authoritative input/state, never from an
// error prefix. Infrastructure errors and valid proposals have no admission.
func collectionPreviewBusinessFailure(tool string, args map[string]any, state ConversationState, result string) bool {
	own := map[string]any{}
	for k, v := range args {
		if k != "tenant_id" {
			own[k] = v
		}
	}
	reason := ""
	integer := func(v any) (int64, bool) {
		n, ok := v.(json.Number)
		if !ok {
			return 0, false
		}
		i, err := n.Int64()
		return i, err == nil && i > -9007199254740992 && i < 9007199254740992
	}
	switch tool {
	case "recharge":
		amount, ok := integer(own["amount_money"])
		if len(own) == 1 && ok && amount <= 0 {
			reason = "充值金额必须大于 0"
		}
	case "refund_recharge":
		if len(own) != 1 {
			return false
		}
		if order, ok := own["order_no"].(string); ok && order != "" {
			for _, row := range state.Own.Orders {
				if row.OrderNo != order {
					continue
				}
				if row.Status != store.RechargePaid {
					reason = fmt.Sprintf("订单 %s 尚未支付，无法退款", order)
				} else if row.AmountCent > 0 && state.Own.BalanceCent == 0 {
					reason = fmt.Sprintf("当前余额为 0，订单 %s 无可退额度", order)
				}
			}
		} else if amount, ok := integer(own["amount_money"]); ok {
			if amount <= 0 {
				reason = "退款金额必须大于 0"
			} else if amount <= 92233720368547758 && state.Own.BalanceCent == 0 {
				reason = "当前余额为 0，无可退额度"
			}
		}
	case "cancel_subscription":
		if len(own) == 0 && len(state.Own.Subscriptions) == 0 && len(state.Allowance) == 0 {
			reason = "当前没有生效中的订阅，无需退订"
		}
	case "subscribe_plan", "change_plan":
		if state.AnswerAuthority == nil || !state.AnswerAuthority.CatalogComplete || state.AnswerAuthority.Plans == nil {
			return false
		}
		if windowOnlyKeys(own, "plan_id", "plan_name") != nil || len(own) == 0 {
			return false
		}
		id, hasID := int64(0), false
		if value, ok := own["plan_id"]; ok {
			id, hasID = integer(value)
			if !hasID || id <= 0 {
				return false
			}
		}
		name, hasName := "", false
		if value, ok := own["plan_name"]; ok {
			name, hasName = value.(string)
			if !hasName || strings.TrimSpace(name) == "" {
				return false
			}
		}
		reason = collectionPreviewPlanRejection(state.AnswerAuthority.Plans, id, hasID, name, hasName)
	}
	return reason != "" && result == "操作不可执行："+reason
}
func collectionPreviewPlanRejection(plans []store.Plan, id int64, hasID bool, name string, hasName bool) string {
	normalize := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '·' {
				return -1
			}
			return r
		}, s)
	}
	var selected *store.Plan
	if hasName {
		name = normalize(name)
		if name == "" {
			return ""
		}
		exact, partial := []*store.Plan{}, []*store.Plan{}
		for i := range plans {
			p := &plans[i]
			if p.Status != 0 || p.PlanType != store.PlanTypeRecurring {
				continue
			}
			candidate := normalize(p.Name)
			if candidate == name {
				exact = append(exact, p)
			} else if strings.Contains(candidate, name) {
				partial = append(partial, p)
			}
		}
		matches := exact
		if len(matches) == 0 {
			matches = partial
		}
		if len(matches) == 0 {
			alias := name
			for _, suffix := range []string{"套餐", "档位", "档"} {
				alias = strings.TrimSuffix(alias, suffix)
			}
			if alias != "" && alias != name {
				for i := range plans {
					p := &plans[i]
					if p.Status == 0 && p.PlanType == store.PlanTypeRecurring && strings.Contains(normalize(p.Name), alias) {
						matches = append(matches, p)
					}
				}
			}
		}
		if len(matches) == 0 {
			return fmt.Sprintf("未找到名称为「%s」的在售定期套餐，请查询套餐目录", name)
		}
		if len(matches) != 1 {
			return fmt.Sprintf("套餐名称「%s」匹配多个在售定期套餐，请提供完整名称", name)
		}
		selected = matches[0]
		if hasID && selected.ID != uint64(id) {
			return "套餐 ID 与名称不一致，请重新选择"
		}
	} else if hasID {
		for i := range plans {
			if plans[i].ID == uint64(id) {
				selected = &plans[i]
				break
			}
		}
		if selected == nil {
			return fmt.Sprintf("套餐 #%d 不存在", id)
		}
	}
	if selected == nil {
		return ""
	}
	if selected.Status != 0 {
		return fmt.Sprintf("套餐 #%d 已下架", selected.ID)
	}
	if selected.PlanType != store.PlanTypeRecurring {
		return fmt.Sprintf("只能选择在售定期订阅套餐（%s 是一次性套餐）", selected.Name)
	}
	return ""
}

func collectionPreviewReadArguments(tool string, args, resolved map[string]any) bool {
	if !qualityJSONEqual(args, resolved) {
		return false
	}
	switch tool {
	case "get_balance", "get_consumption", "list_plans", "get_my_subscription":
		return windowOnlyKeys(args, "tenant_id") == nil
	case "list_models":
		if windowOnlyKeys(args, "tenant_id", "vendor") != nil {
			return false
		}
		if vendor, ok := args["vendor"]; ok {
			_, valid := vendor.(string)
			return valid
		}
		return true
	case "get_recent_bills":
		if windowOnlyKeys(args, "tenant_id", "limit") != nil {
			return false
		}
		if limit, ok := args["limit"]; ok {
			n, err := windowNonnegativeInteger(limit)
			return err == nil && n >= 1 && n <= 10
		}
		return true
	}
	return false
}
