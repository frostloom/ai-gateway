package taskbench

import (
	"fmt"
	"math"
	"strings"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// This is a pure rendering check for the five frozen agent/tools.go previews.
// Its caller first verifies qualityPreviewTarget; no tool or HTTP is invoked.
func collectionPreviewRecoveryRendering(tool string, resolved map[string]any, f Fixture, before ConversationState) (string, error) {
	fail := func() (string, error) { return "", fmt.Errorf("source preview unavailable") }
	switch tool {
	case "recharge", "refund_recharge":
		amount, err := windowNonnegativeInteger(resolved["amount_money"])
		if tool == "recharge" {
			if err != nil || amount <= 0 || amount > math.MaxInt64/100 {
				return fail()
			}
			return fmt.Sprintf("将模拟充值 ¥%d（模拟支付，不产生真实扣款）。确认后到账。", amount), nil
		}
		balance := before.Own.BalanceCent
		if order, ok := resolved["order_no"].(string); ok && order != "" {
			var selected *store.RechargeOrder
			for i := range before.Own.Orders {
				o := &before.Own.Orders[i]
				if o.OrderNo == order {
					if selected != nil || o.TenantID != f.TenantID || o.Status != store.RechargePaid || o.AmountCent <= 0 {
						return fail()
					}
					selected = o
				}
			}
			if selected == nil || balance <= 0 {
				return fail()
			}
			return fmt.Sprintf("将退款充值订单 %s：订单额度 ¥%.2f，当前可用余额 ¥%.2f，实际退款 ¥%.2f（最多退到当前余额）。确认后执行。", order, float64(selected.AmountCent)/100, float64(balance)/100, float64(min(selected.AmountCent, balance))/100), nil
		}
		if err != nil || amount <= 0 || amount > math.MaxInt64/100 || balance <= 0 {
			return fail()
		}
		return fmt.Sprintf("将退款 ¥%d：当前可用余额 ¥%.2f，实际退款 ¥%.2f（最多退到当前余额）。确认后执行。", amount, float64(balance)/100, float64(min(amount*100, balance))/100), nil
	case "subscribe_plan", "change_plan":
		id, err := windowNonnegativeInteger(resolved["plan_id"])
		name, ok := resolved["plan_name"].(string)
		if err != nil || !ok || before.AnswerAuthority == nil || !before.AnswerAuthority.CatalogComplete {
			return fail()
		}
		var selected *store.Plan
		for i := range before.AnswerAuthority.Plans {
			p := &before.AnswerAuthority.Plans[i]
			if p.ID == uint64(id) {
				if selected != nil || p.Name != name || p.Status != 0 || p.PlanType != store.PlanTypeRecurring {
					return fail()
				}
				selected = p
			}
		}
		if selected == nil {
			return fail()
		}
		if tool == "change_plan" {
			return fmt.Sprintf("将把当前订阅改为定期套餐「%s」：下期续费生效，当期已订购额度与到期日不变。确认后生效。", selected.Name), nil
		}
		quota, parts := selected.TierQuotaMap(), []string{}
		for _, tier := range store.TierNames() {
			if n, exists := quota[tier]; exists {
				text := fmt.Sprintf("%d", n)
				if n%10000 == 0 {
					text = fmt.Sprintf("%d万", n/10000)
				} else if n > 10000 {
					text = fmt.Sprintf("%.1f万", float64(n)/10000)
				}
				parts = append(parts, tier+text)
			}
		}
		return fmt.Sprintf("将订阅「%s」：月费 ¥%d，每 %d 小时刷新额度：%s。确认后开通。", selected.Name, selected.PriceMoney, selected.RefreshHours, strings.Join(parts, "/")), nil
	case "cancel_subscription":
		var selected *store.Subscription
		for i := range before.Own.Subscriptions {
			s := &before.Own.Subscriptions[i]
			if s.TenantID == f.TenantID && s.Status == store.SubActive && s.PlanType == store.PlanTypeRecurring {
				if selected != nil {
					return fail()
				}
				selected = s
			}
		}
		if selected == nil {
			return fail()
		}
		end := "无到期日"
		if selected.CycleEnd != nil {
			end = selected.CycleEnd.Format("2006-01-02")
		}
		return fmt.Sprintf("将退订当前套餐（#%d）：当期不退、只取消下期续费，本期用到 %s 为止。确认后退订。", selected.PlanID, end), nil
	}
	return fail()
}
